package app

import (
	"context"
	"errors"

	"github.com/cicconee/cbsaga/internal/orchestrator/domain"
	"github.com/cicconee/cbsaga/internal/orchestrator/repo"
	"github.com/cicconee/cbsaga/internal/platform/apperr"
	"github.com/cicconee/cbsaga/internal/platform/codec"
	"github.com/cicconee/cbsaga/internal/platform/fields"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

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
	tr trigger,
) (CreateWithdrawalResult, error) {
	ctx, span := s.tracer.Start(ctx, "orchestrator.app.fail_and_reconcile")
	defer span.End()

	span.SetAttributes(attribute.String("idempotency.key", p.IdempotencyKey))

	errf := errFailed(tr.Err).WithAttrs(tr.Attrs)
	p.AppErrorCode = &errf.Code
	p.ErrorMessage = &errf.Message

	outcome, err := s.failIdempotencyWithRetry(ctx, p)
	finalizeFault := err != nil
	if err == nil && outcome == repo.FinalizeApplied {
		span.SetAttributes(
			attribute.String("fail.finalize", "applied"),
			attribute.String("fail.recovery", "not_needed"),
		)
		span.SetStatus(codes.Ok, "")
		return CreateWithdrawalResult{}, errf
	}

	if err != nil {
		span.RecordError(err)
		span.SetAttributes(
			attribute.String("fail.finalize", "error"),
			attribute.String("internal.fault.name", "finalize_idempotency_failed"),
		)
		span.SetStatus(codes.Error, "apply_failed")
	} else {
		span.SetAttributes(attribute.String("fail.finalize", "already_finalized"))
	}

	res, rerr := s.reconcile(ctx, p.UserID, p.IdempotencyKey)
	if rerr == nil {
		span.SetAttributes(attribute.String("fail.recovery", "ok"))
		if !finalizeFault {
			span.SetStatus(codes.Ok, "")
		}
		return res, nil
	}

	var ae *apperr.Error
	if errors.As(rerr, &ae) && ae.Code != apperr.CodeInternal {
		span.SetAttributes(attribute.String("fail.recovery", "business_error"))
		if !finalizeFault {
			span.SetStatus(codes.Ok, "")
		}
		return CreateWithdrawalResult{}, rerr
	}

	span.RecordError(rerr)
	span.SetAttributes(attribute.String("fail.recovery", "internal_error"))
	span.SetStatus(codes.Error, "internal")
	return CreateWithdrawalResult{}, rerr
}

func (s *Service) reconcile(
	ctx context.Context,
	userID string,
	idemKey string,
) (CreateWithdrawalResult, error) {
	ctx, span := s.tracer.Start(ctx, "orchestrator.app.reconcile")
	defer span.End()

	span.SetAttributes(attribute.String("idempotency.key", idemKey))

	idemRow, err := s.repo.GetIdem(ctx, s.db, repo.GetIdemParams{
		UserID:         userID,
		IdempotencyKey: idemKey,
	})
	if err != nil {
		span.RecordError(err)
		span.SetAttributes(attribute.String("reconcile.outcome", "get_idem_failed"))
		span.SetStatus(codes.Error, "internal")
		return CreateWithdrawalResult{}, errInternal(err)
	}

	span.SetAttributes(
		attribute.String("idempotency.status", idemRow.Status),
		attribute.String("withdrawal.id", idemRow.WithdrawalID),
	)

	switch idemRow.Status {
	case domain.IdemCompleted:
		if idemRow.ResponseBody == nil {
			errInv := errors.New("invariant violation: idem completed with NULL response_body")
			span.RecordError(errInv)
			span.SetAttributes(
				attribute.String("reconcile.outcome", "invariant_violation"),
				attribute.String("invariant", "idem_completed_missing_response_body"),
			)
			span.SetStatus(codes.Error, "internal")
			return CreateWithdrawalResult{}, errInternal(errInv)
		}

		var res CreateWithdrawalResult
		if err := codec.DecodeJSONPtr(idemRow.ResponseBody, &res); err != nil {
			span.RecordError(err)
			span.SetAttributes(attribute.String("reconcile.outcome", "decode_response_failed"))
			span.SetStatus(codes.Error, "internal")
			return CreateWithdrawalResult{}, errInternal(err)
		}

		res.replay = true
		span.SetAttributes(
			attribute.String("reconcile.outcome", "replayed_completed"),
			attribute.Bool("idempotency.replay", true),
		)
		span.SetStatus(codes.Ok, "")
		return res, nil
	case domain.IdemFailed:
		if idemRow.AppErrorCode == nil || idemRow.ErrorMessage == nil {
			errInv := errors.New(
				"invariant violation: idem failed with NULL error_code/error_message",
			)
			span.RecordError(errInv)
			span.SetAttributes(
				attribute.String("reconcile.outcome", "invariant_violation"),
				attribute.String("invariant", "idem_failed_missing_error_fields"),
			)
			span.SetStatus(codes.Error, "internal")
			return CreateWithdrawalResult{}, errInternal(errInv)
		}

		span.SetAttributes(
			attribute.String("reconcile.outcome", "replayed_failed"),
			attribute.Bool("idempotency.replay", true),
		)
		span.SetStatus(codes.Ok, "")

		return CreateWithdrawalResult{}, apperr.New(
			*idemRow.AppErrorCode,
			*idemRow.ErrorMessage,
			false,
			nil,
		).WithAttr("replay", true)
	case domain.IdemInProgress:
		// Concurrent requests may see IN_PROGRESS status, before the owner request flips status to
		// COMPLETED/FAILED. For simplicity, do not recheck. Either the owner request will return
		// the finalized status correctly, or a request retry will. To avoid unnecessary requests,
		// this should reread the idempotency row and/or the withdrawal row.
		span.SetAttributes(attribute.String("reconcile.outcome", "in_progress"))
		span.SetStatus(codes.Ok, "")
		return CreateWithdrawalResult{
			WithdrawalID: idemRow.WithdrawalID,
			Status:       domain.WithdrawalStatusInProgress,
		}, nil
	default:
		span.SetAttributes(attribute.String("reconcile.outcome", "unknown_status"))
		span.SetStatus(codes.Error, "internal")
		return CreateWithdrawalResult{}, errInternal(nil)
	}
}
