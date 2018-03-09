package surf

import (
	"container/list"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"sync"
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
		requestID := r.URL.Query().Get("req")
		c, ok := dt.reqInfo(requestID)
		if !ok {
			fmt.Fprintln(w, "no request information")
		} else {
			tmpl.Execute(w, c)
		}
		return
	}

	debugID := generateID()
	ctx := r.Context()

	var logrec logRecorder
	ctx = attachLogger(r.Context(), &logrec)

	r = r.WithContext(ctx)

	dt.handler.ServeHTTP(w, r)

	if w.Header().Get("Content-Type") == "text/html" {
		fmt.Fprintf(w, `
			<a style="position:absolute;top:4px;right:4px;" target="_blank" href="%s?req=%s">DT</a>
		`, dt.rootPath, debugID)
	}

	var traceSpans []*span
	if tr, ok := ctx.Value("surf:trace").(*trace); ok {
		traceSpans = tr.spans
	}
	dt.addReqInfo(debugtoolbarContext{
		RequestID:     debugID,
		RequestURL:    r.URL,
		RequestHeader: r.Header,
		TraceSpans:    traceSpans,
		LogEntries:    logrec.entries,
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
	RequestID     string
	RequestURL    *url.URL
	RequestHeader http.Header

	TraceSpans []*span
	LogEntries []*logEntry
}

var tmpl = template.Must(template.New("").Parse(`
<!doctype html>
<link rel="stylesheet" href="//fonts.googleapis.com/css?family=Roboto:300,300italic,700,700italic">
<link rel="stylesheet" href="//cdn.rawgit.com/necolas/normalize.css/master/normalize.css">
<link rel="stylesheet" href="https://cdnjs.cloudflare.com/ajax/libs/milligram/1.3.0/milligram.min.css" integrity="sha256-Ro/wP8uUi8LR71kwIdilf78atpu8bTEwrK5ZotZo+Zc=" crossorigin="anonymous">
<div>
	<h1>MOAR SVG!</h1>

	<h2>Request</h2>
	<table>
		<tbody>
			<tr>
				<th>ID</th>
				<th>{{.RequestID}}</th>
			</tr>
			<tr>
				<th>URL</th>
				<th>{{.RequestURL}}</th>
			</tr>
			<tr>
				<th>Header</th>
				<th>
					<table>
						{{range $key, $value := .RequestHeader}}
							<tr>
								<td>{{$key}}</td>
								<td><code>{{$value}}</code></td>
							</td>
						{{end}}
					</table>
				</th>
			</tr>
		</tbody>
	</table>

	{{if .TraceSpans}}
		<h2>Traces</h2>
		<table>
		<tr>
			<th>Description</th>
			<th>Duration</th>
			<th>Args</th>
		</tr>
		{{range .TraceSpans}}
			<tr>
				<td>{{.Description}}</td>
				<td>{{.End.Sub .Start}}</td>
				<td>{{if .Args}}{{.Args}}{{else}}-{{end}}</td>
			</tr>
		{{end}}
		</table>
	{{end}}

	{{if .LogEntries}}
		<h2>Log messages</h2>
		<table>
		<tr>
			<th>Created</th>
			<th>Level</th>
			<th>Message</th>
			<th>Args</th>
		</tr>
		{{range .LogEntries}}
			<tr>
				<td>{{.Created.Format "15:04:05.000"}}</td>
				<td>{{.Level}}</td>
				<td>
					{{- if .Error}}
						<span>{{.Error}}</span>:
					{{end -}}
					{{.Message}}
				</td>
				<td>{{if .Args}}{{.Args}}{{else}}-{{end}}</td>
			</tr>
		{{end}}
	{{end}}
</div>
`))
