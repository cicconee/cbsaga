package app

import (
	"context"
	"errors"
	"time"

	"github.com/cicconee/cbsaga/internal/orchestrator/repo"
	"github.com/cicconee/cbsaga/internal/platform/apperr"
	"github.com/cicconee/cbsaga/internal/platform/codec"
	"github.com/cicconee/cbsaga/internal/platform/db/postgres"
	"github.com/cicconee/cbsaga/internal/platform/logging"
	"github.com/cicconee/cbsaga/internal/shared/identity"
	"github.com/cicconee/cbsaga/internal/shared/orchestrator"
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
			return CreateWithdrawalResult{}, apperr.New(
				apperr.CodeInvalidArgument,
				SubjectWithdrawalCreate,
				StepReserveIdempotencyTx,
				"idempotency key cannot be reused with a different request",
				false,
				err,
			)
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
				SubjectWithdrawalCreate,
				StepReserveIdempotencyCommitTx,
				err,
				map[string]any{"db_op": cu.Op},
			)
		}

		var bt postgres.BeginTxError
		if errors.As(err, &bt) {
			return CreateWithdrawalResult{}, apperr.New(
				apperr.CodeInternal,
				SubjectWithdrawalCreate,
				StepReserveIdempotencyBeginTx,
				"unable to process request; please retry",
				true,
				err,
			)
		}

		return CreateWithdrawalResult{}, apperr.New(
			apperr.CodeInternal,
			SubjectWithdrawalCreate,
			StepReserveIdempotencyTx,
			"unable to process request; please retry",
			true,
			err,
		)
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
		return s.failAndReconcile(ctx, 13, finalParams, StepCreateWithdrawalBeginTx, err)
	}
	defer func() { _ = workTx.Rollback(ctx) }()

	// Encode payloads for the outbox_events tables.
	identityPayload, err := codec.EncodeValid(&identity.IdentityRequestCmdPayload{
		WithdrawalID: idemRow.WithdrawalID,
		UserID:       v.UserID,
	})
	if err != nil {
		return s.failAndReconcile(ctx, 13, finalParams, StepEncodeIdentityPayload, err)
	}
	withdrawPayload, err := codec.EncodeValid(&orchestrator.WithdrawalRequestPayload{
		WithdrawalID: idemRow.WithdrawalID,
		UserID:       v.UserID,
	})
	if err != nil {
		return s.failAndReconcile(ctx, 13, finalParams, StepEncodeWithdrawalPayload, err)
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
		return s.failAndReconcile(ctx, 13, finalParams, StepCreateWithdrawalTx, err)
	}

	// Mark the idempotency key as completed status.
	outcome, err := s.completeIdempotency(ctx, workTx, 0, finalParams)
	if err != nil {
		if errors.Is(err, repo.ErrLostLeaseOwnership) {
			return s.reconcile(ctx, v.UserID, v.IdempotencyKey)
		}
		return s.failAndReconcile(ctx, 13, finalParams, StepFinalizeIdempotencyCompleted, err)
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

		return s.reconcileAndRecover(
			rctx,
			key,
			SubjectWithdrawalCreate,
			StepCreateWithdrawalCommitTx,
			trigger,
			nil,
		)
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
		Status:         orchestrator.IdemCompleted,
	})
}

type reconcileKey struct {
	TraceID      string
	UserID       string
	IdemKey      string
	WithdrawalID string
}

func (s *Service) failAndReconcile(
	ctx context.Context,
	grpcCode int,
	p finalizeIdemParams,
	causeStep string,
	causeErr error,
) (CreateWithdrawalResult, error) {
	outcome, err := s.failIdempotencyWithRetry(ctx, grpcCode, p)
	if err == nil && outcome == repo.FinalizeApplied {
		// Finalized applied successfully (marked FAILED), so return the domain error.
		return CreateWithdrawalResult{}, apperr.New(
			apperr.CodeFailed,
			SubjectWithdrawalCreate,
			causeStep,
			"failed to create a withdrawal; resubmit a new request",
			false,
			causeErr,
		)
	}

	key := reconcileKey{
		TraceID:      p.traceID,
		UserID:       p.userID,
		IdemKey:      p.idemKey,
		WithdrawalID: p.withdrawalID,
	}
	return s.reconcileAndRecover(
		ctx,
		key,
		SubjectWithdrawalCreate,
		StepFinalizeIdempotencyFailed,
		err,
		map[string]any{
			"finalize_outcome": outcome,
			"grpc_code":        grpcCode,
			"cause_step":       causeStep,
			"cause_err":        causeErr,
		},
	)
}

func (s *Service) reconcileAndRecover(
	ctx context.Context,
	key reconcileKey,
	subject string,
	step string,
	triggerErr error,
	extra map[string]any,
) (CreateWithdrawalResult, error) {
	if extra == nil {
		extra = make(map[string]any)
	}

	// Try to recover
	res, rerr := s.reconcile(ctx, key.UserID, key.IdemKey)
	if rerr == nil {
		// Log recovery
		fields := []any{
			"trace_id", key.TraceID,
			"user_id", key.UserID,
			"idempotency_key", key.IdemKey,
			"withdrawal_id", key.WithdrawalID,
			"subject", subject,
			"step", step,
		}
		if triggerErr != nil {
			fields = append(fields, "trigger_err", triggerErr)
		}
		for k, v := range extra {
			fields = append(fields, k, v)
		}

		var cu postgres.CommitUnknownError
		var bt postgres.BeginTxError
		switch {
		case triggerErr != nil && errors.As(triggerErr, &cu):
			s.log.Warn("recovered via reconcile after db commit outcome unknown", fields...)
		case triggerErr != nil && errors.As(triggerErr, &bt):
			s.log.Warn("recovered via reconcile after db begin tx failure", fields...)
		default:
			s.log.Debug("recovered via reconcile", fields...)
		}

		return res, nil
	}

	// did not recover, need an apperr.Error for the handler
	ae := apperr.New(
		apperr.CodeInternal,
		subject,
		step,
		"unable to process request; please retry",
		true,
		triggerErr,
	)
	ae.Fields = map[string]any{
		"trace_id":        key.TraceID,
		"user_id":         key.UserID,
		"idempotency_key": key.IdemKey,
		"withdrawal_id":   key.WithdrawalID,
		"trigger_err":     triggerErr,
		"reconcile_err":   rerr,
	}
	for k, v := range extra {
		ae.Fields[k] = v
	}

	return CreateWithdrawalResult{}, ae
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
			Status:         orchestrator.IdemFailed,
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

func (s *Service) reconcile(
	ctx context.Context,
	userID string,
	idemKey string,
) (CreateWithdrawalResult, error) {
	idemRow, err := s.repo.GetIdem(ctx, s.db, repo.GetIdemParams{
		UserID:         userID,
		IdempotencyKey: idemKey,
	})
	if err != nil {
		return CreateWithdrawalResult{}, apperr.New(
			apperr.CodeInternal,
			SubjectWithdrawalCreate,
			StepReconcileGetIdempotency,
			"unable to process request; please retry",
			true,
			err,
		)
	}

	switch idemRow.Status {
	case orchestrator.IdemCompleted:
		w, err := s.repo.GetWithdrawal(ctx, s.db, repo.GetWithdrawalParams{
			WithdrawalID: idemRow.WithdrawalID,
		})
		if err != nil {
			return CreateWithdrawalResult{}, apperr.New(
				apperr.CodeInternal,
				SubjectWithdrawalCreate,
				StepReconcileGetWithdrawal,
				"unable to process request; please retry",
				true,
				err,
			)
		}

		return CreateWithdrawalResult{
			WithdrawalID: w.WithdrawalID,
			Status:       orchestrator.WithdrawalStatusRequested,
		}, nil

	case orchestrator.IdemFailed:
		return CreateWithdrawalResult{}, apperr.New(
			apperr.CodeFailed,
			SubjectWithdrawalCreate,
			StepReconcileIdempotencyFailed,
			"request failed; resubmit a new request",
			false,
			nil,
		)

	case orchestrator.IdemInProgress:
		existingWithdrawal, err := s.repo.GetWithdrawal(ctx, s.db, repo.GetWithdrawalParams{
			WithdrawalID: idemRow.WithdrawalID,
		})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return CreateWithdrawalResult{}, apperr.New(
				apperr.CodeInternal,
				SubjectWithdrawalCreate,
				StepReconcileWithdrawalInProgress,
				"unable to process request; please retry",
				true,
				err,
			)
		}
		if err == nil {
			return CreateWithdrawalResult{
				WithdrawalID: existingWithdrawal.WithdrawalID,
				Status:       orchestrator.WithdrawalStatusRequested,
			}, nil
		}

		return CreateWithdrawalResult{}, apperr.New(
			apperr.CodeRetryableConflict,
			SubjectWithdrawalCreate,
			StepReconcileIdempotencyInProgress,
			"request is still in progress; please retry",
			true,
			err,
		)

	default:
		return CreateWithdrawalResult{}, apperr.New(
			apperr.CodeInternal,
			SubjectWithdrawalCreate,
			StepReconcileUnknownIdemStatus,
			"unable to process request; please retry",
			true,
			nil,
		)
	}
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
