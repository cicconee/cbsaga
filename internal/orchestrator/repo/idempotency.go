package repo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cicconee/cbsaga/internal/shared/orchestrator"
	"github.com/jackc/pgx/v5"
)

var (
	ErrIdempotencyKeyReuse = errors.New("idempotency key reuse with different request")
	ErrLostLeaseOwnership  = errors.New("not the lease owner")
)

type ReserveIdempotencyParams struct {
	UserID         string
	IdempotencyKey string
	RequestHash    string
	WithdrawalID   string
	LeaseAttemptID string
	LeaseTTL       time.Duration
	Now            time.Time
}

type ReserveIdempotencyResult struct {
	Owned          bool
	StoleOwnership bool
	Status         string
	WithdrawalID   string
	RequestHash    string
	GRPCCode       int
	LeaseOwner     string
	LeaseExpiresAt time.Time
	LeaseFence     int64
}

func (r *Repo) ReserveIdempotencyTx(ctx context.Context, tx pgx.Tx, p ReserveIdempotencyParams) (ReserveIdempotencyResult, error) {
	var inserted bool

	err := tx.QueryRow(ctx, `
		INSERT INTO orchestrator.idempotency_keys (
			id,
			user_id,
			idempotency_key,
			withdrawal_id,
			request_hash,
			response_code,
			response_body_json, 
			status,
			grpc_code,
			updated_at,
			lease_owner,
			lease_expires_at,
			lease_fence
		)
		VALUES (
			gen_random_uuid(),
			$1, 
			$2,
			$3,
			$4,
			0,
			'{}',
			$5,
			0,
			$6,
			$7,
			$8,
			1
		)
		ON CONFLICT (user_id, idempotency_key) DO NOTHING
		RETURNING true
	`,
		p.UserID,
		p.IdempotencyKey,
		p.WithdrawalID,
		p.RequestHash,
		orchestrator.IdemInProgress,
		p.Now,
		p.LeaseAttemptID,
		p.Now.Add(p.LeaseTTL),
	).Scan(&inserted)

	if err != nil && err != pgx.ErrNoRows {
		return ReserveIdempotencyResult{}, err
	}

	if inserted {
		return ReserveIdempotencyResult{
			Owned:          true,
			Status:         orchestrator.IdemInProgress,
			WithdrawalID:   p.WithdrawalID,
			RequestHash:    p.RequestHash,
			LeaseOwner:     p.LeaseAttemptID,
			LeaseExpiresAt: p.Now.Add(p.LeaseTTL),
			LeaseFence:     1,
		}, nil
	}

	var status string
	var withdrawalID string
	var requestHash string
	var grpcCode int
	var respBody string
	var leaseOwner string
	var leaseExpiresAt time.Time
	var leaseFence int64

	err = tx.QueryRow(ctx, `
		SELECT 
			status,
			withdrawal_id,
			request_hash,
			grpc_code, 
			response_body_json,
			lease_owner,
			lease_expires_at,
			lease_fence
		FROM orchestrator.idempotency_keys
		WHERE
			user_id = $1
			AND idempotency_key = $2
	`,
		p.UserID,
		p.IdempotencyKey,
	).Scan(
		&status,
		&withdrawalID,
		&requestHash,
		&grpcCode,
		&respBody,
		&leaseOwner,
		&leaseExpiresAt,
		&leaseFence,
	)
	if err != nil {
		return ReserveIdempotencyResult{}, err
	}

	if requestHash != p.RequestHash {
		return ReserveIdempotencyResult{}, ErrIdempotencyKeyReuse
	}

	if status == orchestrator.IdemInProgress && !leaseExpiresAt.After(p.Now) {
		var newLeaseFence int64
		var newWithdrawalID string
		var newRequestHash string
		var newLeaseExpiresAt time.Time

		err = tx.QueryRow(ctx, `
			UPDATE orchestrator.idempotency_keys
			SET 
				lease_owner = $4,
				lease_expires_at = $5,
				updated_at = $6,
				lease_fence = lease_fence + 1
			WHERE 
				user_id = $1
				AND idempotency_key = $2
				AND status = $3
				AND lease_expires_at <= $6
			RETURNING 
				lease_fence,
				withdrawal_id,
				request_hash,
				lease_expires_at
		`,
			p.UserID,
			p.IdempotencyKey,
			orchestrator.IdemInProgress,
			p.LeaseAttemptID,
			p.Now.Add(p.LeaseTTL),
			p.Now,
		).Scan(
			&newLeaseFence,
			&newWithdrawalID,
			&newRequestHash,
			&newLeaseExpiresAt,
		)

		if err == nil {
			return ReserveIdempotencyResult{
				Owned:          true,
				StoleOwnership: true,
				Status:         orchestrator.IdemInProgress,
				WithdrawalID:   newWithdrawalID,
				RequestHash:    newRequestHash,
				LeaseOwner:     p.LeaseAttemptID,
				LeaseExpiresAt: p.Now.Add(p.LeaseTTL),
				LeaseFence:     newLeaseFence,
			}, nil
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return ReserveIdempotencyResult{}, err
		}
		// If update didn't take effect (race), fall through.
	}

	return ReserveIdempotencyResult{
		Owned:          false,
		Status:         status,
		WithdrawalID:   withdrawalID,
		RequestHash:    requestHash,
		GRPCCode:       grpcCode,
		LeaseOwner:     leaseOwner,
		LeaseExpiresAt: leaseExpiresAt,
		LeaseFence:     leaseFence,
	}, nil
}

type FinalizeIdempotencyParams struct {
	UserID         string
	IdempotencyKey string
	GRPCCode       int
	Now            time.Time
	LeaseAttemptID string
	LeaseFence     int64
}

type FinalizeOutcome int

const (
	FinalizeApplied          FinalizeOutcome = iota // tx applied the status change
	FinalizeAlreadyFinalized                        // tx found the status change already existed
)

type idemState struct {
	Status         string
	LeaseOwner     string
	LeaseExpiresAt time.Time
	LeaseFence     int64
}

func (r *Repo) readIdemStateTx(ctx context.Context, tx pgx.Tx, userID, idemKey string) (idemState, error) {
	var s idemState
	err := tx.QueryRow(ctx, `
		SELECT
			status,
			lease_owner,
			lease_expires_at,
			lease_fence
		FROM orchestrator.idempotency_keys
		WHERE
			user_id = $1
			AND idempotency_key = $2
	`,
		userID,
		idemKey,
	).Scan(
		&s.Status,
		&s.LeaseOwner,
		&s.LeaseExpiresAt,
		&s.LeaseFence,
	)
	if err != nil {
		return idemState{}, err
	}
	return s, nil
}

func (r *Repo) CompleteIdempotencyTx(ctx context.Context, tx pgx.Tx, p FinalizeIdempotencyParams) (FinalizeOutcome, error) {
	tag, err := tx.Exec(ctx, `
		UPDATE orchestrator.idempotency_keys
		SET
			status = 'COMPLETED',
			grpc_code = $3,
			response_code = 200,
			updated_at = $4
		WHERE
			user_id = $1
			AND idempotency_key = $2
			AND lease_owner = $5
			AND status = $6
			AND lease_fence = $7
	`,
		p.UserID,
		p.IdempotencyKey,
		p.GRPCCode,
		p.Now,
		p.LeaseAttemptID,
		orchestrator.IdemInProgress,
		p.LeaseFence,
	)
	if err != nil {
		return 0, err
	}
	if tag.RowsAffected() == 1 {
		return FinalizeApplied, nil
	}

	// classify miss
	s, err := r.readIdemStateTx(ctx, tx, p.UserID, p.IdempotencyKey)
	if err != nil {
		return 0, err
	}
	if s.Status == orchestrator.IdemCompleted || s.Status == orchestrator.IdemFailed {
		return FinalizeAlreadyFinalized, nil
	}
	return 0, ErrLostLeaseOwnership
}

func (r *Repo) FailIdempotencyTx(ctx context.Context, tx pgx.Tx, p FinalizeIdempotencyParams) (FinalizeOutcome, error) {
	tag, err := tx.Exec(ctx, `
		UPDATE orchestrator.idempotency_keys
		SET
			status = 'FAILED',
			grpc_code = $3,
			response_code = 500,
			updated_at = $4
		WHERE
			user_id = $1
			AND idempotency_key = $2
			AND lease_owner = $5
			AND status = $6
			AND lease_fence = $7 
	`,
		p.UserID,
		p.IdempotencyKey,
		p.GRPCCode,
		p.Now,
		p.LeaseAttemptID,
		orchestrator.IdemInProgress,
		p.LeaseFence,
	)
	if err != nil {
		return 0, err
	}
	if tag.RowsAffected() == 1 {
		return FinalizeApplied, nil
	}

	s, err := r.readIdemStateTx(ctx, tx, p.UserID, p.IdempotencyKey)
	if err != nil {
		return 0, err
	}
	if s.Status == orchestrator.IdemCompleted || s.Status == orchestrator.IdemFailed {
		return FinalizeAlreadyFinalized, nil
	}
	return 0, ErrLostLeaseOwnership
}

type GetIdempotencyParams struct {
	UserID         string
	IdempotencyKey string
}

type GetIdempotencyResult struct {
	Status         string
	WithdrawalID   string
	RequestHash    string
	GRPCCode       int
	LeaseOwner     string
	LeaseExpiresAt time.Time
}

func (r *Repo) GetIdempotencyTx(ctx context.Context, tx pgx.Tx, p GetIdempotencyParams) (GetIdempotencyResult, error) {
	row := GetIdempotencyResult{}

	err := tx.QueryRow(ctx, `
		SELECT
			status,
			withdrawal_id,
			request_hash,
			grpc_code,
			lease_owner,
			lease_expires_at
		FROM orchestrator.idempotency_keys
		WHERE
			user_id = $1
			AND idempotency_key = $2
	`,
		p.UserID,
		p.IdempotencyKey,
	).Scan(
		&row.Status,
		&row.WithdrawalID,
		&row.RequestHash,
		&row.GRPCCode,
		&row.LeaseOwner,
		&row.LeaseExpiresAt,
	)
	if err != nil {
		return GetIdempotencyResult{}, fmt.Errorf("repo.GetIdempotency: %w", err)
	}

	return row, nil
}
