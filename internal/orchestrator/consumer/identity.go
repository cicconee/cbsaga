package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/cicconee/cbsaga/internal/orchestrator/repo"
	"github.com/cicconee/cbsaga/internal/platform/logging"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/segmentio/kafka-go"
)

type Identity struct {
	db   *pgxpool.Pool
	repo *repo.Repo
	log  *logging.Logger
	r    *kafka.Reader
}

func NewIdentity(db *pgxpool.Pool, log *logging.Logger, brokers []string, groupID string, topic string) *Identity {
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:     brokers,
		GroupID:     groupID,
		Topic:       topic,
		MinBytes:    1,
		MaxBytes:    10e6,
		MaxWait:     500 * time.Millisecond,
		StartOffset: kafka.LastOffset,
	})

	return &Identity{
		db:   db,
		repo: repo.New(),
		log:  log,
		r:    reader,
	}
}

func (i *Identity) Close() error {
	return i.r.Close()
}

type IdentityResult struct {
	WithdrawalID string  `json:"withdrawal_id"`
	UserID       string  `json:"user_id"`
	Status       string  `json:"status"` // VERIFIED | REJECTED
	Reason       *string `json:"reason,omitempty"`
}

func (i *Identity) Run(ctx context.Context) error {
	i.log.Info("orchestrator identity consumer started")

	for {
		m, err := i.r.FetchMessage(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				i.log.Info("orchestrator identity consumer stopped")
				return nil
			}
			return err
		}

		traceID, ok := headerValue(m.Headers, "trace_id")
		if !ok || traceID == "" {
			// TODO: This should never be ignored. This must be made apparent the moment it happens.
			traceID = "local-trace-id-orchestrator"
		}

		evt, ok := parseConnectEnvelopeIdentity(m.Value)
		if !ok || evt.WithdrawalID == "" || evt.UserID == "" {
			// TODO: This should never be ignored. Must be made apparent the moment it happens.
			continue
		}

		tx, err := i.db.BeginTx(ctx, pgx.TxOptions{})
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback(ctx) }()

		row, err := i.repo.GetWithdrawalTx(ctx, tx, repo.GetWithdrawalParams{WithdrawalID: evt.WithdrawalID})
		if err != nil {
			return err
		}

		// Build out next event payload.
		// identity = VERIFIED -> RiskCheckCreated event_type to be passed to risk service. Aggregate (withdrawal)
		// is now in IN_PROGRESS status
		//
		// identity = REJECTED -> WithdrawalFailed event_type to be passed to _ service. Aggregate (withdrawal)
		// is now in FAILED status
		var outboxEventType string
		var status string
		switch evt.Status {
		case "VERIFIED":
			outboxEventType = "RiskCheckCreated"
			status = "IN_PROGRESS"
		case "REJECTED":
			outboxEventType = "WithdrawalFailed"
			status = "FAILED"
		default:
		}
		payload := map[string]any{
			"withdrawal_id":    evt.WithdrawalID,
			"user_id":          row.UserID,
			"asset":            row.Asset,
			"amount_minor":     row.AmountMinor,
			"destination_addr": row.DestinationAddr,
			"status":           status,
			"requested_by":     "orchestrator",
		}
		b, _ := json.Marshal(payload)
		if err := i.repo.ApplyIdentityResultTx(ctx, tx, repo.ApplyIdentityResultParams{
			WithdrawalID:    evt.WithdrawalID,
			UserID:          evt.UserID,
			IdentityStatus:  evt.Status,
			Reason:          evt.Reason,
			UpdatedAt:       time.Now().UTC(),
			TraceID:         traceID,
			OutboxEventType: outboxEventType,
			OutboxPayload:   string(b),
		}); err != nil {
			i.log.Error("ApplyIdentityResultTx failed",
				"err", err,
				"withdrawal_id", evt.WithdrawalID,
			)
			return err
		}

		if err := tx.Commit(ctx); err != nil {
			return err
		}

		if err := i.r.CommitMessages(ctx, m); err != nil {
			i.log.Error("CommitMessages failed", "err", err)
			return err
		}

		i.log.Info("identity result applied",
			"withdrawal_id", evt.WithdrawalID,
			"status", evt.Status,
			"trace_id", traceID,
		)
	}
}

func parseConnectEnvelopeIdentity(b []byte) (IdentityResult, bool) {
	var env struct {
		Payload json.RawMessage `json:"payload"`
	}

	if err := json.Unmarshal(b, &env); err == nil && len(env.Payload) > 0 {
		var evt IdentityResult
		if err := json.Unmarshal(env.Payload, &evt); err == nil {
			return evt, true
		}
	}

	var evt IdentityResult
	if err := json.Unmarshal(b, &evt); err == nil {
		return evt, true
	}

	return IdentityResult{}, false
}

func headerValue(headers []kafka.Header, key string) (string, bool) {
	for _, h := range headers {
		if h.Key == key {
			return string(h.Value), true
		}
	}
	return "", false
}
