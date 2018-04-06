package surf

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"runtime/debug"
	"strings"
	"sync"
	"time"
)

type Logger interface {
	Info(context.Context, string, ...string)
	Error(context.Context, error, string, ...string)
}

func LoggingMiddleware(logger Logger) Middleware {
	return func(handler interface{}) Handler {
		h := AsHandler(handler)
		return HandlerFunc(func(w http.ResponseWriter, r *http.Request) Response {
			ctx := attachLogger(r.Context(), logger)
			r = r.WithContext(ctx)
			return h.HandleHTTPRequest(w, r)
		})
	}
}

func attachLogger(ctx context.Context, logger Logger) context.Context {
	if current, ok := ctx.Value("surf:logger").(Logger); ok {
		logger = broadcastLogs(current, logger)
	}
	return context.WithValue(ctx, "surf:logger", logger)
}

// LogInfo writes info log message to logger present in given context. Message is
// discarded if no logger is present in context.
func LogInfo(ctx context.Context, message string, keyvals ...string) {
	if log, ok := ctx.Value("surf:logger").(Logger); ok {
		log.Info(ctx, message, keyvals...)
	}
}

// LogError writes info log message to logger present in given context. Message is
// discarded if no logger is present in context.
func LogError(ctx context.Context, err error, message string, keyvals ...string) {
	if log, ok := ctx.Value("surf:logger").(Logger); ok {
		log.Error(ctx, err, message, keyvals...)
	}
}

func NewLogger(out io.Writer, keyvals ...string) Logger {
	if len(keyvals)%2 == 1 {
		keyvals = append(keyvals, "")
	}
	return &logger{
		out:     out,
		keyvals: keyvals,
	}
}

type logger struct {
	out     io.Writer
	keyvals []string
}

func (lg *logger) log(ctx context.Context, keyvalues ...string) {
	if len(keyvalues)%2 == 1 {
		keyvalues = append(keyvalues, "")
	}

	keyvalues = append(keyvalues, lg.keyvals...)

	// can be done better
	fmt.Fprintln(lg.out, strings.Repeat("-", 80))
	for i := 0; i < len(keyvalues); i += 2 {
		key, value := keyvalues[i], keyvalues[i+1]
		key = strings.Repeat(" ", 14-len(key)) + key
		fmt.Fprintln(lg.out, key, " = ", value)
	}
}

func (lg *logger) Info(ctx context.Context, message string, keyvalues ...string) {
	pairs := append([]string{
		"message", message,
		"level", "info",
		"time", time.Now().Format(time.RFC3339Nano),
	}, keyvalues...)
	lg.log(ctx, pairs...)
}

func (lg *logger) Error(ctx context.Context, err error, message string, keyvalues ...string) {
	// TODO: include stack and source location
	pairs := append([]string{
		"message", message,
		"error", err.Error(),
		"level", "error",
		"stack", string(debug.Stack()),
		"time", time.Now().Format(time.RFC3339Nano),
	}, keyvalues...)
	lg.log(ctx, pairs...)
}

// Discard returns Logger instance that drops all entries. It's the /dev/null
// of loggers
func Discard() Logger {
	return &discardLogger{}
}

type discardLogger struct{}

var _ Logger = (*discardLogger)(nil)

func (discardLogger) Info(ctx context.Context, message string, keyvals ...string) {
}

func (discardLogger) Error(ctx context.Context, err error, message string, keyvals ...string) {
}

func broadcastLogs(loggers ...Logger) Logger {
	switch len(loggers) {
	case 0:
		return Discard()
	case 1:
		return loggers[0]
	default:
		return broadcastingLogger(loggers)
	}
}

type broadcastingLogger []Logger

func (loggers broadcastingLogger) Info(ctx context.Context, message string, keyvals ...string) {
	for _, lg := range loggers {
		lg.Info(ctx, message, keyvals...)
	}
}

func (loggers broadcastingLogger) Error(ctx context.Context, err error, message string, keyvals ...string) {
	for _, lg := range loggers {
		lg.Error(ctx, err, message, keyvals...)
	}
}

type logRecorder struct {
	sync.Mutex
	entries []*logEntry
}

type logEntry struct {
	Created time.Time
	Level   string
	Error   error
	Message string
	Args    map[string]string
}

func (lr *logRecorder) Info(ctx context.Context, message string, keyvals ...string) {
	lr.log("info", nil, message, keyvals)
}

func (lr *logRecorder) Error(ctx context.Context, err error, message string, keyvals ...string) {
	lr.log("error", err, message, keyvals)
}

func (lr *logRecorder) log(level string, err error, message string, keyvals []string) {
	args := make(map[string]string)
	if len(keyvals)%2 == 1 {
		keyvals = append(keyvals, "")
	}
	for i := 0; i < len(keyvals); i += 2 {
		args[keyvals[i]] = keyvals[i+1]
	}

	entry := &logEntry{
		Created: time.Now(),
		Level:   level,
		Message: message,
		Error:   err,
		Args:    args,
	}

	lr.Lock()
	lr.entries = append(lr.entries, entry)
	lr.Unlock()
}
