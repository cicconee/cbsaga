package consumer

import (
	"context"
	"errors"
	"time"

	"github.com/cicconee/cbsaga/internal/orchestrator/repo"
	"github.com/cicconee/cbsaga/internal/platform/codec"
	"github.com/cicconee/cbsaga/internal/platform/logging"
	"github.com/cicconee/cbsaga/internal/platform/messaging"
	"github.com/cicconee/cbsaga/internal/shared/identity"
	"github.com/cicconee/cbsaga/internal/shared/orchestrator"
	"github.com/cicconee/cbsaga/internal/shared/risk"
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

func NewIdentity(
	db *pgxpool.Pool,
	log *logging.Logger,
	brokers []string,
	groupID string,
	topic string,
) *Identity {
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

		headers := messaging.NewHeaders(m.Headers)
		traceID, ok := headers.String("trace_id")
		if !ok || traceID == "" {
			// TODO: This should never be ignored. This must be made apparent the moment it happens.
			traceID = "local-trace-id-orchestrator"
		}

		eventType, ok := headers.String("event_type")
		if !ok || eventType == "" {
			// TODO: Should log.
			continue
		}
		if eventType != identity.EventTypeIdentityVerified &&
			eventType != identity.EventTypeIdentityRejected {
			// TODO: Should log.
			continue
		}

		identityEvtPayload := identity.IdentityRequestEvtPayload{}
		err = messaging.DecodeConnectEnvelopeValid(m.Value, &identityEvtPayload)
		if err != nil {
			// TODO: log, remember that decode also validates the event.
			continue
		}

		tx, err := i.db.BeginTx(ctx, pgx.TxOptions{})
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback(ctx) }()

		row, err := i.repo.GetWithdrawalTx(ctx, tx, repo.GetWithdrawalParams{
			WithdrawalID: identityEvtPayload.WithdrawalID,
		})
		if err != nil {
			return err
		}

		// Build out next event payload.
		// identity = VERIFIED -> RiskCheckCreated event_type to be passed to risk service.
		// Aggregate (withdrawal) is now in IN_PROGRESS status
		//
		// identity = REJECTED -> WithdrawalFailed event_type to be passed to _ service. Aggregate
		// (withdrawal) is now in FAILED status
		var outboxEventType string
		var routeKey string
		switch eventType {
		case identity.EventTypeIdentityVerified:
			outboxEventType = risk.EventTypeRiskCheckRequested
			routeKey = risk.RouteKeyRiskCmd
		case identity.EventTypeIdentityRejected:
			outboxEventType = orchestrator.EventTypeWithdrawalFailed
			routeKey = orchestrator.RouteKeyWithdrawalEvt
		default:
		}

		riskPayload, err := codec.EncodeValid(&risk.RiskCheckRequestPayload{
			WithdrawalID:    identityEvtPayload.WithdrawalID,
			UserID:          row.UserID,
			Asset:           row.Asset,
			AmountMinor:     row.AmountMinor,
			DestinationAddr: row.DestinationAddr,
		})
		if err != nil {
			// TODO: log and continue.
			continue
		}

		if err := i.repo.ApplyIdentityResultTx(ctx, tx, repo.ApplyIdentityResultParams{
			WithdrawalID:      identityEvtPayload.WithdrawalID,
			UserID:            identityEvtPayload.UserID,
			IdentityEventType: eventType,
			Reason:            identityEvtPayload.Reason,
			UpdatedAt:         time.Now().UTC(),
			TraceID:           traceID,
			OutboxEventType:   outboxEventType,
			OutboxPayload:     string(riskPayload),
			RouteKey:          routeKey,
		}); err != nil {
			i.log.Error("ApplyIdentityResultTx failed",
				"err", err,
				"withdrawal_id", identityEvtPayload.WithdrawalID,
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
			"withdrawal_id", identityEvtPayload.WithdrawalID,
			"event_type", eventType,
			"trace_id", traceID,
		)
	}
}
