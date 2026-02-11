package app

import (
	"context"
	"errors"

	"github.com/cicconee/cbsaga/internal/orchestrator/domain"
	"github.com/cicconee/cbsaga/internal/orchestrator/repo"
	"github.com/cicconee/cbsaga/internal/platform/apperr"
	"github.com/cicconee/cbsaga/internal/platform/codec"
	"go.opentelemetry.io/otel/attribute"
)

func (s *Service) failAndReconcile(
	ctx context.Context,
	p repo.FinalizeIdemParams,
	cause error,
) (CreateWithdrawalResult, error) {
	ctx, span := s.tracer.Start(ctx, spanFailReconcile)
	defer span.End()

	span.SetAttributes(
		attribute.String(telKeyPhase, "fail_and_reconcile"),
		attribute.String(telKeyIdemKey, p.IdempotencyKey),
	)

	errf := errFailed(cause)
	p.AppErrorCode = &errf.Code
	p.ErrorMessage = &errf.Message

	outcome, err := s.failIdempotencyWithRetry(ctx, p)
	if err == nil && outcome == repo.FinalizeApplied {
		return CreateWithdrawalResult{}, errf
	}

	if err != nil {
		recordInternal(span, err, "finalize_failed")
	}

	return s.reconcile(ctx, p.UserID, p.IdempotencyKey)
}

func (s *Service) reconcile(
	ctx context.Context,
	userID string,
	idemKey string,
) (CreateWithdrawalResult, error) {
	ctx, span := s.tracer.Start(ctx, spanReconcile)
	defer span.End()

	span.SetAttributes(
		attribute.String(telKeyPhase, "reconcile"),
		attribute.String(telKeyIdemKey, idemKey),
	)

	idemRow, err := s.repo.GetIdem(ctx, s.db, repo.GetIdemParams{
		UserID:         userID,
		IdempotencyKey: idemKey,
	})
	if err != nil {
		recordInternal(span, err, "get_idem_failed")
		return CreateWithdrawalResult{}, errInternal(err)
	}

	span.SetAttributes(
		attribute.String(telKeyIdemStatus, idemRow.Status),
		attribute.String(telKeyWithdrawalID, idemRow.WithdrawalID),
	)

	switch idemRow.Status {
	case domain.IdemCompleted:
		if idemRow.ResponseBody == nil {
			errInv := errors.New("invariant violation: idem completed with NULL response_body")
			recordInternal(span, errInv, "invariant_violation",
				attribute.String(telKeyInvariantName, "idem_completed_missing_response_body"),
			)
			return CreateWithdrawalResult{}, errInternal(errInv)
		}

		var res CreateWithdrawalResult
		if err := codec.DecodeJSONPtr(idemRow.ResponseBody, &res); err != nil {
			recordInternal(span, err, "decode_response_failed")
			return CreateWithdrawalResult{}, errInternal(err)
		}

		res.replay = true
		span.SetAttributes(
			attribute.String(telKeyReconcileOutcome, "replayed_completed"),
			attribute.Bool(telKeyIdemReplay, true),
		)
		return res, nil
	case domain.IdemFailed:
		if idemRow.AppErrorCode == nil || idemRow.ErrorMessage == nil {
			errInv := errors.New("invariant violation: idem failed with NULL error columns")
			recordInternal(span, errInv, "invariant_violation",
				attribute.String(telKeyInvariantName, "idem_failed_missing_error_fields"),
			)
			return CreateWithdrawalResult{}, errInternal(errInv)
		}

		span.SetAttributes(
			attribute.String(telKeyReconcileOutcome, "replayed_failed"),
			attribute.Bool(telKeyIdemReplay, true),
		)

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
		span.SetAttributes(attribute.String(telKeyReconcileOutcome, "in_progress"))
		return CreateWithdrawalResult{
			WithdrawalID: idemRow.WithdrawalID,
			Status:       domain.WithdrawalStatusInProgress,
		}, nil
	default:
		err := errors.New("unknown idempotency status")
		recordInternal(span, err, "unknown_status",
			attribute.String(telKeyReconcileOutcome, "unknown_status"),
		)
		return CreateWithdrawalResult{}, errInternal(err)
	}
}
