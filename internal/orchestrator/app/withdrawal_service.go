package app

import (
	"github.com/cicconee/cbsaga/internal/orchestrator/repo"
	"github.com/cicconee/cbsaga/internal/platform/logging"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

type Service struct {
	db     *pgxpool.Pool
	repo   *repo.Repo
	log    *logging.Logger
	tracer trace.Tracer
}

func NewService(db *pgxpool.Pool, log *logging.Logger) *Service {
	return &Service{
		db:     db,
		repo:   repo.New(),
		log:    log,
		tracer: otel.Tracer("orchestrator/app"),
	}
}
