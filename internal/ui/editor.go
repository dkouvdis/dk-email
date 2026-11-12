package ui

import (
	"errors"
	"fmt"
	"os"
	"os/exec"

	tea "charm.land/bubbletea/v2"
)

// editorDoneMsg reports the editor having exited: text is the file as the
// user left it, orig the template it started from.
type editorDoneMsg struct {
	text, orig string
	err        error
}

// editorCommand is the editor line from $VISUAL, else $EDITOR, else vi.
func editorCommand() string {
	for _, name := range []string{"VISUAL", "EDITOR"} {
		if v := os.Getenv(name); v != "" {
			return v
		}
	}
	return "vi"
}

// shellEditorCmd runs the editor through sh so values with arguments, like
// "code --wait", work. The path is passed as $1, never spliced into the
// script.
func shellEditorCmd(path string) *exec.Cmd {
	return exec.Command("sh", "-c", editorCommand()+` "$1"`, "--", path)
}

// prepareEditor writes text to a new private temp file and returns its path
// with the command that edits it. The caller must pass the path to
// finishEditor.
func prepareEditor(text string, mkCmd func(path string) *exec.Cmd) (string, *exec.Cmd, error) {
	f, err := os.CreateTemp("", "dk-email-*.eml")
	if err != nil {
		return "", nil, fmt.Errorf("create draft file: %w", err)
	}
	_, werr := f.WriteString(text)
	if err := errors.Join(werr, f.Close()); err != nil {
		_ = os.Remove(f.Name())
		return "", nil, fmt.Errorf("write draft file: %w", err)
	}
	return f.Name(), mkCmd(f.Name()), nil
}

// finishEditor reads the edited file and always removes it, whatever
// happened to the editor.
func finishEditor(path, orig string, runErr error) editorDoneMsg {
	data, readErr := os.ReadFile(path)
	rmErr := os.Remove(path)
	if errors.Is(rmErr, os.ErrNotExist) {
		rmErr = nil
	}
	msg := editorDoneMsg{text: string(data), orig: orig}
	switch {
	case runErr != nil:
		msg.err = fmt.Errorf("editor failed: %w", runErr)
	case readErr != nil:
		msg.err = fmt.Errorf("read draft: %w", readErr)
	case rmErr != nil:
		msg.err = fmt.Errorf("remove draft file: %w", rmErr)
	}
	return msg
}

// startEditor opens text in the editor. Bubble Tea releases the terminal
// while it runs and restores it afterwards, including the alt screen and
// mouse mode the last View asked for.
func (m Model) startEditor(text string) (Model, tea.Cmd) {
	if m.editorPath != "" {
		return m, nil
	}
	mk := m.editorCmd
	if mk == nil {
		mk = shellEditorCmd
	}
	path, cmd, err := prepareEditor(text, mk)
	if err != nil {
		m.setError(err)
		return m, nil
	}
	m.editorPath = path
	m.log.Debug("editor started")
	return m, tea.ExecProcess(cmd, func(runErr error) tea.Msg {
		return finishEditor(path, text, runErr)
	})
}

// removeEditorFile deletes a draft file the editor never handed back.
func (m Model) removeEditorFile() error {
	if m.editorPath == "" {
		return nil
	}
	err := os.Remove(m.editorPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
