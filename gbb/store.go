package gbb

import (
	"context"
	"errors"
	"time"
)

type BBStore interface {
	ListPosts(ctx context.Context, createdLte time.Time) ([]*Post, error)
	ListComments(ctx context.Context, postID int64, createdLte time.Time) (*Post, []*Comment, error)
	CreatePost(ctx context.Context, title, content string, userID int64) (*Post, *Comment, error)
	CreateComment(ctx context.Context, postID int64, content string, userID int64) (*Comment, error)

	IncrementPostView(ctx context.Context, postID int64) error
}

type User struct {
	UserID int64
	Name   string
}

type Post struct {
	PostID  int64
	Title   string
	Created time.Time
	Author  User

	CommentsCount int64
	ViewsCount    int64
}

type Comment struct {
	CommentID int64
	PostID    int64
	Content   string
	Created   time.Time
	Author    User
}

var (
	ErrNotFound   = errors.New("not found")
	ErrConstraint = errors.New("constraint")
)
