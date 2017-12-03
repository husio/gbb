package surf

import "testing"

func TestAdjustTemplateTrimming(t *testing.T) {
	// line counting starts with 0 index
	cases := map[string]struct {
		template    string
		inputLineNo int
		wantLineNo  int
	}{
		"simple": {
			template: `Foo
			Baz
			THIS LINE IS 2`,
			inputLineNo: 2,
			wantLineNo:  2,
		},
		"no whitespace cut": {
			template: `Foo
			Baz {{if .Test}}This is test{{end}}
			THIS LINE IS 2`,
			inputLineNo: 2,
			wantLineNo:  2,
		},
		"whitespace cut but without lines": {
			template: `Foo
			Baz {{if .Test -}} This is test  {{- end}}
			THIS LINE IS 2`,
			inputLineNo: 2,
			wantLineNo:  2,
		},
		"left whitespace cut with lines": {
			template: `Foo
			Baz {{if .Test}}

			This is test

			{{- end}}
			THIS LINE IS 6`,
			inputLineNo: 4,
			wantLineNo:  6,
		},
		"right whitespace cut with lines": {
			template: `Foo
			Baz {{if .Test -}}

			This is test

			{{end}}
			THIS LINE IS 6`,
			inputLineNo: 4,
			wantLineNo:  6,
		},
		"both whitespace cut with lines": {
			template: `Foo
			Baz {{if .Test -}}

			This is test

			{{- end}}
			THIS LINE IS 6`,
			inputLineNo: 2,
			wantLineNo:  6,
		},
		"both whitespace cut with lines, empty inside": {
			template: `Foo
			Baz {{if .Test -}}


			{{- end}}
			THIS LINE IS 5`,
			inputLineNo: 2,
			wantLineNo:  5,
		},
		"double whitespace cut, both ends": {
			template: `Foo
			Baz {{if .Test -}}


			{{- .Foo - }}


			{{- end}}
			THIS LINE IS 8`,
			inputLineNo: 2,
			wantLineNo:  8,
		},
		"double whitespace cut, nested": {
			template: `Foo
			Baz {{if .Test -}}

				{{- range .Items}}

					{{.Foo}}

				{{end}}

			{{- end}}
			THIS LINE IS 10`,
			inputLineNo: 6,
			wantLineNo:  10,
		},
	}

	for tname, tc := range cases {
		t.Run(tname, func(t *testing.T) {
			got := adjustTemplateTrimming(tc.inputLineNo, []byte(tc.template))
			if got != tc.wantLineNo {
				t.Fatalf("want %d, got %d", tc.wantLineNo, got)
			}
		})
	}
}
