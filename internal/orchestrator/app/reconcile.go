package app

import (
	"context"
	"errors"

	"github.com/cicconee/cbsaga/internal/orchestrator/domain"
	"github.com/cicconee/cbsaga/internal/orchestrator/repo"
	"github.com/cicconee/cbsaga/internal/platform/apperr"
	"github.com/cicconee/cbsaga/internal/platform/db/postgres"
	"github.com/cicconee/cbsaga/internal/platform/fields"
	"github.com/jackc/pgx/v5"
)

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
	triggerStep string,
	triggerErr error,
) (CreateWithdrawalResult, error) {
	outcome, err := s.failIdempotencyWithRetry(ctx, grpcCode, p)
	if err == nil && outcome == repo.FinalizeApplied {
		// Finalized applied successfully (marked FAILED), so return the domain error.
		return CreateWithdrawalResult{}, errFailed(triggerStep, triggerErr)
	}

	key := reconcileKey{
		TraceID:      p.traceID,
		UserID:       p.userID,
		IdemKey:      p.idemKey,
		WithdrawalID: p.withdrawalID,
	}
	attrs := fields.New().
		Int("finalize_outcome", int(outcome)).
		Int("grpc_code", grpcCode).
		Str("trigger_fail_step", triggerStep).
		Error("trigger_fail_err", triggerErr)

	return s.reconcileAndRecover(ctx, key, StepFinalizeIdempotency, err, attrs)
}

func (s *Service) reconcileAndRecover(
	ctx context.Context,
	key reconcileKey,
	triggerStep string,
	triggerErr error,
	extra *fields.Attrs,
) (CreateWithdrawalResult, error) {
	attrs := fields.New().
		Str("trace_id", key.TraceID).
		Str("user_id", key.UserID).
		Str("idempotency_key", key.IdemKey).
		Str("withdrawal_id", key.WithdrawalID).
		Str("trigger_recover_step", triggerStep).
		Error("trigger_recover_err", triggerErr)

	if extra != nil {
		attrs.Merge(extra)
	}

	// Try to recover
	res, rerr := s.reconcile(ctx, key.UserID, key.IdemKey)
	if rerr == nil {
		// Log recovery
		var cu postgres.CommitUnknownError
		var bt postgres.BeginTxError
		switch {
		case triggerErr != nil && errors.As(triggerErr, &cu):
			s.log.Warn("recovered via reconcile after db commit outcome unknown", attrs.Args()...)
		case triggerErr != nil && errors.As(triggerErr, &bt):
			s.log.Warn("recovered via reconcile after db begin tx failure", attrs.Args()...)
		default:
			s.log.Debug("recovered via reconcile", attrs.Args()...)
		}

		return res, nil
	}

	// did not recover
	var ae *apperr.Error
	if errors.As(rerr, &ae) {
		return CreateWithdrawalResult{}, ae.WithAttrs(attrs)
	}

	rerr = errors.Join(triggerErr, rerr)
	return CreateWithdrawalResult{}, errInternal(StepReconcile, rerr).WithAttrs(attrs)
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
		return CreateWithdrawalResult{}, errInternal(StepReconcile, err)
	}

	switch idemRow.Status {
	case domain.IdemCompleted:
		w, err := s.repo.GetWithdrawal(ctx, s.db, repo.GetWithdrawalParams{
			WithdrawalID: idemRow.WithdrawalID,
		})
		if err != nil {
			return CreateWithdrawalResult{}, errInternal(StepReconcile, err)
		}

		return CreateWithdrawalResult{
			WithdrawalID: w.WithdrawalID,
			Status:       domain.WithdrawalStatusRequested,
		}, nil
	case domain.IdemFailed:
		return CreateWithdrawalResult{}, errFailed(StepReconcile, nil)
	case domain.IdemInProgress:
		existingWithdrawal, err := s.repo.GetWithdrawal(ctx, s.db, repo.GetWithdrawalParams{
			WithdrawalID: idemRow.WithdrawalID,
		})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return CreateWithdrawalResult{}, errInternal(StepReconcile, err)
		}
		if err == nil {
			return CreateWithdrawalResult{
				WithdrawalID: existingWithdrawal.WithdrawalID,
				Status:       domain.WithdrawalStatusRequested,
			}, nil
		}

		return CreateWithdrawalResult{}, errRetryableConflict(StepReconcile, err)
	default:
		return CreateWithdrawalResult{}, errInternal(StepReconcile, nil)
	}
}
