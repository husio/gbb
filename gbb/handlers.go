package gbb

import (
	"net/http"
	"strings"
	"time"

	"github.com/husio/gbb/pkg/surf"
)

const userID = "rickybobby"

func PostListHandler(
	store BBStore,
	rend surf.Renderer,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		posts, err := store.ListPosts(ctx, time.Now())
		if err != nil {
			surf.Error(ctx, err, "cannot fetch posts")
			rend.RenderStdResponse(w, http.StatusInternalServerError)
			return
		}

		defer surf.CurrentSpan(ctx).StartSpan("render post_list.tmpl", nil).FinishSpan(nil)

		rend.RenderResponse(w, http.StatusOK, "post_list.tmpl", struct {
			Posts []*Post
		}{
			Posts: posts,
		})
	}
}

func PostCreateHandler(
	store BBStore,
	rend surf.Renderer,
) http.HandlerFunc {
	type Content struct {
		Title   string
		Content string
		Errors  map[string]string
	}
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		content := Content{}

		if r.Method == "POST" {
			if err := r.ParseMultipartForm(1e6); err != nil {
				rend.RenderStdResponse(w, http.StatusBadRequest)
				return
			}

			content.Errors = make(map[string]string)

			content.Title = strings.TrimSpace(r.Form.Get("title"))
			if titleLen := len(content.Title); titleLen == 0 {
				content.Errors["Title"] = "Required."
			} else if titleLen < 2 {
				content.Errors["Title"] = "Too short. Must be at least 2 characters"
			}

			content.Content = strings.TrimSpace(r.Form.Get("content"))
			if cLen := len(content.Content); cLen == 0 {
				content.Errors["Content"] = "Required."
			} else if cLen < 2 {
				content.Errors["Content"] = "Too short. Must be at least 2 characters"
			}

			if len(content.Errors) != 0 {
				rend.RenderResponse(w, http.StatusBadRequest, "post_create.tmpl", content)
				return
			}

			post, _, err := store.CreatePost(ctx, content.Title, content.Content, userID)
			if err != nil {
				surf.Error(ctx, err, "cannot create posts")
				rend.RenderStdResponse(w, http.StatusInternalServerError)
				return
			}

			http.Redirect(w, r, "/p/"+post.PostID+"/#bottom", http.StatusSeeOther)
		}

		rend.RenderResponse(w, http.StatusOK, "post_create.tmpl", content)
	}
}

func CommentListHandler(
	store BBStore,
	rend surf.Renderer,
) http.HandlerFunc {
	type Content struct {
		Post     *Post
		Comments []*Comment
	}

	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		postID := surf.PathArg(r, 0)
		post, comments, err := store.ListComments(ctx, postID, time.Now())
		switch err {
		case nil:
			// all good
		case ErrNotFound:
			w.WriteHeader(http.StatusBadRequest)
			rend.RenderStdResponse(w, http.StatusNotFound)
			return
		default:
			surf.Error(ctx, err, "cannot fetch post and comments",
				"postID", postID)
			rend.RenderStdResponse(w, http.StatusInternalServerError)
			return
		}

		content := Content{
			Post:     post,
			Comments: comments,
		}
		rend.RenderResponse(w, http.StatusOK, "comment_list.tmpl", content)

		if err := store.IncrementPostView(ctx, postID); err != nil {
			surf.Error(ctx, err, "cannot increment view counter",
				"postID", post.PostID)
		}
	}
}

func CommentCreateHandler(
	store BBStore,
	rend surf.Renderer,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		if err := r.ParseMultipartForm(1e6); err != nil {
			rend.RenderStdResponse(w, http.StatusBadRequest)
			return
		}

		postID := surf.PathArg(r, 0)
		content := strings.TrimSpace(r.Form.Get("content"))

		if len(content) > 0 {
			switch _, err := store.CreateComment(ctx, postID, content, userID); err {
			case nil:
				// all good
			case ErrNotFound:
				rend.RenderStdResponse(w, http.StatusBadRequest)
				return
			default:
				surf.Error(ctx, err, "cannot create comment",
					"content", content,
					"postID", postID)
				rend.RenderStdResponse(w, http.StatusInternalServerError)
				return
			}
		}
		http.Redirect(w, r, "/p/"+postID+"/#bottom", http.StatusSeeOther)
	}
}
