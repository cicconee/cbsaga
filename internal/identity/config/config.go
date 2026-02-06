package config

import (
	"fmt"
	"time"

	"github.com/cicconee/cbsaga/internal/platform/config"
	"github.com/cicconee/cbsaga/internal/platform/telemetry"
)

type IdentityConfig struct {
	Env                     string
	ShutdownTimeout         time.Duration
	PostgresDSN             string
	KafkaBrokers            []string
	IdentityCmdTopic        string
	IdentityConsumerGroupID string

	OTel telemetry.Config
}

func Load() (IdentityConfig, error) {
	cfg := IdentityConfig{
		Env:             config.GetEnv("CBSAGA_ENV", "dev"),
		ShutdownTimeout: config.GetEnvDuration("CBSAGA_SHUTDOWN_TIMEOUT", 10*time.Second),
		PostgresDSN: config.GetEnv(
			"CBSAGA_IDENTITY_POSTGRES_DSN",
			"postgres://postgres:postgres@localhost:5433/identity?sslmode=disable",
		),
		KafkaBrokers: config.SplitCSV(
			config.GetEnv("CBSAGA_KAFKA_BROKERS", "localhost:9092"),
		),
		IdentityCmdTopic: config.GetEnv("CBSAGA_WITHDRAWAL_TOPIC", "cbsaga.cmd.identity"),
		IdentityConsumerGroupID: config.GetEnv(
			"CBSAGA_IDENTITY_CONSUMER_GROUP_ID",
			"cbsaga-identity",
		),
		OTel: telemetry.Config{
			Enabled:     config.GetEnvBool("CBSAGA_OTEL_ENABLED", false),
			ServiceName: config.GetEnv("CBSAGA_OTEL_SERVICE_NAME", "cbsaga-identity"),
			Endpoint:    config.GetEnv("CBSAGA_OTEL_EXPORTER_OTLP_ENDPOINT", "localhost:4317"),
			Insecure:    config.GetEnvBool("CBSAGA_OTEL_EXPORTER_OTLP_INSECURE", true),
			SampleRatio: config.GetEnvFloat("CBSAGA_OTEL_TRACES_SAMPLE_RATIO", 1.0),
		},
	}

	if cfg.OTel.Enabled && cfg.OTel.Endpoint == "" {
		return IdentityConfig{}, fmt.Errorf(
			"CBSAGA_OTEL_EXPORTER_OTLP_ENDPOINT cannot be empty when tracing enabled",
		)
	}
	if cfg.OTel.SampleRatio < 0 || cfg.OTel.SampleRatio > 1 {
		return IdentityConfig{}, fmt.Errorf(
			"CBSAGA_OTEL_TRACES_SAMPLE_RATIO must be between 0 and 1",
		)
	}

	return cfg, nil
}
