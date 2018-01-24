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
	span := CurrentTrace(ctx).Start(c.prefix+" Get", nil)
	err := c.cache.Get(ctx, key, dest)
	if err != nil {
		span.Finish(map[string]string{
			"key": key,
			"err": err.Error(),
		})
	} else {
		span.Finish(map[string]string{
			"key": key,
		})
	}
	return err
}

func (c *tracedCache) Set(ctx context.Context, key string, value interface{}, exp time.Duration) error {
	span := CurrentTrace(ctx).Start(c.prefix+" Set", nil)
	err := c.cache.Set(ctx, key, value, exp)
	if err != nil {
		span.Finish(map[string]string{
			"key": key,
			"err": err.Error(),
		})
	} else {
		span.Finish(map[string]string{
			"key": key,
		})
	}
	return err
}

func (c *tracedCache) SetNx(ctx context.Context, key string, value interface{}, exp time.Duration) error {
	span := CurrentTrace(ctx).Start(c.prefix+" SetNx", nil)
	err := c.cache.SetNx(ctx, key, value, exp)
	if err != nil {
		span.Finish(map[string]string{
			"key": key,
			"err": err.Error(),
		})
	} else {
		span.Finish(map[string]string{
			"key": key,
		})
	}
	return err
}

func (c *tracedCache) Del(ctx context.Context, key string) error {
	span := CurrentTrace(ctx).Start(c.prefix+" Del", nil)
	err := c.cache.Del(ctx, key)
	if err != nil {
		span.Finish(map[string]string{
			"key": key,
			"err": err.Error(),
		})
	} else {
		span.Finish(map[string]string{
			"key": key,
		})
	}
	return err
}
