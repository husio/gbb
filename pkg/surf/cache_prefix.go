package surf

import (
	"context"
	"time"
)

func PrefixCache(cache CacheService, prefix string) CacheService {
	if prefix == "" {
		return cache
	}
	return &prefixedCache{
		cache:  cache,
		prefix: prefix,
	}
}

type prefixedCache struct {
	cache  CacheService
	prefix string
}

func (c *prefixedCache) Get(ctx context.Context, key string, dest interface{}) error {
	return c.cache.Get(ctx, c.prefix+key, dest)
}

func (c *prefixedCache) Set(ctx context.Context, key string, value interface{}, exp time.Duration) error {
	return c.cache.Set(ctx, c.prefix+key, value, exp)
}

func (c *prefixedCache) SetNx(ctx context.Context, key string, value interface{}, exp time.Duration) error {
	return c.cache.SetNx(ctx, c.prefix+key, value, exp)
}

func (c *prefixedCache) Del(ctx context.Context, key string) error {
	return c.cache.Del(ctx, c.prefix+key)
}
