package surf

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func CsrfMiddleware(
	cache UnboundCacheService,
	tmpl HTMLRenderer) Middleware {
	return func(handler interface{}) Handler {
		return &csrfMiddleware{
			handler: AsHandler(handler),
			cache:   cache,
			tmpl:    tmpl,
		}
	}
}

type csrfMiddleware struct {
	handler Handler
	cache   UnboundCacheService
	tmpl    HTMLRenderer
}

func (m *csrfMiddleware) HandleHTTPRequest(w http.ResponseWriter, r *http.Request) Response {
	ctx := r.Context()

	store := m.cache.Bind(w, r)

	var storeToken string
	switch err := store.Get(ctx, CsrfKey, &storeToken); err {
	case nil, ErrMiss:
	default:
		LogError(ctx, err, "cannot get csrf token from store")
		return rejectResp("cannot get csrf token")
	}

	if !isSafeMethod(r.Method) {
		// if using https, make sure referer does not come from untrusted source
		if r.URL.Scheme == "https" {
			if ref, err := url.Parse(r.Referer()); err != nil {
				return rejectResp("missing referer")
			} else if ref.Scheme != "https" {
				return rejectResp("invalid referer")
			}
		}

		reqToken := requestToken(r)
		if reqToken == "" {
			LogError(ctx, errors.New("no csrf"), "no csrf token in request")
			return rejectResp("no csrf token in request")
		}

		if reqToken != storeToken {
			LogInfo(ctx, "csrf token missmatch",
				"requestToken", reqToken,
				"storeToken", storeToken)
			return rejectResp("csrf token missmatch")
		}
	}

	if storeToken == "" {
		storeToken = newCsrfToken()
		if err := store.Set(ctx, CsrfKey, storeToken, 30*time.Minute); err != nil {
			LogError(ctx, err, "cannot store csrf token")
		}
	}

	ctx = context.WithValue(ctx, "surf:csrf-token", storeToken)
	r = r.WithContext(ctx)

	// protect clients from caching response
	w.Header().Set("vary", "cookie")

	return m.handler.HandleHTTPRequest(w, r)
}

func requestToken(r *http.Request) string {
	if val := r.Header.Get(CsrfKey); val != "" {
		return val
	}
	if val := r.PostFormValue(CsrfKey); val != "" {
		return val
	}
	if r.MultipartForm != nil {
		if vals := r.MultipartForm.Value[CsrfKey]; len(vals) > 0 {
			return vals[0]
		}
	}
	return ""
}

func newCsrfToken() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return strings.TrimRight(base64.URLEncoding.EncodeToString(b), "=")
}

func rejectResp(reason string) Response {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, reason, http.StatusForbidden)
	})
}

func isSafeMethod(method string) bool {
	var safeCsrfMethods = []string{"HEAD", "GET", "OPTIONS", "TRACE"}
	for _, s := range safeCsrfMethods {
		if s == method {
			return true
		}
	}
	return false
}

// CsrfToken returns CSRF protection token attached to given context. Handler
// must be protected by CsrfMiddleware to have csrf token present in the
// context.
func CsrfToken(ctx context.Context) string {
	if s, ok := ctx.Value("surf:csrf-token").(string); ok && s != "" {
		return s
	}

	LogError(ctx, errors.New("no csrf token"), "csrf token not in context")
	return ""
}

func CsrfField(ctx context.Context) template.HTML {
	html := fmt.Sprintf(`<input type="hidden" value="%s" name="%s">`,
		CsrfToken(ctx), CsrfKey)
	return template.HTML(html)
}

// CsrfKey is used for lookup of the csrf token, for example inside of the
// request's header or form
const CsrfKey = "csrftoken"
