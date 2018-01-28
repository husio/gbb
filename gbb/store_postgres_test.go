package gbb

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand"
	"testing"
	"time"
)

func TestListPostsEmpty(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	db := createDatabase(t)
	defer db.Close()

	store := NewPostgresStore(db)
	if _, err := store.ListPosts(ctx, time.Now()); err != nil {
		t.Fatalf("cannot list posts: %s", err)
	}
}

func TestCreatePost(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	db := createDatabase(t)
	defer db.Close()

	ensureUser(t, db, 999, "Bobby")

	store := NewPostgresStore(db)
	post, comment, err := store.CreatePost(ctx, "first", "IMO", 999)
	if err != nil {
		t.Fatalf("cannot create post: %s", err)
	}

	if comment.PostID != post.PostID {
		t.Errorf("comment.PostID != post.PostID: %q != %q", comment.PostID, post.PostID)
	}
	if post.Title != "first" {
		t.Errorf("invalid title: %q", post.Title)
	}
	if post.Author.UserID != 999 || post.Author.Name != "Bobby" {
		t.Errorf("invalid post author: %+v", post.Author)
	}
	if comment.Content != "IMO" {
		t.Errorf("invalid title: %q", post.Title)
	}
	if comment.Author.UserID != 999 || comment.Author.Name != "Bobby" {
		t.Errorf("invalid comment author: %+v", comment.Author)
	}
}

func TestCreateComment(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	db := createDatabase(t)
	defer db.Close()

	ensureUser(t, db, 999, "Bobby")

	store := NewPostgresStore(db)
	post, _, err := store.CreatePost(ctx, "first", "IMO", 999)
	if err != nil {
		t.Fatalf("cannot create post: %s", err)
	}

	comment, err := store.CreateComment(ctx, post.PostID, "IMO 2", 999)
	if err != nil {
		t.Fatalf("cannot create comment: %s", err)
	}

	if comment.PostID != post.PostID {
		t.Errorf("comment.PostID != post.PostID: %q != %q", comment.PostID, post.PostID)
	}
	if comment.Content != "IMO 2" {
		t.Errorf("invalid title: %q", post.Title)
	}
	if comment.Author.UserID != 999 || comment.Author.Name != "Bobby" {
		t.Errorf("invalid comment author: %+v", comment.Author)
	}
}

func TestCreateCommentPostNotFound(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	db := createDatabase(t)
	defer db.Close()

	ensureUser(t, db, 999, "Bobby")

	store := NewPostgresStore(db)
	if c, err := store.CreateComment(ctx, 1244141412, "IMO", 999); err != ErrNotFound {
		t.Fatalf("want ErrNotFound, got %q and %+v", err, c)
	}
}

func createDatabase(t *testing.T) *sql.DB {
	t.Helper()

	rootdbConf := DBOpts{
		Host:    "localhost",
		DBName:  "postgres",
		Port:    5432,
		SSLMode: "disable",
		User:    "postgres",
	}
	testdbConf := DBOpts{
		Host:    "localhost",
		DBName:  fmt.Sprintf("test_database_%d_%d", time.Now().UnixNano(), rand.Intn(1000000)),
		Port:    5432,
		SSLMode: "disable",
		User:    "postgres",
	}

	rootdb, err := sql.Open("postgres", rootdbConf.String())
	if err != nil {
		t.Skipf("cannot connect to postgres: %s", err)
	}
	defer rootdb.Close()

	if err := rootdb.Ping(); err != nil {
		t.Skipf("cannot ping postgres: %s", err)
	}

	if _, err := rootdb.Exec(fmt.Sprintf("CREATE DATABASE %s", testdbConf.DBName)); err != nil {
		t.Fatalf("cannot create database: %s", err)
	}

	testdb, err := sql.Open("postgres", testdbConf.String())
	if err != nil {
		t.Fatalf("cannot connect to created database: %s", err)
	}

	t.Logf("test database created: %s", testdbConf.DBName)

	if err := EnsureSchema(testdb); err != nil {
		t.Fatalf("cannot ensure schema: %s", err)
	}

	return testdb
}

// DBOpts defines options for test database connections
type DBOpts struct {
	User    string
	Port    int
	Host    string
	SSLMode string
	DBName  string
}

func (o DBOpts) String() string {
	return fmt.Sprintf(
		"host='%s' port='%d' user='%s' dbname='%s' sslmode='%s'",
		o.Host, o.Port, o.User, o.DBName, o.SSLMode)
}

func ensureUser(t *testing.T, db *sql.DB, userID int, name string) {
	t.Helper()

	if _, err := db.Exec(`
		INSERT INTO users (user_id, name) VALUES ($1, $2)
		`, userID, name); err != nil {
		t.Fatalf("cannot create user %d %q: %s", userID, name, err)
	}
}
