package api

import (
	"context"
	"errors"
	"time"

	orchestratorv1 "github.com/cicconee/cbsaga/gen/orchestrator/v1"
	"github.com/cicconee/cbsaga/internal/orchestrator/app"
	"github.com/cicconee/cbsaga/internal/platform/apperr"
	"github.com/cicconee/cbsaga/internal/platform/logging"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otelcodes "go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	opCreateWithdrawal = "create_withdrawal"
	opGetWithdrawal    = "get_withdrawal"

	spanPrefix = "orchestrator."
)

var tracer = otel.Tracer("orchestrator/api")

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
	ctx, span := tracer.Start(ctx, spanPrefix+opCreateWithdrawal)
	defer span.End()

	sc := span.SpanContext()

	span.SetAttributes(
		attribute.String("user.id", req.GetUserId()),
		attribute.String("withdrawal.asset", req.GetAsset()),
		attribute.Int64("withdrawal.amount.minor", req.GetAmountMinor()),
		attribute.String("idempotency.key", req.GetIdempotencyKey()),
	)

	h.log.Info(opCreateWithdrawal+" start",
		"trace_id", sc.TraceID().String(),
		"span_id", sc.SpanID().String(),
	)

	res, err := h.svc.CreateWithdrawal(ctx, app.CreateWithdrawalParams{
		UserID:          req.GetUserId(),
		Asset:           req.GetAsset(),
		AmountMinor:     req.GetAmountMinor(),
		DestinationAddr: req.GetDestinationAddr(),
		IdempotencyKey:  req.GetIdempotencyKey(),
	})
	if err != nil {
		return nil, h.handleError(span, opCreateWithdrawal, err)
	}

	span.SetAttributes(
		attribute.String("withdrawal.id", res.WithdrawalID),
		attribute.String("withdrawal.status", res.Status),
		attribute.Bool("idempotency.replay", res.IsReplay()),
	)
	span.SetStatus(otelcodes.Ok, "")

	h.log.Info(opCreateWithdrawal+" success",
		"trace_id", sc.TraceID().String(),
		"span_id", sc.SpanID().String(),
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
	ctx, span := tracer.Start(ctx, spanPrefix+opGetWithdrawal)
	defer span.End()

	sc := span.SpanContext()

	span.SetAttributes(attribute.String("withdrawal.id", req.GetWithdrawalId()))

	h.log.Info(opGetWithdrawal+" start",
		"trace_id", sc.TraceID().String(),
		"span_id", sc.SpanID().String(),
	)

	res, err := h.svc.GetWithdrawal(ctx, app.GetWithdrawalParams{
		WithdrawalID: req.GetWithdrawalId(),
	})
	if err != nil {
		return nil, h.handleError(span, opGetWithdrawal, err)
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
		span.SetAttributes(attribute.String("withdrawal.failure_reason", *res.FailureReason))
	}

	span.SetAttributes(attribute.String("withdrawal.status", res.Status))
	span.SetStatus(otelcodes.Ok, "")

	h.log.Info(opGetWithdrawal+" success",
		"trace_id", sc.TraceID().String(),
		"span_id", sc.SpanID().String(),
	)

	return resp, nil
}

func (h *Handler) handleError(
	span trace.Span,
	op string,
	err error,
) error {
	sc := span.SpanContext()

	span.RecordError(err)
	span.SetStatus(otelcodes.Error, op+"_failed")

	var ae *apperr.Error
	if errors.As(err, &ae) {
		span.SetAttributes(
			attribute.String("error.type", "apperr"),
			attribute.String("error.code", string(ae.Code)),
			attribute.Bool("error.retryable", ae.Retryable),
		)

		h.log.Error(op+" failed",
			"trace_id", sc.TraceID().String(),
			"span_id", sc.SpanID().String(),
			"code", ae.Code,
			"retryable", ae.Retryable,
			"err", err,
		)

		return status.Error(grpcCodeFromAppCode(ae.Code), ae.Message)
	}

	h.log.Error(op+" failed (unknown error type)",
		"trace_id", sc.TraceID().String(),
		"span_id", sc.SpanID().String(),
		"err", err,
	)

	return status.Error(codes.Internal, "internal error")
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
