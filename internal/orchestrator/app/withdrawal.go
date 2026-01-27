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

	leaseAttemptID := uuid.New().String()
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

	reconcileState := func(ctx context.Context, userID, idemKey string) (CreateWithdrawalResult, error) {
		tx, err := s.db.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
		if err != nil {
			return CreateWithdrawalResult{}, fmt.Errorf("internal error: could not open tx for reconciliation: %w", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()

		idemRow, err := s.repo.GetIdempotencyTx(ctx, tx, repo.GetIdempotencyParams{
			UserID:         userID,
			IdempotencyKey: idemKey,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return CreateWithdrawalResult{}, ErrIdempotencyInProgress
			}
			return CreateWithdrawalResult{}, err
		}

		switch idemRow.Status {

		case orchestrator.IdemCompleted:
			w, err := s.repo.GetWithdrawalTx(ctx, tx, repo.GetWithdrawalParams{
				WithdrawalID: idemRow.WithdrawalID,
			})
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return CreateWithdrawalResult{}, fmt.Errorf(
						"invariant violated: idempotency COMPLETED but withdrawal missing (withdrawal_id=%s)",
						idemRow.WithdrawalID,
					)
				}
				return CreateWithdrawalResult{}, err
			}
			return CreateWithdrawalResult{
				WithdrawalID: w.WithdrawalID,
				Status:       orchestrator.WithdrawalStatusRequested,
			}, nil

		case orchestrator.IdemFailed:
			return CreateWithdrawalResult{}, fmt.Errorf("previous attempt failed (grpc_code=%d)", idemRow.GRPCCode)
		case orchestrator.IdemInProgress:
			existingWithdrawal, err := s.repo.GetWithdrawalTx(ctx, tx, repo.GetWithdrawalParams{
				WithdrawalID: idemRow.WithdrawalID,
			})
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return CreateWithdrawalResult{}, err
			}
			if err == nil {
				return CreateWithdrawalResult{
					WithdrawalID: existingWithdrawal.WithdrawalID,
					Status:       orchestrator.WithdrawalStatusRequested,
				}, nil
			}
			return CreateWithdrawalResult{}, ErrIdempotencyInProgress
		default:
			return CreateWithdrawalResult{}, fmt.Errorf("unknown idempotency status: %s", idemRow.Status)
		}
	}

	// Reserve the idempotency key
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
		LeaseAttemptID: leaseAttemptID,
		LeaseTTL:       30 * time.Second,
		Now:            now,
	})
	if err != nil {
		if errors.Is(err, repo.ErrIdempotencyKeyReuse) {
			return CreateWithdrawalResult{}, ErrInvalidIdempotencyKeyReuse
		}
		return CreateWithdrawalResult{}, err
	}
	if err := reserveTx.Commit(ctx); err != nil {
		return reconcileState(ctx, userID, idemKey)
	}

	// Reserve idempotency transaction is committed and idempotency key is reserved in db
	// but current run does not own it.
	if !idemRow.Owned {
		return reconcileState(ctx, userID, idemKey)
	}

	// begin tx that will create the withdrawal.
	withdrawalID = idemRow.WithdrawalID
	workTx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {

		fOutcome, fErr := s.failIdempotencyBestEffort(ctx, userID, idemKey, now, 13, leaseAttemptID)
		if fErr != nil || fOutcome == repo.FinalizeAlreadyFinalized {
			return reconcileState(ctx, userID, idemKey)
		}
		return CreateWithdrawalResult{}, ErrCreateWithdrawalFailed
	}
	defer func() { _ = workTx.Rollback(ctx) }()

	// Idempotency key was previously stolen from another lease owner.
	if idemRow.StoleOwnership {
		// Re-read idempotency inside workTx to avoid racing the previous lease owner’s finalization.
		// Previous owner could have successfully wrote but before changes are reflected, the
		// current tx saw it as a IN_PROGRESS expired-lease idempotency row. So re-read to ensure
		// this did not happen.
		curIdem, err := s.repo.GetIdempotencyTx(ctx, workTx, repo.GetIdempotencyParams{
			UserID:         userID,
			IdempotencyKey: idemKey,
		})
		if err != nil {
			_ = workTx.Rollback(ctx)
			if errors.Is(err, pgx.ErrNoRows) {
				return reconcileState(ctx, userID, idemKey)
			}
			return CreateWithdrawalResult{}, err
		}

		if curIdem.Status != orchestrator.IdemInProgress || curIdem.LeaseOwner != leaseAttemptID {
			_ = workTx.Rollback(ctx)
			return reconcileState(ctx, userID, idemKey)
		}
		// Continues if idempotency row is IN_PROGRESS.
	}

	// Encode payloads for the outbox_events tables.
	identityPayload, err := codec.EncodeValid(&identity.IdentityRequestCmdPayload{
		WithdrawalID: withdrawalID,
		UserID:       userID,
	})
	if err != nil {
		_ = workTx.Rollback(ctx)

		fOutcome, fErr := s.failIdempotencyBestEffort(ctx, userID, idemKey, now, 13, leaseAttemptID)
		if fErr != nil || fOutcome == repo.FinalizeAlreadyFinalized {
			return reconcileState(ctx, userID, idemKey)
		}
		return CreateWithdrawalResult{}, ErrCreateWithdrawalFailed
	}
	withdrawPayload, err := codec.EncodeValid(&orchestrator.WithdrawalRequestPayload{
		WithdrawalID: withdrawalID,
		UserID:       userID,
	})
	if err != nil {
		_ = workTx.Rollback(ctx)

		fOutcome, fErr := s.failIdempotencyBestEffort(ctx, userID, idemKey, now, 13, leaseAttemptID)
		if fErr != nil || fOutcome == repo.FinalizeAlreadyFinalized {
			return reconcileState(ctx, userID, idemKey)
		}
		return CreateWithdrawalResult{}, ErrCreateWithdrawalFailed
	}

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
		_ = workTx.Rollback(ctx)

		// If withdrawal already exists some how, reconcile, do not mark as failure.
		if errors.Is(err, repo.ErrWithdrawalAlreadyExists) {
			return reconcileState(ctx, userID, idemKey)
		}

		fOutcome, fErr := s.failIdempotencyBestEffort(ctx, userID, idemKey, now, 13, leaseAttemptID)
		if fErr != nil || fOutcome == repo.FinalizeAlreadyFinalized {
			return reconcileState(ctx, userID, idemKey)
		}
		return CreateWithdrawalResult{}, ErrCreateWithdrawalFailed
	}

	// Mark the idempotency key as completed status.
	outcome, err := s.completeIdempotency(ctx, workTx, userID, idemKey, now, 0, leaseAttemptID)
	if err != nil {
		_ = workTx.Rollback(ctx)

		if errors.Is(err, repo.ErrLostLeaseOwnership) {
			return reconcileState(ctx, userID, idemKey)
		}

		// We owned it, but couldn't finalize, therefore its a failed request.
		fOutcome, fErr := s.failIdempotencyBestEffort(ctx, userID, idemKey, now, 13, leaseAttemptID)
		if fErr != nil || fOutcome == repo.FinalizeAlreadyFinalized {
			return reconcileState(ctx, userID, idemKey)
		}
		return CreateWithdrawalResult{}, ErrCreateWithdrawalFailed
	}

	// This current run took too long from the moment of ownership till now, that
	// another run gained ownership of the lease and already finalized the withdrawal request.
	if outcome == repo.FinalizeAlreadyFinalized {
		_ = workTx.Rollback(ctx)
		return reconcileState(ctx, userID, idemKey)
	}

	// Commit the atomic transaction: finalizes idempotency key status and inserts withdrawal.
	if err := workTx.Commit(ctx); err != nil {
		// Outcome unknown
		rctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		res, rerr := reconcileState(rctx, userID, idemKey)
		if rerr == nil {
			return res, nil
		}

		if errors.Is(rerr, ErrIdempotencyInProgress) {
			return CreateWithdrawalResult{}, fmt.Errorf("commit outcome unknown; please retry")
		}

		return CreateWithdrawalResult{}, fmt.Errorf("commit outcome unknown; reconcile failed: %v; please retry", rerr)
	}

	return CreateWithdrawalResult{
		WithdrawalID: res.WithdrawalID,
		Status:       res.Status,
	}, nil
}

func (s *Service) completeIdempotency(ctx context.Context, workTx pgx.Tx, userID, idemKey string, now time.Time, grpcCode int, leaseAttemptID string) (repo.FinalizeOutcome, error) {
	return s.repo.CompleteIdempotencyTx(ctx, workTx, repo.FinalizeIdempotencyParams{
		UserID:         userID,
		IdempotencyKey: idemKey,
		GRPCCode:       grpcCode,
		Now:            now,
		LeaseAttemptID: leaseAttemptID,
	})
}

func (s *Service) failIdempotencyBestEffort(ctx context.Context, userID, idemKey string, now time.Time, grpcCode int, leaseAttemptID string) (repo.FinalizeOutcome, error) {
	// TODO: multiple retries with backoff and jitter.
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	outcome, err := s.repo.FailIdempotencyTx(ctx, tx, repo.FinalizeIdempotencyParams{
		UserID:         userID,
		IdempotencyKey: idemKey,
		GRPCCode:       grpcCode,
		Now:            now,
		LeaseAttemptID: leaseAttemptID,
	})
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return outcome, nil
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
