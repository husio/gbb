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

	rend HTMLRenderer
}

func NewRouter() *router {
	return &router{
		rend: newDefaultRenderer(),
	}
}

type Route interface {
	Use(middlewares ...Middleware) Route

	Add(method string, handler interface{}) Route

	Get(handler interface{}) Route
	Post(handler interface{}) Route
	Put(handler interface{}) Route
	Delete(handler interface{}) Route
	Head(handler interface{}) Route
	Options(handler interface{}) Route
	Trace(handler interface{}) Route
	Patch(handler interface{}) Route
}

func (r *router) R(path string) Route {
	return &route{
		router: r,
		path:   path,
	}
}

type route struct {
	path        string
	middlewares []Middleware
	router      *router
}

func (r *route) Use(middlewares ...Middleware) Route {
	r.middlewares = append(r.middlewares, middlewares...)
	return r
}

func (r *route) Add(method string, handler interface{}) Route {
	if len(r.middlewares) > 0 {
		handler = WithMiddlewares(handler, r.middlewares)
	}
	r.router.Add(r.path, method, handler)
	return r
}

func (r *route) Get(handler interface{}) Route {
	return r.Add("GET", handler)
}

func (r *route) Post(handler interface{}) Route {
	return r.Add("POST", handler)
}

func (r *route) Put(handler interface{}) Route {
	return r.Add("PUT", handler)
}

func (r *route) Delete(handler interface{}) Route {
	return r.Add("DELETE", handler)
}

func (r *route) Head(handler interface{}) Route {
	return r.Add("HEAD", handler)
}

func (r *route) Trace(handler interface{}) Route {
	return r.Add("TRACE", handler)
}

func (r *route) Options(handler interface{}) Route {
	return r.Add("OPTIONS", handler)
}

func (r *route) Patch(handler interface{}) Route {
	return r.Add("PATCH", handler)
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
	defer func() {
		if err := recover(); err != nil {
			msg := fmt.Sprintf("invalid %s handler notation for %q: %s", methods, path, err)
			panic(msg)
		}
	}()
	h := AsHandler(handler)

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

// AsHandler takes various handler notations and converts them to surf's
// Handler. If given handler does not implement any known interface, nil and
// false is returned
func AsHandler(h interface{}) Handler {
	switch handler := h.(type) {
	case HandlerFunc:
		return handler
	case Handler:
		return handler
	case func(http.ResponseWriter, *http.Request) Response:
		return HandlerFunc(handler)
	case http.Handler:
		return HandlerFunc(func(w http.ResponseWriter, r *http.Request) Response {
			return handler
		})
	case http.HandlerFunc:
		return HandlerFunc(func(w http.ResponseWriter, r *http.Request) Response {
			return handler
		})
	case func(http.ResponseWriter, *http.Request):
		return HandlerFunc(func(w http.ResponseWriter, r *http.Request) Response {
			return http.HandlerFunc(handler)
		})
	}

	panic("unknown handler interface")
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
	HandleHTTPRequest(http.ResponseWriter, *http.Request) Response
}

type Response interface {
	http.Handler
}

type HandlerFunc func(http.ResponseWriter, *http.Request) Response

func (fn HandlerFunc) HandleHTTPRequest(w http.ResponseWriter, r *http.Request) Response {
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

func (rt *router) HandleHTTPRequest(w http.ResponseWriter, r *http.Request) Response {
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

	ctx := r.Context()

	if pathMatch {
		return StdResponse(ctx, rt.rend, http.StatusMethodNotAllowed)
	}
	return StdResponse(ctx, rt.rend, http.StatusNotFound)
}

var pathArgsKey = struct{}{}

// PathArg return value as matched by path regexp at given index. Indexing of
// matched values starts with 0. If requested argument is out of index, empty
// string is returned.
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

// PathArgInt returns integer value of given path argument. If requested path
// argument is not valid integer value, 0 is returned. Use correct regular
// expression to ensure represented value is a valid number.
func PathArgInt(r *http.Request, index int) int {
	n, _ := strconv.Atoi(PathArg(r, index))
	return n
}

// PathArgInt64 returns integer value of given path argument. If requested path
// argument is not valid integer value, 0 is returned. Use correct regular
// expression to ensure represented value is a valid number.
func PathArgInt64(r *http.Request, index int) int64 {
	n, _ := strconv.ParseInt(PathArg(r, index), 10, 64)
	return n
}
