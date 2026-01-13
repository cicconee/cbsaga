package app

import "errors"

var (
	ErrInvalidIdempotencyKeyReuse = errors.New("idempotency key reused with different request")

	ErrIdempotencyInProgress = errors.New("idempotent request in progress")
)
