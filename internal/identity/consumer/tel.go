package consumer

import (
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const (
	spanIdentityConsume = "identity.consume"

	telKeyErrorKind   = "error.kind"
	telKeyErrorReason = "error.reason"

	telKeyMessagingSystem = "messaging.system"
	telKeyMessagingDest   = "messaging.destination"
	telKeyKafkaPartition  = "messaging.kafka.partition"
	telKeyKafkaOffset     = "messaging.kafka.message.offset"

	telKeyWithdrawalID = "withdrawal.id"

	telKeyUserID = "user.id"

	telKeyIdentityVerifyID  = "identity.verification_id"
	telKeyIdentityOutcome   = "identity.outcome"
	telKeyIdentityEventType = "identity.event_type"
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
