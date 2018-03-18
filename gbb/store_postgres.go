package gbb

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/husio/gbb/pkg/surf"
	"github.com/husio/gbb/pkg/surf/sqldb"
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
		db: db,
	}
}

type pgUserStore struct {
	db *sql.DB
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

func (s *pgBBStore) ListPosts(ctx context.Context, createdLte time.Time, limit int) ([]*Post, error) {
	defer surf.CurrentTrace(ctx).Begin("list posts").Finish()

	surf.Info(ctx, "listing posts",
		"createdLte", createdLte.String())
	var posts []*Post
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
		var p Post
		if err := resp.Scan(&p.PostID, &p.Subject, &p.Created, &p.ViewsCount, &p.CommentsCount, &p.Author.UserID, &p.Author.Name); err != nil {
			return posts, fmt.Errorf("cannot scan row: %s", err)
		}

		posts = append(posts, &p)
	}
	return posts, nil
}

func (s *pgBBStore) ListComments(ctx context.Context, postID int64, offset, limit int) (*Post, []*Comment, error) {
	span := surf.CurrentTrace(ctx).Begin("list comments")
	defer span.Finish()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot open transaction: %s", err)
	}
	defer tx.Rollback()

	var p Post
	fetchPostSpan := span.Begin("query post",
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
	fetchPostSpan.Finish()
	switch err := row.Scan(&p.PostID, &p.Subject, &p.Created, &p.ViewsCount, &p.CommentsCount, &p.Author.UserID, &p.Author.Name); castErr(err) {
	case nil:
		// all good
	case ErrNotFound:
		return nil, nil, ErrNotFound
	default:
		return nil, nil, fmt.Errorf("cannot fetch post: %s", err)
	}

	var comments []*Comment
	fetchCommentSpan := span.Begin("fetch comments")
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
	`, p.PostID, limit, offset)
	fetchCommentSpan.Finish()
	if err != nil {
		return &p, nil, fmt.Errorf("cannot fetch comments: %s", err)
	}
	defer rows.Close()
	for rows.Next() {
		c := Comment{PostID: p.PostID}
		if err := rows.Scan(&c.CommentID, &c.Content, &c.Created, &c.Author.UserID, &c.Author.Name); err != nil {
			return &p, comments, fmt.Errorf("cannot scan comment: %s", err)
		}
		comments = append(comments, &c)
	}

	return &p, comments, nil
}

func (s *pgBBStore) CreatePost(ctx context.Context, subject, content string, userID int64) (*Post, *Comment, error) {
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

	post := Post{
		Subject: subject,
		Author:  user,
		Created: time.Now().UTC(),
	}
	err = tx.QueryRowContext(ctx, `
		INSERT INTO posts (subject, created, author_id, views_count, comments_count)
		VALUES ($1, $2, $3, 0, 0)
		RETURNING post_id
	`, post.Subject, post.Created, user.UserID).Scan(&post.PostID)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot create post: %s", err)
	}

	comment := Comment{
		PostID:  post.PostID,
		Content: content,
		Created: post.Created,
		Author:  user,
	}
	err = tx.QueryRowContext(ctx, `
		INSERT INTO comments (post_id, content, created, author_id)
		VALUES ($1, $2, $3, $4)
		RETURNING comment_id
	`, comment.PostID, comment.Content, comment.Created, user.UserID).Scan(&comment.CommentID)
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
		PostID:  postID,
		Content: content,
		Created: time.Now().UTC(),
		Author:  user,
	}

	switch result, err := tx.ExecContext(ctx, `
		UPDATE posts
		SET
			comments_count = comments_count + 1,
			latest_comment = $2
		WHERE post_id = $1
	`, comment.PostID, comment.Created); err {
	case nil:
		if n, err := result.RowsAffected(); err != nil || n != 1 {
			return nil, ErrNotFound
		}
	default:
		return nil, fmt.Errorf("cannot update post counter: %s", err)
	}

	err = tx.QueryRowContext(ctx, `
		INSERT INTO comments (post_id, content, created, author_id)
		VALUES ($1, $2, $3, $4)
		RETURNING comment_id
	`, comment.PostID, comment.Content, comment.Created, user.UserID).Scan(&comment.CommentID)
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

func (s *pgBBStore) IncrementPostView(ctx context.Context, postID int64) error {
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
	var u User
	// YOLO!
	// add password authentication
	err := s.db.QueryRow(`
		SELECT user_id, name
		FROM users
		WHERE name = $1
		LIMIT 1
	`, login).Scan(&u.UserID, &u.Name)

	return &u, castErr(err)
}

func (s *pgUserStore) Register(ctx context.Context, password string, u User) (*User, error) {
	err := s.db.QueryRow(`
		INSERT INTO users (password, name)
		VALUES ($1, $2)
		RETURNING user_id
	`, password, u.Name).Scan(&u.UserID)
	if err := castErr(err); err != nil {
		return nil, err
	}
	return &u, nil
}
