package config

import (
	"time"

	"github.com/cicconee/cbsaga/internal/platform/config"
)

type IdentityConfig struct {
	Env             string
	PostgresDSN     string
	KafkaBrokers    []string
	KafkaGroupID    string
	KafkaTopic      string
	ShutdownTimeout time.Duration
}

func Load() (IdentityConfig, error) {
	cfg := IdentityConfig{
		Env:             config.GetEnv("CBSAGA_ENV", "dev"),
		PostgresDSN:     config.GetEnv("CBSAGA_IDENTITY_POSTGRES_DSN", "postgres://postgres:postgres@localhost:5433/identity?sslmode=disable"),
		KafkaBrokers:    config.SplitCSV(config.GetEnv("CBSAGA_KAFKA_BROKERS", "localhost:9092")),
		KafkaGroupID:    config.GetEnv("CBSAGA_IDENTITY_GROUP_ID", "cbsaga-identity"),
		KafkaTopic:      config.GetEnv("CBSAGA_IDENTITY_TOPIC", "cbsaga.outbox.withdrawal"),
		ShutdownTimeout: config.GetEnvDuration("CBSAGA_SHUTDOWN_TIMEOUT", 10*time.Second),
	}
	return cfg, nil
}
