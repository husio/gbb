package surf

import (
	"html/template"
	"io"
)

func renderDebugToolbar(w io.Writer, context debugToolbarContext) error {
	return tmpl.Execute(w, context)
}

type debugToolbarContext struct {
	TraceSpans []*span
	LogEntries []*logEntry
}

var tmpl = template.Must(template.New("").Parse(`
<style type="text/css">
#surf-debug-toolbar { background: #FFFEF0; border-left: 2px solid #eee; padding: 30px 10px 10px 10px; height: 100%; position: fixed; right: 0; top: 0; bottom: 0;}
#surf-debug-toolbar.hide { display: none; }
#surf-debug-toolbar-toggle { position: absolute; top: 2px; right: 2px; z-index: 10000; background: #78E2FF; border: 1px solid #75C1D6; border-radius: 4px; padding: 10px; font-size: 16px; font-weight: bold; height: 3.2em; }
#surf-debug-toolbar h2 { border-bottom: 1px solid #ddd; padding-top: 10px; }
#surf-debug-toolbar table { width: 100%; text-align: left; }
</style>
<button id="surf-debug-toolbar-toggle" onClick="document.getElementById('surf-debug-toolbar').classList.toggle('hide')">DT</button>
<div id="surf-debug-toolbar" class="hide">
	{{if .TraceSpans}}
		<h2>Traces</h2>
		<table>
		<tr>
			<th>Span ID</th>
			<th>Parent ID</th>
			<th>Description</th>
			<th>Duration</th>
			<th>Args</th>
		</tr>
		{{range .TraceSpans}}
			<tr>
				<td>{{.ID}}</td>
				<td>{{.Parent}}</td>
				<td>{{.Description}}</td>
				<td>{{.End.Sub .Begin}}</td>
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
