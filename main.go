package main

import (
	"context"
	"database/sql"
	"html/template"
	"net/http"
	"os"

	"github.com/husio/gbb/gbb"
	"github.com/husio/gbb/pkg/surf"
	"github.com/shurcooL/github_flavored_markdown"
)

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

	store := gbb.NewPostgresStore(db)

	renderer := surf.NewHTMLRenderer("./gbb/templates/**.tmpl", template.FuncMap{
		"markdown": func(s string) template.HTML {
			html := github_flavored_markdown.Markdown([]byte(s))
			return template.HTML(html)
		},
	})

	rt := surf.NewRouter()

	rt.Get(`/`, http.RedirectHandler("/p/", http.StatusTemporaryRedirect))
	rt.Get(`/p/`, gbb.PostListHandler(store, renderer))
	rt.Get(`/p/new/`, gbb.PostCreateHandler(store, renderer))
	rt.Post(`/p/new/`, gbb.PostCreateHandler(store, renderer))
	rt.Get(`/p/<post-id:[^/]+>/`, gbb.CommentListHandler(store, renderer))
	rt.Post(`/p/<post-id:[^/]+>/comment/`, gbb.CommentCreateHandler(store, renderer))

	rt.Get(`/_/template/unknown/`, func(w http.ResponseWriter, r *http.Request) {
		renderer.Response(http.StatusOK, "ghost_template.tmpl", nil)
	})
	rt.Get(`/_/template/invalidcontext/`, func(w http.ResponseWriter, r *http.Request) {
		renderer.Response(http.StatusOK, "post_list.tmpl", nil)
	})

	app := surf.NewHTTPApplication(rt, true, logger)

	logger.Info(context.Background(), "starting server",
		"port", "8000")
	if err := http.ListenAndServe(":8000", app); err != nil {
		logger.Error(context.Background(), err, "HTTP server failed")
	}
}
