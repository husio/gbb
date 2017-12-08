package gbb

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/husio/gbb/pkg/surf"
	"github.com/mattn/go-sqlite3"
)

func NewSqliteStore(db *sql.DB) BBStore {
	return &sqliteStore{
		db: db,
	}
}

type sqliteStore struct {
	db *sql.DB
}

const schema = `
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS users (
	user_id TEXT PRIMARY KEY,
	name TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS posts (
	post_id TEXT PRIMARY KEY,
	title TEXT NOT NULL,
	created DATETIME NOT NULL,
	author_id TEXT NOT NULL,

	views_count INT NOT NULL default 0,
	comments_count INT NOT NULL default 1,

	FOREIGN KEY(author_id) REFERENCES users(user_id)
);

CREATE INDEX IF NOT EXISTS posts_created_idx ON posts(created);

CREATE TABLE IF NOT EXISTS comments (
	comment_id TEXT PRIMARY KEY,
	post_id TEXT NOT NULL,
	content TEXT NOT NULL,
	created DATETIME NOT NULL,
	author_id TEXT NOT NULL,

	FOREIGN KEY(author_id) REFERENCES users(user_id),
	FOREIGN KEY(post_id) REFERENCES posts(post_id)
);

CREATE INDEX IF NOT EXISTS comments_created_idx ON comments(created);

INSERT OR IGNORE INTO users VALUES ('rickybobby', "RickyBobby");
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

func (s *sqliteStore) ListPosts(ctx context.Context, createdLte time.Time) ([]*Post, error) {
	defer surf.CurrentSpan(ctx).StartSpan("ListPosts", nil).FinishSpan(nil)

	var posts []*Post
	resp, err := s.db.QueryContext(ctx, `
		SELECT
			p.post_id,
			p.title,
			p.created,
			p.views_count,
			p.comments_count,
			p.author_id,
			u.name
		FROM
			posts p
			INNER JOIN users u ON p.author_id = u.user_id
		WHERE
			p.created <= ?
		ORDER BY
			p.created ASC
		LIMIT 1000
	`, createdLte)
	if err != nil {
		return posts, fmt.Errorf("cannot fetch posts: %s", err)
	}
	defer resp.Close()

	for resp.Next() {
		var p Post
		if err := resp.Scan(&p.PostID, &p.Title, &p.Created, &p.ViewsCount, &p.CommentsCount, &p.Author.UserID, &p.Author.Name); err != nil {
			return posts, fmt.Errorf("cannot scan row: %s", err)
		}

		surf.Info(ctx, "example log while scanning posts",
			"post", fmt.Sprint(p.PostID),
			"post.title", p.Title)

		posts = append(posts, &p)
	}
	return posts, nil
}

func (s *sqliteStore) ListComments(ctx context.Context, postID string, createdLte time.Time) (*Post, []*Comment, error) {
	span := surf.CurrentSpan(ctx).StartSpan("ListComments", nil)
	defer span.FinishSpan(nil)

	tx, err := s.db.Begin()
	if err != nil {
		return nil, nil, fmt.Errorf("cannot open transaction: %s", err)
	}
	defer tx.Rollback()

	var p Post
	fetchPostSpan := span.StartSpan("query post", map[string]string{"post": postID})
	row := tx.QueryRowContext(ctx, `
		SELECT
			p.post_id,
			p.title,
			p.created,
			p.views_count,
			p.comments_count,
			u.user_id,
			u.name
		FROM
			posts p
			INNER JOIN users u ON p.author_id = u.user_id
		WHERE
			p.post_id = ?
		LIMIT 1
	`, postID)
	fetchPostSpan.FinishSpan(nil)
	switch err := row.Scan(&p.PostID, &p.Title, &p.Created, &p.ViewsCount, &p.CommentsCount, &p.Author.UserID, &p.Author.Name); castErr(err) {
	case nil:
		// all good
	case ErrNotFound:
		return nil, nil, ErrNotFound
	default:
		return nil, nil, fmt.Errorf("cannot fetch post: %s", err)
	}

	var comments []*Comment
	defer span.StartSpan("fetch comments", nil).FinishSpan(nil)
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
			c.post_id = ?
			AND c.created <= ?
		ORDER BY
			c.created ASC
		LIMIT
			1000
	`, p.PostID, createdLte)
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

func (s *sqliteStore) CreatePost(ctx context.Context, title, content, userID string) (*Post, *Comment, error) {
	defer surf.CurrentSpan(ctx).StartSpan("CreatePost", nil).FinishSpan(nil)

	tx, err := s.db.Begin()
	if err != nil {
		return nil, nil, fmt.Errorf("cannot open transaction: %s", err)
	}
	defer tx.Rollback()

	user := User{
		UserID: userID,
	}
	err = tx.QueryRowContext(ctx, `
		SELECT name FROM users WHERE user_id = ? LIMIT 1
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
		PostID:  generateID(),
		Title:   title,
		Author:  user,
		Created: time.Now().UTC(),
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO posts (post_id, title, created, author_id, views_count, comments_count)
		VALUES (?, ?, ?, ?, 0, 1)
	`, post.PostID, post.Title, post.Created, user.UserID)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot create post: %s", err)
	}

	comment := Comment{
		PostID:    post.PostID,
		CommentID: generateID(),
		Content:   content,
		Created:   post.Created,
		Author:    user,
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO comments (comment_id, post_id, content, created, author_id)
		VALUES (?, ?, ?, ?, ?)
	`, comment.CommentID, comment.PostID, comment.Content, comment.Created, user.UserID)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot create comment: %s", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, nil, fmt.Errorf("cannot commit transaction: %s", err)
	}
	return &post, &comment, nil
}

func (s *sqliteStore) CreateComment(ctx context.Context, postID, content, userID string) (*Comment, error) {
	defer surf.CurrentSpan(ctx).StartSpan("CreateComment", nil).FinishSpan(nil)

	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("cannot open transaction: %s", err)
	}
	defer tx.Rollback()

	user := User{
		UserID: userID,
	}
	err = tx.QueryRowContext(ctx, `
		SELECT name FROM users WHERE user_id = ? LIMIT 1
	`, userID).Scan(&user.Name)
	switch castErr(err) {
	case nil:
		// all good
	case ErrNotFound:
		return nil, fmt.Errorf("user not found: %s", err)
	default:
		return nil, fmt.Errorf("cannot fetch user: %s", err)
	}

	switch result, err := tx.ExecContext(ctx, `UPDATE posts SET comments_count = comments_count + 1 WHERE post_id = ?`, postID); err {
	case nil:
		if n, err := result.RowsAffected(); err != nil || n != 1 {
			return nil, ErrNotFound
		}
	default:
		return nil, fmt.Errorf("cannot update post counter: %s", err)
	}

	comment := Comment{
		PostID:    postID,
		CommentID: generateID(),
		Content:   content,
		Created:   time.Now().UTC(),
		Author:    user,
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO comments (comment_id, post_id, content, created, author_id)
		VALUES (?, ?, ?, ?, ?)
	`, comment.CommentID, comment.PostID, comment.Content, comment.Created, user.UserID)
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

func (s *sqliteStore) IncrementPostView(ctx context.Context, postID string) error {
	defer surf.CurrentSpan(ctx).StartSpan("IncrementPostView", nil).FinishSpan(nil)

	_, err := s.db.ExecContext(ctx, `
		UPDATE posts SET views_count = views_count + 1
		WHERE post_id = ?
	`, postID)
	if err != nil {
		return fmt.Errorf("cannot execute query: %s", err)
	}
	// it does not matter that if counter was incremented or not -
	// successfult query execution is good enough for this use case
	return nil
}

func castErr(err error) error {
	if err == sql.ErrNoRows {
		return ErrNotFound
	}

	if e, ok := err.(sqlite3.Error); ok {
		switch e.Code {
		case sqlite3.ErrConstraint:
			return ErrConstraint
		}
	}

	return err
}
