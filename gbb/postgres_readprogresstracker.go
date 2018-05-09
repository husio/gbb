package gbb

import (
	"context"
	"database/sql"

	"github.com/husio/gbb/pkg/surf/sqldb"
	"github.com/lib/pq"
)

func NewPostgresReadProgressTracker(db *sql.DB) ReadProgressTracker {
	return &pgReadProgressTracker{
		db: sqldb.PostgresDatabase(db),
	}
}

type pgReadProgressTracker struct {
	db sqldb.Database
}

func (rpt *pgReadProgressTracker) LastReads(ctx context.Context, userID int64, topicIDs []int64) (map[int64]*ReadProgress, error) {
	rows, err := rpt.db.QueryContext(ctx, `
		SELECT topic_id, comment_id, comment_created
		FROM readprogress
		WHERE user_id = $1 AND topic_id = ANY($2)
		LIMIT 1000
	`, userID, pq.Array(topicIDs))
	if err != nil {
		return nil, castErr(err)
	}
	defer rows.Close()

	results := make(map[int64]*ReadProgress)
	for rows.Next() {
		p := &ReadProgress{
			UserID: userID,
		}
		if err := rows.Scan(&p.TopicID, &p.CommentID, &p.CommentCreated); err != nil {
			return results, castErr(err)
		}
		results[p.TopicID] = p
	}

	return results, castErr(rows.Err())
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
