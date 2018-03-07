package surf

import (
	"context"
	"time"
)

func TraceCache(cache CacheService, prefix string) CacheService {
	return &tracedCache{
		prefix: prefix,
		cache:  cache,
	}
}

type tracedCache struct {
	prefix string
	cache  CacheService
}

func (c *tracedCache) Get(ctx context.Context, key string, dest interface{}) error {
	span := CurrentTrace(ctx).Begin(c.prefix + " Get")
	err := c.cache.Get(ctx, key, dest)
	if err != nil {
		span.Finish(
			"key", key,
			"err", err.Error())
	} else {
		span.Finish("key", key)
	}
	return err
}

func (c *tracedCache) Set(ctx context.Context, key string, value interface{}, exp time.Duration) error {
	span := CurrentTrace(ctx).Begin(c.prefix + " Set")
	err := c.cache.Set(ctx, key, value, exp)
	if err != nil {
		span.Finish(
			"key", key,
			"err", err.Error())
	} else {
		span.Finish("key", key)
	}
	return err
}

func (c *tracedCache) SetNx(ctx context.Context, key string, value interface{}, exp time.Duration) error {
	span := CurrentTrace(ctx).Begin(c.prefix + " SetNx")
	err := c.cache.SetNx(ctx, key, value, exp)
	if err != nil {
		span.Finish(
			"key", key,
			"err", err.Error())
	} else {
		span.Finish("key", key)
	}
	return err
}

func (c *tracedCache) Del(ctx context.Context, key string) error {
	span := CurrentTrace(ctx).Begin(c.prefix + " Del")
	err := c.cache.Del(ctx, key)
	if err != nil {
		span.Finish(
			"key", key,
			"err", err.Error())
	} else {
		span.Finish("key", key)
	}
	return err
}
