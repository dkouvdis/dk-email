package ui

import (
	"slices"
	"testing"
)

func TestParseDropped(t *testing.T) {
	for _, tc := range []struct {
		name, in, goos string
		want           []string // nil: not a drop
	}{
		{"plain path", "/tmp/a.pdf", "linux", []string{"/tmp/a.pdf"}},
		{"surrounding space", "  /tmp/a.pdf \n", "linux", []string{"/tmp/a.pdf"}},
		{"escaped spaces", `/Users/me/My\ File.pdf`, "darwin", []string{"/Users/me/My File.pdf"}},
		{"several escaped", `/a/One\ A.pdf /a/Two\ B.png`, "darwin", []string{"/a/One A.pdf", "/a/Two B.png"}},
		{"single quoted gnome", "'/a/x y.pdf' '/a/z.png'", "linux", []string{"/a/x y.pdf", "/a/z.png"}},
		{"double quoted", `"/a/x y.pdf"`, "linux", []string{"/a/x y.pdf"}},
		{"double quoted escapes", `"/a/q\"x\\y"`, "linux", []string{`/a/q"x\y`}},
		{"mixed adjacent", `'/a b'/c "/d e"/f`, "linux", []string{"/a b/c", "/d e/f"}},
		{"file urls", "file:///a/My%20File.pdf\nfile:///b/c.txt\r\n", "darwin", []string{"/a/My File.pdf", "/b/c.txt"}},
		{"file localhost", "file://localhost/a/b.txt", "darwin", []string{"/a/b.txt"}},
		{"file other host", "file://server/a/b.txt", "linux", nil},
		{"home", "~/x.pdf", "linux", []string{"~/x.pdf"}},
		{"windows quoted", `"C:\Users\me\a b.pdf"`, "windows", []string{`C:\Users\me\a b.pdf`}},
		{"windows unquoted", `C:\x.txt`, "windows", []string{`C:\x.txt`}},
		{"windows slash drive", `D:/x.txt`, "windows", []string{`D:/x.txt`}},
		{"windows unc", `\\server\share\a.txt`, "windows", []string{`\\server\share\a.txt`}},
		{"windows file url", "file:///C:/Users/me/a%20b.pdf", "windows", []string{`C:/Users/me/a b.pdf`}},
		{"drive path off windows", `C:\x.txt`, "linux", nil},
		{"words", "see /tmp/a.pdf", "linux", nil},
		{"relative", "notes.txt", "linux", nil},
		{"http", "https://example.com/a.pdf", "linux", nil},
		{"unterminated single", "'/a/b", "linux", nil},
		{"unterminated double", `"/a/b`, "linux", nil},
		{"empty", "", "linux", nil},
		{"whitespace", " \n\t ", "linux", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseDropped(tc.in, tc.goos)
			if ok != (tc.want != nil) || !slices.Equal(got, tc.want) {
				t.Errorf("parseDropped(%q, %q) = %q, %v, want %q", tc.in, tc.goos, got, ok, tc.want)
			}
		})
	}
}
