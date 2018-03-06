package gbb

import (
	"context"
	"errors"
	"time"

	"github.com/husio/gbb/pkg/surf"
)

func CurrentUser(ctx context.Context, boundCache surf.CacheService) (*User, error) {
	var u User

	switch err := boundCache.Get(ctx, "user", &u); err {
	case nil:
		return &u, nil
	case surf.ErrMiss:
		return nil, ErrUnauthenticated
	default:
		return nil, err
	}
}

var ErrUnauthenticated = errors.New("not authenticated")

func Login(ctx context.Context, boundCache surf.CacheService, u User) error {
	return boundCache.Set(ctx, "user", u, 6*time.Hour)
}

func Logout(ctx context.Context, boundCache surf.CacheService) error {
	return boundCache.Del(ctx, "user")
}
