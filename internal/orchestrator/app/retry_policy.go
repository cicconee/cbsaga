package app

import (
	"errors"

	"github.com/cicconee/cbsaga/internal/orchestrator/repo"
	"github.com/cicconee/cbsaga/internal/platform/db/postgres"
	"github.com/cicconee/cbsaga/internal/platform/retry"
)

func reserveIdemRetryPolicy() retry.Config {
	cfg := retry.DefaultConfig()
	cfg.IsRetryable = isRetryableReserveIdem
	return cfg
}

func isRetryableReserveIdem(err error) bool {
	// Key already used for a finalized idempotency key. It will never succeed.
	if errors.Is(err, repo.ErrIdempotencyKeyReuse) {
		return false
	}

	var cuErr postgres.CommitUnknownError
	if errors.As(err, &cuErr) {
		return false
	}

	var btxErr postgres.BeginTxError
	if errors.As(err, &btxErr) {
		return postgres.IsRetryableBeginCause(btxErr.Unwrap())
	}

	return postgres.IsRetryablePostgres(err)
}

func failIdemRetryPolicy() retry.Config {
	cfg := retry.DefaultConfig()
	cfg.IsRetryable = isRetryableFailIdem
	return cfg
}

func isRetryableFailIdem(err error) bool {
	// Lost lease means another worker owns finalization now, retrying would just loop until
	// MaxAttempts.
	if errors.Is(err, repo.ErrLostLeaseOwnership) {
		return false
	}

	var cuErr postgres.CommitUnknownError
	if errors.As(err, &cuErr) {
		return false
	}

	var btxErr postgres.BeginTxError
	if errors.As(err, &btxErr) {
		return postgres.IsRetryableBeginCause(btxErr.Unwrap())
	}

	return postgres.IsRetryablePostgres(err)
}
