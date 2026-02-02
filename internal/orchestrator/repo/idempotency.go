package repo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cicconee/cbsaga/internal/orchestrator/domain"
	"github.com/cicconee/cbsaga/internal/platform/apperr"
	"github.com/cicconee/cbsaga/internal/platform/db/postgres"
	"github.com/jackc/pgx/v5"
)

var (
	ErrIdempotencyKeyReuse = errors.New("idempotency key reuse with different request")
	ErrLostLeaseOwnership  = errors.New("not the lease owner")
)

type ReserveIdemParams struct {
	UserID         string
	IdempotencyKey string
	RequestHash    string
	WithdrawalID   string
	LeaseAttemptID string
	LeaseTTL       time.Duration
	Now            time.Time
}

type ReserveIdemResult struct {
	Owned          bool
	StoleOwnership bool
	Status         string
	WithdrawalID   string
	RequestHash    string
	GRPCCode       int
	ResponseBody   *string
	AppErrorCode   *apperr.Code
	ErrorMessage   *string
	LeaseOwner     string
	LeaseExpiresAt time.Time
	LeaseFence     int64
}

func (r *Repo) ReserveIdemTx(
	ctx context.Context,
	tx pgx.Tx,
	p ReserveIdemParams,
) (ReserveIdemResult, error) {
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
			app_error_code,
			error_message,
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
			NULL,
			NULL,
			NULL,
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
		domain.IdemInProgress,
		p.Now,
		p.LeaseAttemptID,
		p.Now.Add(p.LeaseTTL),
	).Scan(&inserted)

	if err != nil && err != pgx.ErrNoRows {
		return ReserveIdemResult{}, err
	}

	if inserted {
		return ReserveIdemResult{
			Owned:          true,
			Status:         domain.IdemInProgress,
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
	var respBody *string
	var appErrorCode *apperr.Code
	var errorMessage *string
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
			app_error_code,
			error_message,
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
		&appErrorCode,
		&errorMessage,
		&leaseOwner,
		&leaseExpiresAt,
		&leaseFence,
	)
	if err != nil {
		return ReserveIdemResult{}, err
	}

	if requestHash != p.RequestHash {
		return ReserveIdemResult{}, ErrIdempotencyKeyReuse
	}

	if status == domain.IdemInProgress && !leaseExpiresAt.After(p.Now) {
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
			domain.IdemInProgress,
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
			return ReserveIdemResult{
				Owned:          true,
				StoleOwnership: true,
				Status:         domain.IdemInProgress,
				WithdrawalID:   newWithdrawalID,
				RequestHash:    newRequestHash,
				ResponseBody:   respBody,
				AppErrorCode:   appErrorCode,
				ErrorMessage:   errorMessage,
				LeaseOwner:     p.LeaseAttemptID,
				LeaseExpiresAt: p.Now.Add(p.LeaseTTL),
				LeaseFence:     newLeaseFence,
			}, nil
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return ReserveIdemResult{}, err
		}
		// If update didn't take effect (race), fall through.
	}

	return ReserveIdemResult{
		Owned:          false,
		Status:         status,
		WithdrawalID:   withdrawalID,
		RequestHash:    requestHash,
		ResponseBody:   respBody,
		AppErrorCode:   appErrorCode,
		ErrorMessage:   errorMessage,
		GRPCCode:       grpcCode,
		LeaseOwner:     leaseOwner,
		LeaseExpiresAt: leaseExpiresAt,
		LeaseFence:     leaseFence,
	}, nil
}

type GetIdemParams struct {
	UserID         string
	IdempotencyKey string
}

type GetIdemResult struct {
	Status         string
	WithdrawalID   string
	RequestHash    string
	ResponseBody   *string
	AppErrorCode   *apperr.Code
	ErrorMessage   *string
	GRPCCode       int
	LeaseOwner     string
	LeaseExpiresAt time.Time
}

func (r *Repo) GetIdem(
	ctx context.Context,
	db postgres.DBTX,
	p GetIdemParams,
) (GetIdemResult, error) {
	row := GetIdemResult{}

	err := db.QueryRow(ctx, `
		SELECT
			status,
			withdrawal_id,
			request_hash,
			response_body_json,
			app_error_code,
			error_message,
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
		&row.ResponseBody,
		&row.AppErrorCode,
		&row.ErrorMessage,
		&row.GRPCCode,
		&row.LeaseOwner,
		&row.LeaseExpiresAt,
	)
	if err != nil {
		return GetIdemResult{}, fmt.Errorf("repo.GetIdempotency: %w", err)
	}

	return row, nil
}

type IdemState struct {
	Status         string
	LeaseOwner     string
	LeaseExpiresAt time.Time
	LeaseFence     int64
}

func (r *Repo) ReadIdemStateTx(
	ctx context.Context,
	tx pgx.Tx,
	userID string,
	idemKey string,
) (IdemState, error) {
	var s IdemState
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
		return IdemState{}, err
	}
	return s, nil
}

type FinalizeIdemParams struct {
	UserID         string
	IdempotencyKey string
	GRPCCode       int
	Now            time.Time
	ResponseBody   *string
	AppErrorCode   *apperr.Code
	ErrorMessage   *string
	LeaseAttemptID string
	LeaseFence     int64
	Status         string
}

type FinalizeOutcome int

const (
	FinalizeApplied          FinalizeOutcome = iota // tx applied the status change
	FinalizeAlreadyFinalized                        // tx found the status change already existed
)

func (r *Repo) FinalizeIdemTx(
	ctx context.Context,
	tx pgx.Tx,
	p FinalizeIdemParams,
) (FinalizeOutcome, error) {
	tag, err := tx.Exec(ctx, `
		UPDATE orchestrator.idempotency_keys
		SET
			status = $1,
			grpc_code = $2,
			response_code = 200,
			response_body_json = $9,
			app_error_code = $10,
			error_message = $11,
			updated_at = $3
		WHERE
			user_id = $4
			AND idempotency_key = $5
			AND lease_owner = $6
			AND status = $7
			AND lease_fence = $8`,
		p.Status,
		p.GRPCCode,
		p.Now,
		p.UserID,
		p.IdempotencyKey,
		p.LeaseAttemptID,
		domain.IdemInProgress,
		p.LeaseFence,
		p.ResponseBody,
		p.AppErrorCode,
		p.ErrorMessage,
	)
	if err != nil {
		return 0, err
	}
	if tag.RowsAffected() == 1 {
		return FinalizeApplied, nil
	}

	// classify miss
	s, err := r.ReadIdemStateTx(ctx, tx, p.UserID, p.IdempotencyKey)
	if err != nil {
		return 0, err
	}
	if s.Status == domain.IdemCompleted || s.Status == domain.IdemFailed {
		return FinalizeAlreadyFinalized, nil
	}
	return 0, ErrLostLeaseOwnership
}
