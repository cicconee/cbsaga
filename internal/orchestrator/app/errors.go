package app

import (
	"errors"

	"github.com/cicconee/cbsaga/internal/platform/apperr"
)

var (
	ErrInvalidIdempotencyKeyReuse = errors.New("idempotency key reused with different request")
	ErrIdempotencyInProgress      = errors.New("idempotent request in progress")
	ErrCreateWithdrawalFailed     = errors.New("could not create withdrawal request")
)

func errInvalidArgument(err error) *apperr.Error {
	return apperr.New(
		apperr.CodeInvalidArgument,
		"invalid arguments; resubmit a new request",
		false,
		err,
	)
}

func errFailed(err error) *apperr.Error {
	return apperr.New(
		apperr.CodeFailed,
		"failed to create a withdrawal; resubmit a new request",
		false,
		err,
	)
}

func errInternal(err error) *apperr.Error {
	return apperr.New(
		apperr.CodeInternal,
		"unable to process request; please retry",
		true,
		err,
	)
}
