package apperr

import (
	"fmt"

	"github.com/cicconee/cbsaga/internal/platform/fields"
)

type Code string

const (
	CodeInternal          Code = "INTERNAL"
	CodeInvalidArgument   Code = "INVALID_ARGUMENT"
	CodeFailed            Code = "FAILED"
	CodeRetryableConflict Code = "RETRYABLE_CONFLICT"
)

type Error struct {
	Code      Code
	Subject   string
	Step      string
	Message   string // safe to expose
	Retryable bool
	Cause     error
	Attrs     *fields.Attrs
}

func (e *Error) WithAttr(k string, v any) *Error {
	if e.Attrs == nil {
		e.Attrs = fields.New()
	}
	e.Attrs.Set(k, v)
	return e
}

func (e *Error) WithAttrs(a *fields.Attrs) *Error {
	if a == nil {
		return e
	}
	if e.Attrs == nil {
		e.Attrs = fields.New()
	}
	e.Attrs.Merge(a)
	return e
}

func (e *Error) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("%s: %s", e.Code, e.Message)
	}
	return string(e.Code)
}

func (e *Error) Unwrap() error {
	return e.Cause
}

func New(code Code, subject string, step string, msg string, retryable bool, cause error) *Error {
	return &Error{
		Code:      code,
		Subject:   subject,
		Step:      step,
		Message:   msg,
		Retryable: retryable,
		Cause:     cause,
	}
}
