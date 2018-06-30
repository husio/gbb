package gbb

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/husio/gbb/pkg/surf"
)

func CurrentUser(ctx context.Context, boundCache surf.CacheService) (*User, error) {
	var u User

	span := surf.CurrentTrace(ctx).Begin("current user")

	switch err := boundCache.Get(ctx, "user", &u); err {
	case nil:
		span.Finish(
			"id", fmt.Sprint(u.UserID),
			"name", u.Name)
		surf.LogInfo(ctx, "authenticated",
			"name", u.Name,
			"userId", fmt.Sprint(u.UserID))
		return &u, nil
	case surf.ErrMiss:
		span.Finish()
		return nil, ErrUnauthenticated
	default:
		span.Finish()
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
