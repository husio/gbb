package ivatar

import (
	"encoding/base64"
	"testing"
)

func TestAvatarContent(t *testing.T) {
	cases := map[string]struct {
		name string
		size int
		want string
	}{
		"single name": {
			name: "Bob",
			size: 24,
			want: `<svg xmlns="http://www.w3.org/2000/svg" pointer-events="none" width="24" height="24" style="background-color: #3498db; width: 24px; height: 24px;"><text text-anchor="middle" y="50%" x="50%" dy="0.35em" pointer-events="auto" fill="#ffffff" font-family="HelveticaNeue-Light,Helvetica Neue Light,Helvetica Neue,Helvetica,Arial,Lucida Grande,sans-serif" style="font-weight:bold;font-size:10px;">B</text></svg>`,
		},
		"full name": {
			name: "Bob Ross",
			size: 24,
			want: `<svg xmlns="http://www.w3.org/2000/svg" pointer-events="none" width="24" height="24" style="background-color: #27ae60; width: 24px; height: 24px;"><text text-anchor="middle" y="50%" x="50%" dy="0.35em" pointer-events="auto" fill="#ffffff" font-family="HelveticaNeue-Light,Helvetica Neue Light,Helvetica Neue,Helvetica,Arial,Lucida Grande,sans-serif" style="font-weight:bold;font-size:10px;">BR</text></svg>`,
		},
		"triple name": {
			name: "Bob Ross Moss",
			size: 44,
			want: `<svg xmlns="http://www.w3.org/2000/svg" pointer-events="none" width="44" height="44" style="background-color: #b49255; width: 44px; height: 44px;"><text text-anchor="middle" y="50%" x="50%" dy="0.35em" pointer-events="auto" fill="#ffffff" font-family="HelveticaNeue-Light,Helvetica Neue Light,Helvetica Neue,Helvetica,Arial,Lucida Grande,sans-serif" style="font-weight:bold;font-size:19px;">BR</text></svg>`,
		},
	}

	for testName, tc := range cases {
		t.Run(testName, func(t *testing.T) {
			got64 := avatarContent(tc.name, tc.size, tc.size)
			got, err := base64.StdEncoding.DecodeString(got64)
			if err != nil {
				t.Fatalf("cannot decode: %s", err)
			}
			if tc.want != string(got) {
				t.Logf("want %q", tc.want)
				t.Logf("got  %q", string(got))
				t.Fatal("unexpected data")
			}
		})
	}
}
