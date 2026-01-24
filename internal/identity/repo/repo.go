package repo

import (
	"context"

	"github.com/jackc/pgx/v5"
)

type Repo struct{}

func New() *Repo { return &Repo{} }

type VerifyAndEmitParams struct {
	VerificationID  string
	WithdrawalID    string
	UserID          string
	Status          string
	Reason          *string
	OutboxEventType string
	OutboxPayload   string
	TraceID         string
}

func (r *Repo) VerifyAndEmitTx(ctx context.Context, tx pgx.Tx, p VerifyAndEmitParams) error {
	tag, err := tx.Exec(ctx, `
		INSERT INTO identity.verifications
			(verification_id, withdrawal_id, user_id, status, reason)
		VALUES
			($1, $2, $3, $4, $5)
		ON CONFLICT (withdrawal_id) DO NOTHING
	`, p.VerificationID, p.WithdrawalID, p.UserID, p.Status, p.Reason)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return nil
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO identity.outbox_events
			(event_id, aggregate_type, aggregate_id, event_type, payload_json, trace_id)
		VALUES
			(gen_random_uuid(), 'identity', $1, $2, $3, $4)
	`, p.WithdrawalID, p.OutboxEventType, p.OutboxPayload, p.TraceID)
	return err
}
