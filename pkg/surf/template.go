package surf

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
	"net/http"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	texttemplate "text/template"
)

type HTMLRenderer interface {
	Response(ctx context.Context, statusCode int, templateName string, templateContext interface{}) Response
}

// StdResponse returns Response instance with generic HTML page for given
// return code.
func StdResponse(ctx context.Context, r HTMLRenderer, responseCode int) Response {
	return r.Response(ctx, responseCode, "stdresponse.tmpl", struct {
		Code        int
		Title       string
		Description string
	}{
		Code:  responseCode,
		Title: http.StatusText(responseCode),
	})
}

// Redirect returns Response instance that redirects client to another endpoint.
func Redirect(url string, responseCode int) Response {
	return &redirectResponse{
		code: responseCode,
		url:  url,
	}
}

type redirectResponse struct {
	code int
	url  string
}

func (rr *redirectResponse) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, rr.url, rr.code)
}

// NewHTMLRenderer returns HTMLRenderer instance. Default surf template set is
// extended by templates found in provided path and by passed function mapping.
//
// When running in debug mode, templates are always compiled before rendering.
// In non debug mode, templates are compiled once and reused to achieve better
// performance.
// When in debug mode, all template errors are rendered with explanation and
// additional information, instead of generic error page.
func NewHTMLRenderer(templatesGlob string, debug bool, funcs template.FuncMap) HTMLRenderer {
	renderer := &htmlRenderer{
		debug:         debug,
		funcs:         funcs,
		templatesGlob: templatesGlob,
	}
	return renderer
}

type htmlRenderer struct {
	debug         bool
	funcs         template.FuncMap
	templatesGlob string

	mu       sync.RWMutex
	template *template.Template
}

func (rend *htmlRenderer) Response(ctx context.Context, statusCode int, templateName string, templateContext interface{}) Response {
	rootSpan := CurrentTrace(ctx).Begin("html renderer",
		"template", templateName)
	defer rootSpan.Finish()

	var (
		err  error
		tmpl *template.Template
	)
	if rend.debug {
		compileSpan := rootSpan.Begin("compiling templates")
		tmpl, err = defaultTemplate().Funcs(rend.funcs).ParseGlob(rend.templatesGlob)
		compileSpan.Finish()
		if err != nil {
			return rend.renderTemplateParseError(templateName, err)
		}
	} else {
		rend.mu.RLock()
		if rend.template != nil {
			tmpl = rend.template
			rend.mu.RUnlock()
		} else {
			rend.mu.RUnlock()

			rend.mu.Lock()
			if rend.template != nil {
				tmpl = rend.template
			} else {
				compileSpan := rootSpan.Begin("compiling templates")
				tmpl, err = defaultTemplate().Funcs(rend.funcs).ParseGlob(rend.templatesGlob)
				compileSpan.Finish()
				if err != nil {
					LogError(ctx, err, "cannot compile templates",
						"template", templateName)
					return &htmlResponse{
						code: http.StatusInternalServerError,
						body: strings.NewReader(`<!doctype html>Internal Server Error`),
					}
				}
			}
			rend.mu.Unlock()
		}
	}

	var b bytes.Buffer
	execSpan := rootSpan.Begin("executing template")
	err = tmpl.ExecuteTemplate(&b, templateName, templateContext)
	execSpan.Finish()
	if err != nil {
		if !rend.debug {
			LogError(ctx, err, "cannot execute template",
				"template", templateName)
			return &htmlResponse{
				code: http.StatusInternalServerError,
				body: strings.NewReader(`<!doctype html>Internal Server Error`),
			}
		}
		return rend.renderTemplateExecError(templateName, err)
	}

	return &htmlResponse{
		code: statusCode,
		body: &b,
	}
}

type htmlResponse struct {
	code int
	body io.Reader
}

func (resp *htmlResponse) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	header := w.Header()
	header.Set("content-type", "text/html; charset=utf-8")
	w.WriteHeader(resp.code)
	io.Copy(w, resp.body)
}

func (rend *htmlRenderer) renderTemplateParseError(templateName string, tmplErr error) Response {
	templateFile, errLineNo, description, err := parseTemplateParseError(tmplErr)
	if err != nil {
		// this is failure that happened in debug mode, so it is
		// acceptable to leak out internal error information
		stack, _ := stackInformation(1, 8)
		var body bytes.Buffer
		err := defaultTemplate().ExecuteTemplate(&body, "surf/panic_error.tmpl", struct {
			PanicErr  interface{}
			Stack     []stackLine
			FullStack string
		}{
			PanicErr:  err,
			Stack:     stack,
			FullStack: string(debug.Stack()),
		})
		if err != nil {
			panic(err)
		}
		return &htmlResponse{
			code: http.StatusInternalServerError,
			body: &body,
		}
	}

	var codeLines []codeLine
	if templateFiles, err := filepath.Glob(rend.templatesGlob); err == nil {
		found := false
		for _, filename := range templateFiles {
			if strings.HasSuffix(filename, templateFile) {
				templateFile = filename
				found = true
				break
			}
		}
		if found {
			if content, err := ioutil.ReadFile(templateFile); err == nil {
				errLineNo = adjustTemplateTrimming(errLineNo, content) - 1

				codeLines = make([]codeLine, 0, 100)
				for i, line := range bytes.Split(content, []byte("\n")) {
					if i > errLineNo+showCodeSurrounding || i < errLineNo-showCodeSurrounding {
						continue
					}
					codeLines = append(codeLines, codeLine{
						Number:    i,
						Highlight: i == errLineNo,
						Content:   string(line),
					})
				}
			}
		}
	}

	stack, _ := stackInformation(1, 8)
	var b bytes.Buffer
	err = defaultTemplate().ExecuteTemplate(&b, "surf/render_error.tmpl", renderErrorContext{
		Title:           "Cannot Parse Template",
		Description:     description,
		Stack:           stack,
		TemplateName:    templateName,
		TemplateFile:    templateFile,
		TemplateContent: codeLines,
	})
	if err != nil {
		return &htmlResponse{
			code: http.StatusInternalServerError,
			body: strings.NewReader(fmt.Sprintf(`<!doctype html>
				cannot render error template: %s`, err)),
		}
	}
	return &htmlResponse{
		code: http.StatusInternalServerError,
		body: &b,
	}
}

func stackInformation(maxDepth, stackFileSurrounding int) ([]stackLine, error) {
	skip := 4 // TODO: detect what should be the depth

	stackLines := make([]stackLine, 0, 8)
	for n, line := range bytes.Split(debug.Stack(), []byte("\n")) {
		if n < (skip*2)+2 || n > (skip+maxDepth)*2 || n%2 == 1 {
			continue
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}

		// get rid of the address
		line = bytes.SplitN(line, []byte{' '}, 2)[0]

		chunks := bytes.SplitN(line, []byte(":"), 2)
		if len(chunks) != 2 {
			return nil, fmt.Errorf("invalid stack line format: %q", line)
		}
		lineNo, _ := strconv.ParseInt(string(chunks[1]), 10, 32)
		filePath := string(chunks[0])

		lines, err := fileLines(filePath, int(lineNo), stackFileSurrounding)
		if err != nil {
			return nil, fmt.Errorf("cannot read %q file: %s", filePath, err)
		}
		stackLines = append(stackLines, stackLine{
			FilePath: filePath,
			LineNo:   int(lineNo),
			Content:  lines,
		})
	}

	return stackLines, nil
}

func fileLines(filePath string, highlightLineNo, surrounding int) ([]codeLine, error) {
	content, err := ioutil.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("cannot read file: %s", err)
	}

	codeLines := make([]codeLine, 0, surrounding*2+1)
	for i, line := range bytes.Split(content, []byte("\n")) {
		lineNo := i + 1
		if lineNo < highlightLineNo-surrounding {
			continue
		}
		if lineNo > highlightLineNo+surrounding {
			break
		}
		codeLines = append(codeLines, codeLine{
			Number:    lineNo,
			Highlight: lineNo == highlightLineNo,
			Content:   string(line),
		})
	}

	return codeLines, nil
}

func adjustTemplateTrimming(lineNo int, templateContent []byte) int {
	// TODO: use runes

	tc := templateContent
	for i := 0; i < len(tc)-4; i++ {
		// cut on the left
		// {{-
		if tc[i] == '{' && tc[i+1] == '{' && tc[i+2] == '-' && tc[i+3] == ' ' {
			added := 0
		skipEmptyLeft:
			for n := i - 1; n >= 0; n-- {
				switch tc[n] {
				case '\n':
					added++
					lineNo++
				case ' ', '\t':
				default:
					if n > 3 && tc[n-3] == ' ' && tc[n-2] == '-' && tc[n-1] == '}' && tc[n-0] == '}' {
						// there is only empty (white) space between '-}}' and '{{-' so don't count it twice
						lineNo -= added
					}
					break skipEmptyLeft
				}
			}
		}

		// cut on the right
		// -}}
		if tc[i] == ' ' && tc[i+1] == '-' && tc[i+2] == '}' && tc[i+3] == '}' {
		skipEmptyRight:
			for n := i + 4; n < len(tc); n++ {
				switch tc[n] {
				case '\n':
					lineNo++
				case ' ', '\t':
				default:
					break skipEmptyRight
				}
			}
		}
	}
	return lineNo
}

func (rend *htmlRenderer) renderTemplateExecError(templateName string, tmplErr error) Response {
	stack, _ := stackInformation(1, 8)
	if execErr, ok := tmplErr.(texttemplate.ExecError); ok {
		templateFile, errLineNo, _, _, description := parseTemplateExecError(execErr.Err)

		templateFiles, err := filepath.Glob(rend.templatesGlob)
		if err != nil {
			panic(err) // TODO
		}
		found := false
		for _, filename := range templateFiles {
			if strings.HasSuffix(filename, templateFile) {
				templateFile = filename
				found = true
				break
			}
		}
		if !found {
			// TODO
			return &htmlResponse{
				code: http.StatusInternalServerError,
				body: strings.NewReader(fmt.Sprintf(`<!doctype html>
				template file not found: %q\n`, templateFile)),
			}
		}
		templateContent, err := fileLines(templateFile, errLineNo, 8)
		if err != nil {
			panic("cannot read tempalte: " + err.Error())
		}

		var b bytes.Buffer
		err = defaultTemplate().ExecuteTemplate(&b, "surf/render_error.tmpl", renderErrorContext{
			Title:           "Cannot execute template",
			Description:     description,
			Stack:           stack,
			TemplateName:    templateName,
			TemplateFile:    templateFile,
			TemplateContent: templateContent,
		})
		if err != nil {
			return &htmlResponse{
				code: http.StatusInternalServerError,
				body: strings.NewReader(fmt.Sprintf(`<!doctype html>
				cannot render error template: %s`, err)),
			}
		}
		return &htmlResponse{
			code: http.StatusInternalServerError,
			body: &b,
		}
	}

	var body bytes.Buffer
	err := defaultTemplate().ExecuteTemplate(&body, "surf/render_error.tmpl", renderErrorContext{
		Title:        "Template not found",
		Description:  "Template not defined or not in search directory.",
		Stack:        stack,
		TemplateName: templateName,
	})
	if err != nil {
		panic(err)
	}
	return &htmlResponse{
		code: http.StatusInternalServerError,
		body: &body,
	}
}

func parseTemplateParseError(err error) (string, int, string, error) {
	// error must be in format
	//   template: <name>:<line>:<whatever...>
	// for example
	//   template: users_list.tmpl:9: unexpected unclosed action in if
	rx := regexp.MustCompile(`^template: ([^:]+):(\d+):(.*)$`)
	result := rx.FindStringSubmatch(err.Error())
	if len(result) != 4 {
		return "", 0, "", fmt.Errorf("cannot parse template error: %q", err)
	}
	fileName := string(result[1])
	lineNo, _ := strconv.ParseInt(string(result[2]), 10, 32)
	description := string(result[3])
	return fileName, int(lineNo), description, nil
}

func parseTemplateExecError(err error) (string, int, int, string, string) {
	// error must be in format
	//   template: <name>:<line>:<offset>: <message1>: <message2>
	// for example
	//   template: users.tmpl:14:4: executing "users.tmpl" at <.Foo>: can't evaluate field Foo in type gbb.Content
	rx := regexp.MustCompile(`^template: ([^:]+):(\d+):(\d+): ([^:]+): (.*)$`)
	result := rx.FindStringSubmatch(err.Error())
	if len(result) != 6 {
		panic(fmt.Sprintf("cannot parse template error: %q", err))
	}
	fileName := string(result[1])
	lineNo, _ := strconv.ParseInt(string(result[2]), 10, 32)
	offset, _ := strconv.ParseInt(string(result[3]), 10, 32)
	desc1 := string(result[4])
	desc2 := string(result[5])
	return fileName, int(lineNo), int(offset), desc1, desc2
}

type renderErrorContext struct {
	Title           string
	Description     string
	Stack           []stackLine
	TemplateName    string
	TemplateFile    string
	TemplateContent []codeLine
}

type stackLine struct {
	FilePath string
	LineNo   int
	Content  []codeLine
}

type codeLine struct {
	Number    int
	Highlight bool
	Content   string
}

type defaultHtmlRenderer struct {
	tmpl *template.Template
}

func newDefaultRenderer() HTMLRenderer {
	return &defaultHtmlRenderer{
		tmpl: defaultTemplate(),
	}
}

func (rend *defaultHtmlRenderer) Response(ctx context.Context, statusCode int, templateName string, templateContext interface{}) Response {
	var b bytes.Buffer
	if err := rend.tmpl.ExecuteTemplate(&b, templateName, templateContext); err != nil {
		panic(err)
	}
	return &htmlResponse{
		code: statusCode,
		body: &b,
	}
}

// defaultTemplate is used as a fallback and guarantee that certain templates
// are defined.
func defaultTemplate() *template.Template {
	return template.Must(template.New("").Parse(`


{{define "surf/error_header.tmpl" -}}
	<!doctype html>
	<meta charset="utf-8">
	<meta name="viewport" content="width=device-width, initial-scale=1">
{{end}}


{{define "surf/error_css.tmpl"}}
	<style type="text/css">
	body  { margin: 20px auto; max-width: 800px; line-height: 1.6; font-size: 16px; }
	pre   { margin: 0; padding: 0; }
	code  { display: block; width: 100%; font-size: 10px; }
	code.highlight { background: #FFE4A6; }
	em    { background: #D6E4FF; padding: 2px 4px; }
	.template-code { widht: 100%; overflow: auto; }
	.file-content { widht: 100%; overflow: auto; padding: 10px 0; }
	.line-number { color: #8C8C8C; display: inline-block; width: 24px; }
	.description { background: #FCC8C8; padding: 10px; }
	</style>
{{end}}


{{define "stdresponse.tmpl" -}}
	{{template "surf/error_header.tmpl"}}
	{{template "surf/error_css.tmpl"}}
	<title>{{.Title}}</title>
	<h1>{{.Title}}: {{.Code}}</h1>
	{{if .Description -}}
		<p>{{.Description}}</p>
	{{- end}}
{{- end}}


{{define "surf/render_error.tmpl" -}}
	{{template "surf/error_header.tmpl"}}
	{{template "surf/error_css.tmpl"}}

	<h1>{{.Title}}</h1>
	{{if .Description -}}
		<p class="description">{{.Description}}</p>
	{{- end}}

	{{if .TemplateContent -}}
		<h2>Template</h2>
		{{if eq .TemplateFile .TemplateName}}
			<p>Template <em>{{.TemplateName}}</em>:<p>
		{{else}}
			<p>Template <em>{{.TemplateName}}</em> in file <em>{{.TemplateFile}}</em>:</p>
		{{end}}
		<div class="file-content">
		{{range .TemplateContent}}
			<pre><code {{- if .Highlight}} class="highlight"{{end}}><span class="line-number">{{.Number}}</span> {{.Content}}</code></pre>
		{{end}}
		</div>
	{{- end}}

	{{if .Stack -}}
		<h2>Stack</h2>
		{{range .Stack}}
			<em>{{.FilePath}}</em>
			<div class="file-content">
			{{range .Content}}
				<pre><code {{- if .Highlight}} class="highlight"{{end}}><span class="line-number">{{.Number}}</span> {{.Content}}</code></pre>
			{{end}}
			</div>
		{{end}}
	{{- end}}
{{end}}


{{define "surf/panic_error.tmpl" -}}
	{{template "surf/error_header.tmpl"}}
	{{template "surf/error_css.tmpl"}}

	<h1>Application crashed</h1>
	<p>{{.PanicErr}}</h2>

	{{if .Stack}}
	<h2>Location</h2>
	{{range .Stack}}
		<em>{{.FilePath}}</em>
		<div class="file-content">
		{{range .Content}}
			<pre><code {{- if .Highlight}} class="highlight"{{end}}><span class="line-number">{{.Number}}</span> {{.Content}}</code></pre>
		{{end}}
		</div>
	{{end}}
	{{end}}

	<h2>Full stack</h2>
	<code><pre>{{.FullStack}}</pre></code>
{{end}}

`))
}

const showCodeSurrounding = 5
