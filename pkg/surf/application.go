package surf

import (
	"fmt"
	"net/http"
	"os"
	"runtime/debug"
)

func NewHTTPApplication(h http.Handler, debug bool, logger Logger) http.Handler {
	if debug {
		return &debugApplication{
			logger:  logger,
			handler: h,
		}
	}
	return &productionApplication{
		logger:  logger,
		handler: h,
	}
}

type productionApplication struct {
	logger  Logger
	handler http.Handler
}

func (app *productionApplication) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ctx = attachLogger(ctx, app.logger)

	app.handler.ServeHTTP(w, r.WithContext(ctx))
}

type debugApplication struct {
	logger  Logger
	handler http.Handler
}

func (app *debugApplication) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var logRec logRecorder
	ctx = attachLogger(ctx, broadcastLogs(app.logger, &logRec))

	ctx, tr := attachTrace(ctx)

	defer func() {
		finalizeTrace(tr)

		err := renderDebugToolbar(w, debugToolbarContext{
			TraceSpans: tr.spans,
			LogEntries: logRec.entries,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "cannot render debug toolbar template: %s", err)
		}
	}()
	defer handlePanic(w, r)

	app.handler.ServeHTTP(w, r.WithContext(ctx))
}

func handlePanic(w http.ResponseWriter, r *http.Request) {
	panicErr := recover()
	if panicErr == nil {
		return
	}

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
}
