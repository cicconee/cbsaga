package repo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cicconee/cbsaga/internal/shared/orchestrator"
	"github.com/jackc/pgx/v5"
)

type ApplyIdentityResultParams struct {
	WithdrawalID    string
	UserID          string
	IdentityStatus  string // VERIFIED | REJECTED
	Reason          *string
	TraceID         string
	UpdatedAt       time.Time
	OutboxEventType string
	OutboxPayload   string
}

func (i *ApplyIdentityResultParams) validate() error {
	if i.WithdrawalID == "" {
		return errors.New("identity event: missing withdrawal_id")
	}
	if i.UserID == "" {
		return errors.New("identity event: missing user_id")
	}
	if i.IdentityStatus != "VERIFIED" && i.IdentityStatus != "REJECTED" {
		return fmt.Errorf("identity event: invalid status %q", i.IdentityStatus)
	}
	if i.TraceID == "" {
		return errors.New("identity event: missing trace_id")
	}
	if i.OutboxEventType != "RiskCheckCreated" && i.OutboxEventType != "WithdrawalFailed" {
		return fmt.Errorf("identity event: invalid outbox event type: %s", i.OutboxEventType)
	}

	return nil
}

func (r *Repo) ApplyIdentityResultTx(ctx context.Context, tx pgx.Tx, p ApplyIdentityResultParams) error {
	if err := p.validate(); err != nil {
		return err
	}

	_, err := tx.Exec(ctx, `
		UPDATE orchestrator.withdrawals
		SET status = CASE
				WHEN $2 = 'VERIFIED' THEN 'IN_PROGRESS'
				ELSE 'FAILED'
			END,
		    failure_reason = CASE
				WHEN $2 = 'VERIFIED' THEN NULL
				ELSE COALESCE($3, 'identity rejected')
			END,
		    updated_at = $4
		WHERE id = $1
			AND (
				($2 = 'VERIFIED' AND status = 'REQUESTED')
				OR ($2 = 'REJECTED' AND status IN ('REQUESTED', 'IN_PROGRESS'))
			)
	`, p.WithdrawalID, p.IdentityStatus, p.Reason, p.UpdatedAt)
	if err != nil {
		return fmt.Errorf("update withdrawals based on identity result: %w", err)
	}

	tag, err := tx.Exec(ctx, `
		UPDATE orchestrator.saga_instances
				SET current_step = CASE
						WHEN $2 = 'VERIFIED' THEN 'RISK_CHECK'
						ELSE 'FAILED'
					END,
					state = CASE
						WHEN $2 = 'VERIFIED' THEN 'IN_PROGRESS'
						ELSE 'FAILED'
					END,
					updated_at = $3
				WHERE withdrawal_id = $1
					AND current_step = 'IDENTITY_CHECK'
					AND state IN ('STARTED','IN_PROGRESS')
	`, p.WithdrawalID, p.IdentityStatus, p.UpdatedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		// Already processed. Treat as a no-op.
		// TODO: Possibly early if for some reason saga still hasn't updated from initiating the withdrawal
		// from the gRPC request. Maybe use inbox pattern and drain if delivered early?
		return nil
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO orchestrator.outbox_events
			(event_id, aggregate_type, aggregate_id, event_type, payload_json, trace_id)
		VALUES
			(gen_random_uuid(), $1, $2, $3, $4, $5)
	`, orchestrator.AggregateTypeWithdrawal, p.WithdrawalID, p.OutboxEventType, p.OutboxPayload, p.TraceID)
	if err != nil {
		return fmt.Errorf("insert outbox WithdrawalFailed: %w", err)
	}

	return nil
}
