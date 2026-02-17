package consumer

import (
	"context"
	"errors"
	"time"

	identity "github.com/cicconee/cbsaga/internal/contracts/kafka/identity/v1"
	"github.com/cicconee/cbsaga/internal/identity/domain"
	"github.com/cicconee/cbsaga/internal/identity/repo"
	"github.com/cicconee/cbsaga/internal/platform/codec"
	"github.com/cicconee/cbsaga/internal/platform/logging"
	"github.com/cicconee/cbsaga/internal/platform/messaging"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/segmentio/kafka-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

type Consumer struct {
	db     *pgxpool.Pool
	repo   *repo.Repo
	log    *logging.Logger
	r      *kafka.Reader
	tracer trace.Tracer
}

func New(db *pgxpool.Pool, log *logging.Logger, brokers []string, groupID, topic string) *Consumer {
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:     brokers,
		GroupID:     groupID,
		Topic:       topic,
		MinBytes:    1,
		MaxBytes:    10e6, // 10mb
		MaxWait:     500 * time.Millisecond,
		StartOffset: kafka.LastOffset,
	})

	return &Consumer{
		db:     db,
		repo:   repo.New(),
		log:    log,
		r:      reader,
		tracer: otel.Tracer("identity/consumer"),
	}
}

func (c *Consumer) Close() error {
	return c.r.Close()
}

type WithdrawalRequested struct {
	WithdrawalID    string `json:"withdrawal_id"`
	UserID          string `json:"user_id"`
	Asset           string `json:"asset"`
	AmountMinor     int64  `json:"amount_minor"`
	DestinationAddr string `json:"destination_addr"`
	Status          string `json:"status"`
}

func (c *Consumer) Run(ctx context.Context) error {
	c.log.Info("identity consumer started")

	for {
		m, err := c.r.FetchMessage(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				c.log.Info("identity consumer stopped")
				return nil
			}
			return err
		}

		if err := c.handleMessage(ctx, m); err != nil {
			return err
		}
	}
}

func (c *Consumer) handleMessage(ctx context.Context, m kafka.Message) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	headers := messaging.NewHeaders(m.Headers)
	tp, _ := headers.String("traceparent")
	ts, _ := headers.String("tracestate")
	inbound := propagation.MapCarrier{"traceparent": tp, "tracestate": ts}

	ctx = otel.GetTextMapPropagator().Extract(ctx, inbound)
	ctx, span := c.tracer.Start(ctx, spanIdentityConsume,
		trace.WithSpanKind(trace.SpanKindConsumer),
	)
	defer span.End()

	sc := span.SpanContext()
	log := c.log.WithContext(ctx)

	span.SetAttributes(
		attribute.String(telKeyMessagingSystem, "kafka"),
		attribute.String(telKeyMessagingDest, m.Topic),
		attribute.Int(telKeyKafkaPartition, m.Partition),
		attribute.Int64(telKeyKafkaOffset, m.Offset),
	)

	var cmd identity.IdentityRequestCmdPayload
	if err := messaging.DecodeConnectEnvelopeValid(m.Value, &cmd); err != nil {
		// For now halt and return. This should never happen with current code.
		// But to future proof I should either commit + drop, or dlq + commit.
		recordInternal(span, err, "decode_identity_cmd_failed")
		log.Error("decode failed",
			"err", err,
			"topic", m.Topic,
			"partition", m.Partition,
			"offset", m.Offset,
		)
		return err
	}

	span.SetAttributes(
		attribute.String(telKeyWithdrawalID, cmd.WithdrawalID),
		attribute.String(telKeyUserID, cmd.UserID),
	)

	// Mocking identity verification for now. Maybe implement this or add some random REJECTED
	// and delays?
	status := domain.IdentityStatusVerified
	eventType := identity.EventTypeIdentityVerified
	var reason *string

	identityEvtPayload, err := codec.EncodeValid(&identity.IdentityRequestEvtPayload{
		WithdrawalID: cmd.WithdrawalID,
		UserID:       cmd.UserID,
		Reason:       reason,
	})
	if err != nil {
		recordInternal(span, err, "encode_identity_evt_failed")
		log.Error("encode failed", "err", err)
		return err
	}

	tx, err := c.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		recordInternal(span, err, "begin_tx_failed")
		log.Error("begin tx failed", "err", err)
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	verificationID := uuid.NewString()
	span.SetAttributes(attribute.String(telKeyIdentityVerifyID, verificationID))

	// inject the outbound context
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	tp = carrier.Get("traceparent")
	ts = carrier.Get("tracestate")

	if err := c.repo.VerifyAndEmitTx(ctx, tx, repo.VerifyAndEmitParams{
		VerificationID:  verificationID,
		WithdrawalID:    cmd.WithdrawalID,
		UserID:          cmd.UserID,
		Status:          status,
		Reason:          reason,
		OutboxEventType: eventType,
		OutboxPayload:   string(identityEvtPayload),
		TraceID:         sc.TraceID().String(),
		RouteKey:        identity.RouteKeyIdentityEvt,
		TraceParent:     tp,
		TraceState:      ts,
	}); err != nil {
		recordInternal(span, err, "verify_emit_tx_failed")
		log.Error("VerifyAndEmitTx failed", "err", err)
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		recordInternal(span, err, "commit_tx_failed")
		log.Error("commit tx failed", "err", err)
		return err
	}

	if err := c.r.CommitMessages(ctx, m); err != nil {
		recordInternal(span, err, "commit_message_failed")
		log.Error("commit message failed",
			"err", err,
			"topic", m.Topic,
			"partition", m.Partition,
			"offset", m.Offset,
		)
		return err
	}

	span.SetAttributes(
		attribute.String(telKeyIdentityOutcome, status),
		attribute.String(telKeyIdentityEventType, eventType),
	)

	log.Info("identity emitted decision",
		"withdrawal_id", cmd.WithdrawalID,
		"outcome", status,
		"event_type", eventType,
	)

	return nil
}
