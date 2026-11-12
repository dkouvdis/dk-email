package ui

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// editorModel is a started model whose editor command is a stub that is
// never run, with temp files confined to the test.
func editorModel(t *testing.T) Model {
	t.Helper()
	t.Setenv("TMPDIR", t.TempDir())
	m := started(t, newFixture(), 160, 30)
	m.editorCmd = func(path string) *exec.Cmd { return exec.Command("true", path) }
	return m
}

func TestEditor_CKeyOpensEditorOnTemplate(t *testing.T) {
	m := editorModel(t)

	next, cmd := m.Update(press("c"))
	m = next.(Model)

	if cmd == nil || m.editorPath == "" {
		t.Fatalf("cmd %v, pending %q; want an editor under way", cmd != nil, m.editorPath)
	}
	data, err := os.ReadFile(m.editorPath)
	if err != nil || string(data) != "To: \nCc: \nBcc: \nSubject: \n\n" {
		t.Errorf("temp file = %q, %v; want the template", data, err)
	}
	if info, err := os.Stat(m.editorPath); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("temp file mode = %v, %v; want 0600", info.Mode().Perm(), err)
	}
	if !strings.HasSuffix(m.editorPath, ".eml") || !strings.Contains(m.editorPath, "dk-email-") {
		t.Errorf("temp file %q should be dk-email-*.eml", m.editorPath)
	}
	_ = m.Close()
}

func TestEditor_CKeyOpensFromTheReader(t *testing.T) {
	m := pressSettled(t, editorModel(t), "enter")
	if !m.readerPopup {
		t.Fatal("reader popup didn't open")
	}
	next, cmd := m.Update(press("c"))
	if cmd == nil || next.(Model).editorPath == "" {
		t.Error("c in the reader popup didn't open the editor")
	}
	_ = next.(Model).Close()
}

func TestEditor_PrepareBuildsShellCommand(t *testing.T) {
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")
	tests := []struct {
		name, visual, editor, want string
	}{
		{"falls back to vi", "", "", `vi "$1"`},
		{"EDITOR", "", "code --wait", `code --wait "$1"`},
		{"VISUAL wins", "emacs -nw", "nano", `emacs -nw "$1"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("VISUAL", tt.visual)
			t.Setenv("EDITOR", tt.editor)
			cmd := shellEditorCmd("/x/draft.eml")
			want := []string{"sh", "-c", tt.want, "--", "/x/draft.eml"}
			if strings.Join(cmd.Args, "\x00") != strings.Join(want, "\x00") {
				t.Errorf("args = %q, want %q", cmd.Args, want)
			}
		})
	}
}

func TestEditor_FinishReturnsTextAndRemovesFile(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	tests := []struct {
		name    string
		runErr  error
		wantErr bool
	}{
		{"success", nil, false},
		{"editor error", errors.New("exit status 1"), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, cmd, err := prepareEditor("template", func(p string) *exec.Cmd { return exec.Command("true", p) })
			if err != nil || cmd == nil {
				t.Fatalf("prepareEditor() = %v, %v", cmd, err)
			}
			if err := os.WriteFile(path, []byte("edited"), 0o600); err != nil {
				t.Fatal(err)
			}

			msg := finishEditor(path, "template", tt.runErr)

			if msg.text != "edited" || msg.orig != "template" || (msg.err != nil) != tt.wantErr {
				t.Errorf("msg = %+v, want text edited, err %v", msg, tt.wantErr)
			}
			if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("temp file still there (stat err %v)", err)
			}
		})
	}
}

func TestEditor_FinishWithMissingFileReportsError(t *testing.T) {
	msg := finishEditor(t.TempDir()+"/gone.eml", "t", nil)
	if msg.err == nil {
		t.Error("a file the editor deleted should be an error, not an empty draft")
	}
}

func TestEditor_CloseRemovesPendingFile(t *testing.T) {
	m := editorModel(t)
	next, _ := m.Update(press("c"))
	m = next.(Model)
	path := m.editorPath

	if err := m.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("temp file still there after Close (stat err %v)", err)
	}
	if err := m.Close(); err != nil {
		t.Errorf("second Close() error = %v; a removed file is fine", err)
	}
}

func TestEditor_DoneDiscardsTheDraft(t *testing.T) {
	m := editorModel(t)
	m.editorPath = "/gone/draft.eml"

	m = send(t, m, editorDoneMsg{text: "To: a@b.c\n\nhi", orig: placeholderTemplate})

	if m.status != "Draft discarded" || m.statusErr || m.editorPath != "" {
		t.Errorf("status %q (err %v), pending %q; want Draft discarded and nothing pending", m.status, m.statusErr, m.editorPath)
	}
}

func TestEditor_DoneErrorShowsInStatus(t *testing.T) {
	m := send(t, editorModel(t), editorDoneMsg{err: errors.New("editor failed: exit status 1")})
	if !m.statusErr || !strings.Contains(m.status, "editor failed") {
		t.Errorf("status %q (err %v), want the editor error", m.status, m.statusErr)
	}
}

func TestKeys_HelpHasComposeSection(t *testing.T) {
	for _, s := range DefaultKeyMap().HelpSections() {
		if s.Title != "Compose" {
			continue
		}
		// r, R and f are bound but not wired yet, so they aren't advertised.
		if len(s.Keys) != 1 || s.Keys[0].Help().Key != "c" {
			t.Errorf("Compose section keys = %+v, want just c", s.Keys)
		}
		return
	}
	t.Error("help has no Compose section")
}
