package surf

import (
	"bytes"
	"fmt"
	"html/template"
	"io/ioutil"
	"net/http"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"
	texttemplate "text/template"
)

type Renderer interface {
	RenderResponse(w http.ResponseWriter, statusCode int, templateName string, templateContext interface{})
	RenderStdResponse(w http.ResponseWriter, statusCode int)
}

func NewHTMLRenderer(templatesGlob string, funcs template.FuncMap) Renderer {
	renderer := &htmlRenderer{
		funcs:         funcs,
		templatesGlob: templatesGlob,
	}
	return renderer
}

type htmlRenderer struct {
	funcs         template.FuncMap
	templatesGlob string
}

func (rend *htmlRenderer) RenderResponse(w http.ResponseWriter, statusCode int, templateName string, templateContext interface{}) {
	header := w.Header()
	header.Set("content-type", "text/html; charset=utf-8")

	// TODO: cache instead of reading file every time (unless in development mode)
	tmpl, err := defaultTemplate().Funcs(rend.funcs).ParseGlob(rend.templatesGlob)
	if err != nil {
		rend.renderTemplateParseError(w, templateName, err)
		return
	}
	var b bytes.Buffer
	if err := tmpl.ExecuteTemplate(&b, templateName, templateContext); err != nil {
		rend.renderTemplateExecError(w, templateName, err)
		return
	}

	// TODO: support missing template error

	if _, err := b.WriteTo(w); err != nil {
		// TODO: log, but there is no response to be written anymore
	}
}

func (rend *htmlRenderer) renderTemplateParseError(w http.ResponseWriter, templateName string, tmplErr error) {
	templateFile, errLineNo, description := parseTemplateParseError(tmplErr)

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
		fmt.Fprintf(w, "template file not found: %q\n", templateFile)
		return
	}

	content, err := ioutil.ReadFile(templateFile)
	if err != nil {
		// TODO
		fmt.Fprintf(w, "cannot read %q tempalte file: %s\n", templateFile, err)
		return
	}

	errLineNo = adjustTemplateTrimming(errLineNo, content) - 1

	codeLines := make([]codeLine, 0, 100)
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

	stack, err := stackInformation(1, 8)
	if err != nil {
		panic("cannot read stack information: " + err.Error())
	}

	header := w.Header()
	header.Set("content-type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusInternalServerError)
	err = defaultTemplate().ExecuteTemplate(w, "surf/render_error.tmpl", renderErrorContext{
		Title:           "Cannot Parse Template",
		Description:     description,
		Stack:           stack,
		TemplateName:    templateName,
		TemplateFile:    templateFile,
		TemplateContent: codeLines,
	})
	if err != nil {
		panic(err)
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

func (rend *htmlRenderer) renderTemplateExecError(w http.ResponseWriter, templateName string, tmplErr error) {
	stack, err := stackInformation(1, 8)
	if err != nil {
		panic("cannot read stack information: " + err.Error())
	}

	header := w.Header()
	header.Set("content-type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusInternalServerError)

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
			fmt.Fprintf(w, "template file not found: %q\n", templateFile)
			return
		}
		templateContent, err := fileLines(templateFile, errLineNo, 8)
		if err != nil {
			panic("cannot read tempalte: " + err.Error())
		}

		err = defaultTemplate().ExecuteTemplate(w, "surf/render_error.tmpl", renderErrorContext{
			Title:           "Cannot execute template",
			Description:     description,
			Stack:           stack,
			TemplateName:    templateName,
			TemplateFile:    templateFile,
			TemplateContent: templateContent,
		})
		if err != nil {
			panic(err)
		}
		return
	}

	err = defaultTemplate().ExecuteTemplate(w, "surf/render_error.tmpl", renderErrorContext{
		Title:        "Template not found",
		Description:  "Template not defined or not in search directory.",
		Stack:        stack,
		TemplateName: templateName,
	})
	if err != nil {
		panic(err)
	}
}

func parseTemplateParseError(err error) (string, int, string) {
	// error must be in format
	//   template: <name>:<line>:<whatever...>
	// for example
	//   template: users_list.tmpl:9: unexpected unclosed action in if
	rx := regexp.MustCompile(`^template: ([^:]+):(\d+):(.*)$`)
	result := rx.FindStringSubmatch(err.Error())
	if len(result) != 4 {
		panic(fmt.Sprintf("cannot parse template error: %q", err))
	}
	fileName := string(result[1])
	lineNo, _ := strconv.ParseInt(string(result[2]), 10, 32)
	description := string(result[3])
	return fileName, int(lineNo), description
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

func (rend *htmlRenderer) RenderStdResponse(w http.ResponseWriter, statusCode int) {
	rend.RenderResponse(w, statusCode, "stdresponse.tmpl", struct {
		Code        int
		Title       string
		Description string
	}{
		Code:  statusCode,
		Title: http.StatusText(statusCode),
	})
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

	<h2>Location</h2>
	{{range .Stack}}
		<em>{{.FilePath}}</em>
		<div class="file-content">
		{{range .Content}}
			<pre><code {{- if .Highlight}} class="highlight"{{end}}><span class="line-number">{{.Number}}</span> {{.Content}}</code></pre>
		{{end}}
		</div>
	{{end}}

	<h2>Full stack</h2>
	<code><pre>{{.FullStack}}</pre></code>
{{end}}

`))
}

const showCodeSurrounding = 5

type discardRenderer struct{}

func (discardRenderer) RenderResponse(http.ResponseWriter, int, string, interface{}) {}
func (discardRenderer) RenderStdResponse(http.ResponseWriter, int, string)           {}
