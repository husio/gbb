package main

import (
	"context"
	"database/sql"
	"net/http"
	"os"

	"github.com/husio/gbb/gbb"
	"github.com/husio/gbb/pkg/surf"
)

func main() {
	logger := surf.NewLogger(os.Stderr)

	db, err := sql.Open("sqlite3", "/tmp/gbb.sqlite.db")
	if err != nil {
		logger.Error(context.Background(), err, "cannot open sqlite database")
		os.Exit(1)
	}
	defer db.Close()

	if err := gbb.EnsureSchema(db); err != nil {
		logger.Error(context.Background(), err, "cannot ensure store schema")
		os.Exit(1)
	}

	store := gbb.NewSqliteStore(db)

	renderer := surf.NewHTMLRenderer("./gbb/templates/**.tmpl")

	rt := surf.NewRouter()

	rt.Get(`/`, http.RedirectHandler("/p/", http.StatusTemporaryRedirect))
	rt.Get(`/p/`, gbb.PostListHandler(store, renderer))
	rt.Get(`/p/new/`, gbb.PostCreateHandler(store, renderer))
	rt.Post(`/p/new/`, gbb.PostCreateHandler(store, renderer))
	rt.Get(`/p/<post-id:[^/]+>/`, gbb.CommentListHandler(store, renderer))
	rt.Post(`/p/<post-id:[^/]+>/comment/`, gbb.CommentCreateHandler(store, renderer))

	app := surf.NewHTTPApplication(rt, true, logger)

	if err := http.ListenAndServe(":8000", app); err != nil {
		logger.Error(context.Background(), err, "HTTP server failed")
	}
}
