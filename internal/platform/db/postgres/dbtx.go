package postgres

import (
	"context"
	"time"

	"github.com/cicconee/cbsaga/internal/platform/retry"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type DB interface {
	BeginTx(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error)
}

type DBTX interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

func WithTxRetryResult[T any](
	ctx context.Context,
	db DB,
	txOptions pgx.TxOptions,
	op string,
	cfg retry.Config,
	fn func(context.Context, pgx.Tx) (T, error),
) (T, error) {
	var zero T
	var out T

	txFunc := func(ctx context.Context, tx pgx.Tx) error {
		got, err := fn(ctx, tx)
		if err != nil {
			return err
		}
		out = got
		return nil
	}

	err := retry.Do(ctx, cfg, func() error {
		return WithTx(ctx, db, txOptions, op, txFunc)
	})
	if err != nil {
		return zero, err
	}

	return out, nil
}

func WithTxRetry(
	ctx context.Context,
	db DB,
	txOptions pgx.TxOptions,
	op string,
	cfg retry.Config,
	fn func(context.Context, pgx.Tx) error,
) error {
	return retry.Do(ctx, cfg, func() error {
		return WithTx(ctx, db, txOptions, op, fn)
	})
}

func WithTx(
	ctx context.Context,
	db DB,
	txOptions pgx.TxOptions,
	op string,
	fn func(context.Context, pgx.Tx) error,
) error {
	tx, err := db.BeginTx(ctx, txOptions)
	if err != nil {
		return BeginTxError{
			Op:     op,
			Err:    err,
			CtxErr: ctx.Err(),
		}
	}
	defer func() { _ = tx.Rollback(ctx) }()

	start := time.Now()

	if err := fn(ctx, tx); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return CommitUnknownError{
			Op:       op,
			Err:      err,
			Duration: time.Since(start),
			CtxErr:   ctx.Err(),
		}
	}

	return nil
}
