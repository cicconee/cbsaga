package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cicconee/cbsaga/internal/orchestrator/api"
	"github.com/cicconee/cbsaga/internal/orchestrator/app"
	"github.com/cicconee/cbsaga/internal/orchestrator/config"
	"github.com/cicconee/cbsaga/internal/orchestrator/consumer"
	"github.com/cicconee/cbsaga/internal/platform/db/postgres"
	"github.com/cicconee/cbsaga/internal/platform/grpcserver"
	"github.com/cicconee/cbsaga/internal/platform/logging"
	"google.golang.org/grpc"
)

func main() {
	log := logging.New("orchestrator")

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
		log.Error("postgres init failed", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	idc := consumer.NewIdentity(pool, log, cfg.KafkaBrokers, cfg.OrchestratorGroupID, cfg.IdentityTopic)
	defer func() { _ = idc.Close() }()

	go func() {
		if err := idc.Run(ctx); err != nil {
			log.Error("identity consumer crashed", "err", err)
		}
	}()

	svc := app.NewService(pool)

	srv, err := grpcserver.New(
		grpcserver.Options{
			Addr: cfg.GRPCAddr,
		},
		log,
		func(gs *grpc.Server) {
			api.Register(gs, svc, log)
		},
	)
	if err != nil {
		log.Error("grpc server init failed", "err", err)
		os.Exit(1)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Serve(log)
	}()

	log.Info("orchestrator running", "env", cfg.Env, "grpc", cfg.GRPCAddr)

	select {
	case <-ctx.Done():
		log.Info("shutdown signal received")
		srv.GracefulStop(log)
	case err := <-errCh:
		log.Error("server exited", "err", err)
		os.Exit(1)
	}
}
