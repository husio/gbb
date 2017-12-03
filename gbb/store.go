package gbb

import (
	"context"
	"errors"
	"time"
)

type BBStore interface {
	ListPosts(ctx context.Context, createdLte time.Time) ([]*Post, error)
	ListComments(ctx context.Context, postID string, createdLte time.Time) (*Post, []*Comment, error)
	CreatePost(ctx context.Context, title, content, userID string) (*Post, *Comment, error)
	CreateComment(ctx context.Context, postID, content, userID string) (*Comment, error)

	IncrementPostView(ctx context.Context, postID string) error
}

type User struct {
	UserID string
	Name   string
}

type Post struct {
	PostID  string
	Title   string
	Created time.Time
	Author  User

	CommentsCount int64
	ViewsCount    int64
}

type Comment struct {
	CommentID string
	PostID    string
	Content   string
	Created   time.Time
	Author    User
}

var (
	ErrNotFound   = errors.New("not found")
	ErrConstraint = errors.New("constraint")
)
