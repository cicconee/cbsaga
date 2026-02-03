package meta

import (
	"context"

	"github.com/google/uuid"
)

type traceIDKey struct{}

func WithTraceID(ctx context.Context, tid string) context.Context {
	if tid == "" {
		tid = uuid.New().String()
	}
	return context.WithValue(ctx, traceIDKey{}, tid)
}

func TraceID(ctx context.Context) (string, bool) {
	v := ctx.Value(traceIDKey{})
	tid, ok := v.(string)
	if !ok || tid == "" {
		return "", false
	}
	return tid, true
}

func EnsureTraceID(ctx context.Context) (context.Context, string) {
	if tid, ok := TraceID(ctx); ok {
		return ctx, tid
	}
	tid := uuid.New().String()
	return context.WithValue(ctx, traceIDKey{}, tid), tid
}
