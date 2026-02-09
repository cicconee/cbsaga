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
	"github.com/cicconee/cbsaga/internal/platform/logging"
	"github.com/cicconee/cbsaga/internal/platform/retry"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type Service struct {
	db     *pgxpool.Pool
	repo   *repo.Repo
	log    *logging.Logger
	tracer trace.Tracer
}

func NewService(db *pgxpool.Pool, log *logging.Logger) *Service {
	return &Service{
		db:     db,
		repo:   repo.New(),
		log:    log,
		tracer: otel.Tracer("orchestrator/app"),
	}
}

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
	ctx, span := s.tracer.Start(ctx, "orchestrator.app.create_withdrawal")
	defer span.End()
	defer func() {
		if err == nil {
			if res.IsReplay() {
				span.SetAttributes(attribute.String("withdrawal.outcome", "replay"))
			} else {
				span.SetAttributes(attribute.String("withdrawal.outcome", "success"))
			}
			span.SetStatus(codes.Ok, "")
			return
		}

		var ae *apperr.Error
		isBusinessError := errors.As(err, &ae) && ae.Code != apperr.CodeInternal
		if isBusinessError {
			span.SetAttributes(attribute.String("withdrawal.outcome", "business_error"))
			span.SetStatus(codes.Ok, "")
			return
		}

		span.RecordError(err)
		span.SetAttributes(attribute.String("withdrawal.outcome", "failed"))
		span.SetStatus(codes.Error, "internal")
	}()

	v, err := NewValidatedCreateWithdrawal(p)
	if err != nil {
		span.SetAttributes(attribute.Bool("withdrawal.valid", false))
		return CreateWithdrawalResult{}, errInvalidArgument(err)
	}
	span.SetAttributes(attribute.String("idempotency.request_hash", v.RequestHash))

	idemRow, err := s.reserveIdempotency(ctx, v)
	if err != nil {
		var cu *postgres.CommitUnknownError
		if errors.As(err, &cu) {
			return s.reconcile(ctx, v.UserID, v.IdempotencyKey)
		}

		return CreateWithdrawalResult{}, err
	}

	if !idemRow.Owned {
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
	ctx, span := s.tracer.Start(ctx, "orchestrator.app.create_withdrawal.work")
	defer span.End()
	sc := span.SpanContext()

	span.SetAttributes(attribute.String("withdrawal.id", idemRow.WithdrawalID))

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
		span.RecordError(err)
		span.SetStatus(codes.Error, "encode_identity_failed")
		return s.failAndReconcile(ctx, finalParams, err)
	}
	withdrawPayload, err := codec.EncodeValid(&orchestrator.WithdrawalRequestPayload{
		WithdrawalID: idemRow.WithdrawalID,
		UserID:       v.UserID,
	})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "encode_withdrawal_failed")
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
			span.SetAttributes(attribute.String("withdrawal.work.outcome", "commit_unknown"))
			span.RecordError(err)
			span.SetStatus(codes.Error, "commit_unknown")

			rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
			defer cancel()
			return s.reconcile(rctx, v.UserID, v.IdempotencyKey)
		}

		switch {
		case errors.Is(err, errNeedReconcileAlreadyExists):
			span.SetStatus(codes.Ok, "")
			span.SetAttributes(attribute.String("withdrawal.work.outcome", "already_exists"))
			return s.reconcile(ctx, v.UserID, v.IdempotencyKey)
		case errors.Is(err, errNeedReconcileLostLease):
			span.SetStatus(codes.Ok, "")
			span.SetAttributes(attribute.String("withdrawal.work.outcome", "lost_lease"))
			return s.reconcile(ctx, v.UserID, v.IdempotencyKey)
		case errors.Is(err, errNeedReconcileAlreadyFinalized):
			span.SetStatus(codes.Ok, "")
			span.SetAttributes(attribute.String("withdrawal.work.outcome", "already_finalized"))
			return s.reconcile(ctx, v.UserID, v.IdempotencyKey)
		}

		span.SetAttributes(attribute.String("withdrawal.work.outcome", "withdrawal_work_failed"))
		span.RecordError(err)
		span.SetStatus(codes.Error, "withdrawal_tx_failed")
		return s.failAndReconcile(ctx, finalParams, err)
	}

	span.SetStatus(codes.Ok, "")
	span.SetAttributes(attribute.String("withdrawal.work.outcome", "ok"))
	return res, nil
}

func (s *Service) reserveIdempotency(
	ctx context.Context,
	v validatedCreateWithdrawal,
) (repo.ReserveIdemResult, error) {
	ctx, span := s.tracer.Start(ctx, "orchestrator.app.reserve_idempotency")
	defer span.End()

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
			span.SetAttributes(attribute.String("idempotency.outcome", "idempotency_key_reuse"))
			span.SetStatus(codes.Ok, "")
			return repo.ReserveIdemResult{}, errInvalidArgument(err)
		case errors.As(err, &cu):
			span.SetAttributes(attribute.String("idempotency.outcome", "commit_unknown"))
			span.SetStatus(codes.Error, "commit_unknown")
			span.RecordError(err)
			return repo.ReserveIdemResult{}, err
		default:
			span.SetAttributes(attribute.String("idempotency.outcome", "idempotency_failed"))
			span.SetStatus(codes.Error, "reserve_idempotency_failed")
			span.RecordError(err)
			return repo.ReserveIdemResult{}, errInternal(err)
		}
	}

	span.SetAttributes(
		attribute.String("withdrawal.id", idemRow.WithdrawalID),
		attribute.Bool("idempotency.owned", idemRow.Owned),
		attribute.String("idempotency.outcome", "reserved"),
	)
	span.SetStatus(codes.Ok, "")

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

type GetWithdrawalParams struct {
	WithdrawalID string
}

type GetWithdrawalResult struct {
	WithdrawalID    string
	UserID          string
	Asset           string
	AmountMinor     int64
	DestinationAddr string
	Status          string
	FailureReason   *string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (s *Service) GetWithdrawal(
	ctx context.Context,
	p GetWithdrawalParams,
) (GetWithdrawalResult, error) {
	row, err := s.repo.GetWithdrawal(
		ctx,
		s.db,
		repo.GetWithdrawalParams{WithdrawalID: p.WithdrawalID},
	)
	if err != nil {
		return GetWithdrawalResult{}, err
	}

	return GetWithdrawalResult{
		WithdrawalID:    row.WithdrawalID,
		UserID:          row.UserID,
		Asset:           row.Asset,
		AmountMinor:     row.AmountMinor,
		DestinationAddr: row.DestinationAddr,
		Status:          row.Status,
		FailureReason:   row.FailureReason,
		CreatedAt:       row.CreatedAt,
		UpdatedAt:       row.UpdatedAt,
	}, nil
}
