package config

import (
	"time"

	"github.com/cicconee/cbsaga/internal/platform/config"
)

type IdentityConfig struct {
	Env                     string
	ShutdownTimeout         time.Duration
	PostgresDSN             string
	KafkaBrokers            []string
	WithdrawalTopic         string
	IdentityConsumerGroupID string
}

func Load() (IdentityConfig, error) {
	cfg := IdentityConfig{
		Env:                     config.GetEnv("CBSAGA_ENV", "dev"),
		ShutdownTimeout:         config.GetEnvDuration("CBSAGA_SHUTDOWN_TIMEOUT", 10*time.Second),
		PostgresDSN:             config.GetEnv("CBSAGA_IDENTITY_POSTGRES_DSN", "postgres://postgres:postgres@localhost:5433/identity?sslmode=disable"),
		KafkaBrokers:            config.SplitCSV(config.GetEnv("CBSAGA_KAFKA_BROKERS", "localhost:9092")),
		WithdrawalTopic:         config.GetEnv("CBSAGA_WITHDRAWAL_TOPIC", "cbsaga.outbox.withdrawal"),
		IdentityConsumerGroupID: config.GetEnv("CBSAGA_IDENTITY_CONSUMER_GROUP_ID", "cbsaga-identity"),
	}

	return cfg, nil
}
