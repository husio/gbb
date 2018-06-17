package gbb

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/husio/gbb/pkg/surf/sqldb"
	"github.com/lib/pq"
)

func NewPostgresReadProgressTracker(db *sql.DB) (ReadProgressTracker, error) {
	store := &pgReadProgressTracker{
		db: sqldb.PostgresDatabase(db),
	}
	return store, store.ensureSchema(context.Background())
}

type pgReadProgressTracker struct {
	db sqldb.Database
}

func (rpt *pgReadProgressTracker) ensureSchema(ctx context.Context) error {
	const schema = `
CREATE TABLE IF NOT EXISTS readprogress (
	id SERIAL PRIMARY KEY,
	user_id INTEGER NOT NULL,
	topic_id INTEGER NOT NULL,
	comment_id INTEGER NOT NULL,
	comment_created TIMESTAMPTZ NOT NULL,

	UNIQUE(user_id, topic_id)
);

CREATE INDEX IF NOT EXISTS readprogress_user_topic_idx  ON readprogress(user_id, topic_id);

CREATE TABLE IF NOT EXISTS readprogressall (
	id SERIAL PRIMARY KEY,
	user_id INTEGER NOT NULL,
	created TIMESTAMPTZ NOT NULL,

	UNIQUE (user_id)
);
`
	for i, migration := range strings.Split(schema, `;\n\n`) {
		_, err := rpt.db.ExecContext(ctx, migration)
		if err != nil {
			if max := 30; len(migration) > max {
				migration = migration[max:]
			}
			return fmt.Errorf("migration %d (%s): %s", i, migration, err)
		}
	}
	return nil
}

func (rpt *pgReadProgressTracker) LastReads(ctx context.Context, userID int64, topicIDs []int64) (map[int64]*ReadProgress, error) {
	tx, err := rpt.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("transaction begin: %s", err)
	}
	defer tx.Rollback()

	results := make(map[int64]*ReadProgress, len(topicIDs)*2)

	var readall time.Time
	switch tx.QueryRowContext(ctx, `SELECT created FROM readprogressall WHERE user_id = $1 LIMIT 1`, userID).Scan(&readall); err {
	case sql.ErrNoRows:
		// all good, but no "mark all read" was ever executed for this user
	case nil:
		for _, tid := range topicIDs {
			results[tid] = &ReadProgress{
				UserID:         userID,
				TopicID:        tid,
				CommentCreated: readall,
				CommentID:      0, // not provided
			}
		}
	default:
		return nil, fmt.Errorf("select readprogressall: %s", err)
	}

	rows, err := rpt.db.QueryContext(ctx, `
		SELECT topic_id, comment_id, comment_created
		FROM readprogress
		WHERE topic_id = ANY($1) AND user_id = $2
		LIMIT 1000
	`, pq.Array(topicIDs), userID)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("select readprogress: %s", err)
	}
	defer rows.Close()
	for rows.Next() {
		p := ReadProgress{
			UserID: userID,
		}
		if err := rows.Scan(&p.TopicID, &p.CommentID, &p.CommentCreated); err != nil {
			return results, fmt.Errorf("scan readprogress: %s", err)
		}
		if pall, ok := results[p.TopicID]; !ok || p.CommentCreated.After(pall.CommentCreated) {
			results[p.TopicID] = &p
		}
	}
	if err := rows.Err(); err != nil {
		return results, fmt.Errorf("readprogress rows: %s", err)
	}

	return results, nil
}

func (rpt *pgReadProgressTracker) Track(ctx context.Context, p ReadProgress) error {
	_, err := rpt.db.ExecContext(ctx, `
		INSERT INTO
			readprogress (user_id, topic_id, comment_id, comment_created)
			VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_id, topic_id) DO UPDATE
			SET comment_id = EXCLUDED.comment_id, comment_created = EXCLUDED.comment_created
	`, p.UserID, p.TopicID, p.CommentID, p.CommentCreated)
	return castErr(err)
}

func (rpt *pgReadProgressTracker) MarkAllRead(ctx context.Context, userID int64, now time.Time) error {
	tx, err := rpt.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %s", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM readprogress WHERE user_id = $1`, userID); err != nil {
		return fmt.Errorf("delete readprogress: %s", err)
	}
	if _, err := tx.ExecContext(ctx, `
			INSERT INTO readprogressall (user_id, created)
			VALUES ($1, $2)
			ON CONFLICT (user_id) DO UPDATE SET created = EXCLUDED.created
		`, userID, now); err != nil {
		return fmt.Errorf("set readprogressall: %s", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %s", err)
	}
	return nil
}
