package surf

import (
	"context"
	"net/http"
	"sync"
	"time"
)

// CurrentTrace returns TraceSpan that is attached to given context. This
// function always returns valid TraceSpan implementation and it is safe to use
// the result. When no trace is attached to context, implementation that is
// discarding results is provided.
// CurrentTrace alwasys returns first TraceSpan that covers the whole
// measurement period.
func CurrentTrace(ctx context.Context) TraceSpan {
	tr, ok := ctx.Value("surf:trace").(*trace)
	if !ok || tr == nil {
		return discardTraceSpan{}
	}
	return tr.spans[0]
}

type TraceSpan interface {
	// Begin creates and remove new measurement span. Current span is set
	// as parent of newly created and returned one.
	//
	// It is users responsibility to finish span.
	Begin(description string, keyvalues ...string) TraceSpan

	// Finish close given span and finalize measurement.
	Finish(keyvalues ...string)
}

// TracingMiddleware provides trace in request's context with given frequency.
func TracingMiddleware(frequency time.Duration) Middleware {
	return func(handler interface{}) Handler {
		ticker := time.NewTicker(frequency)

		withtrace := func() bool {
			select {
			case <-ticker.C:
				return true
			default:
				return false
			}
		}

		h := AsHandler(handler)
		return HandlerFunc(func(w http.ResponseWriter, r *http.Request) Response {
			if !withtrace() {
				return h.HandleHTTPRequest(w, r)
			}
			ctx, t := attachTrace(r.Context(), "ServeHTTP", "")
			r = r.WithContext(ctx)
			return &tracedResponse{
				trace: t,
				resp:  h.HandleHTTPRequest(w, r),
			}
		})
	}
}

type tracedResponse struct {
	trace *trace
	resp  Response
}

func (tr *tracedResponse) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if tr.resp != nil {
		tr.resp.ServeHTTP(w, r)
	}
	tr.trace.finalize()
}

func attachTrace(ctx context.Context, name, parent string) (context.Context, *trace) {
	tr := &trace{
		now: time.Now,
		spans: []*span{
			{
				ID:          generateID(),
				Parent:      parent,
				Description: name,
				Args:        nil,
				Start:       time.Now(),
			},
		},
	}
	tr.spans[0].trace = tr

	ctx = context.WithValue(ctx, "surf:trace", tr)
	return ctx, tr
}

type trace struct {
	now func() time.Time

	mu    sync.Mutex
	spans []*span
}

func (tr *trace) finalize() {
	tr.mu.Lock()
	now := tr.now()
	for _, s := range tr.spans {
		if s.End == nil {
			s.End = &now
		}
	}
	tr.mu.Unlock()
}

type span struct {
	trace *trace

	ID          string
	Parent      string
	Description string
	Args        []string
	Start       time.Time
	End         *time.Time
}

func (s *span) Begin(description string, keyvalues ...string) TraceSpan {
	if len(keyvalues)%2 == 1 {
		keyvalues = append(keyvalues, "")
	}
	ns := &span{
		trace:       s.trace,
		ID:          generateID(),
		Parent:      s.ID,
		Description: description,
		Start:       s.trace.now(),
		Args:        keyvalues,
	}

	s.trace.mu.Lock()
	s.trace.spans = append(s.trace.spans, ns)
	s.trace.mu.Unlock()

	return ns
}

func (s *span) Finish(keyvalues ...string) {
	now := s.trace.now()
	s.End = &now

	if len(keyvalues)%2 == 1 {
		keyvalues = append(keyvalues, "")
	}
	s.Args = append(s.Args, keyvalues...)
}

type discardTraceSpan struct{}

func (d discardTraceSpan) Begin(string, ...string) TraceSpan { return d }
func (d discardTraceSpan) Finish(...string)                  {}
