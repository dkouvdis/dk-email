package ui

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/dkouvdis/dk-email/internal/mail/mailtest"
)

// useDirs makes "~" and the working directory home and wd for the test.
func useDirs(t *testing.T, home, wd string) {
	t.Helper()
	oldHome, oldWd := homeDir, workDir
	homeDir = func() (string, error) { return home, nil }
	workDir = func() (string, error) { return wd, nil }
	t.Cleanup(func() { homeDir, workDir = oldHome, oldWd })
}

func TestResolvePath(t *testing.T) {
	useDirs(t, "/home/me", "/work/dir")
	for in, want := range map[string]string{
		"~":             "/home/me",
		"~/x":           "/home/me/x",
		"~/a/../b.txt":  "/home/me/b.txt",
		"x.txt":         "/work/dir/x.txt",
		"./sub//y.txt":  "/work/dir/sub/y.txt",
		"../up.txt":     "/work/up.txt",
		"/abs/./z/../w": "/abs/w",
		"~other/x":      "/work/dir/~other/x",
	} {
		got, err := resolvePath(in)
		if err != nil || got != want {
			t.Errorf("resolvePath(%q) = %q, %v, want %q", in, got, err, want)
		}
	}
}

func TestResolvePath_HomeError(t *testing.T) {
	oldHome := homeDir
	homeDir = func() (string, error) { return "", errors.New("no home") }
	t.Cleanup(func() { homeDir = oldHome })
	if _, err := resolvePath("~/x"); err == nil {
		t.Error("want the home error")
	}
}

// fakeDirs serves directory listings from a map of path to entry names; a
// trailing "/" marks a directory.
func fakeDirs(dirs map[string][]string) func(string) ([]fs.DirEntry, error) {
	return func(path string) ([]fs.DirEntry, error) {
		names, ok := dirs[path]
		if !ok {
			return nil, fs.ErrNotExist
		}
		fsys := fstest.MapFS{}
		for _, n := range names {
			if strings.HasSuffix(n, "/") {
				fsys[strings.TrimSuffix(n, "/")+"/.keep"] = &fstest.MapFile{}
			} else {
				fsys[n] = &fstest.MapFile{}
			}
		}
		return fs.ReadDir(fsys, ".")
	}
}

func TestCompletePath(t *testing.T) {
	dirs := fakeDirs(map[string][]string{
		"/home/me":           {"Downloads/", "Documents/", "notes.txt", ".ssh/", ".zshrc"},
		"/home/me/Downloads": {"report.pdf", "readme.md", "photo.png"},
		"/work/dir":          {"alpha.txt", "alps/", ".hidden"},
		"/work/dir/alps":     {"one.txt"},
		"/abs":               {"x.bin"},
		"/accents":           {"\u00e9t\u00e9.txt", "\u00e8re.txt"},
	})
	for name, tc := range map[string]struct {
		in      string
		value   string
		choices []string
	}{
		"unique file":        {"~/not", "~/notes.txt", nil},
		"unique dir":         {"~/Dow", "~/Downloads/", nil},
		"several":            {"~/Do", "~/Do", []string{"Documents/", "Downloads/"}},
		"common prefix":      {"~/Downloads/re", "~/Downloads/re", []string{"readme.md", "report.pdf"}},
		"common prefix long": {"alp", "alp", []string{"alpha.txt", "alps/"}},
		"hidden skipped":     {"~/", "~/", []string{"Documents/", "Downloads/", "notes.txt"}},
		"hidden with dot":    {"~/.", "~/.", []string{".ssh/", ".zshrc"}},
		"hidden unique":      {"~/.s", "~/.ssh/", nil},
		"none":               {"~/zzz", "~/zzz", nil},
		"unreadable dir":     {"~/nope/x", "~/nope/x", nil},
		"case sensitive":     {"~/dow", "~/dow", nil},
		"relative kept":      {"alps/o", "alps/one.txt", nil},
		"absolute":           {"/abs/x", "/abs/x.bin", nil},
		"empty lists wd":     {"", "alp", []string{"alpha.txt", "alps/"}},
		"bare tilde":         {"~", "~/", nil},
		"no split character": {"/accents/", "/accents/", []string{"\u00e8re.txt", "\u00e9t\u00e9.txt"}},
	} {
		value, choices := completePath(tc.in, dirs, "/home/me", "/work/dir")
		if value != tc.value || !reflect.DeepEqual(choices, tc.choices) {
			t.Errorf("%s: completePath(%q) = %q, %q, want %q, %q", name, tc.in, value, choices, tc.value, tc.choices)
		}
	}
}

func TestContentType(t *testing.T) {
	for name, tc := range map[string]struct {
		file string
		data []byte
		want string
	}{
		"extension":      {"a.pdf", []byte("%PDF"), "application/pdf"},
		"sniffed text":   {"notes.zzq", []byte("hello world"), "text/plain; charset=utf-8"},
		"binary no ext":  {"blob", []byte{0, 1, 2, 3, 0xff}, "application/octet-stream"},
		"sniffed binary": {"data.zzq", []byte("\x89PNG\r\n\x1a\n"), "image/png"},
		"empty":          {"empty", nil, "text/plain; charset=utf-8"},
	} {
		if got := contentType(tc.file, tc.data); got != tc.want {
			t.Errorf("%s: contentType = %q, want %q", name, got, tc.want)
		}
	}
}

// attachComposer is the composer open on a draft to zed@example.com, with the
// temp dir as both home and working directory.
func attachComposer(t *testing.T) (Model, *mailtest.DraftSender, string) {
	t.Helper()
	dir := t.TempDir()
	useDirs(t, dir, dir)
	m, s := composeFixture(t)
	m = written(t, m, "zed@example.com", "Files", "see attached")
	return m, s, dir
}

// attachPath types path into the attach prompt and presses enter, returning
// the model after the read finished.
func attachPath(t *testing.T, m Model, path string) Model {
	t.Helper()
	if !m.draft.pop.Attaching() {
		m = send(t, m, press("ctrl+o"))
	}
	m = typed(t, m, path)
	m, cmd := keyCmd(t, m, "enter")
	if cmd == nil {
		t.Fatalf("enter started nothing (status %q)", m.status)
	}
	return send(t, m, cmd())
}

func write(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestAttach_FilesAreSentWithTypeAndBytes(t *testing.T) {
	m, s, dir := attachComposer(t)
	files := map[string][]byte{
		"report.pdf": []byte("%PDF-1.4 not really"),
		"notes.zzq":  []byte("plain words\nsecond line\n"),
		"blob":       {0, 1, 2, 3, 4, 0xff, 0xfe},
	}
	for name, data := range files {
		write(t, filepath.Join(dir, name), data)
	}
	m = attachPath(t, m, "report.pdf")
	if m.draft.pop.Attaching() || !strings.Contains(m.status, "Attached report.pdf.") {
		t.Errorf("after attach: attaching %v, status %q", m.draft.pop.Attaching(), m.status)
	}
	m = attachPath(t, send(t, m, press("ctrl+o")), "~/notes.zzq")
	m = attachPath(t, send(t, m, press("ctrl+o")), filepath.Join(dir, "blob"))
	v := plain(m)
	for _, want := range []string{"Attach", "report.pdf (19 B)", "notes.zzq (24 B)", "blob (7 B)"} {
		if !strings.Contains(v, want) {
			t.Errorf("composer lacks %q:\n%s", want, v)
		}
	}

	m = sendKey(t, m)

	got := s.Sent()
	if len(got) != 1 || len(got[0].Attachments) != 3 {
		t.Fatalf("sent %+v", got)
	}
	for i, want := range []struct{ name, typ string }{
		{"report.pdf", "application/pdf"},
		{"notes.zzq", "text/plain; charset=utf-8"},
		{"blob", "application/octet-stream"},
	} {
		a := got[0].Attachments[i]
		if a.Name != want.name || a.ContentType != want.typ || string(a.Data) != string(files[want.name]) {
			t.Errorf("attachment %d = %q %q %q, want %q %q %q", i, a.Name, a.ContentType, a.Data, want.name, want.typ, files[want.name])
		}
	}
}

func TestAttach_FolderShowsErrorAndKeepsPromptOpen(t *testing.T) {
	m, _, dir := attachComposer(t)
	if err := os.Mkdir(filepath.Join(dir, "stuff"), 0o700); err != nil {
		t.Fatal(err)
	}
	m = attachPath(t, m, "stuff")
	if !m.draft.pop.Attaching() || len(m.draft.base.Attachments) != 0 {
		t.Fatalf("attaching %v, attachments %d", m.draft.pop.Attaching(), len(m.draft.base.Attachments))
	}
	if v := plain(m); !strings.Contains(v, "stuff is a folder") {
		t.Errorf("no folder error:\n%s", v)
	}
}

func TestAttach_MissingFileShowsError(t *testing.T) {
	m, _, _ := attachComposer(t)
	m = attachPath(t, m, "~/nope.txt")
	if !m.draft.pop.Attaching() || len(m.draft.base.Attachments) != 0 {
		t.Fatal("a missing file should keep the prompt open and attach nothing")
	}
	if v := plain(m); !strings.Contains(v, "no such file: ~/nope.txt") {
		t.Errorf("no missing-file error:\n%s", v)
	}
}

func TestAttach_SameFileTwiceKeepsOneCopy(t *testing.T) {
	m, _, dir := attachComposer(t)
	write(t, filepath.Join(dir, "a.txt"), []byte("same"))
	m = attachPath(t, m, "a.txt")
	m = attachPath(t, send(t, m, press("ctrl+o")), "a.txt")
	if n := len(m.draft.base.Attachments); n != 1 {
		t.Errorf("%d attachments, want 1", n)
	}
	if m.status != "a.txt is already attached." || m.draft.pop.Attaching() {
		t.Errorf("status %q, attaching %v", m.status, m.draft.pop.Attaching())
	}

	// Same name with other bytes is a different file.
	write(t, filepath.Join(dir, "a.txt"), []byte("changed"))
	m = attachPath(t, send(t, m, press("ctrl+o")), "a.txt")
	if n := len(m.draft.base.Attachments); n != 2 {
		t.Errorf("%d attachments, want 2", n)
	}
}

func TestAttach_EscClosesOnlyThePrompt(t *testing.T) {
	m, _, dir := attachComposer(t)
	m = send(t, m, press("ctrl+o"))
	m = typed(t, m, "abc")
	m = send(t, m, press("esc"))
	if !m.composing() || m.draft.pop.Attaching() || m.draft.pop.Prompting() {
		t.Fatalf("composing %v, attaching %v, prompting %v", m.composing(), m.draft.pop.Attaching(), m.draft.pop.Prompting())
	}

	write(t, filepath.Join(dir, "a.txt"), []byte("x"))
	m = attachPath(t, send(t, m, press("ctrl+o")), "a.txt")
	m = send(t, m, press("esc"))
	if !m.composing() || !m.draft.pop.Prompting() {
		t.Error("esc after attaching should offer save/discard")
	}
}

func TestAttach_TabCompletionRoundTrip(t *testing.T) {
	m, _, dir := attachComposer(t)
	if err := os.Mkdir(filepath.Join(dir, "Downloads"), 0o700); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "Downloads", "report.pdf"), []byte("x"))
	write(t, filepath.Join(dir, "Downloads", "readme.md"), []byte("x"))
	write(t, filepath.Join(dir, "Downloads", "notes.txt"), []byte("x"))

	m = send(t, m, press("ctrl+o"))
	m = typed(t, m, "~/Dow")
	m, cmd := keyCmd(t, m, "tab")
	if cmd == nil {
		t.Fatal("tab started nothing")
	}
	m = send(t, m, cmd())
	if got := m.draft.pop.AttachPath(); got != "~/Downloads/" {
		t.Errorf("after unique completion: %q", got)
	}

	m = typed(t, m, "re")
	m, cmd = keyCmd(t, m, "tab")
	m = send(t, m, cmd())
	if got := m.draft.pop.AttachPath(); got != "~/Downloads/re" {
		t.Errorf("after ambiguous completion: %q", got)
	}
	if v := plain(m); !strings.Contains(v, "readme.md  report.pdf") {
		t.Errorf("choices not listed:\n%s", v)
	}

	m = typed(t, m, "p")
	m, cmd = keyCmd(t, m, "tab")
	m = send(t, m, cmd())
	if got := m.draft.pop.AttachPath(); got != "~/Downloads/report.pdf" {
		t.Errorf("after completing the file: %q", got)
	}
}

func TestAttach_LateResultsAreDropped(t *testing.T) {
	m, _, dir := attachComposer(t)
	write(t, filepath.Join(dir, "a.txt"), []byte("x"))
	m = send(t, m, press("ctrl+o"))
	m = typed(t, m, "a.txt")
	m, cmd := keyCmd(t, m, "enter")
	msg := cmd()
	m = send(t, m, press("esc"))
	m = send(t, m, msg, completedMsg{value: "zzz"})
	if len(m.draft.base.Attachments) != 0 || m.draft.pop.Attaching() || m.status == "Attached a.txt." {
		t.Errorf("a result after esc was applied: %d attachments, status %q", len(m.draft.base.Attachments), m.status)
	}

	m = send(t, m, press("esc"), press("d"))
	if m.composing() {
		t.Fatal("composer should be closed")
	}
	m = send(t, m, msg, completedMsg{value: "zzz"})
	if m.draft != nil {
		t.Error("late results reopened the composer")
	}
}
