package gbb

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/husio/gbb/pkg/surf"
)

func PostListHandler(
	store BBStore,
	rend surf.Renderer,
) surf.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) surf.Response {
		ctx := r.Context()

		createdLte, ok := timeFromParam(r.URL.Query(), "after")
		if !ok {
			createdLte = time.Now()
		}

		posts, err := store.ListPosts(ctx, createdLte, postsPerPage)
		if err != nil {
			surf.Error(ctx, err, "cannot fetch posts")
			return surf.StdResponse(rend, http.StatusInternalServerError)
		}

		sleepSpan := surf.CurrentTrace(ctx).Begin("sleeping for fun")
		for i := 0; i < 4; i++ {
			span := sleepSpan.Begin("Zzzz",
				"i", fmt.Sprint(i))
			time.Sleep(time.Duration(i) * time.Millisecond)
			span.Finish()

		}
		sleepSpan.Finish()

		nextPageAfter := ""
		if len(posts) == postsPerPage {
			nextPageAfter = posts[len(posts)-1].Created.Format(time.RFC3339)
		}

		return rend.Response(http.StatusOK, "post_list.tmpl", struct {
			Posts         []*Post
			NextPageAfter string
		}{
			Posts:         posts,
			NextPageAfter: nextPageAfter,
		})
	}
}

const postsPerPage = 100

func PostCreateHandler(
	store BBStore,
	ucache surf.UnboundCacheService,
	rend surf.Renderer,
) surf.HandlerFunc {
	type Content struct {
		Subject string
		Content string
		Errors  map[string]string
	}
	return func(w http.ResponseWriter, r *http.Request) surf.Response {
		ctx := r.Context()

		user, err := CurrentUser(ctx, ucache.Bind(w, r))
		switch err {
		case nil:
			// all good
		case ErrUnauthenticated:
			return surf.Redirect("/login/?next="+url.QueryEscape(r.URL.String()), http.StatusTemporaryRedirect)
		default:
			surf.Error(ctx, err, "cannot get current user")
			return surf.StdResponse(rend, http.StatusInternalServerError)
		}

		content := Content{}

		if r.Method == "POST" {
			if err := r.ParseMultipartForm(1e6); err != nil {
				fmt.Println("Invalid content type", err)
				return surf.StdResponse(rend, http.StatusBadRequest)
			}

			content.Errors = make(map[string]string)

			content.Subject = strings.TrimSpace(r.Form.Get("subject"))
			if sLen := len(content.Subject); sLen == 0 {
				content.Errors["Subject"] = "Subject is required."
			} else if sLen < 2 {
				content.Errors["Subject"] = "Too short. Must be at least 2 characters"
			}

			content.Content = strings.TrimSpace(r.Form.Get("content"))
			if cLen := len(content.Content); cLen == 0 {
				content.Errors["Content"] = "Content is required."
			} else if cLen < 2 {
				content.Errors["Content"] = "Too short. Must be at least 2 characters"
			}

			if len(content.Errors) != 0 {
				return rend.Response(http.StatusBadRequest, "post_create.tmpl", content)
			}

			post, _, err := store.CreatePost(ctx, content.Subject, content.Content, user.UserID)
			if err != nil {
				surf.Error(ctx, err, "cannot create posts")
				return surf.StdResponse(rend, http.StatusInternalServerError)
			}

			url := fmt.Sprintf("/p/%d/#bottom", post.PostID)
			return surf.Redirect(url, http.StatusSeeOther)
		}

		return rend.Response(http.StatusOK, "post_create.tmpl", content)
	}
}

func CommentListHandler(
	store BBStore,
	rend surf.Renderer,
) surf.HandlerFunc {
	type Content struct {
		Post       *Post
		Comments   []*Comment
		Pagination *paginator
	}

	return func(w http.ResponseWriter, r *http.Request) surf.Response {
		ctx := r.Context()

		postID := surf.PathArgInt64(r, 0)

		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if page < 1 {
			page = 1
		}
		offset := page * commentsPerPage

		post, comments, err := store.ListComments(ctx, postID, offset, commentsPerPage)
		switch err {
		case nil:
			// all good
		case ErrNotFound:
			w.WriteHeader(http.StatusBadRequest)
			return surf.StdResponse(rend, http.StatusNotFound)
		default:
			surf.Error(ctx, err, "cannot fetch post and comments",
				"postID", fmt.Sprint(postID))
			return surf.StdResponse(rend, http.StatusInternalServerError)
		}

		surf.Info(ctx, "listing comments",
			"post.id", fmt.Sprint(post.PostID),
			"post.subject", post.Subject)

		if err := store.IncrementPostView(ctx, postID); err != nil {
			surf.Error(ctx, err, "cannot increment view counter",
				"postID", fmt.Sprint(post.PostID))
		}

		surf.Error(ctx, errors.New("roar!"), "this is just a test")

		return rend.Response(http.StatusOK, "comment_list.tmpl", Content{
			Post:     post,
			Comments: comments,
			Pagination: &paginator{
				total:    post.CommentsCount,
				pageSize: commentsPerPage,
				page:     page,
			},
		})
	}
}

type paginator struct {
	total    int64
	pageSize int
	page     int
}

func (p *paginator) CurrentPage() int {
	return p.page
}

func (p *paginator) PageCount() int {
	return int(p.total) / p.pageSize
}

func (p *paginator) NextPage() int {
	return p.page + 1
}

func (p *paginator) HasNextPage() bool {
	return int64((p.page+1)*p.pageSize) < p.total
}

func (p *paginator) PrevPage() int {
	if p.page < 1 {
		return 1
	}
	prev := p.page - 1
	if int64(prev*p.pageSize) > p.total {
		return int(p.total) / p.pageSize
	}
	return prev
}

func (p *paginator) HasPrevPage() bool {
	return p.page > 1
}

func (p *paginator) Pages() []PaginatorPage {
	pages := make([]PaginatorPage, p.PageCount())
	for i := range pages {
		pages[i] = PaginatorPage{
			Number:  i + 1,
			Active:  p.page == i+1,
			IsFirst: i == 0,
			IsLast:  i == len(pages)-1,
		}
	}
	return pages
}

type PaginatorPage struct {
	Number  int
	Active  bool
	IsFirst bool
	IsLast  bool
}

const commentsPerPage = 100

func CommentCreateHandler(
	store BBStore,
	ucache surf.UnboundCacheService,
	rend surf.Renderer,
) surf.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) surf.Response {
		ctx := r.Context()

		if err := r.ParseMultipartForm(1e6); err != nil {
			return surf.StdResponse(rend, http.StatusBadRequest)
		}

		postID := surf.PathArgInt64(r, 0)
		content := strings.TrimSpace(r.Form.Get("content"))

		user, err := CurrentUser(ctx, ucache.Bind(w, r))
		switch err {
		case nil:
			// all good
		case ErrUnauthenticated:
			return surf.Redirect("/login/?next="+url.QueryEscape(r.URL.String()), http.StatusTemporaryRedirect)
		default:
			surf.Error(ctx, err, "cannot get current user")
			return surf.StdResponse(rend, http.StatusInternalServerError)
		}

		if len(content) > 0 {
			switch _, err := store.CreateComment(ctx, postID, content, user.UserID); err {
			case nil:
				// all good
			case ErrNotFound:
				return surf.StdResponse(rend, http.StatusBadRequest)
			default:
				surf.Error(ctx, err, "cannot create comment",
					"content", content,
					"postID", fmt.Sprint(postID))
				return surf.StdResponse(rend, http.StatusInternalServerError)
			}
		}
		return surf.Redirect("/p/", http.StatusSeeOther)
	}
}

func LoginHandler(
	authStore surf.UnboundCacheService,
	users UserStore,
	rend surf.Renderer,
) surf.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) surf.Response {
		ctx := r.Context()

		boundCache := authStore.Bind(w, r)

		var errors []string

		if r.Method == "POST" {
			login := r.FormValue("login")
			passwd := r.FormValue("passwd")

			switch user, err := users.Authenticate(ctx, login, passwd); err {
			case nil:
				if err := Login(ctx, boundCache, *user); err != nil {
					surf.Error(ctx, err, "cannot login user",
						"login", login)
					errors = append(errors, "Temporary issues. Please try again later.")
				} else {
					next := r.FormValue("next")
					if next == "" {
						next = "/"
					}
					return surf.Redirect(next, http.StatusSeeOther)
				}
			case ErrNotFound:
				surf.Info(ctx, "failed authentication attempt",
					"login", login)
				errors = append(errors, "Invalid login and/or password.")

			default:
				surf.Error(ctx, err, "cannot authenticate user",
					"login", login)
				errors = append(errors, "Temporary issues. Please try again later.")
			}
		}

		user, err := CurrentUser(ctx, boundCache)
		if err != nil && err != ErrUnauthenticated {
			surf.Error(ctx, err, "cannot get current user from cache")
			// continue - this is not a critical error
		}

		code := http.StatusOK
		if len(errors) != 0 {
			code = http.StatusBadRequest
		}

		return rend.Response(code, "login.tmpl", struct {
			Errors []string
			User   *User
			Next   string
		}{
			Errors: errors,
			User:   user,
			Next:   r.URL.Query().Get("next"),
		})
	}
}

func LogoutHandler(
	authStore surf.UnboundCacheService,
	users UserStore,
	rend surf.Renderer,
) surf.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) surf.Response {
		ctx := r.Context()

		if r.Method == "GET" {
			user, err := CurrentUser(ctx, authStore.Bind(w, r))
			if err != nil && err != ErrUnauthenticated {
				surf.Error(ctx, err, "cannot get current user from cache")
				// continue - this is not a critical error
			}

			return rend.Response(http.StatusOK, "logout.tmpl", struct {
				User *User
			}{
				User: user,
			})
		}

		if err := Logout(ctx, authStore.Bind(w, r)); err != nil {
			surf.Error(ctx, err, "cannot logout user")
		}
		return surf.Redirect("/", http.StatusSeeOther)
	}
}

func RegisterHandler(
	authStore surf.UnboundCacheService,
	users UserStore,
	rend surf.Renderer,
) surf.HandlerFunc {
	type Context struct {
		Login  string
		Errors map[string]string
	}

	return func(w http.ResponseWriter, r *http.Request) surf.Response {
		ctx := r.Context()

		boundCache := authStore.Bind(w, r)

		if _, err := CurrentUser(ctx, boundCache); err == nil {
			return rend.Response(http.StatusBadRequest, "error_4xx.tmpl", "Already logged in")
		}

		if r.Method == "GET" {
			return rend.Response(http.StatusOK, "register.tmpl", Context{})
		}

		context := Context{
			Errors: make(map[string]string),
		}

		context.Login = strings.TrimSpace(r.FormValue("login"))
		if n := len(context.Login); n == 0 {
			context.Errors["Login"] = "Login is required"
		} else if n < 3 {
			context.Errors["Login"] = "Login is too short"
		} else if n > 30 {
			context.Errors["Login"] = "Login is too long"
		}

		password := r.FormValue("password")
		password2 := r.FormValue("password2")
		if password != password2 {
			context.Errors["Password"] = "Password is not repeated correctly"
		}
		if len(password) < 8 {
			context.Errors["Password"] = "At least 8 characters are required"
		}

		if len(context.Errors) != 0 {
			return rend.Response(http.StatusBadRequest, "register.tmpl", context)
		}

		switch user, err := users.Register(ctx, password, User{Name: context.Login}); err {
		case nil:
			surf.Info(ctx, "new user registered",
				"name", user.Name,
				"id", fmt.Sprint(user.UserID))
			if err := Login(ctx, boundCache, *user); err != nil {
				surf.Error(ctx, err, "cannot login user",
					"id", fmt.Sprint(user.UserID),
					"name", user.Name)
				return surf.StdResponse(rend, http.StatusInternalServerError)
			} else {
				return surf.Redirect("/", http.StatusSeeOther)
			}
		case ErrConstraint:
			context.Errors["Login"] = "Login already in use"
			return rend.Response(http.StatusBadRequest, "register.tmpl", context)
		default:
			surf.Error(ctx, err, "cannot register user")
			return surf.StdResponse(rend, http.StatusInternalServerError)
		}
	}
}

func MeHandler(
	authStore surf.UnboundCacheService,
) surf.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) surf.Response {
		ctx := r.Context()

		switch user, err := CurrentUser(ctx, authStore.Bind(w, r)); err {
		case nil:
			surf.JSONResp(w, http.StatusOK, user)
		case ErrUnauthenticated:
			surf.StdJSONResp(w, http.StatusUnauthorized)
		default:
			surf.StdJSONResp(w, http.StatusInternalServerError)
		}
		return nil
	}
}

func timeFromParam(query url.Values, name string) (time.Time, bool) {
	val := query.Get(name)
	if val == "" {
		return time.Time{}, false
	}
	for _, f := range timeFormats {
		t, err := time.Parse(f, val)
		if err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

var timeFormats = []string{
	time.RFC3339,
	"2006-01-02T15:04:05",
	"2006-01-02T15:04",
	"2006-01-02",
}
