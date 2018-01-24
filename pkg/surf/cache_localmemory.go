package surf

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

type LocalMemCache struct {
	mu  sync.Mutex
	mem map[string]*cacheitem
}

type cacheitem struct {
	Key   string
	Value json.RawMessage
	ExpAt time.Time
}

var _ CacheService = (*LocalMemCache)(nil)

// NewLocalMemCache returns local memory cache intance. This is strictly for
// testing and must not be used for end application.
func NewLocalMemCache() *LocalMemCache {
	return &LocalMemCache{
		mem: make(map[string]*cacheitem),
	}
}

func (c *LocalMemCache) Get(ctx context.Context, key string, dest interface{}) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	it, ok := c.mem[key]
	if !ok {
		return ErrMiss
	}
	if it.ExpAt.Before(time.Now()) {
		delete(c.mem, key)
		return ErrMiss
	}
	return json.Unmarshal(it.Value, dest)
}

func (c *LocalMemCache) Set(ctx context.Context, key string, value interface{}, exp time.Duration) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}

	it := cacheitem{
		Key:   key,
		Value: b,
		ExpAt: time.Now().Add(exp),
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.mem[key] = &it
	return nil
}

func (c *LocalMemCache) SetNx(ctx context.Context, key string, value interface{}, exp time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if it, ok := c.mem[key]; ok {
		if it.ExpAt.After(time.Now()) {
			return ErrConflict
		}
	}

	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	it := cacheitem{
		Key:   key,
		Value: b,
		ExpAt: time.Now().Add(exp),
	}
	c.mem[key] = &it
	return nil
}

func (c *LocalMemCache) Del(ctx context.Context, key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, ok := c.mem[key]; !ok {
		return ErrMiss
	}
	delete(c.mem, key)
	return nil
}

func (c *LocalMemCache) Flush() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.mem = make(map[string]*cacheitem)
}

func (c *LocalMemCache) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	c.mu.Lock()
	b, err := json.MarshalIndent(c.mem, "", "\t")
	c.mu.Unlock()

	if err != nil {
		fmt.Fprintln(w, `{"errors": ["cannot encode cache"]}`)
	} else {
		w.Write(b)
	}
}
