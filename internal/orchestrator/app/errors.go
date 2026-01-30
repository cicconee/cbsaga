package app

import (
	"errors"

	"github.com/cicconee/cbsaga/internal/platform/apperr"
)

var (
	ErrInvalidIdempotencyKeyReuse = errors.New("idempotency key reused with different request")

	ErrIdempotencyInProgress = errors.New("idempotent request in progress")

	ErrCreateWithdrawalFailed = errors.New("could not create withdrawal request")
)

func errInvalidArgument(step string, err error) error {
	return apperr.New(
		apperr.CodeInvalidArgument,
		SubjectWithdrawalCreate,
		step,
		"invalid arguments; resubmit a new request",
		false,
		err,
	)
}

func errFailed(step string, err error) error {
	return apperr.New(
		apperr.CodeFailed,
		SubjectWithdrawalCreate,
		step,
		"failed to create a withdrawal; resubmit a new request",
		false,
		err,
	)
}

func errInternal(step string, err error) error {
	return apperr.New(
		apperr.CodeInternal,
		SubjectWithdrawalCreate,
		step,
		"unable to process request; please retry",
		true,
		err,
	)
}

func errInternalWithFields(step string, err error, fields map[string]any) error {
	ae := apperr.New(
		apperr.CodeInternal,
		SubjectWithdrawalCreate,
		step,
		"unable to process request; please retry",
		true,
		err,
	)

	if fields != nil {
		ae.Fields = fields
	}

	return ae
}

func errRetryableConflict(step string, err error) error {
	return apperr.New(
		apperr.CodeRetryableConflict,
		SubjectWithdrawalCreate,
		step,
		"request is still in progress; please retry",
		true,
		err,
	)
}
