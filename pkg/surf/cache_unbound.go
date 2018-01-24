package surf

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"strings"
)

func NewUnboundCache(cache CacheService, key string) UnboundCacheService {
	return &unboundCacheService{
		cache: cache,
		key:   key,
	}
}

type unboundCacheService struct {
	key   string
	cache CacheService
}

func (c *unboundCacheService) Bind(w http.ResponseWriter, r *http.Request) CacheService {
	sessionID := c.sessionID(r)
	http.SetCookie(w, &http.Cookie{
		Name:  c.key,
		Value: sessionID,
		Path:  "/",
	})
	return PrefixCache(c.cache, sessionID)
}

func (c unboundCacheService) sessionID(r *http.Request) string {
	if c, err := r.Cookie(c.key); err == nil {
		return c.Value
	}
	if val := r.Header.Get(c.key); val != "" {
		return val
	}
	if val := r.URL.Query().Get(c.key); val != "" {
		return val
	}

	b := make([]byte, 18)
	if _, err := rand.Read(b); err != nil {
		panic("cannot read random value")
	}
	return strings.TrimRight(base64.URLEncoding.EncodeToString(b), "=")
}
