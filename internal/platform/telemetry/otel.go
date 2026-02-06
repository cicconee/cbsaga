package telemetry

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
)

type Config struct {
	Enabled     bool
	ServiceName string
	Endpoint    string
	Insecure    bool
	SampleRatio float64
}

func Init(ctx context.Context, cfg Config) (shutdown func(context.Context) error, err error) {
	if !cfg.Enabled {
		otel.SetTracerProvider(
			trace.NewTracerProvider(trace.WithSampler(trace.NeverSample())),
		)
		return func(context.Context) error { return nil }, nil
	}

	opts := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint(cfg.Endpoint),
	}
	if cfg.Insecure {
		opts = append(opts, otlptracegrpc.WithInsecure())
	}
	exp, err := otlptracegrpc.New(ctx, opts...)
	if err != nil {
		return nil, err
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(cfg.ServiceName),
		),
	)
	if err != nil {
		return nil, err
	}

	var sampler trace.Sampler
	switch {
	case cfg.SampleRatio <= 0:
		sampler = trace.NeverSample()
	case cfg.SampleRatio >= 1:
		sampler = trace.AlwaysSample()
	default:
		sampler = trace.TraceIDRatioBased(cfg.SampleRatio)
	}

	tp := trace.NewTracerProvider(
		trace.WithBatcher(exp),
		trace.WithResource(res),
		trace.WithSampler(sampler),
	)

	otel.SetTracerProvider(tp)

	return func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		return tp.Shutdown(ctx)
	}, nil
}
