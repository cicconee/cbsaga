package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgconn"
)

func IsRetryablePostgres(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case
			"40001", // serialization_failure
			"40P01", // deadlock_detected
			"55P03", // lock_not_available
			"57P01", // admin_shutdown
			"57P02", // crash_shutdown
			"57P03": // cannot_connect_now
			return true
		default:
			return false
		}
	}

	return false
}
