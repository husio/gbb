package gbb

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-surf/surf"
)

const cssStyle = `
* 			{ box-sizing: border-box; }
body  			{ margin: 40px auto; max-width: 1000px; line-height: 180%; padding: 0 10px; font-family: "Open Sans",arial,x-locale-body,sans-serif; font-size: 15px; }
textarea,
input[type=text],
input[type=password] 	{ width: 100%; margin: 2px 0; padding: 12px 8px; }
input[type=search] 	{ width: 200px; padding: 0px; margin: 0; }

textarea 		{ padding: 12px; resize: vertical; }
textarea.big 		{ min-height: 400px; }
h1 			{ font-size: 1.6em; }
h2 			{ font-size: 1.4em; }
h3 			{ font-size: 1.3em; }
hr 			{ border: 1px solid #eee; }
fieldset 		{ border: none; }
code, pre 		{ font-family: terminus, fixed, monospace; }
code 			{ background: #3C3C3C; color: #EFEFEF; padding: 3px 5px; }
pre code 		{ color: #454545; background: none; padding: 0; }
pre 			{ background: #F8F9F3; border: 1px solid #EEEEDE; border-radius: 2px; padding: 12px 18px; }
.box-info 		{ padding: 12px 20px; border: 1px solid #ACD8E9; background: #E5F5FC; border-radius: 3px; }
.box-danger 		{ padding: 12px 20px; border: 1px solid #ECBFBF; background: #F6E7E7; border-radius: 3px; }
.menu 			{ padding: 10px 0; margin: 18px 0; }
.menu a 		{ text-decoration: none; }
.menu a:visited 	{ color: blue; }
.menu .btn,
.menu a.btn 		{ background: #4A9AD0; padding: 3px 10px; color: #fff; }
.separator:after 	{ content: "/"; padding: 0 10px; font-size: 80%; }

.comment 		{ padding: 10px; margin: 20px 0; }
.comment-content 	{ padding-left: 20px; }
.comment-content img 	{ max-width: 400px; max-height: 400px; margin: auto; }

ul.errors  		{ background: #FFF1F1; padding: 10px; }
ul.errors li 		{ list-style-type: none; margin: 10px; }

.result 		{ padding: 30px 0 10px 0; }
.comment-content 	{ padding-left: 20px; }


.topic 			{ margin: 8px 0; }
.topic-tagline 		{ font-size:80%; padding-left: 10px; color: #444; }
.topic .new-content-tag { background: #FFFEDC; marign: 3px; padding: 3px; font-size: 80%; }
.topic .pagination 	{ font-size: 9px; }
`

func PublicContentHandler(minify bool) surf.HandlerFunc {
	now := time.Now()

	style := cssStyle

	if minify {
		replacements := [][2]string{
			{`\s+\}\n`, "}"},
			{`\s+\{\s+`, "{"},
			{`\s*;\s+`, ";"},
			{`;}`, "}"},
			{`\n`, ""},
			{`\: `, ":"},
			{`\s{2,}`, ""},
		}
		for _, r := range replacements {
			style = regexp.MustCompile(r[0]).ReplaceAllString(style, r[1])
		}
	}

	compressedStyle, _ := gzipStr(style)

	return func(w http.ResponseWriter, r *http.Request) surf.Response {
		w.Header().Set("content-type", "text/css")

		// when available, use gzip compressed stylesheet
		if compressedStyle != "" && strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			w.Header().Set("content-encoding", "gzip")
			http.ServeContent(w, r, r.URL.Path, now, strings.NewReader(compressedStyle))
		} else {
			http.ServeContent(w, r, r.URL.Path, now, strings.NewReader(style))
		}
		return nil
	}
}

func gzipStr(s string) (string, error) {
	var b bytes.Buffer
	gz, err := gzip.NewWriterLevel(&b, gzip.BestCompression)
	if err != nil {
		return "", fmt.Errorf("cannot create gzip writer: %s", err)
	}
	if _, err := io.WriteString(gz, s); err != nil {
		return "", fmt.Errorf("cannot gzip string: %s", err)
	}
	if err := gz.Close(); err != nil {
		return "", fmt.Errorf("cannot flush gzip writer: %s", err)
	}
	return b.String(), nil
}
