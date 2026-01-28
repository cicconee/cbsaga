package postgres

import (
	"context"
	"errors"
	"net"

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

func IsRetryableBeginCause(err error) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	var ne net.Error
	if errors.As(err, &ne) {
		if ne.Timeout() {
			return true
		}
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case
			"57P03", // cannot_connect_now
			"57P01", // admin_shutdown
			"57P02", // crash_shutdown
			"08006", // connection_failure
			"08001": // sqlclient_unable_to_establish_sqlconnection
			return true
		default:
			return false
		}
	}

	return false
}
