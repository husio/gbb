package main

import (
	"context"
	"database/sql"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/husio/gbb/gbb"
	"github.com/husio/gbb/pkg/surf"
	"github.com/shurcooL/github_flavored_markdown"
)

func main() {
	logger := surf.NewLogger(os.Stderr)

	conf := struct {
		Debug    bool
		HttpPort string
		Secret   string
		PgConf   string
		NoCsrf   bool
	}{
		Debug:    envBool("DEBUG", false),
		HttpPort: env("PORT", "8000"),
		Secret:   env("SECRET", "asoihqw0hqf098yr1309ry{RQ#Y)ASY{F[0u9rq3[0uqfafasffas"),
		PgConf:   env("DATABASE_URL", `host='localhost' port='5432' user='postgres' dbname='postgres' sslmode='disable'`),
		NoCsrf:   envBool("NO_CSRF", false),
	}

	db, err := sql.Open("postgres", conf.PgConf)
	if err != nil {
		logger.Error(context.Background(), err, "cannot open SQL database")
		os.Exit(1)
	}
	defer db.Close()

	if err := gbb.EnsureSchema(db); err != nil {
		logger.Error(context.Background(), err, "cannot ensure store schema")
		os.Exit(1)
	}

	readTracker := gbb.NewPostgresReadProgressTracker(db)
	bbStore := gbb.NewPostgresBBStore(db)
	userStore := gbb.NewPostgresUserStore(db)

	renderer := surf.NewHTMLRenderer("./gbb/templates/**.tmpl", conf.Debug, template.FuncMap{
		"markdown": func(s string) template.HTML {
			html := github_flavored_markdown.Markdown([]byte(s))
			return template.HTML(html)
		},
		"timeago": func(t time.Time) template.HTML {
			ago := timeago(t)
			html := fmt.Sprintf(`<span title="%s">%s</span>`, t.Format("Mon, Jan 2 2006, 15:04"), ago)
			return template.HTML(html)
		},
	})

	authStore, err := surf.NewCookieCache("auth", []byte(conf.Secret))
	if err != nil {
		logger.Error(context.Background(), err, "cannot create cookie cache")
		os.Exit(1)
	}

	csrfStore, err := surf.NewCookieCache("csrf", []byte(conf.Secret))
	if err != nil {
		logger.Error(context.Background(), err, "cannot create cookie cache")
		os.Exit(1)
	}
	csrf := surf.CsrfMiddleware(csrfStore, renderer)
	if conf.NoCsrf {
		csrf = surf.AsHandler // pass through
	}

	rt := surf.NewRouter()

	rt.R(`/`).
		Get(http.RedirectHandler("/t/", http.StatusTemporaryRedirect))
	rt.R(`/t/`).
		Get(gbb.TopicListHandler(bbStore, readTracker, authStore, renderer))
	rt.R(`/t/search/`).
		Get(gbb.SearchHandler(bbStore, renderer))
	rt.R(`/t/new/`).
		Use(csrf).
		Get(gbb.TopicCreateHandler(bbStore, authStore, renderer)).
		Post(gbb.TopicCreateHandler(bbStore, authStore, renderer))
	rt.R(`/t/<post-id:[^/]+>/last-comment/.*`).
		Get(gbb.LastSeenCommentHandler(bbStore, readTracker, authStore, renderer))
	rt.R(`/t/<post-id:[^/]+>/.*`).
		Use(csrf).
		Get(gbb.CommentListHandler(bbStore, readTracker, authStore, renderer)).
		Post(gbb.CommentCreateHandler(bbStore, authStore, renderer))
	rt.R(`/c/<comment-id:[^/]+>/edit/`).
		Use(csrf).
		Get(gbb.CommentEditHandler(authStore, bbStore, renderer)).
		Post(gbb.CommentEditHandler(authStore, bbStore, renderer))
	rt.R(`/c/<comment-id:[^/]+>/delete/`).
		Use(csrf).
		Get(gbb.CommentDeleteHandler(authStore, bbStore, renderer)).
		Post(gbb.CommentDeleteHandler(authStore, bbStore, renderer))
	rt.R(`/c/<comment-id:[^/]+>/`).
		Get(gbb.GotoCommentHandler(bbStore, renderer))
	rt.R(`/u/<user-id:\d+>/`).
		Get(gbb.UserDetailsHandler(userStore, authStore, renderer))
	rt.R(`/login/`).
		Get(gbb.LoginHandler(authStore, userStore, renderer)).
		Post(gbb.LoginHandler(authStore, userStore, renderer))
	rt.R(`/logout/`).
		Get(gbb.LogoutHandler(authStore, userStore, renderer)).
		Post(gbb.LogoutHandler(authStore, userStore, renderer))
	rt.R(`/register/`).
		Get(gbb.RegisterHandler(authStore, userStore, renderer)).
		Post(gbb.RegisterHandler(authStore, userStore, renderer))

	app := surf.NewHTTPApplication(rt, logger, true)

	logger.Info(context.Background(), "starting server",
		"port", "8000")
	if err := http.ListenAndServe(":"+conf.HttpPort, app); err != nil {
		logger.Error(context.Background(), err, "HTTP server failed")
	}
}

func timeago(t time.Time) string {
	age := time.Now().Sub(t)

	if d := age / (24 * time.Hour); d == 1 {
		return "1 day ago"
	} else if d > 0 {
		return fmt.Sprintf("%d days ago", d)
	}
	if h := age / time.Hour; h == 1 {
		return "1 hour ago"
	} else if h > 0 {
		return fmt.Sprintf("%d hours ago", h)
	}
	if m := age / time.Minute; m == 1 {
		return "1 minute ago"
	} else if m > 0 {
		return fmt.Sprintf("%d minutes ago", m)
	}
	return "just now"
}

func env(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

func envBool(name string, fallback bool) bool {
	if v := os.Getenv(name); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return fallback
}
