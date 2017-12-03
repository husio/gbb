package surf

import (
	"context"
	"sync"
	"time"
)

type TraceSpan interface {
	StartSpan(description string, args map[string]string) TraceSpan
	FinishSpan(args map[string]string)
}

func attachTrace(ctx context.Context) (context.Context, *trace) {
	tr := &trace{
		now: time.Now,
		spans: []*span{
			{
				ID:          generateID(),
				Parent:      "",
				Description: "Handler",
				Args:        nil,
				Begin:       time.Now(),
			},
		},
	}
	tr.spans[0].trace = tr

	ctx = context.WithValue(ctx, "surf:trace", tr)
	return ctx, tr
}

func finalizeTrace(tr *trace) {
	tr.mu.Lock()
	now := tr.now()
	for _, s := range tr.spans {
		if s.End == nil {
			s.End = &now
		}
	}
	tr.mu.Unlock()
}

func CurrentSpan(ctx context.Context) TraceSpan {
	tr, ok := ctx.Value("surf:trace").(*trace)
	if !ok || tr == nil {
		return discardTraceSpan{}
	}
	return tr.spans[0]
}

type trace struct {
	now func() time.Time

	mu    sync.Mutex
	spans []*span
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

func (s *span) StartSpan(description string, args map[string]string) TraceSpan {
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

func (s *span) FinishSpan(args map[string]string) {
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

func (d discardTraceSpan) StartSpan(string, map[string]string) TraceSpan { return d }
func (discardTraceSpan) FinishSpan(map[string]string)                    {}
