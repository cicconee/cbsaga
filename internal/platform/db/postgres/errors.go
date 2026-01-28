package postgres

import (
	"fmt"
	"time"
)

type BeginTxError struct {
	Op     string
	Err    error
	CtxErr error
}

func (e BeginTxError) Error() string {
	return fmt.Sprintf("begin tx failed (%s): %v", e.Op, e.Err)
}

func (e BeginTxError) Unwrap() error {
	return e.Err
}

type CommitUnknownError struct {
	Op       string
	Err      error
	Duration time.Duration
	CtxErr   error
}

func (e CommitUnknownError) Error() string {
	return fmt.Sprintf("commit outcome unknown (%s): %v", e.Op, e.Err)
}

func (e CommitUnknownError) Unwrap() error {
	return e.Err
}
