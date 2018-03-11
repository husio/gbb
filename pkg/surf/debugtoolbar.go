package surf

import (
	"container/list"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

func DebugToolbarMiddleware(rootPath string) Middleware {
	return func(h http.Handler) http.Handler {
		return &debugtoolbarMiddleware{
			handler:  h,
			rootPath: rootPath,
			history:  list.New(),
		}
	}
}

type debugtoolbarMiddleware struct {
	handler  http.Handler
	rootPath string

	mu      sync.Mutex
	history *list.List
}

func (dt *debugtoolbarMiddleware) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, dt.rootPath) {
		requestID := Path(r.URL.Path).LastChunk()
		c, ok := dt.reqInfo(requestID)
		if !ok {
			fmt.Fprintln(w, "no request information")
		} else {
			if err := tmpl.Execute(w, c); err != nil {
				Error(r.Context(), err, "cannot render surf's debutoolbar")
			}
		}
		return
	}

	debugID := generateID()
	ctx := r.Context()

	var logrec logRecorder
	ctx = attachLogger(r.Context(), &logrec)

	r = r.WithContext(ctx)

	dt.handler.ServeHTTP(w, r)

	if strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") {
		fmt.Fprintf(w, `
			<a style="position:fixed;top:4px;right:4px;" target="_blank" href="%s%s/">DT</a>
		`, dt.rootPath, debugID)
	}

	var traceSpans []*span
	if tr, ok := ctx.Value("surf:trace").(*trace); ok {
		traceSpans = tr.spans
	}
	dt.addReqInfo(debugtoolbarContext{
		RequestID:  debugID,
		RequestURL: r.URL,
		traceSpans: traceSpans,
		LogEntries: logrec.entries,
	})
}

func (dt *debugtoolbarMiddleware) addReqInfo(c debugtoolbarContext) {
	dt.mu.Lock()
	defer dt.mu.Unlock()

	dt.history.PushFront(&c)

	for dt.history.Len() > 25 {
		dt.history.Remove(dt.history.Back())
	}
}

func (dt *debugtoolbarMiddleware) reqInfo(reqID string) (*debugtoolbarContext, bool) {
	dt.mu.Lock()
	defer dt.mu.Unlock()

	for e := dt.history.Front(); e != nil; e = e.Next() {
		c := e.Value.(*debugtoolbarContext)
		if c.RequestID == reqID {
			return c, true
		}
	}
	return nil, false
}

// debugtoolbarContext contains information about single request.
type debugtoolbarContext struct {
	RequestID  string
	RequestURL *url.URL

	traceSpans []*span
	LogEntries []*logEntry
}

func (dc *debugtoolbarContext) TraceSpans() []*tracespan {
	if len(dc.traceSpans) == 0 {
		return nil
	}

	// first span is the longest and covers the whole request
	start := dc.traceSpans[0].Start
	end := dc.traceSpans[0].End
	period := float64(end.Sub(start))

	spans := make([]*tracespan, 0, len(dc.traceSpans))
	for _, span := range dc.traceSpans {
		offset := (float64(span.Start.Sub(start)) / period) * 100
		spans = append(spans, &tracespan{
			SpanID:      span.ID,
			Start:       span.Start,
			End:         *span.End,
			Description: span.Description,
			Args:        span.Args,

			Duration:   span.End.Sub(span.Start),
			OffsetPerc: offset,
			WidthPerc:  (float64(span.End.Sub(start))/period)*100 - offset,
		})
	}
	return spans
}

type tracespan struct {
	SpanID      string
	Description string
	Args        []string
	Start       time.Time
	End         time.Time
	Duration    time.Duration

	OffsetPerc float64
	WidthPerc  float64
}

func (ts *tracespan) ArgPairs() map[string]string {
	if len(ts.Args) == 0 {
		return nil
	}
	pairs := make(map[string]string)
	for i := 0; i < len(ts.Args); i += 2 {
		pairs[ts.Args[i]] = ts.Args[i+1]
	}
	return pairs
}

var tmpl = template.Must(template.New("").Parse(`
<!doctype html>
<link rel="stylesheet" href="//cdn.rawgit.com/necolas/normalize.css/master/normalize.css">
<style>
  body  { margin: 40px auto; max-width: 1000px; line-height: 180%; padding: 0 10px; font-family: sans-serif; }
  * { box-sizing: border-box;  }

  table { width: 100%; border-spacing: 0; }
  table td { padding: 2px 4px; }

  .logentry td { padding: 8px; }
  .logentry.error { background: #FFE8E8; }

  .traces-graph { width: 100%; padding: 2px 140px 8px 0; border: 1px solid #ddd; background: url(data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAoAAAAKCAYAAACNMs+9AAAAH0lEQVQYlWN4+PBh2tevXwlihocPH6YxEANGFVJFIQAPZjvIf8HYugAAAABJRU5ErkJggg==); }
  .traces-graph .bar { border-bottom: 6px solid #B5D6EB; font-size: 10px; white-space: nowrap; padding: 10px 4px 0 4px; line-height: 12px; }
  .traces-graph .bar:first-child { border-color: #72BBE9; }
</style>

<div>
  <h1>{{.RequestURL}}</h1>
  <p>
    Request ID: <code>{{.RequestID}}</code>
  </p>

  {{if .TraceSpans}}
    <h2>Traces</h2>
    <div class="traces-graph">
      {{range .TraceSpans}}
        <div class="bar" style="margin-left: {{.OffsetPerc}}%; width: {{.WidthPerc}}%" title="
Description: {{ .Description}}
Duration:    {{.Duration}}
{{ range $key, $value := .ArgPairs }}
{{$key}}: {{$value}}
{{end -}}
          ">
        {{.Description}}
      </div>
      {{end}}
    </div>
  {{end}}

  {{if .LogEntries}}
    <h2>Log messages</h2>
    <table>
      <tbody>
        {{range .LogEntries}}
          <tr class="logentry {{.Level}}">
            <td>
              {{- if .Error}}
                <strong>{{.Error}}</strong>
              {{end -}}
              {{.Message}}
            </td>
            <td>
              {{if .Args}}
	        <table>
                {{ range $key, $value := .Args }}
                    <tr>
                      <td><code>{{$key}}</code></td>
                      <td>{{$value}}</td>
                    </tr>
                {{end -}}
                </table>
              {{else}}
                -
              {{end}}
            </td>
          </tr>
        {{end}}
      </tbody>
    </table>
  {{end}}
</div>
`))
