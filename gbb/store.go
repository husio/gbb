package gbb

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"
)

type UserStore interface {
	Register(ctx context.Context, password string, u User) (*User, error)
	Authenticate(ctx context.Context, login, password string) (*User, error)
	UserInfo(ctx context.Context, userID int64) (*UserInfo, error)
}

type BBStore interface {
	ListTopics(ctx context.Context, createdLte time.Time, limit int) ([]*Topic, error)
	TopicByID(ctx context.Context, topicID int64) (*Topic, error)
	ListComments(ctx context.Context, topicID int64, offset, limit int) ([]*Comment, error)
	CreateTopic(ctx context.Context, subject, content string, userID int64) (*Topic, *Comment, error)
	CreateComment(ctx context.Context, postID int64, content string, userID int64) (*Comment, error)

	IncrementTopicView(ctx context.Context, postID int64) error
}

type User struct {
	UserID int64
	Name   string
}

type UserInfo struct {
	User
	TopicsCount   int64
	CommentsCount int64
}

type Topic struct {
	TopicID int64
	Subject string
	Created time.Time
	Author  User

	CommentsCount int64
	ViewsCount    int64
}

func (t *Topic) SlugInfo() string {
	info := t.Created.Format("2006-01-02") + "/" + slugRx.ReplaceAllString(t.Subject, "-")
	if len(info) > 300 {
		info = info[:300]
	}
	return fmt.Sprintf("%s-%d", info, t.CommentsCount)
}

var slugRx = regexp.MustCompile(`[^a-zA-Z0-9\-_]+`)

type Comment struct {
	CommentID int64
	TopicID   int64
	Content   string
	Created   time.Time
	Author    User
}

var (
	ErrNotFound   = errors.New("not found")
	ErrConstraint = errors.New("constraint")
)
