package surf

import (
	"fmt"
	"net/http"
	"runtime/debug"
	"time"
)

func NewHTTPApplication(app http.Handler, logger Logger, debug bool) http.Handler {
	middlewares := []Middleware{
		PanicMiddleware(logger),
		LoggingMiddleware(logger),
		TracingMiddleware(time.Second),
	}

	if debug {
		middlewares = append(middlewares, DebugToolbarMiddleware("/_/debugtoolbar/"))
	}

	return WithMiddlewares(app, middlewares)
}

func PanicMiddleware(logger Logger) Middleware {
	return func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				panicErr := recover()
				if panicErr == nil {
					return
				}

				err, ok := panicErr.(error)
				if !ok {
					err = fmt.Errorf("panic: %s", panicErr)
				}
				logger.Error(r.Context(), err, "panic")

				w.WriteHeader(http.StatusInternalServerError)
				// TODO make stack nice

				stack, err := stackInformation(1, 8)
				if err != nil {
					panic("cannot read stack information: " + err.Error())
				}
				defaultTemplate().ExecuteTemplate(w, "surf/panic_error.tmpl", struct {
					Request   *http.Request
					PanicErr  interface{}
					Stack     []stackLine
					FullStack string
				}{
					Request:   r,
					PanicErr:  panicErr,
					Stack:     stack,
					FullStack: string(debug.Stack()),
				})

			}()

			h.ServeHTTP(w, r)
		})
	}
}
