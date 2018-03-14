package sqldb

import (
	"context"
	"database/sql"
	"errors"
)

type Database interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...interface{}) Row

	BeginTx(ctx context.Context, opts *sql.TxOptions) (Transaction, error)
	Close() error
}

type Transaction interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...interface{}) Row

	Commit() error
	Rollback() error
}

type Row interface {
	Scan(...interface{}) error
}

var (
	ErrNotFound   = errors.New("not found")
	ErrConstraint = errors.New("constraint")
)
