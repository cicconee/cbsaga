package app

import (
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const (
	spanCreateWithdrawal     = "orchestrator.app.create_withdrawal"
	spanCreateWithdrawalWork = "orchestrator.app.create_withdrawal_work"
	spanReserveIdem          = "orchestrator.app.reserve_idempotency"
	spanFailReconcile        = "orchestrator.app.reconcile"
	spanReconcile            = "orchestrator.app.fail_and_reconcile"

	telKeyPhase           = "orchestrator.phase"
	telKeyReconcileReason = "orchestrator.reconcile_reason"

	telKeyErrorKind     = "error.kind"
	telKeyErrorReason   = "error.reason"
	telKeyInvariantName = "invariant.name"

	telKeyWithdrawalOutcome = "withdrawal.outcome"
	telKeyWithdrawalID      = "withdrawal.id"

	telKeyIdemOutcome = "idempotency.outcome"
	telKeyIdemKey     = "idempotency.key"
	telKeyIdemReqHash = "idempotency.request_hash"
	telKeyIdemOwned   = "idempotency.owned"
	telKeyIdemStatus  = "idempotency.status"
	telKeyIdemReplay  = "idempotency.replay"

	telKeyReconcileOutcome = "reconcile.outcome"
)

func recordInternal(span trace.Span, err error, reason string, attrs ...attribute.KeyValue) {
	attrs = append(attrs, attribute.String(telKeyErrorKind, "internal"))
	attrs = append(attrs, attribute.String(telKeyErrorReason, reason))

	if err != nil {
		span.RecordError(err)
	}
	span.SetStatus(codes.Error, reason)
	span.SetAttributes(attrs...)
}
