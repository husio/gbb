package sqldb

import (
	"context"
	"database/sql"

	"github.com/lib/pq"
)

func PostgresDatabase(db *sql.DB) Database {
	return &tracedDatabase{
		db: &postgresDatabase{db: db},
	}
}

type postgresDatabase struct {
	db *sql.DB
}

func (pg *postgresDatabase) ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	result, err := pg.db.ExecContext(ctx, query, args...)
	return result, castPgErr(err)
}

func (pg *postgresDatabase) QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	rows, err := pg.db.QueryContext(ctx, query, args...)
	return rows, castPgErr(err)
}

func (pg *postgresDatabase) QueryRowContext(ctx context.Context, query string, args ...interface{}) Row {
	row := pg.db.QueryRowContext(ctx, query, args...)
	return &pgRow{row: row}
}

type pgRow struct {
	row *sql.Row
}

func (r *pgRow) Scan(dest ...interface{}) error {
	return castPgErr(r.row.Scan(dest...))
}

func (pg *postgresDatabase) Close() error {
	return castPgErr(pg.db.Close())
}

func (pg *postgresDatabase) BeginTx(ctx context.Context, opts *sql.TxOptions) (Transaction, error) {
	tx, err := pg.db.BeginTx(ctx, opts)
	if err != nil {
		return nil, castPgErr(err)
	}
	return &postgresTransaction{tx: tx}, nil
}

type postgresTransaction struct {
	tx *sql.Tx
}

func (tx *postgresTransaction) ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	result, err := tx.tx.ExecContext(ctx, query, args...)
	return result, castPgErr(err)
}

func (tx *postgresTransaction) QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	rows, err := tx.tx.QueryContext(ctx, query, args...)
	return rows, castPgErr(err)
}

func (tx *postgresTransaction) QueryRowContext(ctx context.Context, query string, args ...interface{}) Row {
	row := tx.tx.QueryRowContext(ctx, query, args...)
	return &pgRow{row: row}
}

func (tx *postgresTransaction) Commit() error {
	return castPgErr(tx.tx.Commit())
}

func (tx *postgresTransaction) Rollback() error {
	return castPgErr(tx.tx.Rollback())
}

func castPgErr(err error) error {
	if err == nil {
		return nil
	}

	if err == sql.ErrNoRows {
		return ErrNotFound
	}

	if e, ok := err.(pq.Error); ok {
		switch prefix := e.Code[:2]; prefix {
		case "20":
			return ErrNotFound
		case "23":
			return ErrConstraint
		}
	}

	return err
}
