package surf

import (
	"fmt"
	"net/http"
	"runtime/debug"
	"time"
)

func NewHTTPApplication(app interface{}, logger Logger, debug bool) http.Handler {
	middlewares := []Middleware{
		PanicMiddleware(logger),
		LoggingMiddleware(logger),
		TracingMiddleware(time.Second),
	}

	if debug {
		middlewares = append(middlewares, DebugToolbarMiddleware("/_/debugtoolbar/"))
	}

	return &application{
		h: WithMiddlewares(AsHandler(app), middlewares),
	}
}

type application struct {
	h Handler
}

func (app *application) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h := app.h.HandleHTTPRequest(w, r); h != nil {
		// this is called outside of any middleware!
		defer CurrentTrace(r.Context()).Begin("writing response").Finish()
		h.ServeHTTP(w, r)
	}
}

func PanicMiddleware(logger Logger) Middleware {
	return func(handler interface{}) Handler {
		h := AsHandler(handler)
		return HandlerFunc(func(w http.ResponseWriter, r *http.Request) Response {
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
					PanicErr  interface{}
					Stack     []stackLine
					FullStack string
				}{
					PanicErr:  panicErr,
					Stack:     stack,
					FullStack: string(debug.Stack()),
				})

			}()

			return h.HandleHTTPRequest(w, r)
		})
	}
}
