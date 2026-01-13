package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type OrchestratorConfig struct {
	Env             string
	GRPCAddr        string
	ShutdownTimeout time.Duration
	PostgresDSN     string
}

func LoadOrchestrator() (OrchestratorConfig, error) {
	cfg := OrchestratorConfig{
		Env:             getenv("CBSAGA_ENV", "dev"),
		GRPCAddr:        getenv("CBSAGA_ORCH_GRPC_ADDR", ":9000"),
		ShutdownTimeout: getenvDuration("CBSAGA_SHUTDOWN_TIMEOUT", 10*time.Second),
		PostgresDSN:     getenv("CBSAGA_ORCH_POSTGRES_DSN", "postgres://postgres:postgres@localhost:5432/orchestrator?sslmode=disable"),
	}

	if cfg.GRPCAddr == "" {
		return OrchestratorConfig{}, fmt.Errorf("CBSAGA_ORCH_GRPC_ADDR cannot be empty")
	}

	return cfg, nil
}

type IdentityConfig struct {
	Env             string
	PostgresDSN     string
	KafkaBrokers    []string
	KafkaGroupID    string
	KafkaTopic      string
	ShutdownTimeout time.Duration
}

func LoadIdentity() (IdentityConfig, error) {
	cfg := IdentityConfig{
		Env:             getenv("CBSAGA_ENV", "dev"),
		PostgresDSN:     getenv("CBSAGA_IDENTITY_POSTGRES_DSN", "postgres://postgres:postgres@localhost:5433/identity?sslmode=disable"),
		KafkaBrokers:    splitCSV(getenv("CBSAGA_KAFKA_BROKERS", "localhost:9092")),
		KafkaGroupID:    getenv("CBSAGA_IDENTITY_GROUP_ID", "cbsaga-identity"),
		KafkaTopic:      getenv("CBSAGA_IDENTITY_TOPIC", "cbsaga.outbox.withdrawal"),
		ShutdownTimeout: getenvDuration("CBSAGA_SHUTDOWN_TIMEOUT", 10*time.Second),
	}
	return cfg, nil
}

func getenv(key string, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}

	return def
}

func getenvDuration(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}

	d, err := time.ParseDuration(v)
	if err == nil {
		return d
	}

	if secs, secsErr := strconv.Atoi(v); secsErr == nil {
		return time.Duration(secs) * time.Second
	}

	return def
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}

	return out
}
