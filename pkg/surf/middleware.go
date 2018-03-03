package surf

import "net/http"

type Middleware func(http.Handler) http.Handler

// WithMiddlewares attach given collection of middlewares. First one is called last.
func WithMiddlewares(h http.Handler, middlewares []Middleware) http.Handler {
	// apply in reverse order
	for i := len(middlewares) - 1; i >= 0; i-- {
		h = middlewares[i](h)
	}
	return h
}
