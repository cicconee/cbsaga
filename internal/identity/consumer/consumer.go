package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/cicconee/cbsaga/internal/identity/repo"
	"github.com/cicconee/cbsaga/internal/platform/logging"
	"github.com/cicconee/cbsaga/internal/platform/messaging"
	"github.com/cicconee/cbsaga/internal/shared/identity"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/segmentio/kafka-go"
)

type Consumer struct {
	db   *pgxpool.Pool
	repo *repo.Repo
	log  *logging.Logger
	r    *kafka.Reader
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
		db:   db,
		repo: repo.New(),
		log:  log,
		r:    reader,
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

		headers := messaging.NewHeaders(m.Headers)
		traceID, ok := headers.String("trace_id")
		if !ok || traceID == "" {
			// TODO: This should never be ignored. This must be made apparent the moment it happens.
			traceID = "local-trace-id-identity"
		}

		identityPayload := identity.IdentityRequestCmdPayload{}
		err = messaging.DecodeConnectEnvelopeValid(m.Value, &identityPayload)
		if err != nil {
			// TODO: log and continue, remember decoding also validates.
			continue
		}

		// Mocking identity verification for now. Maybe implement this or add some random REJECTED and delays?
		status := identity.IdentityStatusVerified
		outboxType := identity.EventTypeIdentityVerified
		var reason *string

		payload := map[string]any{
			"withdrawal_id": identityPayload.WithdrawalID,
			"user_id":       identityPayload.UserID,
		}
		if reason != nil {
			payload["reason"] = *reason
		}
		b, _ := json.Marshal(payload)

		tx, err := c.db.BeginTx(ctx, pgx.TxOptions{})
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback(ctx) }()

		verificationID := uuid.New().String()

		if err := c.repo.VerifyAndEmitTx(ctx, tx, repo.VerifyAndEmitParams{
			VerificationID:  verificationID,
			WithdrawalID:    identityPayload.WithdrawalID,
			UserID:          identityPayload.UserID,
			Status:          status,
			Reason:          reason,
			OutboxEventType: outboxType,
			OutboxPayload:   string(b),
			TraceID:         traceID,
			RouteKey:        identity.RouteKeyIdentityEvt,
		}); err != nil {
			c.log.Error("VerifyAndEmitTx failed", "err", err, "withdrawal_id", identityPayload.WithdrawalID)
			return err
		}

		if err := tx.Commit(ctx); err != nil {
			return err
		}

		if err := c.r.CommitMessages(ctx, m); err != nil {
			c.log.Error("CommitMessages failed", "err", err)
			return err
		}

		c.log.Info("identity emitted decision",
			"withdrawal_id", identityPayload.WithdrawalID,
			"decision", status,
			"event_type", outboxType,
			"trace_id", traceID,
		)
	}
}
