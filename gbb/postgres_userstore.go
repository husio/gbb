package gbb

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/husio/gbb/pkg/surf/sqldb"
	"golang.org/x/crypto/bcrypt"
)

func NewPostgresUserStore(db *sql.DB) UserStore {
	return &pgUserStore{
		db: sqldb.PostgresDatabase(db),
	}
}

type pgUserStore struct {
	db sqldb.Database
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
		SELECT user_id, name, scopes
		FROM users
		WHERE name = $1
		LIMIT 1
	`, login).Scan(&u.UserID, &u.Name, &u.Scopes)
	return &u, castErr(err)
}

func (s *pgUserStore) Register(ctx context.Context, password string, u User) (*User, error) {
	passhash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("cannot hash password: %s", err)
	}
	err = s.db.QueryRowContext(ctx, `
		INSERT INTO users (password, name, scopes)
		VALUES ($1, $2, $3)
		RETURNING user_id
	`, passhash, u.Name, u.Scopes).Scan(&u.UserID)
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
			u.scopes,
			(SELECT COUNT(*) FROM topics t WHERE t.author_id = u.user_id) AS topics_count,
			(SELECT COUNT(*) FROM comments c WHERE c.author_id = u.user_id) AS comments_count
		FROM users u
		WHERE u.user_id = $1
		LIMIT 1
	`, userID).Scan(
		&u.Name,
		&u.Scopes,
		&u.TopicsCount,
		&u.CommentsCount)
	return &u, castErr(err)
}
