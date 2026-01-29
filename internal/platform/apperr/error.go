package apperr

import "fmt"

type Code string

const (
	CodeBeginDbTx       Code = "BEGIN_DB_TX"
	CodeCommitDbUnknown Code = "COMMIT_DB_UNKNOWN"
	CodeInternal        Code = "INTERNAL"
)

type Error struct {
	Code      Code
	Subject   string
	Step      string
	Message   string // safe to expose
	Retryable bool
	Cause     error
	Fields    map[string]any
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
