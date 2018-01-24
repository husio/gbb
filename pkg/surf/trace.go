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
	// Start creates and remove new measurement span. Current span is set
	// as parent of newly created and returned one.
	//
	// It is users responsibility to finish span.
	Start(description string, args map[string]string) TraceSpan

	// Finish close given span and finalize measurement.
	Finish(args map[string]string)
}

// GatherTrace attach trace to request's context with given frequency.
func GatherTrace(frequency time.Duration, h http.Handler) http.HandlerFunc {
	ticker := time.NewTicker(frequency)

	withtrace := func() bool {
		select {
		case <-ticker.C:
			return true
		default:
			return false
		}
	}

	return func(w http.ResponseWriter, r *http.Request) {
		if withtrace() {
			ctx, t := attachTrace(r.Context(), "ServeHTTP", "")
			r = r.WithContext(ctx)
			defer t.finalize()
		}

		h.ServeHTTP(w, r)
	}
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
				Begin:       time.Now(),
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
	Args        map[string]string
	Begin       time.Time
	End         *time.Time
}

func (s *span) Start(description string, args map[string]string) TraceSpan {
	ns := &span{
		trace:       s.trace,
		ID:          generateID(),
		Parent:      s.ID,
		Description: description,
		Begin:       s.trace.now(),
		Args:        args,
	}

	s.trace.mu.Lock()
	s.trace.spans = append(s.trace.spans, ns)
	s.trace.mu.Unlock()

	return ns
}

func (s *span) Finish(args map[string]string) {
	now := s.trace.now()
	s.End = &now

	if args != nil {
		if s.Args == nil {
			s.Args = args
		} else {
			for k, v := range args {
				s.Args[k] = v
			}
		}
	}
}

type discardTraceSpan struct{}

func (d discardTraceSpan) Start(string, map[string]string) TraceSpan { return d }
func (discardTraceSpan) Finish(map[string]string)                    {}
