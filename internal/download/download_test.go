package download

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestClean(t *testing.T) {
	tests := []struct{ in, want string }{
		{"../../.zshrc", "zshrc"},
		{`a/b\c.pdf`, "c.pdf"},
		{"..", "attachment"},
		{"", "attachment"},
		{"(unnamed)", "attachment"},
		{"a\x00b\x1f\u0085.txt", "ab.txt"},
		{"invoice\u202Efdp.exe", "invoicefdp.exe"},
		{"a\u200bb.txt", "ab.txt"},
		{"a:b?.txt", "a_b_.txt"},
		{"  report.pdf. . ", "report.pdf"},
		{"x<y>z\"|*.txt", "x_y_z___.txt"},
		{"dir/", "attachment"},
	}
	for _, tt := range tests {
		if got := clean(tt.in); got != tt.want {
			t.Errorf("clean(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestClean_LongKeepsExtension(t *testing.T) {
	got := clean(strings.Repeat("a", 300) + ".pdf")
	if len(got) > maxName || !strings.HasSuffix(got, ".pdf") {
		t.Errorf("got %d bytes %q", len(got), got[len(got)-10:])
	}
}

func TestClean_LongDoesNotSplitRune(t *testing.T) {
	got := clean(strings.Repeat("é", 150) + ".txt") // 2 bytes each
	if len(got) > maxName || !utf8.ValidString(got) || !strings.HasSuffix(got, ".txt") {
		t.Errorf("bad name %q (%d bytes)", got, len(got))
	}
	got = clean(strings.Repeat("日", 100)) // 3 bytes each, no extension
	if len(got) > maxName || !utf8.ValidString(got) {
		t.Errorf("bad name (%d bytes)", len(got))
	}
}

func TestSave_WritesExactBytes(t *testing.T) {
	dir := t.TempDir()
	data := []byte("hello")
	path, err := Save(dir, "a.txt", data)
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(dir, "a.txt") {
		t.Errorf("path = %q", path)
	}
	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, data) {
		t.Errorf("content = %q", got)
	}
	if runtime.GOOS != "windows" {
		fi, _ := os.Stat(path)
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("perm = %v", fi.Mode().Perm())
		}
	}
}

func TestSave_SameBytesReused(t *testing.T) {
	dir := t.TempDir()
	p1, _ := Save(dir, "a.txt", []byte("x"))
	p2, err := Save(dir, "a.txt", []byte("x"))
	if err != nil || p1 != p2 {
		t.Fatalf("p2 = %q, err = %v", p2, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "a (1).txt")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a (1).txt exists: %v", err)
	}
}

func TestSave_DifferentBytesGetNewName(t *testing.T) {
	dir := t.TempDir()
	p1, _ := Save(dir, "a.txt", []byte("one"))
	p2, err := Save(dir, "a.txt", []byte("two"))
	if err != nil {
		t.Fatal(err)
	}
	if p2 != filepath.Join(dir, "a (1).txt") {
		t.Errorf("p2 = %q", p2)
	}
	if got, _ := os.ReadFile(p1); string(got) != "one" {
		t.Errorf("original changed: %q", got)
	}
	p3, _ := Save(dir, "a.txt", []byte("three"))
	if p3 != filepath.Join(dir, "a (2).txt") {
		t.Errorf("p3 = %q", p3)
	}
	// An earlier variant with the same bytes is found again.
	if p4, _ := Save(dir, "a.txt", []byte("two")); p4 != p2 {
		t.Errorf("p4 = %q, want %q", p4, p2)
	}
}

func TestSave_SkipsDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "a.txt"), 0o700); err != nil {
		t.Fatal(err)
	}
	p, err := Save(dir, "a.txt", []byte("x"))
	if err != nil {
		t.Fatal(err)
	}
	if p != filepath.Join(dir, "a (1).txt") {
		t.Errorf("p = %q", p)
	}
}

func TestSave_CreatesMissingDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "x", "y")
	p, err := Save(dir, "a.txt", []byte("x"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Error(err)
	}
}

func TestRunnable(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"invoice.pdf.exe", true},
		{"setup.EXE", true},
		{"run.Sh", true},
		{"Foo.app", true},
		{"link.webloc", true},
		{"notes.txt", false},
		{"photo.jpg", false},
		{"Makefile", false},
		{"exe", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := Runnable(tt.name); got != tt.want {
			t.Errorf("Runnable(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestDirFor(t *testing.T) {
	const home = "/home/u"
	env := func(m map[string]string) func(string) string {
		return func(k string) string { return m[k] }
	}
	files := func(m map[string]string) func(string) ([]byte, error) {
		return func(p string) ([]byte, error) {
			if s, ok := m[p]; ok {
				return []byte(s), nil
			}
			return nil, os.ErrNotExist
		}
	}
	userDirs := "/home/u/.config/user-dirs.dirs"
	tests := []struct {
		name  string
		goos  string
		env   map[string]string
		files map[string]string
		want  string
	}{
		{"darwin", "darwin", map[string]string{"XDG_DOWNLOAD_DIR": "/x"}, nil, filepath.Join(home, "Downloads")},
		{"windows", "windows", nil, nil, filepath.Join(home, "Downloads")},
		{"linux env", "linux", map[string]string{"XDG_DOWNLOAD_DIR": "/data/dl"}, nil, "/data/dl"},
		{"linux relative env ignored", "linux", map[string]string{"XDG_DOWNLOAD_DIR": "dl"}, nil, filepath.Join(home, "Downloads")},
		{"linux user-dirs with HOME", "linux", nil, map[string]string{userDirs: "# c\nXDG_DESKTOP_DIR=\"$HOME/Desktop\"\nXDG_DOWNLOAD_DIR=\"$HOME/Herunter\"\n"}, "/home/u/Herunter"},
		{"linux user-dirs absolute", "linux", nil, map[string]string{userDirs: "XDG_DOWNLOAD_DIR=\"/mnt/dl\"\n"}, "/mnt/dl"},
		{"linux user-dirs commented", "linux", nil, map[string]string{userDirs: "# XDG_DOWNLOAD_DIR=\"/mnt/dl\"\n"}, filepath.Join(home, "Downloads")},
		{"linux user-dirs relative", "linux", nil, map[string]string{userDirs: "XDG_DOWNLOAD_DIR=\"dl\"\n"}, filepath.Join(home, "Downloads")},
		{"linux XDG_CONFIG_HOME", "linux", map[string]string{"XDG_CONFIG_HOME": "/cfg"}, map[string]string{"/cfg/user-dirs.dirs": "XDG_DOWNLOAD_DIR=\"/mnt/dl\"\n"}, "/mnt/dl"},
		{"linux relative XDG_CONFIG_HOME ignored", "linux", map[string]string{"XDG_CONFIG_HOME": "cfg"}, map[string]string{userDirs: "XDG_DOWNLOAD_DIR=\"/mnt/dl\"\n"}, "/mnt/dl"},
		{"linux fallback", "linux", nil, nil, filepath.Join(home, "Downloads")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := dirFor(tt.goos, env(tt.env), home, files(tt.files)); got != tt.want {
				t.Errorf("dirFor = %q, want %q", got, tt.want)
			}
		})
	}
}
