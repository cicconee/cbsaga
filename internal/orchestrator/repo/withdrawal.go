package repo

import (
	"context"
	"errors"
	"time"

	"github.com/cicconee/cbsaga/internal/platform/db/postgres"
	"github.com/cicconee/cbsaga/internal/shared/orchestrator"
	"github.com/jackc/pgconn"
	"github.com/jackc/pgx/v5"
)

var ErrWithdrawalAlreadyExists = errors.New("withdrawal already exists")

type OutboxEvent struct {
	EventType string
	Payload   string
	RouteKey  string
}

type CreateWithdrawalParams struct {
	WithdrawalID    string
	SagaID          string
	UserID          string
	Asset           string
	AmountMinor     int64
	DestinationAddr string
	TraceID         string
	OutboxEvents    []OutboxEvent
}

type CreateWithdrawalResult struct {
	WithdrawalID string
	Status       string
}

func (r *Repo) CreateWithdrawalTx(
	ctx context.Context,
	tx pgx.Tx,
	p CreateWithdrawalParams,
) (CreateWithdrawalResult, error) {
	_, err := tx.Exec(ctx, `
		INSERT INTO orchestrator.withdrawals (
			id,
			user_id,
			asset,
			amount_minor,
			destination_addr,
			status
		)
		VALUES (
			$1,
			$2,
			$3,
			$4,
			$5,
			$6
		)
	`,
		p.WithdrawalID,
		p.UserID,
		p.Asset,
		p.AmountMinor,
		p.DestinationAddr,
		orchestrator.WithdrawalStatusRequested,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return CreateWithdrawalResult{}, ErrWithdrawalAlreadyExists
		}
		return CreateWithdrawalResult{}, err
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO orchestrator.saga_instances (
			saga_id,
			withdrawal_id,
			state,
			current_step,
			attempt
		)
		VALUES (
			$1,
			$2,
			$3,
			$4,
			0
		)
	`,
		p.SagaID,
		p.WithdrawalID,
		orchestrator.SagaStateStarted,
		orchestrator.SagaStepIdentityCheck,
	)
	if err != nil {
		return CreateWithdrawalResult{}, err
	}

	for _, evt := range p.OutboxEvents {
		_, err = tx.Exec(ctx, `
		INSERT INTO orchestrator.outbox_events (
			event_id,
			aggregate_type,
			aggregate_id,
			event_type,
			payload_json,
			trace_id,
			route_key
		)
		VALUES (
			gen_random_uuid(),
			$1,
			$2,
			$3,
			$4,
			$5,
			$6
		)
	`,
			orchestrator.AggregateTypeWithdrawal,
			p.WithdrawalID,
			evt.EventType,
			evt.Payload,
			p.TraceID,
			evt.RouteKey,
		)
		if err != nil {
			return CreateWithdrawalResult{}, err
		}
	}

	return CreateWithdrawalResult{
		WithdrawalID: p.WithdrawalID,
		Status:       orchestrator.WithdrawalStatusRequested,
	}, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}

type GetWithdrawalParams struct {
	WithdrawalID string
}

type GetWithdrawalResult struct {
	WithdrawalID    string
	UserID          string
	Asset           string
	AmountMinor     int64
	DestinationAddr string
	Status          string
	FailureReason   *string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (r *Repo) GetWithdrawal(
	ctx context.Context,
	db postgres.DBTX,
	p GetWithdrawalParams,
) (GetWithdrawalResult, error) {
	var res GetWithdrawalResult
	err := db.QueryRow(ctx, `
		SELECT
			id,
			user_id,
			asset,
			amount_minor,
			destination_addr,
			status,
			failure_reason,
			created_at,
			updated_at
		FROM orchestrator.withdrawals
		WHERE 
			id = $1
	`,
		p.WithdrawalID,
	).Scan(
		&res.WithdrawalID,
		&res.UserID,
		&res.Asset,
		&res.AmountMinor,
		&res.DestinationAddr,
		&res.Status,
		&res.FailureReason,
		&res.CreatedAt,
		&res.UpdatedAt,
	)
	return res, err
}
