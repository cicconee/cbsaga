package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cicconee/cbsaga/internal/identity/config"
	"github.com/cicconee/cbsaga/internal/identity/consumer"
	"github.com/cicconee/cbsaga/internal/platform/db/postgres"
	"github.com/cicconee/cbsaga/internal/platform/logging"
)

func main() {
	log := logging.New("identity-srv")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		log.Error("config load failed", "err", err)
		os.Exit(1)
	}

	// TODO: start up time out define in configuration.
	startupCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	pool, err := postgres.NewPool(startupCtx, cfg.PostgresDSN, log)
	if err != nil {
		log.Error("identity postgres init failed", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	c := consumer.New(pool, log, cfg.KafkaBrokers, cfg.IdentityConsumerGroupID, cfg.IdentityCmdTopic)
	defer func() { _ = c.Close() }()

	log.Info("identity-svc running",
		"topic", cfg.IdentityCmdTopic,
		"group", cfg.IdentityConsumerGroupID,
		"brokers", cfg.KafkaBrokers,
	)

	if err := c.Run(ctx); err != nil {
		log.Error("identity consumer failed", "err", err)
		os.Exit(1)
	}
}
