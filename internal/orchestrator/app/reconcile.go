package app

import (
	"context"
	"errors"

	"github.com/cicconee/cbsaga/internal/orchestrator/domain"
	"github.com/cicconee/cbsaga/internal/orchestrator/repo"
	"github.com/cicconee/cbsaga/internal/platform/db/postgres"
	"github.com/jackc/pgx/v5"
)

type reconcileKey struct {
	TraceID      string
	UserID       string
	IdemKey      string
	WithdrawalID string
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
			"trigger_err", triggerErr,
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

	// did not recover
	fields := map[string]any{
		"trace_id":        key.TraceID,
		"user_id":         key.UserID,
		"idempotency_key": key.IdemKey,
		"withdrawal_id":   key.WithdrawalID,
		"trigger_err":     triggerErr,
		"reconcile_err":   rerr,
	}
	for k, v := range extra {
		fields[k] = v
	}

	return CreateWithdrawalResult{}, errInternalWithFields(step, triggerErr, fields)
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
		return CreateWithdrawalResult{}, errFailed(causeStep, causeErr)
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
		return CreateWithdrawalResult{}, errInternal(StepReconcileGetIdempotency, err)
	}

	switch idemRow.Status {
	case domain.IdemCompleted:
		w, err := s.repo.GetWithdrawal(ctx, s.db, repo.GetWithdrawalParams{
			WithdrawalID: idemRow.WithdrawalID,
		})
		if err != nil {
			return CreateWithdrawalResult{}, errInternal(StepReconcileGetWithdrawal, err)
		}

		return CreateWithdrawalResult{
			WithdrawalID: w.WithdrawalID,
			Status:       domain.WithdrawalStatusRequested,
		}, nil
	case domain.IdemFailed:
		return CreateWithdrawalResult{}, errFailed(StepReconcileIdempotencyFailed, nil)
	case domain.IdemInProgress:
		existingWithdrawal, err := s.repo.GetWithdrawal(ctx, s.db, repo.GetWithdrawalParams{
			WithdrawalID: idemRow.WithdrawalID,
		})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return CreateWithdrawalResult{}, errInternal(StepReconcileWithdrawalInProgress, err)
		}
		if err == nil {
			return CreateWithdrawalResult{
				WithdrawalID: existingWithdrawal.WithdrawalID,
				Status:       domain.WithdrawalStatusRequested,
			}, nil
		}

		return CreateWithdrawalResult{}, errRetryableConflict(
			StepReconcileIdempotencyInProgress,
			err,
		)
	default:
		return CreateWithdrawalResult{}, errInternal(StepReconcileUnknownIdemStatus, nil)
	}
}
