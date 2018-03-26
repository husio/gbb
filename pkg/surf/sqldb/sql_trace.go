package sqldb

import (
	"context"
	"database/sql"

	"github.com/husio/gbb/pkg/surf"
)

type tracedDatabase struct {
	prefix string
	db     Database
}

func (tdb *tracedDatabase) ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	span := surf.CurrentTrace(ctx).Begin(
		tdb.prefix+"database exec context",
		"sql", query)

	result, err := tdb.db.ExecContext(ctx, query, args...)

	if err != nil {
		span.Finish("error", err.Error())
	} else {
		span.Finish()
	}
	return result, err
}

func (tdb *tracedDatabase) QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	span := surf.CurrentTrace(ctx).Begin(
		tdb.prefix+"database query context",
		"sql", query)

	result, err := tdb.db.QueryContext(ctx, query, args...)

	if err != nil {
		span.Finish("error", err.Error())
	} else {
		span.Finish()
	}
	return result, err
}

func (tdb *tracedDatabase) QueryRowContext(ctx context.Context, query string, args ...interface{}) Row {
	span := surf.CurrentTrace(ctx).Begin(
		tdb.prefix+"database query row context",
		"sql", query)

	return &tracedRow{
		span: span,
		row:  tdb.db.QueryRowContext(ctx, query, args...),
	}
}

type tracedRow struct {
	span surf.TraceSpan
	row  Row
}

func (tr *tracedRow) Scan(dest ...interface{}) error {
	err := tr.row.Scan(dest...)
	if err != nil {
		tr.span.Finish("error", err.Error())
	} else {
		tr.span.Finish()
	}
	return err
}

func (tdb *tracedDatabase) BeginTx(ctx context.Context, opts *sql.TxOptions) (Transaction, error) {
	span := surf.CurrentTrace(ctx).Begin(tdb.prefix + "database begin tx")

	tx, err := tdb.db.BeginTx(ctx, opts)

	if err != nil {
		span.Finish("error", err.Error())
		return tx, err
	}

	return &tracedTransaction{
		root: span,
		tx:   tx,
	}, nil
}

func (tdb *tracedDatabase) Close() error {
	return tdb.db.Close()
}

type tracedTransaction struct {
	root surf.TraceSpan
	tx   Transaction
}

func (ttx *tracedTransaction) ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	span := ttx.root.Begin("exec context",
		"sql", query)

	result, err := ttx.tx.ExecContext(ctx, query, args...)
	if err != nil {
		span.Finish("err", err.Error())
	} else {
		span.Finish()
	}
	return result, err
}
func (ttx *tracedTransaction) QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	span := ttx.root.Begin("query context",
		"sql", query)

	result, err := ttx.tx.QueryContext(ctx, query, args...)

	if err != nil {
		span.Finish("err", err.Error())
	} else {
		span.Finish()
	}
	return result, err
}

func (ttx *tracedTransaction) QueryRowContext(ctx context.Context, query string, args ...interface{}) Row {
	span := ttx.root.Begin("query context",
		"sql", query)

	return &tracedRow{
		span: span,
		row:  ttx.tx.QueryRowContext(ctx, query, args...),
	}
}

func (ttx *tracedTransaction) Commit() error {
	if err := ttx.tx.Commit(); err != nil {
		ttx.root.Finish(
			"err", err.Error(),
			"end", "commit")
		return err
	}
	ttx.root.Finish("end", "commit")
	return nil
}

func (ttx *tracedTransaction) Rollback() error {
	if err := ttx.tx.Rollback(); err != nil {
		ttx.root.Finish(
			"err", err.Error(),
			"end", "rollback")
		return err
	}
	ttx.root.Finish("end", "rollback")
	return nil
}
