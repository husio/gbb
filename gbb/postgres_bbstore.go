package gbb

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/husio/gbb/pkg/surf"
	"github.com/husio/gbb/pkg/surf/sqldb"
	"github.com/lib/pq"
)

func NewPostgresBBStore(db *sql.DB) BBStore {
	return &pgBBStore{
		db: sqldb.PostgresDatabase(db),
	}
}

type pgBBStore struct {
	db sqldb.Database
}

func (s *pgBBStore) ListCategories(ctx context.Context) ([]*Category, error) {
	var categories []*Category
	resp, err := s.db.QueryContext(ctx, `
		SELECT
			category_id,
			name
		FROM
			categories
		LIMIT 1000
	`)
	if err != nil {
		return categories, fmt.Errorf("cannot fetch categories: %s", err)
	}
	defer resp.Close()

	for resp.Next() {
		var c Category
		if err := resp.Scan(
			&c.CategoryID,
			&c.Name,
		); err != nil {
			return categories, fmt.Errorf("cannot scan row: %s", err)
		}

		categories = append(categories, &c)
	}
	return categories, nil
}

func (s *pgBBStore) ListTopics(ctx context.Context, createdLte time.Time, limit int) ([]*Topic, error) {
	var topics []*Topic
	resp, err := s.db.QueryContext(ctx, `
		SELECT
			t.topic_id,
			t.subject,
			t.created,
			t.views_count,
			t.comments_count,
			t.latest_comment,
			u.user_id,
			u.name,
			cc.category_id,
			cc.name
		FROM
			topics t
			INNER JOIN users u ON t.author_id = u.user_id
			INNER JOIN categories cc ON t.category_id = cc.category_id
		WHERE
			t.latest_comment <= $1
		ORDER BY
			t.latest_comment DESC
		LIMIT $2
	`, createdLte, limit)
	if err != nil {
		return topics, fmt.Errorf("cannot fetch topics: %s", err)
	}
	defer resp.Close()

	for resp.Next() {
		var t Topic
		if err := resp.Scan(
			&t.TopicID,
			&t.Subject,
			&t.Created,
			&t.ViewsCount,
			&t.CommentsCount,
			&t.Updated,
			&t.Author.UserID,
			&t.Author.Name,
			&t.Category.CategoryID,
			&t.Category.Name,
		); err != nil {
			return topics, fmt.Errorf("cannot scan row: %s", err)
		}

		topics = append(topics, &t)
	}
	return topics, nil
}

func (s *pgBBStore) ListComments(ctx context.Context, topicID int64, offset, limit int) ([]*Comment, error) {
	var comments []*Comment
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			c.comment_id,
			c.content,
			c.created,
			c.author_id,
			u.name
		FROM
			comments c
			INNER JOIN users u ON c.author_id = u.user_id
		WHERE
			c.topic_id = $1
		ORDER BY
			c.created ASC
		LIMIT $2
		OFFSET $3
	`, topicID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("cannot fetch comments: %s", err)
	}
	defer rows.Close()
	for rows.Next() {
		c := Comment{TopicID: topicID}
		if err := rows.Scan(
			&c.CommentID,
			&c.Content,
			&c.Created,
			&c.Author.UserID,
			&c.Author.Name,
		); err != nil {
			return comments, fmt.Errorf("cannot scan comment: %s", err)
		}
		comments = append(comments, &c)
	}

	return comments, castErr(rows.Err())
}

func (s *pgBBStore) Search(ctx context.Context, text string, categories []int64, offset, limit int64) ([]*SearchResult, error) {
	var results []*SearchResult

	rows, err := s.db.QueryContext(ctx, `
		SELECT
			t.topic_id,
			t.subject,
			t.created,
			t.author_id,
			t.views_count,
			t.comments_count,
			t.latest_comment,
			c.comment_id,
			c.content,
			c.created,
			c.author_id,
			u.name,
			cc.category_id,
			cc.name
		FROM
			comments c
			INNER JOIN topics t ON c.topic_id = t.topic_id
			INNER JOIN users u ON c.author_id = u.user_id
			INNER JOIN categories CC on t.category_id = cc.category_id
		WHERE
			(char_length($1) = 0 OR c.content ILIKE '%' || $1 || '%')
			AND ($2::INTEGER[] IS NULL OR t.category_id = ANY($2::INTEGER[]))
		ORDER BY
			c.created ASC
		LIMIT $3
		OFFSET $4
	`, text, pq.Array(categories), limit, offset)
	if err != nil {
		return nil, fmt.Errorf("cannot execute query: %s", err)
	}
	defer rows.Close()
	for rows.Next() {
		var r SearchResult
		if err := rows.Scan(
			&r.Topic.TopicID,
			&r.Topic.Subject,
			&r.Topic.Created,
			&r.Topic.Author.UserID,
			&r.Topic.ViewsCount,
			&r.Topic.CommentsCount,
			&r.Topic.Updated,
			&r.Comment.CommentID,
			&r.Comment.Content,
			&r.Comment.Created,
			&r.Comment.Author.UserID,
			&r.Comment.Author.Name,
			&r.Topic.Category.CategoryID,
			&r.Topic.Category.Name,
		); err != nil {
			return results, fmt.Errorf("cannot scan row: %s", err)
		}
		results = append(results, &r)
	}

	return results, castErr(rows.Err())
}

func (s *pgBBStore) TopicByID(ctx context.Context, topicID int64) (*Topic, error) {
	var t Topic
	row := s.db.QueryRowContext(ctx, `
		SELECT
			t.topic_id,
			t.subject,
			t.created,
			t.views_count,
			t.comments_count,
			t.latest_comment,
			u.user_id,
			u.name,
			cc.category_id,
			cc.name
		FROM
			topics t
			INNER JOIN users u ON t.author_id = u.user_id
			INNER JOIN categories cc ON t.category_id = cc.category_id
		WHERE
			t.topic_id = $1
		LIMIT 1
	`, topicID)
	err := row.Scan(
		&t.TopicID,
		&t.Subject,
		&t.Created,
		&t.ViewsCount,
		&t.CommentsCount,
		&t.Updated,
		&t.Author.UserID,
		&t.Author.Name,
		&t.Category.CategoryID,
		&t.Category.Name,
	)
	return &t, castErr(err)
}

func (s *pgBBStore) CreateTopic(ctx context.Context, subject, content string, categoryID int64, userID int64) (*Topic, *Comment, error) {
	defer surf.CurrentTrace(ctx).Begin("create topic").Finish()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot open transaction: %s", err)
	}
	defer tx.Rollback()

	user := User{
		UserID: userID,
	}
	err = tx.QueryRowContext(ctx, `
		SELECT name FROM users WHERE user_id = $1 LIMIT 1
	`, userID).Scan(&user.Name)
	switch castErr(err) {
	case nil:
		// all good
	case ErrNotFound:
		return nil, nil, fmt.Errorf("user not found: %s", err)
	default:
		return nil, nil, fmt.Errorf("cannot fetch user: %s", err)
	}

	now := time.Now().UTC()
	topic := Topic{
		Subject: subject,
		Author:  user,
		Created: now,
		Updated: now,
		Category: Category{
			CategoryID: categoryID,
		},
	}
	err = tx.QueryRowContext(ctx, `
		INSERT INTO topics (subject, created, author_id, category_id, views_count, comments_count)
		VALUES ($1, $2, $3, $4, 0, 0)
		RETURNING topic_id
	`, topic.Subject, topic.Created, user.UserID, topic.Category.CategoryID).Scan(&topic.TopicID)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot create topic: %s", err)
	}

	comment := Comment{
		TopicID: topic.TopicID,
		Content: content,
		Created: now,
		Author:  user,
	}
	err = tx.QueryRowContext(ctx, `
		INSERT INTO comments (topic_id, content, created, author_id)
		VALUES ($1, $2, $3, $4)
		RETURNING comment_id
	`, comment.TopicID, comment.Content, comment.Created, user.UserID).Scan(&comment.CommentID)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot create comment: %s", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, nil, fmt.Errorf("cannot commit transaction: %s", err)
	}
	return &topic, &comment, nil
}

func (s *pgBBStore) CreateComment(ctx context.Context, topicID int64, content string, userID int64) (*Comment, error) {
	defer surf.CurrentTrace(ctx).Begin("create comment").Finish()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("cannot open transaction: %s", err)
	}
	defer tx.Rollback()

	user := User{
		UserID: userID,
	}
	err = tx.QueryRowContext(ctx, `
		SELECT name FROM users WHERE user_id = $1 LIMIT 1
	`, userID).Scan(&user.Name)
	switch castErr(err) {
	case nil:
		// all good
	case ErrNotFound:
		return nil, fmt.Errorf("user not found: %s", err)
	default:
		return nil, fmt.Errorf("cannot fetch user: %s", err)
	}

	comment := Comment{
		TopicID: topicID,
		Content: content,
		Created: time.Now().UTC(),
		Author:  user,
	}

	err = tx.QueryRowContext(ctx, `
		INSERT INTO comments (topic_id, content, created, author_id)
		VALUES ($1, $2, $3, $4)
		RETURNING comment_id
	`, comment.TopicID, comment.Content, comment.Created, user.UserID).Scan(&comment.CommentID)
	switch err := castErr(err); err {
	case nil:
		// all good
	case ErrConstraint:
		return nil, ErrNotFound
	default:
		return nil, fmt.Errorf("cannot create comment: %s", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("cannot commit transaction: %s", err)
	}
	return &comment, nil
}

func (s *pgBBStore) IncrementTopicView(ctx context.Context, topicID int64) error {
	defer surf.CurrentTrace(ctx).Begin("increment topic view").Finish()

	_, err := s.db.ExecContext(ctx, `
		UPDATE topics SET views_count = views_count + 1
		WHERE topic_id = $1
	`, topicID)
	if err != nil {
		return fmt.Errorf("cannot execute query: %s", err)
	}
	// it does not matter that if counter was incremented or not -
	// successfult query execution is good enough for this use case
	return nil
}

func (s *pgBBStore) UpdateComment(ctx context.Context, commentID int64, content string) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE comments
		SET content = $2
		WHERE comment_id = $1
	`, commentID, content)
	if err != nil {
		return castErr(err)
	}

	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *pgBBStore) UpdateTopic(ctx context.Context, topicID int64, subject string) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE topics
		SET subject = $2
		WHERE topic_id = $1
	`, topicID, subject)
	if err != nil {
		return castErr(err)
	}

	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *pgBBStore) DeleteTopic(ctx context.Context, topicID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("cannot start transaction: %s", err)
	}
	defer tx.Rollback()

	if _, err = tx.ExecContext(ctx, `DELETE FROM comments WHERE topic_id = $1`, topicID); err != nil {
		return castErr(err)
	}

	if res, err := tx.ExecContext(ctx, `DELETE FROM topics WHERE topic_id = $1`, topicID); err != nil {
		return castErr(err)
	} else {
		if n, err := res.RowsAffected(); err != nil {
			return err
		} else if n == 0 {
			return ErrNotFound
		}
	}
	return castErr(tx.Commit())
}

func (s *pgBBStore) DeleteComment(ctx context.Context, commentID int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM comments WHERE comment_id = $1`, commentID)
	if err != nil {
		return castErr(err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return castErr(err)
	} else if n != 1 {
		return ErrNotFound
	}
	return nil
}

func (s *pgBBStore) CommentByID(ctx context.Context, commentID int64) (*Topic, *Comment, int, error) {
	var (
		t          Topic
		c          Comment
		commentPos int
	)
	row := s.db.QueryRowContext(ctx, `
		SELECT
			t.topic_id,
			t.subject,
			t.created,
			t.views_count,
			t.comments_count,
			tu.user_id AS topic_user_id,
			tu.name AS topic_user_name,
			c.comment_id,
			c.content,
			c.created,
			cc.category_id,
			cc.name,
			cu.user_id AS comment_user_id,
			cu.name AS comment_user_name,
			(SELECT COUNT(*) FROM comments WHERE topic_id = c.topic_id AND created < c.created) AS comment_pos
		FROM
			comments c
			INNER JOIN users cu ON c.author_id = cu.user_id
			INNER JOIN topics t ON t.topic_id = c.topic_id
			INNER JOIN users tu ON t.author_id = tu.user_id
			INNER JOIN categories cc ON t.category_id = cc.category_id
		WHERE
			c.comment_id = $1
		LIMIT 1
	`, commentID)
	err := row.Scan(
		&t.TopicID,
		&t.Subject,
		&t.Created,
		&t.ViewsCount,
		&t.CommentsCount,
		&t.Author.UserID,
		&t.Author.Name,
		&c.CommentID,
		&c.Content,
		&c.Created,
		&t.Category.CategoryID,
		&t.Category.Name,
		&c.Author.UserID,
		&c.Author.Name,
		&commentPos,
	)
	return &t, &c, commentPos, castErr(err)
}
