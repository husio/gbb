package gbb

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/husio/gbb/pkg/surf/sqldb"
)

type BBStore interface {
	ListTopics(ctx context.Context, createdLte time.Time, limit int) ([]*Topic, error)
	CreateTopic(ctx context.Context, subject, content string, categoryID int64, userID int64) (*Topic, *Comment, error)
	TopicByID(ctx context.Context, topicID int64) (*Topic, error)
	UpdateTopic(ctx context.Context, topicID int64, subject string) error
	IncrementTopicView(ctx context.Context, postID int64) error
	DeleteTopic(ctx context.Context, topicID int64) error

	ListComments(ctx context.Context, topicID int64, offset, limit int) ([]*Comment, error)
	CommentByID(ctx context.Context, commentID int64) (*Topic, *Comment, int, error)
	CreateComment(ctx context.Context, postID int64, content string, userID int64) (*Comment, error)
	UpdateComment(ctx context.Context, commentID int64, content string) error
	DeleteComment(ctx context.Context, commentID int64) error

	Search(ctx context.Context, searchText string, categories []int64, offset, limit int64) ([]*SearchResult, error)

	ListCategories(ctx context.Context) ([]*Category, error)
	AddCategory(ctx context.Context, name string) error
	RemoveCategory(ctx context.Context, categoryID int64) error

	RegisterUser(ctx context.Context, password string, u User) (*User, error)
	AuthenticateUser(ctx context.Context, login, password string) (*User, error)
	UserInfo(ctx context.Context, userID int64) (*UserInfo, error)
}

type ReadProgressTracker interface {
	LastReads(ctx context.Context, userID int64, topicIDs []int64) (map[int64]*ReadProgress, error)
	Track(context.Context, ReadProgress) error
	MarkAllRead(ctx context.Context, userID int64, now time.Time) error
}

type ReadProgress struct {
	UserID         int64
	TopicID        int64
	CommentID      int64
	CommentCreated time.Time
}

type Category struct {
	CategoryID int64
	Name       string
}

type User struct {
	UserID int64
	Name   string
	Scopes UserScope
}

type UserScope uint16

const (
	adminScope UserScope = 1 << iota
	moderatorScope

	createTopicScope
	createCommentScope
	changeSettingsScope
)

func (s UserScope) String() string {
	return fmt.Sprintf("%b", s)
}

func (s UserScope) Names() []string {
	var names []string
	if s&adminScope != 0 {
		names = append(names, "admin")
	}
	if s&moderatorScope != 0 {
		names = append(names, "moderator")
	}
	if s&createTopicScope != 0 {
		names = append(names, "createTopic")
	}
	if s&createCommentScope != 0 {
		names = append(names, "createComment")
	}
	if s&changeSettingsScope != 0 {
		names = append(names, "changeSettings")
	}
	return names
}

// HasAny returns true if scope contains any of given scopes
func (s UserScope) HasAny(scopes ...UserScope) bool {
	for _, scope := range scopes {
		if s&scope != 0 {
			return true
		}
	}
	return false
}

func (s UserScope) Add(scopes UserScope) UserScope {
	return s | scopes
}

func (s UserScope) Remove(scopes UserScope) UserScope {
	return s & ^scopes
}

func (u *User) Authenticated() bool {
	return u != nil && u.UserID > 0
}

type UserInfo struct {
	User
	TopicsCount   int64
	CommentsCount int64
}

type Topic struct {
	TopicID  int64
	Subject  string
	Created  time.Time
	Updated  time.Time
	Author   User
	Category Category

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

type SearchResult struct {
	Topic   Topic
	Comment Comment
}

var (
	ErrNotFound   = errors.New("not found")
	ErrConstraint = errors.New("constraint")
)

// castErr map sql error to gbb's local representation
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
