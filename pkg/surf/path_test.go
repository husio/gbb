package surf

import "testing"

func TestPathLastChunk(t *testing.T) {
	cases := map[string]struct {
		path string
		want string
	}{
		"root": {
			path: "/",
			want: "",
		},
		"not dir": {
			path: "/foo/bar",
			want: "bar",
		},
		"dir": {
			path: "/foo/bar/",
			want: "bar",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			p := Path(tc.path)
			got := p.LastChunk()

			if got != tc.want {
				t.Fatalf("want %q, got %q", tc.want, got)
			}
		})
	}
}
