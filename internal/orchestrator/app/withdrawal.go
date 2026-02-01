package app

import (
	"context"
	"errors"
	"time"

	identity "github.com/cicconee/cbsaga/internal/contracts/kafka/identity/v1"
	orchestrator "github.com/cicconee/cbsaga/internal/contracts/kafka/orchestrator/v1"
	"github.com/cicconee/cbsaga/internal/orchestrator/domain"
	"github.com/cicconee/cbsaga/internal/orchestrator/repo"
	"github.com/cicconee/cbsaga/internal/platform/codec"
	"github.com/cicconee/cbsaga/internal/platform/db/postgres"
	"github.com/cicconee/cbsaga/internal/platform/fields"
	"github.com/cicconee/cbsaga/internal/platform/logging"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	db   *pgxpool.Pool
	repo *repo.Repo
	log  *logging.Logger
}

func NewService(db *pgxpool.Pool, log *logging.Logger) *Service {
	return &Service{
		db:   db,
		repo: repo.New(),
		log:  log,
	}
}

type CreateWithdrawalParams struct {
	UserID          string
	Asset           string
	AmountMinor     int64
	DestinationAddr string
	IdempotencyKey  string
	TraceID         string
}

type CreateWithdrawalResult struct {
	WithdrawalID string
	Status       string
}

func (s *Service) CreateWithdrawal(
	ctx context.Context,
	p CreateWithdrawalParams,
) (CreateWithdrawalResult, error) {
	now := time.Now().UTC()

	v, err := NewValidatedCreateWithdrawal(p)
	if err != nil {
		return CreateWithdrawalResult{}, err
	}

	// Reserve the idempotency key
	reserveTxFunc := func(ctx context.Context, tx pgx.Tx) (repo.ReserveIdemResult, error) {
		return s.repo.ReserveIdemTx(ctx, tx, repo.ReserveIdemParams{
			UserID:         v.UserID,
			IdempotencyKey: v.IdempotencyKey,
			RequestHash:    v.RequestHash,
			WithdrawalID:   uuid.NewString(),
			LeaseAttemptID: uuid.NewString(),
			LeaseTTL:       30 * time.Second,
			Now:            now,
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
		if errors.Is(err, repo.ErrIdempotencyKeyReuse) {
			return CreateWithdrawalResult{}, errInvalidArgument(StepReserveIdempotency, err)
		}

		var cu postgres.CommitUnknownError
		if errors.As(err, &cu) {
			key := reconcileKey{
				TraceID: v.TraceID,
				UserID:  v.UserID,
				IdemKey: v.IdempotencyKey,
			}

			return s.reconcileAndRecover(
				ctx,
				key,
				StepReserveIdempotency,
				err,
				fields.New().Str("db_op", cu.Op),
			)
		}

		return CreateWithdrawalResult{}, errInternal(StepReserveIdempotency, err)
	}

	// Reserve idempotency transaction is committed and idempotency key is reserved in db
	// but current run does not own it.
	if !idemRow.Owned {
		return s.reconcile(ctx, v.UserID, v.IdempotencyKey)
	}

	// begin tx that will create the withdrawal.
	finalParams := finalizeIdemParams{
		userID:         v.UserID,
		idemKey:        v.IdempotencyKey,
		now:            now,
		leaseAttemptID: idemRow.LeaseOwner,
		leaseFence:     idemRow.LeaseFence,
		traceID:        v.TraceID,
		withdrawalID:   idemRow.WithdrawalID,
	}

	workTx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return s.failAndReconcile(ctx, 13, finalParams, StepCreateWithdrawal, err)
	}
	defer func() { _ = workTx.Rollback(ctx) }()

	// Encode payloads for the outbox_events tables.
	identityPayload, err := codec.EncodeValid(&identity.IdentityRequestCmdPayload{
		WithdrawalID: idemRow.WithdrawalID,
		UserID:       v.UserID,
	})
	if err != nil {
		return s.failAndReconcile(ctx, 13, finalParams, StepCreateWithdrawal, err)
	}
	withdrawPayload, err := codec.EncodeValid(&orchestrator.WithdrawalRequestPayload{
		WithdrawalID: idemRow.WithdrawalID,
		UserID:       v.UserID,
	})
	if err != nil {
		return s.failAndReconcile(ctx, 13, finalParams, StepCreateWithdrawal, err)
	}

	res, err := s.repo.CreateWithdrawalTx(ctx, workTx, repo.CreateWithdrawalParams{
		WithdrawalID:    idemRow.WithdrawalID,
		SagaID:          uuid.NewString(),
		UserID:          v.UserID,
		Asset:           v.Asset,
		AmountMinor:     v.AmountMinor,
		DestinationAddr: v.DestinationAddr,
		TraceID:         v.TraceID,
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
		// If withdrawal already exists some how, reconcile, do not mark as failure.
		if errors.Is(err, repo.ErrWithdrawalAlreadyExists) {
			return s.reconcile(ctx, v.UserID, v.IdempotencyKey)
		}
		return s.failAndReconcile(ctx, 13, finalParams, StepCreateWithdrawal, err)
	}

	// Mark the idempotency key as completed status.
	outcome, err := s.completeIdempotency(ctx, workTx, 0, finalParams)
	if err != nil {
		if errors.Is(err, repo.ErrLostLeaseOwnership) {
			return s.reconcile(ctx, v.UserID, v.IdempotencyKey)
		}
		return s.failAndReconcile(ctx, 13, finalParams, StepFinalizeIdempotency, err)
	}

	// This current run took too long from the moment of ownership till now, that
	// another run gained ownership of the lease and already finalized the withdrawal request.
	if outcome == repo.FinalizeAlreadyFinalized {
		return s.reconcile(ctx, v.UserID, v.IdempotencyKey)
	}

	// Commit the atomic transaction: finalizes idempotency key status and inserts withdrawal.
	if err := workTx.Commit(ctx); err != nil {
		trigger := postgres.CommitUnknownError{
			Op:       "withdrawal/work_tx_commit",
			Err:      err,
			Duration: time.Since(now), // place holder until wrap in WithTx
			CtxErr:   ctx.Err(),
		}

		// Outcome unknown
		rctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		key := reconcileKey{
			TraceID:      v.TraceID,
			UserID:       v.UserID,
			IdemKey:      v.IdempotencyKey,
			WithdrawalID: idemRow.WithdrawalID,
		}

		return s.reconcileAndRecover(rctx, key, StepCreateWithdrawal, trigger, nil)
	}

	return CreateWithdrawalResult{
		WithdrawalID: res.WithdrawalID,
		Status:       res.Status,
	}, nil
}

type finalizeIdemParams struct {
	userID         string
	idemKey        string
	now            time.Time
	leaseAttemptID string
	leaseFence     int64
	traceID        string
	withdrawalID   string
}

func (s *Service) completeIdempotency(
	ctx context.Context,
	workTx pgx.Tx,
	grpcCode int,
	p finalizeIdemParams,
) (repo.FinalizeOutcome, error) {
	return s.repo.FinalizeIdemTx(ctx, workTx, repo.FinalizeIdemParams{
		UserID:         p.userID,
		IdempotencyKey: p.idemKey,
		GRPCCode:       grpcCode,
		Now:            p.now,
		LeaseAttemptID: p.leaseAttemptID,
		LeaseFence:     p.leaseFence,
		Status:         domain.IdemCompleted,
	})
}

func (s *Service) failIdempotencyWithRetry(
	ctx context.Context,
	grpcCode int,
	p finalizeIdemParams,
) (repo.FinalizeOutcome, error) {
	txFunc := func(ctx context.Context, tx pgx.Tx) (repo.FinalizeOutcome, error) {
		return s.repo.FinalizeIdemTx(ctx, tx, repo.FinalizeIdemParams{
			UserID:         p.userID,
			IdempotencyKey: p.idemKey,
			GRPCCode:       grpcCode,
			Now:            p.now,
			LeaseAttemptID: p.leaseAttemptID,
			LeaseFence:     p.leaseFence,
			Status:         domain.IdemFailed,
		})
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
