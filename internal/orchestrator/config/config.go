package config

import (
	"fmt"
	"time"

	"github.com/cicconee/cbsaga/internal/platform/config"
)

type OrchestratorConfig struct {
	Env                 string
	GRPCAddr            string
	ShutdownTimeout     time.Duration
	PostgresDSN         string
	KafkaBrokers        []string
	IdentityTopic       string
	OrchestratorGroupID string
}

func Load() (OrchestratorConfig, error) {
	cfg := OrchestratorConfig{
		Env:                 config.GetEnv("CBSAGA_ENV", "dev"),
		GRPCAddr:            config.GetEnv("CBSAGA_ORCH_GRPC_ADDR", ":9000"),
		ShutdownTimeout:     config.GetEnvDuration("CBSAGA_SHUTDOWN_TIMEOUT", 10*time.Second),
		PostgresDSN:         config.GetEnv("CBSAGA_ORCH_POSTGRES_DSN", "postgres://postgres:postgres@localhost:5432/orchestrator?sslmode=disable"),
		KafkaBrokers:        config.SplitCSV(config.GetEnv("CBSAGA_KAFKA_BROKERS", "localhost:9092")),
		IdentityTopic:       config.GetEnv("CBSAGA_ORCH_IDENTITY_TOPIC", "cbsaga.outbox.identity"),
		OrchestratorGroupID: config.GetEnv("CBSAGA_ORCH_GROUP_ID", "cbsaga-orchestrator"),
	}

	if cfg.GRPCAddr == "" {
		return OrchestratorConfig{}, fmt.Errorf("CBSAGA_ORCH_GRPC_ADDR cannot be empty")
	}

	return cfg, nil
}
