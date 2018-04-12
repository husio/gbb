package gbb

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/husio/gbb/pkg/surf"
	"github.com/husio/gbb/pkg/surf/sqldb"
	"golang.org/x/crypto/bcrypt"
)

func NewPostgresBBStore(db *sql.DB) BBStore {
	return &pgBBStore{
		db: sqldb.PostgresDatabase(db),
	}
}

type pgBBStore struct {
	db sqldb.Database
}

func NewPostgresUserStore(db *sql.DB) UserStore {
	return &pgUserStore{
		db: sqldb.PostgresDatabase(db),
	}
}

type pgUserStore struct {
	db sqldb.Database
}

const schema = `
CREATE TABLE IF NOT EXISTS users (
	user_id SERIAL PRIMARY KEY,
	password TEXT NOT NULL,
	name TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS topics (
	topic_id SERIAL PRIMARY KEY,
	subject TEXT NOT NULL,
	created TIMESTAMPTZ NOT NULL,
	author_id INTEGER NOT NULL REFERENCES users(user_id),

	views_count INTEGER NOT NULL default 0 CHECK (views_count >= 0),
	comments_count INTEGER NOT NULL default 1 CHECK (comments_count >= 0),
	latest_comment TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS comments (
	comment_id SERIAL PRIMARY KEY,
	topic_id INTEGER NOT NULL REFERENCES topics(topic_id),
	content TEXT NOT NULL,
	created TIMESTAMPTZ NOT NULL,
	author_id INTEGER NOT NULL REFERENCES users(user_id)
);

CREATE INDEX IF NOT EXISTS comments_created_idx ON comments(created);


ALTER TABLE topics ADD COLUMN IF NOT EXISTS latest_comment TIMESTAMPTZ NOT NULL DEFAULT now();

CREATE INDEX IF NOT EXISTS topics_created_idx ON topics(latest_comment);

`

func EnsureSchema(db *sql.DB) error {
	for i, query := range strings.Split(schema, ";\n\n") {
		if _, err := db.Exec(query); err != nil {
			begin := query
			if len(begin) > 60 {
				begin = begin[:58] + ".."
			}
			return fmt.Errorf("cannot execute %d query (%q): %s", i, begin, err)
		}
	}
	return nil
}

func (s *pgBBStore) ListTopics(ctx context.Context, createdLte time.Time, limit int) ([]*Topic, error) {
	defer surf.CurrentTrace(ctx).Begin("list topics").Finish()

	surf.LogInfo(ctx, "listing topics",
		"createdLte", createdLte.String())
	var topics []*Topic
	resp, err := s.db.QueryContext(ctx, `
		SELECT
			t.topic_id,
			t.subject,
			t.created,
			t.views_count,
			t.comments_count,
			t.author_id,
			u.name
		FROM
			topics t
			INNER JOIN users u ON t.author_id = u.user_id
		WHERE
			t.created <= $1
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
			&t.Author.UserID,
			&t.Author.Name,
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

func (s *pgBBStore) Search(ctx context.Context, text string, limit int64) ([]*SearchResult, error) {
	var results []*SearchResult

	rows, err := s.db.QueryContext(ctx, `
		SELECT
			t.topic_id,
			t.subject,
			t.created,
			t.author_id,
			t.views_count,
			t.comments_count,
			c.comment_id,
			c.content,
			c.created,
			c.author_id,
			u.name
		FROM
			comments c
			INNER JOIN topics t ON c.topic_id = t.topic_id
			INNER JOIN users u ON c.author_id = u.user_id
		WHERE
			c.content ILIKE '%' || $1 || '%'
		ORDER BY
			c.created ASC
		LIMIT $2
	`, text, limit)
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
			&r.Comment.CommentID,
			&r.Comment.Content,
			&r.Comment.Created,
			&r.Comment.Author.UserID,
			&r.Comment.Author.Name,
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
			u.user_id,
			u.name
		FROM
			topics t
			INNER JOIN users u ON t.author_id = u.user_id
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
		&t.Author.UserID,
		&t.Author.Name,
	)
	return &t, castErr(err)
}

func (s *pgBBStore) CreateTopic(ctx context.Context, subject, content string, userID int64) (*Topic, *Comment, error) {
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

	topic := Topic{
		Subject: subject,
		Author:  user,
		Created: time.Now().UTC(),
	}
	err = tx.QueryRowContext(ctx, `
		INSERT INTO topics (subject, created, author_id, views_count, comments_count)
		VALUES ($1, $2, $3, 0, 0)
		RETURNING topic_id
	`, topic.Subject, topic.Created, user.UserID).Scan(&topic.TopicID)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot create topic: %s", err)
	}

	comment := Comment{
		TopicID: topic.TopicID,
		Content: content,
		Created: topic.Created,
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

	switch result, err := tx.ExecContext(ctx, `
		UPDATE topics
		SET
			comments_count = (SELECT COUNT(*) - 1 FROM comments WHERE topic_id = $1),
			latest_comment = $2
		WHERE topic_id = $1
	`, comment.TopicID, comment.Created); err {
	case nil:
		if n, err := result.RowsAffected(); err != nil || n != 1 {
			return nil, ErrNotFound
		}
	default:
		return nil, fmt.Errorf("cannot update topic counter: %s", err)
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

func castErr(err error) error {
	switch err {
	case sqldb.ErrNotFound:
		return ErrNotFound
	case sqldb.ErrConstraint:
		return ErrConstraint
	default:
		return err
	}
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
			cu.user_id AS comment_user_id,
			cu.name AS comment_user_name,
			(SELECT COUNT(*) FROM comments WHERE topic_id = c.topic_id AND created < c.created) AS comment_pos
		FROM
			comments c
			INNER JOIN users cu ON c.author_id = cu.user_id
			INNER JOIN topics t ON t.topic_id = c.topic_id
			INNER JOIN users tu ON t.author_id = tu.user_id
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
		&c.Author.UserID,
		&c.Author.Name,
		&commentPos,
	)
	return &t, &c, commentPos, castErr(err)
}

func (s *pgUserStore) Authenticate(ctx context.Context, login, password string) (*User, error) {
	var passhash string
	switch err := s.db.QueryRowContext(ctx, `
		SELECT password
		FROM users
		WHERE name = $1
		LIMIT 1
	`, login).Scan(&passhash); err {
	case nil:
		// all good
	case sqldb.ErrNotFound:
		return nil, ErrNotFound
	default:
		return nil, fmt.Errorf("database: %s", err)
	}

	switch err := bcrypt.CompareHashAndPassword([]byte(passhash), []byte(password)); err {
	case nil:
		// all good
	case bcrypt.ErrMismatchedHashAndPassword:
		return nil, ErrNotFound
	default:
		return nil, fmt.Errorf("bcrypt: %s", err)
	}

	var u User
	err := s.db.QueryRowContext(ctx, `
		SELECT user_id, name
		FROM users
		WHERE name = $1
		LIMIT 1
	`, login).Scan(&u.UserID, &u.Name)
	return &u, castErr(err)
}

func (s *pgUserStore) Register(ctx context.Context, password string, u User) (*User, error) {
	passhash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("cannot hash password: %s", err)
	}
	err = s.db.QueryRowContext(ctx, `
		INSERT INTO users (password, name)
		VALUES ($1, $2)
		RETURNING user_id
	`, passhash, u.Name).Scan(&u.UserID)
	if err := castErr(err); err != nil {
		return nil, err
	}
	return &u, nil
}

func (s *pgUserStore) UserInfo(ctx context.Context, userID int64) (*UserInfo, error) {
	u := UserInfo{
		User: User{UserID: userID},
	}
	err := s.db.QueryRowContext(ctx, `
		SELECT
			u.name,
			(SELECT COUNT(*) FROM topics t WHERE t.author_id = u.user_id) AS topics_count,
			(SELECT COUNT(*) FROM comments c WHERE c.author_id = u.user_id) AS comments_count
		FROM users u
		WHERE u.user_id = $1
		LIMIT 1
	`, userID).Scan(&u.Name, &u.TopicsCount, &u.CommentsCount)
	return &u, castErr(err)
}
