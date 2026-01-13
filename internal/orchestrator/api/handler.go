package api

import (
	"context"
	"errors"
	"time"

	orchestratorv1 "github.com/cicconee/cbsaga/gen/orchestrator/v1"
	"github.com/cicconee/cbsaga/internal/orchestrator/app"
	"github.com/cicconee/cbsaga/internal/platform/logging"
	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Handler struct {
	orchestratorv1.UnimplementedOrchestratorServiceServer
	svc *app.Service
	log *logging.Logger
}

func NewHandler(svc *app.Service, log *logging.Logger) *Handler {
	return &Handler{svc: svc, log: log}
}

func (h *Handler) CreateWithdrawal(ctx context.Context, req *orchestratorv1.CreateWithdrawalRequest) (*orchestratorv1.CreateWithdrawalResponse, error) {
	h.log.Info("CreateWithdrawal called",
		"user_id", req.GetUserId(),
		"asset", req.GetAsset(),
		"amount_miner", req.GetAmountMinor(),
		"idem", req.GetIdempotencyKey(),
	)

	res, err := h.svc.CreateWithdrawal(ctx, app.CreateWithdrawalParams{
		UserID:          req.GetUserId(),
		Asset:           req.GetAsset(),
		AmountMinor:     req.GetAmountMinor(),
		DestinationAddr: req.GetDestinationAddr(),
		IdempotencyKey:  req.GetIdempotencyKey(),
		TraceID:         "local-trace-id",
	})
	if err != nil {
		switch {
		case errors.Is(err, app.ErrInvalidIdempotencyKeyReuse):
			h.log.Error("CreateWithdrawal failed: idempotency key reuse", "err", err)
			return nil, status.Error(codes.FailedPrecondition, "idempotency_key already used for a different request")

		case errors.Is(err, app.ErrIdempotencyInProgress):
			h.log.Info("CreateWithdrawal in progress replay", "withdrawal_id", res.WithdrawalID)
			return nil, status.Error(codes.Aborted, "request in progress; retry later; withdrawal_id="+res.WithdrawalID)

		default:
			h.log.Error("CreateWithdrawal failed", "err", err)
			return nil, status.Error(codes.Internal, "internal error")
		}
	}

	h.log.Info("CreateWithdrawal success",
		"withdrawal_id", res.WithdrawalID,
		"status", res.Status,
	)

	return &orchestratorv1.CreateWithdrawalResponse{
		WithdrawalId: res.WithdrawalID,
		Status:       res.Status,
	}, nil
}

func (h *Handler) GetWithdrawal(ctx context.Context, req *orchestratorv1.GetWithdrawalRequest) (*orchestratorv1.GetWithdrawalResponse, error) {
	h.log.Info("GetWithdrawal called", "withdrawal_id", req.GetWithdrawalId())

	res, err := h.svc.GetWithdrawal(ctx, app.GetWithdrawalParams{
		WithdrawalID: req.GetWithdrawalId(),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, status.Error(codes.NotFound, "withdrawal not found")
		}
		h.log.Error("GetWithdrawal failed", "err", err)
		return nil, status.Error(codes.Internal, "internal error")
	}

	resp := &orchestratorv1.GetWithdrawalResponse{
		WithdrawalId:    res.WithdrawalID,
		UserId:          res.UserID,
		Asset:           res.Asset,
		AmountMinor:     res.AmountMinor,
		DestinationAddr: res.DestinationAddr,
		Status:          res.Status,
		CreatedAt:       res.CreatedAt.Format(time.RFC3339Nano),
		UpdatedAt:       res.UpdatedAt.Format(time.RFC3339Nano),
	}
	if res.FailureReason != nil {
		resp.FailureReason = *res.FailureReason
	}

	return resp, nil
}
