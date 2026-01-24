package repo

import (
	"context"
	"errors"
	"time"

	"github.com/cicconee/cbsaga/internal/shared/orchestrator"
	"github.com/jackc/pgx/v5"
)

var ErrIdempotencyKeyReuse = errors.New("idempotency key reuse with different request")

type ReserveIdempotencyParams struct {
	UserID         string
	IdempotencyKey string
	RequestHash    string
	WithdrawalID   string
	Now            time.Time
}

type ReserveIdempotencyResult struct {
	Owned            bool
	Status           string
	WithdrawalID     string
	RequestHash      string
	GRPCCode         int
	ResponseBodyJSON string
}

func (r *Repo) ReserveIdempotencyTx(ctx context.Context, tx pgx.Tx, p ReserveIdempotencyParams) (ReserveIdempotencyResult, error) {
	var inserted bool

	err := tx.QueryRow(ctx, `
		INSERT INTO orchestrator.idempotency_keys
			(id, user_id, idempotency_key, withdrawal_id, request_hash,
			 response_code, response_body_json, status, grpc_code, updated_at)
		VALUES
			(gen_random_uuid(), $1, $2, $3, $4, 0, '{}', 'IN_PROGRESS', 0, $5)
		ON CONFLICT (user_id, idempotency_key) DO NOTHING
		RETURNING true
	`, p.UserID, p.IdempotencyKey, p.WithdrawalID, p.RequestHash, p.Now).Scan(&inserted)

	if err != nil && err != pgx.ErrNoRows {
		return ReserveIdempotencyResult{}, err
	}

	if inserted {
		return ReserveIdempotencyResult{
			Owned:        true,
			Status:       orchestrator.IdemInProgress,
			WithdrawalID: p.WithdrawalID,
			RequestHash:  p.RequestHash,
		}, nil
	}

	var status string
	var withdrawalID string
	var requestHash string
	var grpcCode int
	var respBody string

	err = tx.QueryRow(ctx, `
		SELECT status, withdrawal_id, request_hash, grpc_code, response_body_json
		FROM orchestrator.idempotency_keys
		WHERE user_id = $1 AND idempotency_key = $2
	`, p.UserID, p.IdempotencyKey).Scan(&status, &withdrawalID, &requestHash, &grpcCode, &respBody)
	if err != nil {
		return ReserveIdempotencyResult{}, err
	}

	if requestHash != p.RequestHash {
		return ReserveIdempotencyResult{}, ErrIdempotencyKeyReuse
	}

	return ReserveIdempotencyResult{
		Owned:            false,
		Status:           status,
		WithdrawalID:     withdrawalID,
		RequestHash:      requestHash,
		GRPCCode:         grpcCode,
		ResponseBodyJSON: respBody,
	}, nil
}

type FinalizeIdempotencyParams struct {
	UserID           string
	IdempotencyKey   string
	GRPCCode         int
	ResponseBodyJSON string
	Now              time.Time
}

func (r *Repo) CompleteIdempotencyTx(ctx context.Context, tx pgx.Tx, p FinalizeIdempotencyParams) error {
	_, err := tx.Exec(ctx, `
		UPDATE orchestrator.idempotency_keys
		SET status = 'COMPLETED',
		    grpc_code = $3,
		    response_code = 200,
		    response_body_json = $4,
		    updated_at = $5
		WHERE user_id = $1 AND idempotency_key = $2
	`, p.UserID, p.IdempotencyKey, p.GRPCCode, p.ResponseBodyJSON, p.Now)
	return err
}

func (r *Repo) FailIdempotencyTx(ctx context.Context, tx pgx.Tx, p FinalizeIdempotencyParams) error {
	_, err := tx.Exec(ctx, `
		UPDATE orchestrator.idempotency_keys
		SET status = 'FAILED',
		    grpc_code = $3,
		    response_code = 500,
		    response_body_json = $4,
		    updated_at = $5
		WHERE user_id = $1 AND idempotency_key = $2
	`, p.UserID, p.IdempotencyKey, p.GRPCCode, p.ResponseBodyJSON, p.Now)
	return err
}
