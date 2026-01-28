package postgres

import (
	"context"
	"time"

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
