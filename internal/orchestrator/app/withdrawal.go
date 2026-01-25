package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cicconee/cbsaga/internal/orchestrator/repo"
	"github.com/cicconee/cbsaga/internal/platform/codec"
	"github.com/cicconee/cbsaga/internal/shared/identity"
	"github.com/cicconee/cbsaga/internal/shared/orchestrator"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	db   *pgxpool.Pool
	repo *repo.Repo
}

func NewService(db *pgxpool.Pool) *Service {
	return &Service{
		db:   db,
		repo: repo.New(),
	}
}

type CreateWithdrawalParams struct {
	UserID          string
	Asset           string
	AmountMinor     int64
	DestinationAddr string
	IdempotencyKey  string
	TraceID         string
}

type CreateWithdrawalResult struct {
	WithdrawalID string
	Status       string
}

func (s *Service) CreateWithdrawal(ctx context.Context, p CreateWithdrawalParams) (CreateWithdrawalResult, error) {
	now := time.Now().UTC()

	userID := strings.TrimSpace(p.UserID)
	asset := strings.ToUpper(strings.TrimSpace(p.Asset))
	dest := strings.TrimSpace(p.DestinationAddr)
	idemKey := strings.TrimSpace(p.IdempotencyKey)

	if userID == "" || asset == "" || dest == "" || idemKey == "" {
		return CreateWithdrawalResult{}, fmt.Errorf("invalid input: missing required fields")
	}
	if p.AmountMinor <= 0 {
		return CreateWithdrawalResult{}, fmt.Errorf("invalid input: amount_minor must be > 0")
	}

	canonical := fmt.Sprintf("user_id=%s|asset=%s|amount_minor=%d|destination_addr=%s",
		userID, asset, p.AmountMinor, dest)
	sum := sha256.Sum256([]byte(canonical))
	reqHash := hex.EncodeToString(sum[:])

	withdrawalID := uuid.New().String()
	sagaID := uuid.New().String()

	identityPayload, err := codec.EncodeValid(&identity.IdentityRequestPayload{
		WithdrawalID: withdrawalID,
		UserID:       userID,
	})
	if err != nil {
		return CreateWithdrawalResult{}, err
	}

	withdrawPayload, err := codec.EncodeValid(&orchestrator.WithdrawalRequestPayload{
		WithdrawalID: withdrawalID,
		UserID:       userID,
	})
	if err != nil {
		return CreateWithdrawalResult{}, err
	}

	reserveTx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return CreateWithdrawalResult{}, err
	}
	defer func() { _ = reserveTx.Rollback(ctx) }()

	idemRow, err := s.repo.ReserveIdempotencyTx(ctx, reserveTx, repo.ReserveIdempotencyParams{
		UserID:         userID,
		IdempotencyKey: idemKey,
		RequestHash:    reqHash,
		WithdrawalID:   withdrawalID,
		Now:            now,
	})
	if err != nil {
		if errors.Is(err, repo.ErrIdempotencyKeyReuse) {
			return CreateWithdrawalResult{}, ErrInvalidIdempotencyKeyReuse
		}
		return CreateWithdrawalResult{}, err
	}

	if err := reserveTx.Commit(ctx); err != nil {
		return CreateWithdrawalResult{}, err
	}

	// TODO: I still need a stuck IN_PROGRESS strategy just incase the withdrawal tx fails followed by
	// the idempotency tx failing when updating status to FAILED.
	//
	// Also maybe make FAILED retryable? Worth a thought.
	if !idemRow.Owned {
		switch idemRow.Status {
		case orchestrator.IdemInProgress:
			return CreateWithdrawalResult{
				WithdrawalID: idemRow.WithdrawalID,
				Status:       orchestrator.WithdrawalStatusRequested,
			}, ErrIdempotencyInProgress
		case orchestrator.IdemCompleted:
			// TODO: Get the withdrawal status of the withdrawal.
			return CreateWithdrawalResult{
				WithdrawalID: idemRow.WithdrawalID,
				Status:       orchestrator.WithdrawalStatusRequested,
			}, nil
		case orchestrator.IdemFailed:
			return CreateWithdrawalResult{}, fmt.Errorf("previous attempt failed (grpc_code=%d)", idemRow.GRPCCode)
		default:
			return CreateWithdrawalResult{}, fmt.Errorf("unknown idempotency status: %s", idemRow.Status)
		}
	}

	workTx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		_ = s.failIdempotencyBestEffort(ctx, userID, idemKey, now, 13, `{"error":"begin work tx failed"}`)
		return CreateWithdrawalResult{}, err
	}
	defer func() { _ = workTx.Rollback(ctx) }()

	res, err := s.repo.CreateWithdrawalTx(ctx, workTx, repo.CreateWithdrawalParams{
		WithdrawalID:    withdrawalID,
		SagaID:          sagaID,
		UserID:          userID,
		Asset:           asset,
		AmountMinor:     p.AmountMinor,
		DestinationAddr: dest,
		TraceID:         p.TraceID,
		OutboxEvents: []repo.OutboxEvent{
			{
				EventType: orchestrator.EventTypeWithdrawalRequested,
				Payload:   string(withdrawPayload),
				RouteKey:  orchestrator.RouteKeyWithdrawalEvt,
			},
			{
				EventType: identity.EventTypeIdentityRequested,
				Payload:   string(identityPayload),
				RouteKey:  identity.RouteKeyIdentityCmd,
			},
		},
	})
	if err != nil {
		_ = s.failIdempotencyBestEffort(ctx, userID, idemKey, now, 13, `{"error":"work tx failed"}`)
		return CreateWithdrawalResult{}, err
	}

	if err := workTx.Commit(ctx); err != nil {
		_ = s.failIdempotencyBestEffort(ctx, userID, idemKey, now, 13, `{"error":"work tx commit failed"}`)
		return CreateWithdrawalResult{}, err
	}

	if err := s.completeIdempotency(ctx, userID, idemKey, now, 0, string(identityPayload)); err != nil {
		// TODO: Withdrawal is created, but idempotency still IN_PROGRESS. This must be fixed. No excuses.
		return CreateWithdrawalResult{}, err
	}

	return CreateWithdrawalResult{
		WithdrawalID: res.WithdrawalID,
		Status:       res.Status,
	}, nil
}

func (s *Service) completeIdempotency(ctx context.Context, userID, idemKey string, now time.Time, grpcCode int, body string) error {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	err = s.repo.CompleteIdempotencyTx(ctx, tx, repo.FinalizeIdempotencyParams{
		UserID:           userID,
		IdempotencyKey:   idemKey,
		GRPCCode:         grpcCode,
		ResponseBodyJSON: body,
		Now:              now,
	})
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Service) failIdempotencyBestEffort(ctx context.Context, userID, idemKey string, now time.Time, grpcCode int, body string) error {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	err = s.repo.FailIdempotencyTx(ctx, tx, repo.FinalizeIdempotencyParams{
		UserID:           userID,
		IdempotencyKey:   idemKey,
		GRPCCode:         grpcCode,
		ResponseBodyJSON: body,
		Now:              now,
	})
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
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

func (s *Service) GetWithdrawal(ctx context.Context, p GetWithdrawalParams) (GetWithdrawalResult, error) {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return GetWithdrawalResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	row, err := s.repo.GetWithdrawalTx(ctx, tx, repo.GetWithdrawalParams{WithdrawalID: p.WithdrawalID})
	if err != nil {
		return GetWithdrawalResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return GetWithdrawalResult{}, err
	}

	return GetWithdrawalResult{
		WithdrawalID:    row.WithdrawalID,
		UserID:          row.UserID,
		Asset:           row.Asset,
		AmountMinor:     row.AmountMinor,
		DestinationAddr: row.DestinationAddr,
		Status:          row.Status,
		FailureReason:   row.FailureReason,
		CreatedAt:       row.CreatedAt,
		UpdatedAt:       row.UpdatedAt,
	}, nil
}
