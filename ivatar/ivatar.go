package ivatar

import (
	"encoding/base64"
	"fmt"
	"hash/fnv"
	"html"
	"html/template"
	"io"
	"strings"
	"unicode"
)

// avatarContent returns ivatar built from given name. The same name always
// returns the same ivatar representation.
func avatarContent(name string, width, height int) string {
	initials := make([]rune, 0, 2)
	for i, r := range name {
		if !unicode.IsLetter(r) {
			continue
		}
		initials = append(initials, r)
		add := false
		for i, r := range name[i:] {
			if add {
				initials = append(initials, r)
				break
			}
			if unicode.IsSpace(r) && len(name) > i {
				add = true
			}
		}
		break
	}

	if len(initials) == 0 {
		initials = []rune(name[:2])
	}

	iname := html.EscapeString(strings.ToUpper(string(initials)))

	h := fnv.New32()
	io.WriteString(h, name)
	color := colors[int(h.Sum32())%len(colors)]
	svg := fmt.Sprintf(
		`<svg xmlns="http://www.w3.org/2000/svg" pointer-events="none" width="%d" height="%d" style="background-color: %s; width: %dpx; height: %dpx;"><text text-anchor="middle" y="50%%" x="50%%" dy="0.35em" pointer-events="auto" fill="#ffffff" font-family="HelveticaNeue-Light,Helvetica Neue Light,Helvetica Neue,Helvetica,Arial,Lucida Grande,sans-serif" style="font-weight:bold;font-size:%dpx;">%s</text></svg>`,
		width, height, color, width, height, (width*4)/9, iname)
	return base64.StdEncoding.EncodeToString([]byte(svg))
}

// ImgSrc returns an encoded avatar content, ready to be used as a src value of
// an image tag.
func ImgSrc(name string, size int) template.HTMLAttr {
	content := avatarContent(name, size, size)
	src := `src="data:image/svg+xml;base64,` + content + `"`
	return template.HTMLAttr(src)
}

var colors = []string{
	"#1abc9c", "#16a085", "#f1c40f", "#f39c12", "#2ecc71", "#27ae60",
	"#e67e22", "#d35400", "#3498db", "#2980b9", "#e74c3c", "#c0392b",
	"#9b59b6", "#8e44ad", "#bdc3c7", "#34495e", "#2c3e50", "#95a5a6",
	"#7f8c8d", "#ec87bf", "#d870ad", "#f69785", "#9ba37e", "#b49255",
	"#b49255", "#a94136",
}
