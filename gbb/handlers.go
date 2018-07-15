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
	bbStore BBStore,
	authStore surf.UnboundCacheService,
	rend surf.HTMLRenderer,
) surf.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) surf.Response {
		ctx := r.Context()

		userID := surf.PathArgInt64(r, 0)

		currentUser, err := CurrentUser(ctx, authStore.Bind(w, r))
		if err != nil && err != ErrUnauthenticated {
			surf.LogError(ctx, err, "cannot authenticated user")
		}

		switch browsedUser, err := bbStore.UserInfo(ctx, userID); err {
		case nil:
			return rend.Response(ctx, http.StatusOK, "user_details.tmpl", struct {
				User        *UserInfo
				CurrentUser *User
			}{
				User:        browsedUser,
				CurrentUser: currentUser,
			})

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
	bbStore BBStore,
	readTracker ReadProgressTracker,
	authStore surf.UnboundCacheService,
	rend surf.HTMLRenderer,
) surf.HandlerFunc {

	const postsPerPage = 100

	type TrackedTopic struct {
		*Topic
		NewContent bool
		Progress   *ReadProgress
		Pages      []int
	}

	lotsOfPages := make([]int, 75)
	for i := range lotsOfPages {
		lotsOfPages[i] = i + 1
	}

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

		topics, err := bbStore.ListTopics(ctx, createdLte, postsPerPage)
		if err != nil {
			surf.LogError(ctx, err, "cannot fetch topics")
			return surf.StdResponse(ctx, rend, http.StatusInternalServerError)
		}

		nextPageAfter := ""
		if len(topics) == postsPerPage {
			nextPageAfter = topics[len(topics)-1].Created.Format(time.RFC3339)
		}

		trackedTopics := make([]*TrackedTopic, len(topics))
		for i, t := range topics {
			trackedTopics[i] = &TrackedTopic{
				Topic: t,
				Pages: lotsOfPages[:t.CommentsCount/commentsPerPage+1],
			}
		}

		if user.Authenticated() {
			topicIDs := make([]int64, len(topics))
			for i, t := range topics {
				topicIDs[i] = t.TopicID
			}
			if progress, err := readTracker.LastReads(ctx, user.UserID, topicIDs); err != nil {
				surf.LogError(ctx, err, "cannot get read progress",
					"user", fmt.Sprint(user.UserID))
			} else {
				for _, t := range trackedTopics {
					t.Progress = progress[t.TopicID]
					if t.Progress == nil {
						t.NewContent = true
					} else {
						t.NewContent = t.Progress.CommentCreated.Before(t.Updated)
					}
				}
			}
		}

		return rend.Response(ctx, http.StatusOK, "topic_list.tmpl", struct {
			CurrentUser       *User
			Topics            []*TrackedTopic
			NextPageAfter     string
			CanChangeSettings func(*User) bool
		}{
			CurrentUser:   user,
			Topics:        trackedTopics,
			NextPageAfter: nextPageAfter,
			CanChangeSettings: func(u *User) bool {
				return u != nil && u.Scopes.HasAny(adminScope, changeSettingsScope)
			},
		})
	}
}

func TopicCreateHandler(
	bbStore BBStore,
	authStore surf.UnboundCacheService,
	rend surf.HTMLRenderer,
) surf.HandlerFunc {
	type Content struct {
		Input struct {
			Subject  string
			Content  string
			Category int64
		}
		Errors     map[string]string
		CsrfField  template.HTML
		Categories []*Category
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

		if !user.Scopes.HasAny(adminScope, createTopicScope) {
			surf.LogInfo(ctx, "user action rejected due to missing topic creation scope",
				"scopes", user.Scopes.String(),
				"user", fmt.Sprint(user.UserID))
			return rend.Response(ctx, http.StatusOK, "error_scope.tmpl", struct {
				Message string
			}{
				Message: "Not allowed to create topic.",
			})
		}

		categories, err := bbStore.ListCategories(ctx)
		if err != nil {
			surf.LogError(ctx, err, "cannot list categories")
		}

		content := Content{
			CsrfField:  surf.CsrfField(ctx),
			Categories: categories,
		}

		if r.Method == "POST" {
			if err := r.ParseMultipartForm(1e6); err != nil {
				surf.LogInfo(ctx, "invalid content type",
					"error", err.Error())
				return surf.StdResponse(ctx, rend, http.StatusBadRequest)
			}

			content.Errors = make(map[string]string)

			content.Input.Subject = strings.TrimSpace(r.Form.Get("subject"))
			if sLen := len(content.Input.Subject); sLen == 0 {
				content.Errors["Subject"] = "Subject is required."
			} else if sLen < 2 {
				content.Errors["Subject"] = "Too short. Must be at least 2 characters"
			}

			content.Input.Content = strings.TrimSpace(r.Form.Get("content"))
			if cLen := len(content.Input.Content); cLen == 0 {
				content.Errors["Content"] = "Content is required."
			} else if cLen < 2 {
				content.Errors["Content"] = "Too short. Must be at least 2 characters"
			}

			content.Input.Category, _ = strconv.ParseInt(r.Form.Get("category"), 10, 64)
			if content.Input.Category == 0 {
				content.Errors["Categories"] = "Category is required."
			} else if !containsCategory(categories, content.Input.Category) {
				content.Errors["Categories"] = "Invalid value."
			}

			if len(content.Errors) != 0 {
				return rend.Response(ctx, http.StatusBadRequest, "topic_create.tmpl", content)
			}

			topic, comment, err := bbStore.CreateTopic(ctx, content.Input.Subject, content.Input.Content, content.Input.Category, user.UserID)
			if err != nil {
				surf.LogError(ctx, err, "cannot create topic")
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

func containsCategory(categories []*Category, categoryID int64) bool {
	for _, c := range categories {
		if c.CategoryID == categoryID {
			return true
		}
	}
	return false
}

func CommentListHandler(
	bbStore BBStore,
	readTracker ReadProgressTracker,
	authStore surf.UnboundCacheService,
	rend surf.HTMLRenderer,
) surf.HandlerFunc {

	type Content struct {
		CurrentUser *User
		CsrfField   template.HTML
		Topic       *Topic
		Comments    []*Comment
		Pagination  *surf.Paginator
		CanModify   func(*Comment) bool
	}

	return func(w http.ResponseWriter, r *http.Request) surf.Response {
		ctx := r.Context()

		topicID := surf.PathArgInt64(r, 0)

		user, err := CurrentUser(ctx, authStore.Bind(w, r))
		if err != nil && err != ErrUnauthenticated {
			surf.LogError(ctx, err, "cannot authenticated user")
		}
		surf.LogInfo(ctx, "current user fetched",
			"user", fmt.Sprint(user))

		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if page < 1 {
			page = 1
		}
		offset := (page - 1) * commentsPerPage

		topic, err := bbStore.TopicByID(ctx, topicID)
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

		comments, err := bbStore.ListComments(ctx, topicID, offset, commentsPerPage)
		if err != nil {
			surf.LogError(ctx, err, "cannot fetch comments",
				"topicID", fmt.Sprint(topicID))
			return surf.StdResponse(ctx, rend, http.StatusInternalServerError)
		}

		surf.LogInfo(ctx, "listing comments",
			"topic.id", fmt.Sprint(topic.TopicID),
			"topic.subject", topic.Subject)

		if err := bbStore.IncrementTopicView(ctx, topicID); err != nil {
			surf.LogError(ctx, err, "cannot increment view counter",
				"topicID", fmt.Sprint(topic.TopicID))
		}

		if user.Authenticated() && len(comments) > 0 {
			lastComment := comments[len(comments)-1]
			err := readTracker.Track(ctx, ReadProgress{
				UserID:         user.UserID,
				TopicID:        topic.TopicID,
				CommentID:      lastComment.CommentID,
				CommentCreated: lastComment.Created,
			})
			if err != nil {
				surf.LogError(ctx, err, "cannot track comment",
					"user", fmt.Sprint(user.UserID),
					"topic", fmt.Sprint(topic.TopicID),
					"comment", fmt.Sprint(lastComment.CommentID))
			}
		}

		surf.LogInfo(ctx, "template context prepared")

		return rend.Response(ctx, http.StatusOK, "comment_list.tmpl", Content{
			CurrentUser: user,
			CsrfField:   surf.CsrfField(ctx),
			Topic:       topic,
			Comments:    comments,
			CanModify: func(comment *Comment) bool {
				if !user.Authenticated() {
					return false
				}
				if user.UserID == comment.Author.UserID {
					return true
				}
				return user.Scopes.HasAny(adminScope, moderatorScope)
			},
			Pagination: &surf.Paginator{
				Total:    topic.CommentsCount,
				PageSize: commentsPerPage,
				Page:     page,
			},
		})
	}
}

const commentsPerPage = 100

func LastCommentHandler(
	bbStore BBStore,
	readTracker ReadProgressTracker,
	authStore surf.UnboundCacheService,
	rend surf.HTMLRenderer,
) surf.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) surf.Response {
		ctx := r.Context()
		topicID := surf.PathArgInt64(r, 0)

		topic, err := bbStore.TopicByID(ctx, topicID)
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

		var url string
		if page := int(topic.CommentsCount / commentsPerPage); page < 2 {
			url = fmt.Sprintf("/t/%d/%s/#bottom", topic.TopicID, topic.SlugInfo())
		} else {
			url = fmt.Sprintf("/t/%d/%s/?page=%d#bottom", topic.TopicID, topic.SlugInfo(), page+1)
		}
		return surf.Redirect(url, http.StatusSeeOther)
	}
}

func LastSeenCommentHandler(
	bbStore BBStore,
	readTracker ReadProgressTracker,
	authStore surf.UnboundCacheService,
	rend surf.HTMLRenderer,
) surf.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) surf.Response {
		ctx := r.Context()
		topicID := surf.PathArgInt64(r, 0)

		topic, err := bbStore.TopicByID(ctx, topicID)
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

		user, err := CurrentUser(ctx, authStore.Bind(w, r))
		if err != nil && err != ErrUnauthenticated {
			surf.LogError(ctx, err, "cannot get user information")
		}

		if user.Authenticated() {
			if results, err := readTracker.LastReads(ctx, user.UserID, []int64{topic.TopicID}); err != nil {
				surf.LogError(ctx, err, "cannot get last reads",
					"topic", fmt.Sprint(topic.TopicID),
					"user", fmt.Sprint(user.UserID))
			} else if len(results) == 1 {
				p := results[topic.TopicID]

				_, _, position, err := bbStore.CommentByID(ctx, p.CommentID)
				if err != nil {
					surf.LogError(ctx, err, "cannot fetch comment",
						"comment", fmt.Sprint(p.CommentID))
					url := fmt.Sprintf("/t/%d/%s/", topic.TopicID, topic.SlugInfo())
					return surf.Redirect(url, http.StatusSeeOther)
				}

				var url string
				if position < commentsPerPage {
					url = fmt.Sprintf("/t/%d/%s/#comment-%d", topic.TopicID, topic.SlugInfo(), p.CommentID)
				} else {
					page := (position / commentsPerPage) + 1
					url = fmt.Sprintf("/t/%d/%s/?page=%d#comment-%d", topic.TopicID, topic.SlugInfo(), page+1, p.CommentID)
				}
				return surf.Redirect(url, http.StatusSeeOther)
			} else {
				// user is looking at the topic for the first time
				url := fmt.Sprintf("/t/%d/%s/", topic.TopicID, topic.SlugInfo())
				return surf.Redirect(url, http.StatusSeeOther)
			}
		}

		var url string
		if page := int(topic.CommentsCount / commentsPerPage); page < 2 {
			url = fmt.Sprintf("/t/%d/%s/#bottom", topic.TopicID, topic.SlugInfo())
		} else {
			url = fmt.Sprintf("/t/%d/%s/?page=%d#bottom", topic.TopicID, topic.SlugInfo(), page+1)
		}
		return surf.Redirect(url, http.StatusSeeOther)
	}
}

func GotoCommentHandler(
	bbStore BBStore,
	rend surf.HTMLRenderer,
) surf.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) surf.Response {
		ctx := r.Context()
		commentID := surf.PathArgInt64(r, 0)

		topic, comment, position, err := bbStore.CommentByID(ctx, commentID)
		switch err := castErr(err); err {
		case nil:
			// all good
		case ErrNotFound:
			return surf.StdResponse(ctx, rend, http.StatusNotFound)
		default:
			surf.LogError(ctx, err, "cannot fetch comment",
				"comment", fmt.Sprint(commentID))
			return surf.StdResponse(ctx, rend, http.StatusInternalServerError)
		}

		var url string
		if position < commentsPerPage {
			url = fmt.Sprintf("/t/%d/%s/#comment-%d", topic.TopicID, topic.SlugInfo(), comment.CommentID)
		} else {
			page := (position / commentsPerPage) + 1
			url = fmt.Sprintf("/t/%d/%s/?page=%d#comment-%d", topic.TopicID, topic.SlugInfo(), page+1, comment.CommentID)
		}
		return surf.Redirect(url, http.StatusSeeOther)
	}
}

func CommentCreateHandler(
	bbStore BBStore,
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

		if !user.Scopes.HasAny(adminScope, createCommentScope) {
			surf.LogInfo(ctx, "user action rejected due to missing comment creation scope",
				"scopes", user.Scopes.String(),
				"user", fmt.Sprint(user.UserID))
			return rend.Response(ctx, http.StatusOK, "error_scope.tmpl", struct {
				Message string
			}{
				Message: "Not allowed to comment.",
			})
		}

		// TODO: validate input

		topic, err := bbStore.TopicByID(ctx, topicID)
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

		comment, err := bbStore.CreateComment(ctx, topicID, content, user.UserID)
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
	bbStore BBStore,
	rend surf.HTMLRenderer,
) surf.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) surf.Response {
		ctx := r.Context()

		boundCache := authStore.Bind(w, r)

		var errors []string

		if r.Method == "POST" {
			login := r.FormValue("login")
			passwd := r.FormValue("password")

			switch user, err := bbStore.AuthenticateUser(ctx, login, passwd); err {
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
			Errors    []string
			User      *User
			Next      string
			CsrfField template.HTML
		}{
			Errors:    errors,
			User:      user,
			Next:      r.URL.Query().Get("next"),
			CsrfField: surf.CsrfField(ctx),
		})
	}
}

func LogoutHandler(
	authStore surf.UnboundCacheService,
	bbStore BBStore,
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
				User      *User
				CsrfField template.HTML
			}{
				User:      user,
				CsrfField: surf.CsrfField(ctx),
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
	bbStore BBStore,
	rend surf.HTMLRenderer,
) surf.HandlerFunc {
	type Context struct {
		Next      string
		Login     string
		CsrfField template.HTML
		Errors    map[string]string
	}

	return func(w http.ResponseWriter, r *http.Request) surf.Response {
		ctx := r.Context()

		boundCache := authStore.Bind(w, r)

		if _, err := CurrentUser(ctx, boundCache); err == nil {
			return rend.Response(ctx, http.StatusBadRequest, "error_4xx.tmpl", "Already logged in")
		}

		if r.Method == "GET" {
			return rend.Response(ctx, http.StatusOK, "register.tmpl", Context{
				CsrfField: surf.CsrfField(ctx),
				Next:      r.URL.Query().Get("next"),
			})
		}

		context := Context{
			Next:      r.FormValue("next"),
			CsrfField: surf.CsrfField(ctx),
			Errors:    make(map[string]string),
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

		baseScopes := createTopicScope.Add(createCommentScope)
		switch user, err := bbStore.RegisterUser(ctx, password, User{Name: context.Login, Scopes: baseScopes}); err {
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
	const searchResultLimit = 30

	return func(w http.ResponseWriter, r *http.Request) surf.Response {
		ctx := r.Context()
		query := r.URL.Query()

		categories, err := bbstore.ListCategories(ctx)
		if err != nil {
			surf.LogError(ctx, err, "cannot list categories")
		}

		content := struct {
			SearchTerm       string
			SearchCategories map[int64]bool
			Categories       []*Category
			NextPage         int64
			Results          []*SearchResult
			HasMore          bool
		}{
			SearchTerm: query.Get("q"),
			Categories: categories,
		}

		if content.SearchTerm != "" {
			page, _ := strconv.ParseInt(query.Get("page"), 10, 32)
			if page < 1 {
				page = 1
			}

			var categories []int64
			searchCategories := make(map[int64]bool)
			for _, raw := range query["c"] {
				if cid, err := strconv.ParseInt(raw, 10, 64); err == nil {
					categories = append(categories, cid)
					searchCategories[cid] = true
				}
			}
			content.SearchCategories = searchCategories

			results, err := bbstore.Search(ctx, content.SearchTerm, categories, searchResultLimit*(page-1), searchResultLimit)
			if err != nil && err != ErrNotFound {
				surf.LogError(ctx, err, "database failure, cannot search",
					"q", content.SearchTerm)
				return surf.StdResponse(ctx, rend, http.StatusInternalServerError)
			}
			content.Results = results
			content.HasMore = len(results) == searchResultLimit // be optimistic
			content.NextPage = page + 1
		}

		return rend.Response(ctx, http.StatusOK, "search_result.tmpl", content)
	}
}

func CommentEditHandler(
	authStore surf.UnboundCacheService,
	bbstore BBStore,
	rend surf.HTMLRenderer,
) surf.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) surf.Response {
		ctx := r.Context()

		user, err := CurrentUser(ctx, authStore.Bind(w, r))
		switch err {
		case nil:
			// all good
		case ErrUnauthenticated:
			return surf.StdResponse(ctx, rend, http.StatusUnauthorized)
		default:
			surf.LogError(ctx, err, "cannot get current user")
			return surf.StdResponse(ctx, rend, http.StatusInternalServerError)
		}

		topic, comment, commentPos, err := bbstore.CommentByID(ctx, surf.PathArgInt64(r, 0))
		switch err {
		case nil:
			// all good
		case ErrNotFound:
			return surf.StdResponse(ctx, rend, http.StatusNotFound)
		default:
			surf.LogError(ctx, err, "cannot get comment",
				"comment", surf.PathArg(r, 0))
			return surf.StdResponse(ctx, rend, http.StatusInternalServerError)
		}

		if comment.Author.UserID != user.UserID && !user.Scopes.HasAny(adminScope, moderatorScope) {
			surf.LogInfo(ctx, "rejected edit because of permissions",
				"user", fmt.Sprint(user.UserID),
				"author", fmt.Sprint(comment.Author.UserID),
				"comment", fmt.Sprint(comment.CommentID))
			return rend.Response(ctx, http.StatusOK, "error_scope.tmpl", struct {
				Message string
			}{
				Message: "Not allowed to edit.",
			})
		}

		content := struct {
			Errors struct {
				Subject string
				Content string
			}
			CurrentUser *User
			Topic       *Topic
			Comment     *Comment
			CommentPos  int
			CsrfField   template.HTML
			Input       struct {
				Subject string
				Content string
			}
		}{
			CurrentUser: user,
			Topic:       topic,
			Comment:     comment,
			CommentPos:  commentPos,
			CsrfField:   surf.CsrfField(ctx),
			Input: struct {
				Subject string
				Content string
			}{
				Subject: topic.Subject,
				Content: comment.Content,
			},
		}

		if r.Method == "GET" {
			return rend.Response(ctx, http.StatusOK, "comment_edit.tmpl", content)
		}

		if err := r.ParseMultipartForm(1e6); err != nil {
			surf.LogInfo(ctx, "invalid content type",
				"error", err.Error())
			return surf.StdResponse(ctx, rend, http.StatusBadRequest)
		}

		content.Input.Content = strings.TrimSpace(r.Form.Get("content"))
		if cLen := len(content.Input.Content); cLen == 0 {
			content.Errors.Content = "Required."
		} else if cLen < 2 {
			content.Errors.Content = "Too short. Must be at least 2 characters."
		}

		if commentPos == 0 {
			content.Input.Subject = strings.TrimSpace(r.Form.Get("subject"))
			if sLen := len(content.Input.Subject); sLen == 0 {
				content.Errors.Subject = "Required."
			} else if sLen < 2 {
				content.Errors.Subject = "Too short. Must be at least 2 characters."
			}
		}

		if content.Errors.Subject == "" && content.Errors.Content == "" {
			if commentPos == 0 && topic.Subject != content.Input.Subject {
				switch err := bbstore.UpdateTopic(ctx, topic.TopicID, content.Input.Subject); err {
				case nil:
					// all good
				case ErrNotFound:
					return surf.StdResponse(ctx, rend, http.StatusNotFound)
				default:
					surf.LogError(ctx, err, "cannot update topic",
						"user", fmt.Sprint(user.UserID),
						"topic", fmt.Sprint(topic.TopicID))
					return surf.StdResponse(ctx, rend, http.StatusInternalServerError)
				}
			}
			switch err := bbstore.UpdateComment(ctx, comment.CommentID, content.Input.Content); err {
			case nil:
				var url string
				if page := int(commentPos / commentsPerPage); page < 2 {
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
			case ErrNotFound:
				return surf.StdResponse(ctx, rend, http.StatusNotFound)
			default:
				surf.LogError(ctx, err, "cannot update content",
					"user", fmt.Sprint(user.UserID),
					"content", fmt.Sprint(comment.CommentID))
				return surf.StdResponse(ctx, rend, http.StatusInternalServerError)
			}
		}

		return rend.Response(ctx, http.StatusOK, "comment_edit.tmpl", content)
	}
}

func CommentDeleteHandler(
	authStore surf.UnboundCacheService,
	bbstore BBStore,
	rend surf.HTMLRenderer,
) surf.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) surf.Response {
		ctx := r.Context()

		commentID := surf.PathArgInt64(r, 0)

		user, err := CurrentUser(ctx, authStore.Bind(w, r))
		switch err {
		case nil:
			// all good
		case ErrUnauthenticated:
			return surf.StdResponse(ctx, rend, http.StatusUnauthorized)
		default:
			surf.LogError(ctx, err, "cannot get current user")
			return surf.StdResponse(ctx, rend, http.StatusInternalServerError)
		}

		topic, comment, pos, err := bbstore.CommentByID(ctx, commentID)
		switch err {
		case nil:
			if comment.Author.UserID != user.UserID && !user.Scopes.HasAny(adminScope, moderatorScope) {
				surf.LogInfo(ctx, "comment deletion forbidden",
					"comment", fmt.Sprint(commentID),
					"author", fmt.Sprint(comment.Author.UserID),
					"user", fmt.Sprint(user.UserID))
				return rend.Response(ctx, http.StatusOK, "error_scope.tmpl", struct {
					Message string
				}{
					Message: "Not allowed to delete.",
				})
			}
		case ErrNotFound:
			surf.LogInfo(ctx, "comment not found",
				"comment", fmt.Sprint(commentID))
			return surf.StdResponse(ctx, rend, http.StatusNotFound)
		default:
			surf.LogError(ctx, err, "cannot get comment",
				"comment", fmt.Sprint(commentID))
			return surf.StdResponse(ctx, rend, http.StatusInternalServerError)
		}

		if r.Method == "GET" {
			return rend.Response(ctx, http.StatusOK, "comment_delete.tmpl", struct {
				CsrfField  template.HTML
				Topic      *Topic
				Comment    *Comment
				CommentPos int
			}{
				CsrfField:  surf.CsrfField(ctx),
				Topic:      topic,
				Comment:    comment,
				CommentPos: pos,
			})
		}

		// if it's the first comment, the entire topic is being deleted
		if pos == 0 {
			switch err := bbstore.DeleteTopic(ctx, topic.TopicID); err {
			case nil:
				// all good
			case ErrNotFound:
				surf.LogInfo(ctx, "cannot delete because topic not found",
					"comment", fmt.Sprint(commentID))
				return surf.StdResponse(ctx, rend, http.StatusNotFound)
			default:
				surf.LogError(ctx, err, "cannot delete topic",
					"comment", fmt.Sprint(commentID))
				return surf.StdResponse(ctx, rend, http.StatusInternalServerError)
			}
			return surf.Redirect("/t/", http.StatusSeeOther)
		}

		switch err := bbstore.DeleteComment(ctx, commentID); err {
		case nil:
			// all good
		case ErrNotFound:
			surf.LogInfo(ctx, "cannot delete because comment not found",
				"comment", fmt.Sprint(commentID))
			return surf.StdResponse(ctx, rend, http.StatusNotFound)
		default:
			surf.LogError(ctx, err, "cannot delete comment",
				"comment", fmt.Sprint(commentID))
			return surf.StdResponse(ctx, rend, http.StatusInternalServerError)
		}
		return surf.Redirect(fmt.Sprintf("/t/%d/%s/", topic.TopicID, topic.SlugInfo()), http.StatusSeeOther)

	}
}

func MarkAllReadHandler(
	authStore surf.UnboundCacheService,
	readTracker ReadProgressTracker,
) surf.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) surf.Response {
		ctx := r.Context()

		user, err := CurrentUser(ctx, authStore.Bind(w, r))
		if err != nil {
			return surf.Redirect("/t/", http.StatusSeeOther)
		}

		if err := readTracker.MarkAllRead(ctx, user.UserID, time.Now()); err != nil {
			surf.LogError(ctx, err, "cannot mark all topics as read",
				"user.id", fmt.Sprint(user.UserID),
				"user.name", user.Name)
		}

		return surf.Redirect("/t/", http.StatusSeeOther)
	}
}

func SettingsHandler(
	authStore surf.UnboundCacheService,
	bbstore BBStore,
	rend surf.HTMLRenderer,
) surf.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) surf.Response {
		ctx := r.Context()

		user, err := CurrentUser(ctx, authStore.Bind(w, r))
		if err != nil || !user.Scopes.HasAny(adminScope, changeSettingsScope) {
			return surf.Redirect("/t/", http.StatusSeeOther)
		}

		categories, err := bbstore.ListCategories(ctx)
		if err != nil {
			surf.LogError(ctx, err, "cannot list categories")
			return surf.StdResponse(ctx, rend, http.StatusInternalServerError)
		}

		return rend.Response(ctx, http.StatusOK, "settings.tmpl", struct {
			Categories []*Category
		}{
			Categories: categories,
		})
	}
}

func SettingsChangeCategoriesHandler(
	authStore surf.UnboundCacheService,
	bbstore BBStore,
	rend surf.HTMLRenderer,
) surf.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) surf.Response {
		ctx := r.Context()

		user, err := CurrentUser(ctx, authStore.Bind(w, r))
		if err != nil || !user.Scopes.HasAny(adminScope, changeSettingsScope) {
			return surf.Redirect("/t/", http.StatusSeeOther)
		}

		if r.Method != "POST" {
			return surf.Redirect(r.URL.Path, http.StatusSeeOther)
		}

		if err := r.ParseForm(); err != nil {
			return surf.StdResponse(ctx, rend, http.StatusBadRequest)
		}

		want := make(map[string]struct{})
		for _, name := range strings.Split(r.Form.Get("categories"), "\n") {
			name = strings.TrimSpace(name)
			if name != "" {
				want[name] = struct{}{}
			}
		}

		existing := make(map[string]*Category)
		if categories, err := bbstore.ListCategories(ctx); err != nil {
			surf.LogError(ctx, err, "cannot list categories")
			return surf.StdResponse(ctx, rend, http.StatusInternalServerError)
		} else {
			for _, c := range categories {
				existing[c.Name] = c
			}
		}

		return surf.Redirect(r.URL.Path, http.StatusSeeOther)
	}
}
