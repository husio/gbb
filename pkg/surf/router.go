package surf

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

type router struct {
	endpoints []endpoint

	rend Renderer
}

func NewRouter() *router {
	return &router{
		rend: newDefaultRenderer(),
	}
}

// Add registers handler to be called whenever request matching given path
// regexp and method must be handled.
//
// Use <name> or <name:regexp> to match part of the path and pass result to
// handler. First example will use [^/]+ to make match, in second one provided
// regexp is used. Name is not used and is required only for documentation
// purposes.
//
// Using '*' as methods will match any method.
func (r *router) Add(path, methods string, handler interface{}) {
	var h Handler
	switch handler := handler.(type) {
	case HandlerFunc:
		h = handler
	case Handler:
		h = handler
	case func(http.ResponseWriter, *http.Request) http.Handler:
		h = HandlerFunc(handler)
	case http.Handler:
		h = HandlerFunc(func(w http.ResponseWriter, r *http.Request) http.Handler {
			return handler
		})
	case http.HandlerFunc:
		h = HandlerFunc(func(w http.ResponseWriter, r *http.Request) http.Handler {
			return handler
		})
	case func(http.ResponseWriter, *http.Request):
		h = HandlerFunc(func(w http.ResponseWriter, r *http.Request) http.Handler {
			return http.HandlerFunc(handler)
		})
	default:
		msg := fmt.Sprintf("invalid %s handler notation for %q", methods, path)
		panic(msg)
	}

	builder := regexp.MustCompile(`\<.*?\>`)
	raw := builder.ReplaceAllStringFunc(path, func(s string) string {
		s = s[1 : len(s)-1]
		// every <name> can be optionally contain separate regexp
		// definition using notation <name:regexp>
		chunks := strings.SplitN(s, ":", 2)
		if len(chunks) == 1 {
			return `([^/]+)`
		}
		return `(` + chunks[1] + `)`
	})
	// replace {} with regular expressions syntax
	rx, err := regexp.Compile(`^` + raw + `$`)
	if err != nil {
		panic(fmt.Sprintf("invalid routing path %q: %s", path, err))
	}

	methodsSet := make(map[string]struct{})
	for _, method := range strings.Split(methods, ",") {
		methodsSet[strings.TrimSpace(method)] = struct{}{}
	}

	r.endpoints = append(r.endpoints, endpoint{
		methods: methodsSet,
		path:    rx,
		handler: h,
	})

}

func (r *router) Get(path string, handler interface{}) {
	r.Add(path, "GET", handler)
}

func (r *router) Post(path string, handler interface{}) {
	r.Add(path, "POST", handler)
}

func (r *router) Put(path string, handler interface{}) {
	r.Add(path, "PUT", handler)
}

func (r *router) Delete(path string, handler interface{}) {
	r.Add(path, "DELETE", handler)
}

type Handler interface {
	HandleHTTPRequest(http.ResponseWriter, *http.Request) http.Handler
}

type HandlerFunc func(http.ResponseWriter, *http.Request) http.Handler

func (fn HandlerFunc) HandleHTTPRequest(w http.ResponseWriter, r *http.Request) http.Handler {
	return fn(w, r)
}

type endpoint struct {
	methods map[string]struct{}
	path    *regexp.Regexp
	handler Handler
}

func (rt *router) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h := rt.HandleHTTPRequest(w, r); h != nil {
		h.ServeHTTP(w, r)
	}
}

func (rt *router) HandleHTTPRequest(w http.ResponseWriter, r *http.Request) http.Handler {
	var pathMatch bool

	for _, endpoint := range rt.endpoints {
		match := endpoint.path.FindAllStringSubmatch(r.URL.Path, 1)
		if len(match) == 0 {
			continue
		}

		pathMatch = true

		_, ok := endpoint.methods[r.Method]
		if !ok {
			_, ok = endpoint.methods["*"]
		}
		if !ok {
			continue
		}

		args := match[0][1:]
		r = r.WithContext(context.WithValue(r.Context(), pathArgsKey, args))
		return endpoint.handler.HandleHTTPRequest(w, r)
	}

	if pathMatch {
		return StdResponse(rt.rend, http.StatusMethodNotAllowed)
	}
	return StdResponse(rt.rend, http.StatusNotFound)
}

var pathArgsKey = struct{}{}

// PathArg return value as matched by path regexp at given index.
func PathArg(r *http.Request, index int) string {
	args, ok := r.Context().Value(pathArgsKey).([]string)
	if !ok {
		return ""
	}
	if len(args) <= index {
		return ""
	}
	return args[index]
}

func PathArgInt(r *http.Request, index int) int {
	n, _ := strconv.Atoi(PathArg(r, index))
	return n
}

func PathArgInt64(r *http.Request, index int) int64 {
	n, _ := strconv.ParseInt(PathArg(r, index), 10, 64)
	return n
}
