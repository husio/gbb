package gbb

import (
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/husio/gbb/pkg/surf"
)

func UserDetailsHandler(
	users UserStore,
	authStore surf.UnboundCacheService,
	rend surf.HTMLRenderer,
) surf.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) surf.Response {
		userID := surf.PathArgInt64(r, 0)

		ctx := r.Context()
		switch user, err := users.UserInfo(ctx, userID); err {
		case nil:
			return rend.Response(ctx, http.StatusOK, "user_details.tmpl", user)
		case ErrNotFound:
			return surf.StdResponse(ctx, rend, http.StatusNotFound)
		default:
			surf.LogError(ctx, err, "cannot get user",
				"userid", fmt.Sprint(userID))
			return surf.StdResponse(ctx, rend, http.StatusInternalServerError)
		}
	}
}

func TopicListHandler(
	store BBStore,
	authStore surf.UnboundCacheService,
	rend surf.HTMLRenderer,
) surf.HandlerFunc {

	const postsPerPage = 100

	return func(w http.ResponseWriter, r *http.Request) surf.Response {
		ctx := r.Context()

		user, err := CurrentUser(ctx, authStore.Bind(w, r))
		if err != nil && err != ErrUnauthenticated {
			surf.LogError(ctx, err, "cannot authenticated user")
		}

		createdLte, ok := timeFromParam(r.URL.Query(), "after")
		if !ok {
			createdLte = time.Now()
		}

		posts, err := store.ListTopics(ctx, createdLte, postsPerPage)
		if err != nil {
			surf.LogError(ctx, err, "cannot fetch posts")
			return surf.StdResponse(ctx, rend, http.StatusInternalServerError)
		}

		nextPageAfter := ""
		if len(posts) == postsPerPage {
			nextPageAfter = posts[len(posts)-1].Created.Format(time.RFC3339)
		}

		return rend.Response(ctx, http.StatusOK, "topic_list.tmpl", struct {
			CurrentUser   *User
			Topics        []*Topic
			NextPageAfter string
		}{
			CurrentUser:   user,
			Topics:        posts,
			NextPageAfter: nextPageAfter,
		})
	}
}

func TopicCreateHandler(
	store BBStore,
	authStore surf.UnboundCacheService,
	rend surf.HTMLRenderer,
) surf.HandlerFunc {
	type Content struct {
		Subject   string
		Content   string
		Errors    map[string]string
		CsrfField template.HTML
	}
	return func(w http.ResponseWriter, r *http.Request) surf.Response {
		ctx := r.Context()

		user, err := CurrentUser(ctx, authStore.Bind(w, r))
		switch err {
		case nil:
			// all good
		case ErrUnauthenticated:
			return surf.Redirect("/login/?next="+url.QueryEscape(r.URL.String()), http.StatusTemporaryRedirect)
		default:
			surf.LogError(ctx, err, "cannot get current user")
			return surf.StdResponse(ctx, rend, http.StatusInternalServerError)
		}

		content := Content{
			CsrfField: surf.CsrfField(ctx),
		}

		if r.Method == "POST" {
			if err := r.ParseMultipartForm(1e6); err != nil {
				fmt.Println("Invalid content type", err)
				return surf.StdResponse(ctx, rend, http.StatusBadRequest)
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
				return rend.Response(ctx, http.StatusBadRequest, "topic_create.tmpl", content)
			}

			topic, comment, err := store.CreateTopic(ctx, content.Subject, content.Content, user.UserID)
			if err != nil {
				surf.LogError(ctx, err, "cannot create posts")
				return surf.StdResponse(ctx, rend, http.StatusInternalServerError)
			}

			url := fmt.Sprintf("/t/%d/%s/#comment-%d",
				topic.TopicID,
				topic.SlugInfo(),
				comment.CommentID,
			)
			return surf.Redirect(url, http.StatusSeeOther)
		}

		return rend.Response(ctx, http.StatusOK, "topic_create.tmpl", content)
	}
}

func CommentListHandler(
	store BBStore,
	authStore surf.UnboundCacheService,
	rend surf.HTMLRenderer,
) surf.HandlerFunc {

	type Content struct {
		CurrentUser *User
		CsrfField   template.HTML
		Topic       *Topic
		Comments    []*Comment
		Pagination  *paginator
	}

	return func(w http.ResponseWriter, r *http.Request) surf.Response {
		ctx := r.Context()

		topicID := surf.PathArgInt64(r, 0)

		user, err := CurrentUser(ctx, authStore.Bind(w, r))
		if err != nil && err != ErrUnauthenticated {
			surf.LogError(ctx, err, "cannot authenticated user")
		}

		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if page < 1 {
			page = 1
		}
		offset := (page - 1) * commentsPerPage

		topic, err := store.TopicByID(ctx, topicID)
		switch err := castErr(err); err {
		case nil:
			// all good
		case ErrNotFound:
			w.WriteHeader(http.StatusBadRequest)
			return surf.StdResponse(ctx, rend, http.StatusNotFound)
		default:
			surf.LogError(ctx, err, "cannot fetch topic",
				"topicID", fmt.Sprint(topicID))
			return surf.StdResponse(ctx, rend, http.StatusInternalServerError)
		}

		comments, err := store.ListComments(ctx, topicID, offset, commentsPerPage)
		if err != nil {
			surf.LogError(ctx, err, "cannot fetch comments",
				"topicID", fmt.Sprint(topicID))
			return surf.StdResponse(ctx, rend, http.StatusInternalServerError)
		}

		surf.LogInfo(ctx, "listing comments",
			"topic.id", fmt.Sprint(topic.TopicID),
			"topic.subject", topic.Subject)

		if err := store.IncrementTopicView(ctx, topicID); err != nil {
			surf.LogError(ctx, err, "cannot increment view counter",
				"topicID", fmt.Sprint(topic.TopicID))
		}

		return rend.Response(ctx, http.StatusOK, "comment_list.tmpl", Content{
			CurrentUser: user,
			CsrfField:   surf.CsrfField(ctx),
			Topic:       topic,
			Comments:    comments,
			Pagination: &paginator{
				total:    topic.CommentsCount,
				pageSize: commentsPerPage,
				page:     page,
			},
		})
	}
}

const commentsPerPage = 100

// TODO: this handler must redirect to the last comment seen by the user, not
// to the last comment that belongs to the topic
func LastCommentHandler(
	store BBStore,
	authStore surf.UnboundCacheService,
	rend surf.HTMLRenderer,
) surf.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) surf.Response {
		ctx := r.Context()
		topicID := surf.PathArgInt64(r, 0)

		topic, err := store.TopicByID(ctx, topicID)
		switch err := castErr(err); err {
		case nil:
			// all good
		case ErrNotFound:
			return surf.StdResponse(ctx, rend, http.StatusNotFound)
		default:
			surf.LogError(ctx, err, "cannot fetch topic",
				"topic", fmt.Sprint(topicID))
			return surf.StdResponse(ctx, rend, http.StatusInternalServerError)
		}

		// TODO: replace #bottom with #comment-%d
		var url string
		if page := int(topic.CommentsCount / commentsPerPage); page < 2 {
			url = fmt.Sprintf("/t/%d/%s/#bottom", topic.TopicID, topic.SlugInfo())
		} else {
			url = fmt.Sprintf("/t/%d/%s/?page=%d#bottom", topic.TopicID, topic.SlugInfo(), page+1)
		}
		http.Redirect(w, r, url, http.StatusSeeOther)
		return nil
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
	return int(p.total)/p.pageSize + 1
}

func (p *paginator) NextPage() int {
	return p.page + 1
}

func (p *paginator) HasNextPage() bool {
	return int64((p.page)*p.pageSize) < p.total
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

func CommentCreateHandler(
	store BBStore,
	authStore surf.UnboundCacheService,
	rend surf.HTMLRenderer,
) surf.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) surf.Response {
		ctx := r.Context()

		if err := r.ParseMultipartForm(1e6); err != nil {
			return surf.StdResponse(ctx, rend, http.StatusBadRequest)
		}

		topicID := surf.PathArgInt64(r, 0)
		content := strings.TrimSpace(r.Form.Get("content"))

		user, err := CurrentUser(ctx, authStore.Bind(w, r))
		switch err {
		case nil:
			// all good
		case ErrUnauthenticated:
			return surf.Redirect("/login/?next="+url.QueryEscape(r.URL.String()), http.StatusTemporaryRedirect)
		default:
			surf.LogError(ctx, err, "cannot get current user")
			return surf.StdResponse(ctx, rend, http.StatusInternalServerError)
		}

		// TODO: validate input

		topic, err := store.TopicByID(ctx, topicID)
		switch err := castErr(err); err {
		case nil:
			// all good
		case ErrNotFound:
			return surf.StdResponse(ctx, rend, http.StatusNotFound)
		default:
			surf.LogError(ctx, err, "cannot fetch topic",
				"topic", fmt.Sprint(topicID))
			return surf.StdResponse(ctx, rend, http.StatusInternalServerError)
		}

		comment, err := store.CreateComment(ctx, topicID, content, user.UserID)
		switch err {
		case nil:
			// all good
		case ErrNotFound:
			return surf.StdResponse(ctx, rend, http.StatusBadRequest)
		default:
			surf.LogError(ctx, err, "cannot create comment",
				"content", content,
				"topicID", fmt.Sprint(topicID))
			return surf.StdResponse(ctx, rend, http.StatusInternalServerError)
		}

		var url string
		if page := int(topic.CommentsCount / commentsPerPage); page < 2 {
			url = fmt.Sprintf("/t/%d/%s/#comment-%d",
				topic.TopicID,
				topic.SlugInfo(),
				comment.CommentID)
		} else {
			url = fmt.Sprintf("/t/%d/%s/?page=%d#comment-%d",
				topic.TopicID,
				topic.SlugInfo(),
				page+1,
				comment.CommentID)
		}

		return surf.Redirect(url, http.StatusSeeOther)
	}
}

func LoginHandler(
	authStore surf.UnboundCacheService,
	users UserStore,
	rend surf.HTMLRenderer,
) surf.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) surf.Response {
		ctx := r.Context()

		boundCache := authStore.Bind(w, r)

		var errors []string

		if r.Method == "POST" {
			login := r.FormValue("login")
			passwd := r.FormValue("password")

			switch user, err := users.Authenticate(ctx, login, passwd); err {
			case nil:
				if err := Login(ctx, boundCache, *user); err != nil {
					surf.LogError(ctx, err, "cannot login user",
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
				surf.LogInfo(ctx, "failed authentication attempt",
					"login", login)
				errors = append(errors, "Invalid login and/or password.")

			default:
				surf.LogError(ctx, err, "cannot authenticate user",
					"login", login)
				errors = append(errors, "Temporary issues. Please try again later.")
			}
		}

		user, err := CurrentUser(ctx, boundCache)
		if err != nil && err != ErrUnauthenticated {
			surf.LogError(ctx, err, "cannot get current user from cache")
			// continue - this is not a critical error
		}

		code := http.StatusOK
		if len(errors) != 0 {
			code = http.StatusBadRequest
		}

		return rend.Response(ctx, code, "login.tmpl", struct {
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
	rend surf.HTMLRenderer,
) surf.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) surf.Response {
		ctx := r.Context()

		if r.Method == "GET" {
			user, err := CurrentUser(ctx, authStore.Bind(w, r))
			if err != nil && err != ErrUnauthenticated {
				surf.LogError(ctx, err, "cannot get current user from cache")
				// continue - this is not a critical error
			}

			return rend.Response(ctx, http.StatusOK, "logout.tmpl", struct {
				User *User
			}{
				User: user,
			})
		}

		if err := Logout(ctx, authStore.Bind(w, r)); err != nil {
			surf.LogError(ctx, err, "cannot logout user")
		}
		return surf.Redirect("/", http.StatusSeeOther)
	}
}

func RegisterHandler(
	authStore surf.UnboundCacheService,
	users UserStore,
	rend surf.HTMLRenderer,
) surf.HandlerFunc {
	type Context struct {
		Next   string
		Login  string
		Errors map[string]string
	}

	return func(w http.ResponseWriter, r *http.Request) surf.Response {
		ctx := r.Context()

		boundCache := authStore.Bind(w, r)

		if _, err := CurrentUser(ctx, boundCache); err == nil {
			return rend.Response(ctx, http.StatusBadRequest, "error_4xx.tmpl", "Already logged in")
		}

		if r.Method == "GET" {
			return rend.Response(ctx, http.StatusOK, "register.tmpl", Context{
				Next: r.URL.Query().Get("next"),
			})
		}

		context := Context{
			Next:   r.FormValue("next"),
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
			return rend.Response(ctx, http.StatusBadRequest, "register.tmpl", context)
		}

		switch user, err := users.Register(ctx, password, User{Name: context.Login}); err {
		case nil:
			surf.LogInfo(ctx, "new user registered",
				"name", user.Name,
				"id", fmt.Sprint(user.UserID))
			if err := Login(ctx, boundCache, *user); err != nil {
				surf.LogError(ctx, err, "cannot login user",
					"id", fmt.Sprint(user.UserID),
					"name", user.Name)
				return surf.StdResponse(ctx, rend, http.StatusInternalServerError)
			} else {
				next := context.Next
				if next == "" {
					next = "/"
				}
				return surf.Redirect(next, http.StatusSeeOther)
			}
		case ErrConstraint:
			context.Errors["Login"] = "Login already in use"
			return rend.Response(ctx, http.StatusBadRequest, "register.tmpl", context)
		default:
			surf.LogError(ctx, err, "cannot register user")
			return surf.StdResponse(ctx, rend, http.StatusInternalServerError)
		}
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

func SearchHandler(
	bbstore BBStore,
	rend surf.HTMLRenderer,
) surf.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) surf.Response {
		ctx := r.Context()

		searchTerm := strings.TrimSpace(r.URL.Query().Get("q"))
		if searchTerm == "" {
			panic("todo")
		}

		results, err := bbstore.Search(ctx, searchTerm, 100)
		if err != nil && err != ErrNotFound {
			surf.LogError(ctx, err, "database failure, cannot search",
				"searchTerm", searchTerm)
			return surf.StdResponse(ctx, rend, http.StatusInternalServerError)
		}
		return rend.Response(ctx, http.StatusOK, "search_result.tmpl", struct {
			SearchTerm string
			Results    []*SearchResult
		}{
			SearchTerm: searchTerm,
			Results:    results,
		})
	}
}
