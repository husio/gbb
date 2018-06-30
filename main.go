package main

import (
	"context"
	"database/sql"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"time"

	"github.com/husio/gbb/gbb"
	"github.com/husio/gbb/pkg/surf"
	"github.com/shurcooL/github_flavored_markdown"
)

func main() {
	env := surf.NewEnvConf()
	conf := configuration{
		Debug:       env.Bool("DEBUG", false, "Provide additional debug information."),
		HttpPort:    env.Str("HTTP_PORT", "8000", "HTTP server port."),
		Secret:      env.Secret("SECRET", "asoihqw0hqf098yr1309ry{RQ#Y)ASY{F[0u9rq3[0uqfafasffas", "Secret used for security"),
		DatabaseUrl: env.Secret("DATABASE_URL", `host='localhost' port='5432' user='postgres' dbname='postgres' sslmode='disable'`, "Database connection details."),
		NoCsrf:      env.Bool("NO_CSRF", false, "Do not requir CSRF token. This should be used only during local development."),
	}

	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "-h", "--help", "help":
			env.WriteHelp(os.Stderr)
			os.Exit(0)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		sigc := make(chan os.Signal, 1)
		signal.Notify(sigc, os.Interrupt)
		<-sigc
		fmt.Fprintln(os.Stderr, "SIGINT")
		cancel()
		signal.Stop(sigc)
	}()

	if err := run(ctx, conf); err != nil {
		fmt.Fprintf(os.Stderr, "application: %s\n", err)
		os.Exit(1)
	}
}

type configuration struct {
	Debug       bool
	HttpPort    string
	Secret      string
	DatabaseUrl string
	NoCsrf      bool
}

func run(ctx context.Context, conf configuration) error {
	db, err := sql.Open("postgres", conf.DatabaseUrl)
	if err != nil {
		return fmt.Errorf("cannot open SQL database: %s", err)
	}
	defer db.Close()

	readTracker, err := gbb.NewPostgresReadProgressTracker(db)
	if err != nil {
		return fmt.Errorf("cannot create read progress tracker: %s", err)
	}

	bbStore, err := gbb.NewPostgresBBStore(db)
	if err != nil {
		return fmt.Errorf("cannot create bb store: %s", err)
	}

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
		return fmt.Errorf("cannot create cookie cache: %s", err)
	}

	csrfStore, err := surf.NewCookieCache("csrf", []byte(conf.Secret))
	if err != nil {
		return fmt.Errorf("cannot create csrf store: %s", err)
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
	rt.R(`/t/mark-all-read/`).
		Get(gbb.MarkAllReadHandler(authStore, readTracker))
	rt.R(`/t/new/`).
		Use(csrf).
		Get(gbb.TopicCreateHandler(bbStore, authStore, renderer)).
		Post(gbb.TopicCreateHandler(bbStore, authStore, renderer))
	rt.R(`/t/<post-id:[^/]+>/last-seen-comment/.*`).
		Get(gbb.LastSeenCommentHandler(bbStore, readTracker, authStore, renderer))
	rt.R(`/t/<post-id:[^/]+>/last-comment/.*`).
		Get(gbb.LastCommentHandler(bbStore, readTracker, authStore, renderer))
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
		Get(gbb.UserDetailsHandler(bbStore, authStore, renderer))
	rt.R(`/login/`).
		Use(csrf).
		Get(gbb.LoginHandler(authStore, bbStore, renderer)).
		Post(gbb.LoginHandler(authStore, bbStore, renderer))
	rt.R(`/logout/`).
		Use(csrf).
		Get(gbb.LogoutHandler(authStore, bbStore, renderer)).
		Post(gbb.LogoutHandler(authStore, bbStore, renderer))
	rt.R(`/register/`).
		Use(csrf).
		Get(gbb.RegisterHandler(authStore, bbStore, renderer)).
		Post(gbb.RegisterHandler(authStore, bbStore, renderer))

	logger := surf.NewLogger(os.Stdout)

	app := surf.NewHTTPApplication(rt, logger, true)

	server := http.Server{
		Addr:    ":" + conf.HttpPort,
		Handler: app,
	}
	go func() {
		<-ctx.Done()
		logger.Info(ctx, "stopping HTTP server")
		server.Shutdown(ctx)
	}()

	logger.Info(ctx, "starting HTTP server",
		"port", "8000")
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("http server: %s", err)
	}
	return nil
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
	if v, ok := os.LookupEnv(name); ok {
		return v
	}
	return fallback
}

func envBool(name string, fallback bool) bool {
	if v, ok := os.LookupEnv(name); ok {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return fallback
}
