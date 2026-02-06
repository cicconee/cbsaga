package config

import (
	"fmt"
	"time"

	"github.com/cicconee/cbsaga/internal/platform/config"
	"github.com/cicconee/cbsaga/internal/platform/telemetry"
)

type OrchestratorConfig struct {
	Env                 string
	GRPCAddr            string
	ShutdownTimeout     time.Duration
	PostgresDSN         string
	KafkaBrokers        []string
	IdentityEvtTopic    string
	OrchestratorGroupID string

	OTel telemetry.Config
}

func Load() (OrchestratorConfig, error) {
	cfg := OrchestratorConfig{
		Env:             config.GetEnv("CBSAGA_ENV", "dev"),
		GRPCAddr:        config.GetEnv("CBSAGA_ORCH_GRPC_ADDR", ":9000"),
		ShutdownTimeout: config.GetEnvDuration("CBSAGA_SHUTDOWN_TIMEOUT", 10*time.Second),
		PostgresDSN: config.GetEnv(
			"CBSAGA_ORCH_POSTGRES_DSN",
			"postgres://postgres:postgres@localhost:5432/orchestrator?sslmode=disable",
		),
		KafkaBrokers: config.SplitCSV(
			config.GetEnv("CBSAGA_KAFKA_BROKERS", "localhost:9092"),
		),
		IdentityEvtTopic:    config.GetEnv("CBSAGA_ORCH_IDENTITY_TOPIC", "cbsaga.evt.identity"),
		OrchestratorGroupID: config.GetEnv("CBSAGA_ORCH_GROUP_ID", "cbsaga-orchestrator"),

		OTel: telemetry.Config{
			Enabled:     config.GetEnvBool("CBSAGA_OTEL_ENABLED", false),
			ServiceName: config.GetEnv("CBSAGA_OTEL_SERVICE_NAME", "cbsaga-orchestrator"),
			Endpoint:    config.GetEnv("CBSAGA_OTEL_EXPORTER_OTLP_ENDPOINT", "localhost:4317"),
			Insecure:    config.GetEnvBool("CBSAGA_OTEL_EXPORTER_OTLP_INSECURE", true),
			SampleRatio: config.GetEnvFloat("CBSAGA_OTEL_TRACES_SAMPLE_RATIO", 1.0),
		},
	}

	if cfg.GRPCAddr == "" {
		return OrchestratorConfig{}, fmt.Errorf("CBSAGA_ORCH_GRPC_ADDR cannot be empty")
	}

	if cfg.OTel.Enabled && cfg.OTel.Endpoint == "" {
		return OrchestratorConfig{}, fmt.Errorf(
			"CBSAGA_OTEL_EXPORTER_OTLP_ENDPOINT cannot be empty when tracing enabled",
		)
	}
	if cfg.OTel.SampleRatio < 0 || cfg.OTel.SampleRatio > 1 {
		return OrchestratorConfig{}, fmt.Errorf(
			"CBSAGA_OTEL_TRACES_SAMPLE_RATIO must be between 0 and 1",
		)
	}

	return cfg, nil
}
