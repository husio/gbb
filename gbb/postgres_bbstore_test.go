package gbb

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand"
	"testing"
	"time"
)

func TestListTopicsEmpty(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	db := createDatabase(t)
	defer db.Close()

	store, err := NewPostgresBBStore(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ListTopics(ctx, time.Now(), 100); err != nil {
		t.Fatalf("cannot list posts: %s", err)
	}
}

func TestCreateTopic(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	db := createDatabase(t)
	defer db.Close()

	store, err := NewPostgresBBStore(db)
	if err != nil {
		t.Fatal(err)
	}

	ensureUser(t, db, 999, "Bobby")

	topic, comment, err := store.CreateTopic(ctx, "first", "IMO", 1, 999)
	if err != nil {
		t.Fatalf("cannot create topic: %s", err)
	}

	if comment.TopicID != topic.TopicID {
		t.Errorf("comment.TopicID != topic.TopicID: %q != %q", comment.TopicID, topic.TopicID)
	}
	if topic.Subject != "first" {
		t.Errorf("invalid subject: %q", topic.Subject)
	}
	if topic.Author.UserID != 999 || topic.Author.Name != "Bobby" {
		t.Errorf("invalid topic author: %+v", topic.Author)
	}
	if comment.Content != "IMO" {
		t.Errorf("invalid subject: %q", topic.Subject)
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

	store, err := NewPostgresBBStore(db)
	if err != nil {
		t.Fatal(err)
	}

	ensureUser(t, db, 999, "Bobby")

	topic, _, err := store.CreateTopic(ctx, "first", "IMO", 1, 999)
	if err != nil {
		t.Fatalf("cannot create topic: %s", err)
	}

	comment, err := store.CreateComment(ctx, topic.TopicID, "IMO 2", 999)
	if err != nil {
		t.Fatalf("cannot create comment: %s", err)
	}

	if comment.TopicID != topic.TopicID {
		t.Errorf("comment.TopicID != topic.TopicID: %q != %q", comment.TopicID, topic.TopicID)
	}
	if comment.Content != "IMO 2" {
		t.Errorf("invalid title: %q", topic.Subject)
	}
	if comment.Author.UserID != 999 || comment.Author.Name != "Bobby" {
		t.Errorf("invalid comment author: %+v", comment.Author)
	}
}

func TestCreateCommentTopicNotFound(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	db := createDatabase(t)
	defer db.Close()

	store, err := NewPostgresBBStore(db)
	if err != nil {
		t.Fatal(err)
	}

	ensureUser(t, db, 999, "Bobby")

	if c, err := store.CreateComment(ctx, 1244141412, "IMO", 999); !ErrTopicNotFound.Is(err) {
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
		INSERT INTO users (user_id, name, password) VALUES ($1, $2, '-')
		`, userID, name); err != nil {
		t.Fatalf("cannot create user %d %q: %s", userID, name, err)
	}
}
