package repo

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type TransitionWithdrawalParams struct {
	WithdrawalID  string
	FromStatus    string
	ToStatus      string
	FailureReason *string
	Now           time.Time
}

func (r *Repo) TransitionWithdrawalStatusTx(ctx context.Context, tx pgx.Tx, p TransitionWithdrawalParams) error {
	tag, err := tx.Exec(ctx, `
		UPDATE orchestrator.withdrawals
		SET status = $3,
		    failure_reason = $4,
		    updated_at = $5
		WHERE id = $1 AND status = $2
	`, p.WithdrawalID, p.FromStatus, p.ToStatus, p.FailureReason, p.Now)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("withdrawal transition failed (expected %s -> %s)", p.FromStatus, p.ToStatus)
	}
	return nil
}
