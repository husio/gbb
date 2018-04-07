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

CREATE TABLE IF NOT EXISTS posts (
	post_id SERIAL PRIMARY KEY,
	subject TEXT NOT NULL,
	created TIMESTAMPTZ NOT NULL,
	author_id INTEGER NOT NULL REFERENCES users(user_id),

	views_count INTEGER NOT NULL default 0 CHECK (views_count >= 0),
	comments_count INTEGER NOT NULL default 1 CHECK (comments_count >= 0),
	latest_comment TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS comments (
	comment_id SERIAL PRIMARY KEY,
	post_id INTEGER NOT NULL REFERENCES posts(post_id),
	content TEXT NOT NULL,
	created TIMESTAMPTZ NOT NULL,
	author_id INTEGER NOT NULL REFERENCES users(user_id)
);

CREATE INDEX IF NOT EXISTS comments_created_idx ON comments(created);


ALTER TABLE posts ADD COLUMN IF NOT EXISTS latest_comment TIMESTAMPTZ NOT NULL DEFAULT now();

CREATE INDEX IF NOT EXISTS posts_created_idx ON posts(latest_comment);

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
	defer surf.CurrentTrace(ctx).Begin("list posts").Finish()

	surf.LogInfo(ctx, "listing posts",
		"createdLte", createdLte.String())
	var posts []*Topic
	resp, err := s.db.QueryContext(ctx, `
		SELECT
			p.post_id,
			p.subject,
			p.created,
			p.views_count,
			p.comments_count,
			p.author_id,
			u.name
		FROM
			posts p
			INNER JOIN users u ON p.author_id = u.user_id
		WHERE
			p.created <= $1
		ORDER BY
			p.latest_comment DESC
		LIMIT $2
	`, createdLte, limit)
	if err != nil {
		return posts, fmt.Errorf("cannot fetch posts: %s", err)
	}
	defer resp.Close()

	for resp.Next() {
		var p Topic
		if err := resp.Scan(&p.TopicID, &p.Subject, &p.Created, &p.ViewsCount, &p.CommentsCount, &p.Author.UserID, &p.Author.Name); err != nil {
			return posts, fmt.Errorf("cannot scan row: %s", err)
		}

		posts = append(posts, &p)
	}
	return posts, nil
}

func (s *pgBBStore) ListComments(ctx context.Context, postID int64, offset, limit int) (*Topic, []*Comment, error) {
	span := surf.CurrentTrace(ctx).Begin("list comments")
	defer span.Finish()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot open transaction: %s", err)
	}
	defer tx.Rollback()

	var p Topic
	fetchTopicsSpan := span.Begin("query topics",
		"post", fmt.Sprint(postID))
	row := tx.QueryRowContext(ctx, `
		SELECT
			p.post_id,
			p.subject,
			p.created,
			p.views_count,
			p.comments_count,
			u.user_id,
			u.name
		FROM
			posts p
			INNER JOIN users u ON p.author_id = u.user_id
		WHERE
			p.post_id = $1
		LIMIT 1
	`, postID)
	fetchTopicsSpan.Finish()
	switch err := row.Scan(&p.TopicID, &p.Subject, &p.Created, &p.ViewsCount, &p.CommentsCount, &p.Author.UserID, &p.Author.Name); castErr(err) {
	case nil:
		// all good
	case ErrNotFound:
		return nil, nil, ErrNotFound
	default:
		return nil, nil, fmt.Errorf("cannot fetch post: %s", err)
	}

	defer span.Begin("fetch comments").Finish()

	var comments []*Comment
	rows, err := tx.QueryContext(ctx, `
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
			c.post_id = $1
		ORDER BY
			c.created ASC
		LIMIT $2
		OFFSET $3
	`, postID, limit, offset)
	if err != nil {
		return &p, nil, fmt.Errorf("cannot fetch comments: %s", err)
	}
	defer rows.Close()
	for rows.Next() {
		c := Comment{TopicID: p.TopicID}
		if err := rows.Scan(&c.CommentID, &c.Content, &c.Created, &c.Author.UserID, &c.Author.Name); err != nil {
			return &p, comments, fmt.Errorf("cannot scan comment: %s", err)
		}
		comments = append(comments, &c)
	}

	return &p, comments, rows.Err()
}

func (s *pgBBStore) CreateTopic(ctx context.Context, subject, content string, userID int64) (*Topic, *Comment, error) {
	defer surf.CurrentTrace(ctx).Begin("create post").Finish()

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

	post := Topic{
		Subject: subject,
		Author:  user,
		Created: time.Now().UTC(),
	}
	err = tx.QueryRowContext(ctx, `
		INSERT INTO posts (subject, created, author_id, views_count, comments_count)
		VALUES ($1, $2, $3, 0, 0)
		RETURNING post_id
	`, post.Subject, post.Created, user.UserID).Scan(&post.TopicID)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot create post: %s", err)
	}

	comment := Comment{
		TopicID: post.TopicID,
		Content: content,
		Created: post.Created,
		Author:  user,
	}
	err = tx.QueryRowContext(ctx, `
		INSERT INTO comments (post_id, content, created, author_id)
		VALUES ($1, $2, $3, $4)
		RETURNING comment_id
	`, comment.TopicID, comment.Content, comment.Created, user.UserID).Scan(&comment.CommentID)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot create comment: %s", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, nil, fmt.Errorf("cannot commit transaction: %s", err)
	}
	return &post, &comment, nil
}

func (s *pgBBStore) CreateComment(ctx context.Context, postID int64, content string, userID int64) (*Comment, error) {
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
		TopicID: postID,
		Content: content,
		Created: time.Now().UTC(),
		Author:  user,
	}

	err = tx.QueryRowContext(ctx, `
		INSERT INTO comments (post_id, content, created, author_id)
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
		UPDATE posts
		SET
			comments_count = (SELECT COUNT(*) - 1 FROM comments WHERE post_id = $1),
			latest_comment = $2
		WHERE post_id = $1
	`, comment.TopicID, comment.Created); err {
	case nil:
		if n, err := result.RowsAffected(); err != nil || n != 1 {
			return nil, ErrNotFound
		}
	default:
		return nil, fmt.Errorf("cannot update post counter: %s", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("cannot commit transaction: %s", err)
	}
	return &comment, nil
}

func (s *pgBBStore) IncrementTopicView(ctx context.Context, postID int64) error {
	defer surf.CurrentTrace(ctx).Begin("increment post view").Finish()

	_, err := s.db.ExecContext(ctx, `
		UPDATE posts SET views_count = views_count + 1
		WHERE post_id = $1
	`, postID)
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
			(SELECT COUNT(*) FROM posts p WHERE p.author_id = u.user_id) AS posts_count,
			(SELECT COUNT(*) FROM comments c WHERE c.author_id = u.user_id) AS comments_count
		FROM users u
		WHERE u.user_id = $1
		LIMIT 1
	`, userID).Scan(&u.Name, &u.TopicsCount, &u.CommentsCount)
	return &u, castErr(err)
}
