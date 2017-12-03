package gbb

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

func TestListPostsEmpty(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	db := createDatabase(t)
	defer db.Close()

	store := NewSqliteStore(db)
	if _, err := store.ListPosts(ctx, time.Now()); err != nil {
		t.Fatalf("cannot list posts: %s", err)
	}
}

func TestCreatePost(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	db := createDatabase(t)
	defer db.Close()

	ensureUser(t, db, "bob1", "Bobby")

	store := NewSqliteStore(db)
	post, comment, err := store.CreatePost(ctx, "first", "IMO", "bob1")
	if err != nil {
		t.Fatalf("cannot create post: %s", err)
	}

	if comment.PostID != post.PostID {
		t.Errorf("comment.PostID != post.PostID: %q != %q", comment.PostID, post.PostID)
	}
	if post.Title != "first" {
		t.Errorf("invalid title: %q", post.Title)
	}
	if post.Author.UserID != "bob1" || post.Author.Name != "Bobby" {
		t.Errorf("invalid post author: %+v", post.Author)
	}
	if comment.Content != "IMO" {
		t.Errorf("invalid title: %q", post.Title)
	}
	if comment.Author.UserID != "bob1" || comment.Author.Name != "Bobby" {
		t.Errorf("invalid comment author: %+v", comment.Author)
	}
}

func TestCreateComment(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	db := createDatabase(t)
	defer db.Close()

	ensureUser(t, db, "bob1", "Bobby")

	store := NewSqliteStore(db)
	post, _, err := store.CreatePost(ctx, "first", "IMO", "bob1")
	if err != nil {
		t.Fatalf("cannot create post: %s", err)
	}

	comment, err := store.CreateComment(ctx, post.PostID, "IMO 2", "bob1")
	if err != nil {
		t.Fatalf("cannot create comment: %s", err)
	}

	if comment.PostID != post.PostID {
		t.Errorf("comment.PostID != post.PostID: %q != %q", comment.PostID, post.PostID)
	}
	if comment.Content != "IMO 2" {
		t.Errorf("invalid title: %q", post.Title)
	}
	if comment.Author.UserID != "bob1" || comment.Author.Name != "Bobby" {
		t.Errorf("invalid comment author: %+v", comment.Author)
	}
}

func TestCreateCommentPostNotFound(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	db := createDatabase(t)
	defer db.Close()

	ensureUser(t, db, "bob1", "Bobby")

	store := NewSqliteStore(db)
	if c, err := store.CreateComment(ctx, "does-not-existts", "IMO", "bob1"); err != ErrNotFound {
		t.Fatalf("want ErrNotFound, got %q and %+v", err, c)
	}
}

func createDatabase(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("cannot open database: %s", err)
	}

	if err := EnsureSchema(db); err != nil {
		t.Fatalf("cannot ensure schema: %s", err)
	}
	return db
}

func ensureUser(t *testing.T, db *sql.DB, userID, name string) {
	t.Helper()

	if _, err := db.Exec(`INSERT INTO users (user_id, name) VALUES (?, ?)`, userID, name); err != nil {
		t.Fatalf("cannot create user %q: %s", userID, err)
	}
}
