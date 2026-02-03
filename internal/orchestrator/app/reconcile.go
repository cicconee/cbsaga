package app

import (
	"context"
	"errors"

	"github.com/cicconee/cbsaga/internal/orchestrator/domain"
	"github.com/cicconee/cbsaga/internal/orchestrator/repo"
	"github.com/cicconee/cbsaga/internal/platform/apperr"
	"github.com/cicconee/cbsaga/internal/platform/codec"
	"github.com/cicconee/cbsaga/internal/platform/fields"
	"github.com/cicconee/cbsaga/internal/platform/meta"
	"github.com/jackc/pgx/v5"
)

type reconcileKey struct {
	UserID       string
	IdemKey      string
	WithdrawalID string
}

type trigger struct {
	Step  string
	Err   error
	Attrs *fields.Attrs
}

func newTrigger(step string, reason string, err error) trigger {
	return trigger{
		Step: step,
		Err:  err,
		Attrs: fields.New().
			Str("trigger_step", step).
			Str("trigger_reason", reason).
			Error("trigger_err", err),
	}
}

func (s *Service) failAndReconcile(
	ctx context.Context,
	p repo.FinalizeIdemParams,
	key reconcileKey,
	tr trigger,
) (CreateWithdrawalResult, error) {
	errf := errFailed(tr.Step, tr.Err).WithAttrs(tr.Attrs)

	p.AppErrorCode = &errf.Code
	p.ErrorMessage = &errf.Message
	outcome, err := s.failIdempotencyWithRetry(ctx, p)
	if err == nil && outcome == repo.FinalizeApplied {
		return CreateWithdrawalResult{}, errf
	}

	var outcomeStr string
	if err != nil {
		outcomeStr = "finalize_error"
	} else {
		outcomeStr = "finalize_already_applied"
	}
	tr.Attrs = fields.New().
		Merge(tr.Attrs).
		Str("path", "fail_and_reconcile").
		Str("finalize_outcome", outcomeStr).
		Error("finalize_err", err)

	return s.reconcileAndRecover(ctx, key, tr)
}

func (s *Service) reconcileAndRecover(
	ctx context.Context,
	key reconcileKey,
	tr trigger,
) (CreateWithdrawalResult, error) {
	ctx, traceID := meta.EnsureTraceID(ctx)
	res, rerr := s.reconcile(ctx, key.UserID, key.IdemKey)

	logAttrs := fields.New().
		Str("trace_id", traceID).
		Str("user_id", key.UserID).
		Str("idempotency_key", key.IdemKey).
		Str("withdrawal_id", key.WithdrawalID).
		Merge(tr.Attrs)

	if rerr == nil {
		s.log.Debug("recovered via reconcile (success)", logAttrs.Args()...)
		return res, nil
	}

	errAttrs := fields.New().
		Str("withdrawal_id", key.WithdrawalID).
		Merge(tr.Attrs).
		Error("recover_err", rerr)

	var ae *apperr.Error
	if errors.As(rerr, &ae) {
		if ae.Code != apperr.CodeInternal {
			s.log.Debug("recovered via reconcile (determined outcome)", logAttrs.Args()...)
		}
		return CreateWithdrawalResult{}, ae.WithAttrs(errAttrs)
	}

	return CreateWithdrawalResult{}, errInternal(StepReconcile, rerr).WithAttrs(errAttrs)
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
		if idemRow.ResponseBody != nil {
			var res CreateWithdrawalResult
			if err := codec.DecodeJSONPtr(idemRow.ResponseBody, &res); err != nil {
				return CreateWithdrawalResult{}, errInternal(StepReconcile, err)
			}
			res.replay = true
			return res, nil
		}

		// Fallback - maybe error, not sure yet. Leaning towards error.
		return CreateWithdrawalResult{
			WithdrawalID: w.WithdrawalID,
			Status:       domain.WithdrawalStatusRequested,
			replay:       true,
		}, nil
	case domain.IdemFailed:
		if idemRow.AppErrorCode != nil && idemRow.ErrorMessage != nil {
			return CreateWithdrawalResult{}, apperr.New(
				*idemRow.AppErrorCode,
				SubjectWithdrawalCreate,
				StepReconcile,
				*idemRow.ErrorMessage,
				false,
				nil,
			).WithAttr("replay", true)
		}

		// Fallback - maybe error (not replay error), not sure yet. Leaning towards error.
		err := errFailed(StepReconcile, nil).
			WithAttr("replay", true).
			WithAttr("replay_not_found", true)
		return CreateWithdrawalResult{}, err
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
