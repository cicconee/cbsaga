package app

import (
	"context"
	"errors"
	"time"

	identity "github.com/cicconee/cbsaga/internal/contracts/kafka/identity/v1"
	orchestrator "github.com/cicconee/cbsaga/internal/contracts/kafka/orchestrator/v1"
	"github.com/cicconee/cbsaga/internal/orchestrator/domain"
	"github.com/cicconee/cbsaga/internal/orchestrator/repo"
	"github.com/cicconee/cbsaga/internal/platform/apperr"
	"github.com/cicconee/cbsaga/internal/platform/codec"
	"github.com/cicconee/cbsaga/internal/platform/db/postgres"
	"github.com/cicconee/cbsaga/internal/platform/retry"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type CreateWithdrawalParams struct {
	UserID          string
	Asset           string
	AmountMinor     int64
	DestinationAddr string
	IdempotencyKey  string
}

type CreateWithdrawalResult struct {
	WithdrawalID string `json:"withdrawal_id"`
	Status       string `json:"status"`
	replay       bool
}

func (r *CreateWithdrawalResult) IsReplay() bool {
	return r.replay
}

func (s *Service) CreateWithdrawal(
	ctx context.Context,
	p CreateWithdrawalParams,
) (res CreateWithdrawalResult, err error) {
	ctx, span := s.tracer.Start(ctx, spanCreateWithdrawal)
	defer span.End()
	defer func() { recordCreateWithdrawalSpan(span, res, err) }()

	v, err := NewValidatedCreateWithdrawal(p)
	if err != nil {
		return CreateWithdrawalResult{}, errInvalidArgument(err)
	}
	span.SetAttributes(
		attribute.String(telKeyIdemKey, v.IdempotencyKey),
		attribute.String(telKeyIdemReqHash, v.RequestHash),
	)

	idemRow, err := s.reserveIdempotency(ctx, v)
	if err != nil {
		var cu *postgres.CommitUnknownError
		if errors.As(err, &cu) {
			span.SetAttributes(attribute.String(telKeyReconcileReason, "commit_unknown"))
			return s.reconcile(ctx, v.UserID, v.IdempotencyKey)
		}
		return CreateWithdrawalResult{}, err
	}

	span.SetAttributes(
		attribute.String(telKeyWithdrawalID, idemRow.WithdrawalID),
		attribute.Bool(telKeyIdemOwned, idemRow.Owned),
	)

	if !idemRow.Owned {
		span.SetAttributes(attribute.String(telKeyReconcileReason, "not_owned"))
		return s.reconcile(ctx, v.UserID, v.IdempotencyKey)
	}

	return s.createWithdrawalWork(ctx, v, idemRow)
}

var (
	errNeedReconcileAlreadyExists    = errors.New("already_exists")
	errNeedReconcileLostLease        = errors.New("lost_lease")
	errNeedReconcileAlreadyFinalized = errors.New("already_finalized")
)

func (s *Service) createWithdrawalWork(
	ctx context.Context,
	v validatedCreateWithdrawal,
	idemRow repo.ReserveIdemResult,
) (CreateWithdrawalResult, error) {
	ctx, span := s.tracer.Start(ctx, spanCreateWithdrawalWork)
	defer span.End()
	sc := span.SpanContext()

	span.SetAttributes(
		attribute.String(telKeyPhase, "withdrawal.work"),
		attribute.String(telKeyWithdrawalID, idemRow.WithdrawalID),
		attribute.String(telKeyIdemKey, v.IdempotencyKey),
	)

	finalParams := repo.FinalizeIdemParams{
		UserID:         v.UserID,
		IdempotencyKey: v.IdempotencyKey,
		LeaseAttemptID: idemRow.LeaseOwner,
		LeaseFence:     idemRow.LeaseFence,
	}

	identityPayload, err := codec.EncodeValid(&identity.IdentityRequestCmdPayload{
		WithdrawalID: idemRow.WithdrawalID,
		UserID:       v.UserID,
	})
	if err != nil {
		recordInternal(span, err, "encode_identity_failed")
		return s.failAndReconcile(ctx, finalParams, err)
	}
	withdrawPayload, err := codec.EncodeValid(&orchestrator.WithdrawalRequestPayload{
		WithdrawalID: idemRow.WithdrawalID,
		UserID:       v.UserID,
	})
	if err != nil {
		recordInternal(span, err, "encode_withdrawal_failed")
		return s.failAndReconcile(ctx, finalParams, err)
	}

	txFunc := func(ctx context.Context, tx pgx.Tx) (CreateWithdrawalResult, error) {
		workRes, err := s.repo.CreateWithdrawalTx(ctx, tx, repo.CreateWithdrawalParams{
			WithdrawalID:    idemRow.WithdrawalID,
			SagaID:          uuid.NewString(),
			UserID:          v.UserID,
			Asset:           v.Asset,
			AmountMinor:     v.AmountMinor,
			DestinationAddr: v.DestinationAddr,
			TraceID:         sc.TraceID().String(),
			OutboxEvents: []repo.OutboxEvent{
				{
					EventType: orchestrator.EventTypeWithdrawalRequested,
					Payload:   string(withdrawPayload),
					RouteKey:  orchestrator.RouteKeyWithdrawalEvt,
				},
				{
					EventType: identity.EventTypeIdentityRequested,
					Payload:   string(identityPayload),
					RouteKey:  identity.RouteKeyIdentityCmd,
				},
			},
		})
		if err != nil {
			if errors.Is(err, repo.ErrWithdrawalAlreadyExists) {
				return CreateWithdrawalResult{}, errNeedReconcileAlreadyExists
			}
			return CreateWithdrawalResult{}, err
		}

		res := CreateWithdrawalResult{
			WithdrawalID: workRes.WithdrawalID,
			Status:       workRes.Status,
		}
		respBody, err := codec.EncodeJSONPtr(res)
		if err != nil {
			return CreateWithdrawalResult{}, err
		}
		finalParams.ResponseBody = respBody

		outcome, err := s.completeIdempotency(ctx, tx, finalParams)
		if err != nil {
			if errors.Is(err, repo.ErrLostLeaseOwnership) {
				return CreateWithdrawalResult{}, errNeedReconcileLostLease
			}
			return CreateWithdrawalResult{}, err
		}
		if outcome == repo.FinalizeAlreadyFinalized {
			return CreateWithdrawalResult{}, errNeedReconcileAlreadyFinalized
		}

		return res, nil
	}

	res, err := postgres.WithTxRetryResult(
		ctx,
		s.db,
		pgx.TxOptions{},
		"withdrawal/create",
		retry.DefaultConfig(),
		txFunc,
	)
	if err != nil {
		var cu *postgres.CommitUnknownError
		if errors.As(err, &cu) {
			recordInternal(span, err, "commit_unknown",
				attribute.String(telKeyReconcileReason, "commit_unknown"),
			)
			rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
			defer cancel()
			return s.reconcile(rctx, v.UserID, v.IdempotencyKey)
		}

		switch {
		case errors.Is(err, errNeedReconcileAlreadyExists):
			span.SetAttributes(attribute.String(telKeyReconcileReason, "already_exists"))
			return s.reconcile(ctx, v.UserID, v.IdempotencyKey)
		case errors.Is(err, errNeedReconcileLostLease):
			span.SetAttributes(attribute.String(telKeyReconcileReason, "lost_lease"))
			return s.reconcile(ctx, v.UserID, v.IdempotencyKey)
		case errors.Is(err, errNeedReconcileAlreadyFinalized):
			span.SetAttributes(attribute.String(telKeyReconcileReason, "already_finalized"))
			return s.reconcile(ctx, v.UserID, v.IdempotencyKey)
		}

		recordInternal(span, err, "withdrawal_tx_failed")
		return s.failAndReconcile(ctx, finalParams, err)
	}

	return res, nil
}

func (s *Service) reserveIdempotency(
	ctx context.Context,
	v validatedCreateWithdrawal,
) (repo.ReserveIdemResult, error) {
	ctx, span := s.tracer.Start(ctx, spanReserveIdem)
	defer span.End()

	span.SetAttributes(attribute.String(telKeyPhase, "idempotency_reserve"))

	reserveTxFunc := func(ctx context.Context, tx pgx.Tx) (repo.ReserveIdemResult, error) {
		return s.repo.ReserveIdemTx(ctx, tx, repo.ReserveIdemParams{
			UserID:         v.UserID,
			IdempotencyKey: v.IdempotencyKey,
			RequestHash:    v.RequestHash,
			WithdrawalID:   uuid.NewString(),
			LeaseAttemptID: uuid.NewString(),
			LeaseTTL:       30 * time.Second,
		})
	}
	idemRow, err := postgres.WithTxRetryResult(
		ctx,
		s.db,
		pgx.TxOptions{},
		"idempotency/reserve",
		reserveIdemRetryPolicy(),
		reserveTxFunc,
	)
	if err != nil {

		var cu *postgres.CommitUnknownError
		switch {
		case errors.Is(err, repo.ErrIdempotencyKeyReuse):
			span.SetAttributes(attribute.String(telKeyIdemOutcome, "idempotency_key_reuse"))
			return repo.ReserveIdemResult{}, errInvalidArgument(err)
		case errors.As(err, &cu):
			recordInternal(span, err, "commit_unknown")
			return repo.ReserveIdemResult{}, err
		default:
			recordInternal(span, err, "reserve_idempotency_failed")
			return repo.ReserveIdemResult{}, errInternal(err)
		}
	}

	span.SetAttributes(
		attribute.String(telKeyWithdrawalID, idemRow.WithdrawalID),
		attribute.Bool(telKeyIdemOwned, idemRow.Owned),
		attribute.String(telKeyIdemOutcome, "reserved"),
	)

	return idemRow, nil
}

func (s *Service) completeIdempotency(
	ctx context.Context,
	workTx pgx.Tx,
	p repo.FinalizeIdemParams,
) (repo.FinalizeOutcome, error) {
	p.Status = domain.IdemCompleted
	return s.repo.FinalizeIdemTx(ctx, workTx, p)
}

func (s *Service) failIdempotencyWithRetry(
	ctx context.Context,
	p repo.FinalizeIdemParams,
) (repo.FinalizeOutcome, error) {
	p.Status = domain.IdemFailed

	txFunc := func(ctx context.Context, tx pgx.Tx) (repo.FinalizeOutcome, error) {
		return s.repo.FinalizeIdemTx(ctx, tx, p)
	}

	return postgres.WithTxRetryResult[repo.FinalizeOutcome](
		ctx,
		s.db,
		pgx.TxOptions{},
		"idempotency/set_failed",
		failIdemRetryPolicy(),
		txFunc,
	)
}

func recordCreateWithdrawalSpan(span trace.Span, res CreateWithdrawalResult, err error) {
	if err == nil {
		if res.IsReplay() {
			span.SetAttributes(attribute.String(telKeyWithdrawalOutcome, "replay"))
		} else {
			span.SetAttributes(attribute.String(telKeyWithdrawalOutcome, "success"))
		}
		return
	}

	var ae *apperr.Error
	isBusinessError := errors.As(err, &ae) && ae.Code != apperr.CodeInternal
	if isBusinessError {
		span.SetAttributes(
			attribute.String(telKeyWithdrawalOutcome, "business_error"),
			attribute.String("app.error.code", string(ae.Code)),
		)
		return
	}

	recordInternal(span, err, "failed")
}
