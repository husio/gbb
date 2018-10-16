package ivatar

import (
	"html/template"
	"testing"
)

func TestBuildImg(t *testing.T) {
	cases := map[string]struct {
		name string
		want template.HTML
	}{
		"single name": {
			name: "Bob",
			want: "",
		},
		"full name": {
			name: "Bob Ross",
			want: "",
		},
		"triple name": {
			name: "Bob Ross Moss",
			want: "",
		},
	}

	for testName, tc := range cases {
		t.Run(testName, func(t *testing.T) {
			got := BuildImg(tc.name)
			if tc.want != got {
				t.Fatalf("want %q image, got %q", tc.want, got)
			}
		})
	}
}
