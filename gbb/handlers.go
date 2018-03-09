package gbb

import (
	"fmt"
	"net/http"
	"net/url"
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

		posts, err := store.ListPosts(ctx, time.Now())
		if err != nil {
			surf.Error(ctx, err, "cannot fetch posts")
			return surf.StdResponse(rend, http.StatusInternalServerError)
		}

		return rend.Response(http.StatusOK, "post_list.tmpl", struct {
			Posts []*Post
		}{
			Posts: posts,
		})
	}
}

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
			http.Redirect(w, r, "/login/?next="+url.QueryEscape(r.URL.String()), http.StatusTemporaryRedirect)
			return nil
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
			http.Redirect(w, r, url, http.StatusSeeOther)
			return nil
		}

		return rend.Response(http.StatusOK, "post_create.tmpl", content)
	}
}

func CommentListHandler(
	store BBStore,
	rend surf.Renderer,
) surf.HandlerFunc {
	type Content struct {
		Post     *Post
		Comments []*Comment
	}

	return func(w http.ResponseWriter, r *http.Request) surf.Response {
		ctx := r.Context()

		postID := surf.PathArgInt64(r, 0)
		post, comments, err := store.ListComments(ctx, postID, time.Now())
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

		if err := store.IncrementPostView(ctx, postID); err != nil {
			surf.Error(ctx, err, "cannot increment view counter",
				"postID", fmt.Sprint(post.PostID))
		}

		return rend.Response(http.StatusOK, "comment_list.tmpl", Content{
			Post:     post,
			Comments: comments,
		})
	}
}

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
			http.Redirect(w, r, "/login/?next="+url.QueryEscape(r.URL.String()), http.StatusTemporaryRedirect)
			return nil
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
		url := fmt.Sprintf("/p/%d/#bottom", postID)
		http.Redirect(w, r, url, http.StatusSeeOther)
		return nil
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
					http.Redirect(w, r, "/", http.StatusSeeOther)
					return nil
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
		}{
			Errors: errors,
			User:   user,
		})
	}
}

func LogoutHandler(
	authStore surf.UnboundCacheService,
) surf.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) surf.Response {
		ctx := r.Context()
		if err := Logout(ctx, authStore.Bind(w, r)); err != nil {
			surf.Error(ctx, err, "cannot logout user")
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return nil
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
				http.Redirect(w, r, "/", http.StatusSeeOther)
				return nil
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
