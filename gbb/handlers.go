package gbb

import (
	"fmt"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/husio/gbb/pkg/surf"
)

func userID() int64 {
	return rand.Int63n(2) + 1
}

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
	rend surf.Renderer,
) surf.HandlerFunc {
	type Content struct {
		Title   string
		Content string
		Errors  map[string]string
	}
	return func(w http.ResponseWriter, r *http.Request) surf.Response {
		ctx := r.Context()

		content := Content{}

		if r.Method == "POST" {
			if err := r.ParseMultipartForm(1e6); err != nil {
				return surf.StdResponse(rend, http.StatusBadRequest)
			}

			content.Errors = make(map[string]string)

			content.Title = strings.TrimSpace(r.Form.Get("title"))
			if titleLen := len(content.Title); titleLen == 0 {
				content.Errors["Title"] = "Title is required."
			} else if titleLen < 2 {
				content.Errors["Title"] = "Too short. Must be at least 2 characters"
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

			post, _, err := store.CreatePost(ctx, content.Title, content.Content, userID())
			if err != nil {
				surf.Error(ctx, err, "cannot create posts")
				return surf.StdResponse(rend, http.StatusInternalServerError)
			}

			url := fmt.Sprintf("/p/%d/#bottom", post.PostID)
			http.Redirect(w, r, url, http.StatusSeeOther)
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
	rend surf.Renderer,
) surf.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) surf.Response {
		ctx := r.Context()

		if err := r.ParseMultipartForm(1e6); err != nil {
			return surf.StdResponse(rend, http.StatusBadRequest)
		}

		postID := surf.PathArgInt64(r, 0)
		content := strings.TrimSpace(r.Form.Get("content"))

		if len(content) > 0 {
			switch _, err := store.CreateComment(ctx, postID, content, userID()); err {
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
