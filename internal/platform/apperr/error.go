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
	attrs     *fields.Attrs
}

func (e *Error) WithAttr(k string, v any) *Error {
	if e.attrs == nil {
		e.attrs = fields.New()
	}
	e.attrs.Set(k, v)
	return e
}

func (e *Error) WithAttrs(a *fields.Attrs) *Error {
	if a == nil {
		return e
	}
	if e.attrs == nil {
		e.attrs = fields.New()
	}
	e.attrs.Merge(a)
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

func (e *Error) LogArgs() []any {
	return fields.New().
		Str("app_code", string(e.Code)).
		Str("subject", e.Subject).
		Str("step", e.Step).
		Str("message", e.Message).
		Bool("retryable", e.Retryable).
		Merge(e.attrs).
		Args()
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
