package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cicconee/cbsaga/internal/orchestrator/repo"
	"github.com/cicconee/cbsaga/internal/platform/codec"
	"github.com/cicconee/cbsaga/internal/platform/db/postgres"
	"github.com/cicconee/cbsaga/internal/platform/retry"
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

func (s *Service) CreateWithdrawal(
	ctx context.Context,
	p CreateWithdrawalParams,
) (CreateWithdrawalResult, error) {
	now := time.Now().UTC()

	v, err := NewValidatedCreateWithdrawal(p)
	if err != nil {
		return CreateWithdrawalResult{}, err
	}

	// Reserve the idempotency key
	reserveTx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return CreateWithdrawalResult{}, err
	}
	defer func() { _ = reserveTx.Rollback(ctx) }()

	idemRow, err := s.repo.ReserveIdemTx(ctx, reserveTx, repo.ReserveIdemParams{
		UserID:         v.UserID,
		IdempotencyKey: v.IdempotencyKey,
		RequestHash:    v.RequestHash,
		WithdrawalID:   uuid.NewString(),
		LeaseAttemptID: uuid.NewString(),
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
		return s.reconcile(ctx, v.UserID, v.IdempotencyKey)
	}

	// Reserve idempotency transaction is committed and idempotency key is reserved in db
	// but current run does not own it.
	if !idemRow.Owned {
		return s.reconcile(ctx, v.UserID, v.IdempotencyKey)
	}

	// begin tx that will create the withdrawal.
	finalParams := finalizeIdemParams{
		userID:         v.UserID,
		idemKey:        v.IdempotencyKey,
		now:            now,
		leaseAttemptID: idemRow.LeaseOwner,
		leaseFence:     idemRow.LeaseFence,
	}

	workTx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return s.failAndReconcile(ctx, 13, finalParams)
	}
	defer func() { _ = workTx.Rollback(ctx) }()

	// Encode payloads for the outbox_events tables.
	identityPayload, err := codec.EncodeValid(&identity.IdentityRequestCmdPayload{
		WithdrawalID: idemRow.WithdrawalID,
		UserID:       v.UserID,
	})
	if err != nil {
		_ = workTx.Rollback(ctx)
		return s.failAndReconcile(ctx, 13, finalParams)
	}
	withdrawPayload, err := codec.EncodeValid(&orchestrator.WithdrawalRequestPayload{
		WithdrawalID: idemRow.WithdrawalID,
		UserID:       v.UserID,
	})
	if err != nil {
		_ = workTx.Rollback(ctx)
		return s.failAndReconcile(ctx, 13, finalParams)
	}

	res, err := s.repo.CreateWithdrawalTx(ctx, workTx, repo.CreateWithdrawalParams{
		WithdrawalID:    idemRow.WithdrawalID,
		SagaID:          uuid.NewString(),
		UserID:          v.UserID,
		Asset:           v.Asset,
		AmountMinor:     p.AmountMinor,
		DestinationAddr: v.DestinationAddr,
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
			return s.reconcile(ctx, v.UserID, v.IdempotencyKey)
		}
		return s.failAndReconcile(ctx, 13, finalParams)
	}

	// Mark the idempotency key as completed status.
	outcome, err := s.completeIdempotency(ctx, workTx, 0, finalParams)
	if err != nil {
		_ = workTx.Rollback(ctx)
		if errors.Is(err, repo.ErrLostLeaseOwnership) {
			return s.reconcile(ctx, v.UserID, v.IdempotencyKey)
		}
		return s.failAndReconcile(ctx, 13, finalParams)
	}

	// This current run took too long from the moment of ownership till now, that
	// another run gained ownership of the lease and already finalized the withdrawal request.
	if outcome == repo.FinalizeAlreadyFinalized {
		_ = workTx.Rollback(ctx)
		return s.reconcile(ctx, v.UserID, v.IdempotencyKey)
	}

	// Commit the atomic transaction: finalizes idempotency key status and inserts withdrawal.
	if err := workTx.Commit(ctx); err != nil {
		// Outcome unknown
		rctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		res, rerr := s.reconcile(rctx, v.UserID, v.IdempotencyKey)
		if rerr == nil {
			return res, nil
		}

		if errors.Is(rerr, ErrIdempotencyInProgress) {
			return CreateWithdrawalResult{}, fmt.Errorf("commit outcome unknown; please retry")
		}

		return CreateWithdrawalResult{}, fmt.Errorf(
			"commit outcome unknown; reconcile failed: %v; please retry",
			rerr,
		)
	}

	return CreateWithdrawalResult{
		WithdrawalID: res.WithdrawalID,
		Status:       res.Status,
	}, nil
}

type finalizeIdemParams struct {
	userID         string
	idemKey        string
	now            time.Time
	leaseAttemptID string
	leaseFence     int64
}

func (s *Service) completeIdempotency(
	ctx context.Context,
	workTx pgx.Tx,
	grpcCode int,
	p finalizeIdemParams,
) (repo.FinalizeOutcome, error) {
	return s.repo.FinalizeIdemTx(ctx, workTx, repo.FinalizeIdemParams{
		UserID:         p.userID,
		IdempotencyKey: p.idemKey,
		GRPCCode:       grpcCode,
		Now:            p.now,
		LeaseAttemptID: p.leaseAttemptID,
		LeaseFence:     p.leaseFence,
		Status:         orchestrator.IdemCompleted,
	})
}

func (s *Service) failAndReconcile(
	ctx context.Context,
	grpcCode int,
	p finalizeIdemParams,
) (CreateWithdrawalResult, error) {
	fOutcome, fErr := s.failIdempotencyWithRetry(ctx, grpcCode, p)
	if fErr != nil || fOutcome == repo.FinalizeAlreadyFinalized {
		return s.reconcile(ctx, p.userID, p.idemKey)
	}
	return CreateWithdrawalResult{}, ErrCreateWithdrawalFailed
}

func (s *Service) failIdempotencyWithRetry(
	ctx context.Context,
	grpcCode int,
	p finalizeIdemParams,
) (repo.FinalizeOutcome, error) {
	var outcome repo.FinalizeOutcome

	finalize := func() error {
		var o repo.FinalizeOutcome

		err := postgres.WithTx(ctx, s.db, pgx.TxOptions{}, "idempotency/set_failed",
			func(ctx context.Context, tx pgx.Tx) error {
				got, err := s.repo.FinalizeIdemTx(ctx, tx, repo.FinalizeIdemParams{
					UserID:         p.userID,
					IdempotencyKey: p.idemKey,
					GRPCCode:       grpcCode,
					Now:            p.now,
					LeaseAttemptID: p.leaseAttemptID,
					LeaseFence:     p.leaseFence,
					Status:         orchestrator.IdemFailed,
				})
				if err != nil {
					return err
				}

				o = got
				return nil
			})
		if err != nil {
			return err
		}

		outcome = o
		return nil
	}

	if err := retry.Do(ctx, failIdemRetryPolicy(), finalize); err != nil {
		return 0, err
	}

	return outcome, nil
}

func (s *Service) reconcile(
	ctx context.Context,
	userID, idemKey string,
) (CreateWithdrawalResult, error) {
	idemRow, err := s.repo.GetIdem(ctx, s.db, repo.GetIdemParams{
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
		w, err := s.repo.GetWithdrawal(ctx, s.db, repo.GetWithdrawalParams{
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
		return CreateWithdrawalResult{}, fmt.Errorf(
			"previous attempt failed (grpc_code=%d)",
			idemRow.GRPCCode,
		)
	case orchestrator.IdemInProgress:
		existingWithdrawal, err := s.repo.GetWithdrawal(ctx, s.db, repo.GetWithdrawalParams{
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
		return CreateWithdrawalResult{}, fmt.Errorf(
			"unknown idempotency status: %s",
			idemRow.Status,
		)
	}
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

func (s *Service) GetWithdrawal(
	ctx context.Context,
	p GetWithdrawalParams,
) (GetWithdrawalResult, error) {
	row, err := s.repo.GetWithdrawal(
		ctx,
		s.db,
		repo.GetWithdrawalParams{WithdrawalID: p.WithdrawalID},
	)
	if err != nil {
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
