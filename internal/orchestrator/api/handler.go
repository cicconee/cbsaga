package api

import (
	"context"
	"errors"
	"time"

	orchestratorv1 "github.com/cicconee/cbsaga/gen/orchestrator/v1"
	"github.com/cicconee/cbsaga/internal/orchestrator/app"
	"github.com/cicconee/cbsaga/internal/platform/apperr"
	"github.com/cicconee/cbsaga/internal/platform/logging"
	"github.com/cicconee/cbsaga/internal/platform/meta"
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

func (h *Handler) CreateWithdrawal(
	ctx context.Context,
	req *orchestratorv1.CreateWithdrawalRequest,
) (*orchestratorv1.CreateWithdrawalResponse, error) {
	ctx, traceID := meta.EnsureTraceID(ctx)

	h.log.Info("CreateWithdrawal called",
		"trace_id", traceID,
		"user_id", req.GetUserId(),
		"asset", req.GetAsset(),
		"amount_miner", req.GetAmountMinor(),
		"destination_addr", req.GetDestinationAddr(),
		"idempotency_key", req.GetIdempotencyKey(),
	)

	res, err := h.svc.CreateWithdrawal(ctx, app.CreateWithdrawalParams{
		UserID:          req.GetUserId(),
		Asset:           req.GetAsset(),
		AmountMinor:     req.GetAmountMinor(),
		DestinationAddr: req.GetDestinationAddr(),
		IdempotencyKey:  req.GetIdempotencyKey(),
	})
	if err != nil {
		var ae *apperr.Error
		if errors.As(err, &ae) {
			args := []any{"trace_id", traceID}
			args = append(args, ae.LogArgs())
			h.log.Error("CreateWithdrawal failed", args...)
			return nil, status.Error(grpcCodeFromAppCode(ae.Code), ae.Message)
		}

		h.log.Error("CreateWithdrawal failed (unknown error type)",
			"trace_id", traceID,
			"err", err,
		)
		return nil, status.Error(codes.Internal, "internal error")
	}

	h.log.Info("CreateWithdrawal success",
		"trace_id", traceID,
		"withdrawal_id", res.WithdrawalID,
		"status", res.Status,
		"replay", res.IsReplay(),
	)

	return &orchestratorv1.CreateWithdrawalResponse{
		WithdrawalId: res.WithdrawalID,
		Status:       res.Status,
	}, nil
}

func (h *Handler) GetWithdrawal(
	ctx context.Context,
	req *orchestratorv1.GetWithdrawalRequest,
) (*orchestratorv1.GetWithdrawalResponse, error) {
	ctx, traceID := meta.EnsureTraceID(ctx)

	h.log.Info("GetWithdrawal called",
		"trace_id", traceID,
		"withdrawal_id", req.GetWithdrawalId(),
	)

	res, err := h.svc.GetWithdrawal(ctx, app.GetWithdrawalParams{
		WithdrawalID: req.GetWithdrawalId(),
	})
	if err != nil {
		var ae *apperr.Error
		if errors.As(err, &ae) {
			args := []any{"trace_id", traceID}
			args = append(args, ae.LogArgs())
			h.log.Error("GetWithdrawal failed", args...)
			return nil, status.Error(grpcCodeFromAppCode(ae.Code), ae.Message)
		}

		h.log.Error("GetWithdrawal failed (unknown error type)",
			"trace_id", traceID,
			"err", err,
		)
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

func grpcCodeFromAppCode(code apperr.Code) codes.Code {
	switch code {
	case apperr.CodeInvalidArgument:
		return codes.InvalidArgument

	case apperr.CodeRetryableConflict:
		return codes.Aborted

	case apperr.CodeFailed, apperr.CodeInternal:
		return codes.Internal

	default:
		return codes.Internal
	}
}
