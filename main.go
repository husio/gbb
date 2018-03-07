package main

import (
	"context"
	"database/sql"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"time"

	"github.com/husio/gbb/gbb"
	"github.com/husio/gbb/pkg/surf"
	"github.com/shurcooL/github_flavored_markdown"
)

const secret = "asoihqw0hqf098yr1309ry{RQ#Y)ASY{F[0u9rq3[0uqfafasffas"

func main() {
	logger := surf.NewLogger(os.Stderr)

	db, err := sql.Open("postgres", `host='localhost' port='5432' user='postgres' dbname='postgres' sslmode='disable'`)
	if err != nil {
		logger.Error(context.Background(), err, "cannot open SQL database")
		os.Exit(1)
	}
	defer db.Close()

	if err := gbb.EnsureSchema(db); err != nil {
		logger.Error(context.Background(), err, "cannot ensure store schema")
		os.Exit(1)
	}

	bbStore := gbb.NewPostgresBBStore(db)
	userStore := gbb.NewPostgresUserStore(db)

	renderer := surf.NewHTMLRenderer("./gbb/templates/**.tmpl", template.FuncMap{
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

	unboundCache, err := surf.NewCookieCache("", []byte(secret))
	if err != nil {
		logger.Error(context.Background(), err, "cannot create cookie cache")
		os.Exit(1)
	}

	rt := surf.NewRouter()

	rt.Get(`/`, http.RedirectHandler("/p/", http.StatusTemporaryRedirect))
	rt.Get(`/p/`, gbb.PostListHandler(bbStore, renderer))
	rt.Get(`/p/new/`, gbb.PostCreateHandler(bbStore, unboundCache, renderer))
	rt.Post(`/p/new/`, gbb.PostCreateHandler(bbStore, unboundCache, renderer))
	rt.Get(`/p/<post-id:[^/]+>/.*`, gbb.CommentListHandler(bbStore, renderer))
	rt.Post(`/p/<post-id:[^/]+>/comment/`, gbb.CommentCreateHandler(bbStore, unboundCache, renderer))
	rt.Get(`/login/`, gbb.LoginHandler(unboundCache, userStore, renderer))
	rt.Post(`/login/`, gbb.LoginHandler(unboundCache, userStore, renderer))
	rt.Post(`/logout/`, gbb.LogoutHandler(unboundCache))
	rt.Get(`/register/`, gbb.RegisterHandler(unboundCache, userStore, renderer))
	rt.Post(`/register/`, gbb.RegisterHandler(unboundCache, userStore, renderer))

	rt.Get(`/api/me/`, gbb.MeHandler(unboundCache))

	rt.Get(`/_/template/unknown/`, func(w http.ResponseWriter, r *http.Request) {
		renderer.Response(http.StatusOK, "ghost_template.tmpl", nil)
	})
	rt.Get(`/_/template/invalidcontext/`, func(w http.ResponseWriter, r *http.Request) {
		renderer.Response(http.StatusOK, "post_list.tmpl", nil)
	})

	app := surf.NewHTTPApplication(rt, logger, true)

	logger.Info(context.Background(), "starting server",
		"port", "8000")
	if err := http.ListenAndServe(":8000", app); err != nil {
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
