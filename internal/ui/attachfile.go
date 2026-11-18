package ui

import (
	"errors"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/dkouvdis/dk-email/internal/mail"
	"github.com/dkouvdis/dk-email/internal/ui/popup"
)

// completedMsg is the result of completing the attach prompt's path.
type completedMsg struct {
	value   string
	choices []string
}

// attachReadMsg is the result of reading the file the attach prompt names.
type attachReadMsg struct {
	file mail.File
	err  error
}

// homeDir and workDir are where "~" and relative paths start; tests replace
// them.
var (
	homeDir = os.UserHomeDir
	workDir = os.Getwd
)

// expandPath is input as an absolute path, with a leading "~" or "~/"
// replaced by home and a relative path taken from wd.
func expandPath(input, home, wd string) string {
	switch {
	case input == "~":
		input = home
	case strings.HasPrefix(input, "~/"):
		input = filepath.Join(home, input[2:])
	case !filepath.IsAbs(input):
		input = filepath.Join(wd, input)
	}
	return filepath.Clean(input)
}

// resolvePath is the cleaned absolute path input names.
func resolvePath(input string) (string, error) {
	var home, wd string
	var err error
	if input == "~" || strings.HasPrefix(input, "~/") {
		if home, err = homeDir(); err != nil {
			return "", err
		}
	} else if !filepath.IsAbs(input) {
		if wd, err = workDir(); err != nil {
			return "", err
		}
	}
	return expandPath(input, home, wd), nil
}

// completePath completes the last element of input against the directory it
// is in. One match completes fully (a directory gets a "/"); several complete
// to their longest common prefix and are returned, sorted, as choices. The
// directory part keeps the form the user typed.
func completePath(input string, readDir func(string) ([]fs.DirEntry, error), home, wd string) (string, []string) {
	dirPart, prefix := "", input
	if i := strings.LastIndex(input, "/"); i >= 0 {
		dirPart, prefix = input[:i+1], input[i+1:]
	}
	if input == "~" {
		return "~/", nil
	}
	lookup := dirPart
	if lookup == "" {
		lookup = "."
	}
	entries, err := readDir(expandPath(lookup, home, wd))
	if err != nil {
		return input, nil
	}
	var names []string
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, prefix) || (strings.HasPrefix(name, ".") && !strings.HasPrefix(prefix, ".")) {
			continue
		}
		if e.IsDir() {
			name += "/"
		}
		names = append(names, name)
	}
	switch len(names) {
	case 0:
		return input, nil
	case 1:
		return dirPart + names[0], nil
	}
	slices.Sort(names)
	common := names[0]
	for _, n := range names[1:] {
		for !strings.HasPrefix(n, common) {
			common = common[:len(common)-1]
		}
	}
	for !utf8.ValidString(common) { // never stop inside a character
		common = common[:len(common)-1]
	}
	return dirPart + common, names
}

// completeAttach runs in a tea.Cmd: it lists directories, which Update never does.
func completeAttach(input string) tea.Cmd {
	return func() tea.Msg {
		home, _ := homeDir()
		wd, _ := workDir()
		value, choices := completePath(input, os.ReadDir, home, wd)
		return completedMsg{value: value, choices: choices}
	}
}

// readAttachment runs in a tea.Cmd and reads the file input names.
func readAttachment(input string) tea.Cmd {
	return func() tea.Msg {
		f, err := readFile(input)
		return attachReadMsg{file: f, err: err}
	}
}

// readFile reads the regular file input names. Its errors are fit to show in
// the attach prompt.
func readFile(input string) (mail.File, error) {
	path, err := resolvePath(input)
	if err != nil {
		return mail.File{}, err
	}
	fail := func(err error) (mail.File, error) {
		if errors.Is(err, fs.ErrNotExist) {
			return mail.File{}, errors.New("no such file: " + input)
		}
		return mail.File{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return fail(err)
	}
	switch {
	case info.IsDir():
		return mail.File{}, errors.New(input + " is a folder")
	case !info.Mode().IsRegular():
		return mail.File{}, errors.New(input + " is not a regular file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fail(err)
	}
	name := filepath.Base(path)
	return mail.File{Name: name, ContentType: contentType(name, data), Data: data}, nil
}

// contentType is the type for a file called name with data: from its
// extension, else sniffed from its start, else a plain byte stream.
func contentType(name string, data []byte) string {
	if t := mime.TypeByExtension(filepath.Ext(name)); t != "" {
		return t
	}
	if t := http.DetectContentType(data[:min(len(data), 512)]); t != "" {
		return t
	}
	return "application/octet-stream"
}

// attachPrompt acts on what the attach prompt asked for, which is the two
// results that need the file system.
func (m Model) attachPrompt(res popup.ComposeResult) tea.Cmd {
	switch res {
	case popup.ComposeComplete:
		return completeAttach(m.draft.pop.AttachPath())
	case popup.ComposeAttachFile:
		return readAttachment(m.draft.pop.AttachPath())
	}
	return nil
}

// handleCompleted shows a completion in the attach prompt, if it is still open.
func (m Model) handleCompleted(msg completedMsg) (tea.Model, tea.Cmd) {
	if m.composing() && m.draft.pop.Attaching() {
		m.draft.pop.SetAttachInput(msg.value, msg.choices)
	}
	return m, nil
}

// handleAttachRead adds the file the attach prompt read to the draft. A
// result that arrives after the prompt or composer closed is dropped.
func (m Model) handleAttachRead(msg attachReadMsg) (tea.Model, tea.Cmd) {
	if !m.composing() || !m.draft.pop.Attaching() {
		return m, nil
	}
	d := m.draft
	if msg.err != nil {
		d.pop.SetAttachError(msg.err.Error())
		m.log.Debug("attach failed")
		return m, nil
	}
	added, skipped := d.addAttachments([]mail.File{msg.file})
	d.pop.CloseAttach()
	m.setStatus(attachStatus(added, skipped))
	m.log.Debug("attachment read", "added", len(added), "skipped", len(skipped), "bytes", len(msg.file.Data))
	return m, nil
}

// addAttachments adds the files not already attached (same name and bytes) to
// the draft and returns the names it added and the names it skipped.
func (d *draftState) addAttachments(files []mail.File) (added, skipped []string) {
	list := slices.Clone(d.base.Attachments)
	for _, f := range files {
		if slices.ContainsFunc(list, func(a mail.File) bool {
			return a.Name == f.Name && slices.Equal(a.Data, f.Data)
		}) {
			skipped = append(skipped, f.Name)
			continue
		}
		list = append(list, f)
		added = append(added, f.Name)
	}
	if len(added) > 0 {
		d.base.Attachments = list
		d.pop.SetAttachments(attachmentLabels(list))
	}
	return added, skipped
}

// attachStatus words the outcome of adding files.
func attachStatus(added, skipped []string) string {
	var parts []string
	if len(added) > 0 {
		parts = append(parts, "Attached "+strings.Join(added, ", "))
	}
	if len(skipped) > 0 {
		verb := " is already attached"
		if len(skipped) > 1 {
			verb = " are already attached"
		}
		parts = append(parts, strings.Join(skipped, ", ")+verb)
	}
	return strings.Join(parts, "; ") + "."
}

// handleDropped attaches the files a paste dropped, if the composer is still
// open, and closes the attach prompt if it was.
func (m Model) handleDropped(msg droppedMsg) (tea.Model, tea.Cmd) {
	if !m.composing() {
		return m, nil
	}
	d := m.draft
	added, skipped := d.addAttachments(msg.files)
	d.pop.CloseAttach()
	m.setStatus(attachStatus(added, skipped))
	m.log.Debug("files dropped", "added", len(added), "skipped", len(skipped))
	return m, nil
}

// handleDropFallback pastes the text of a drop that could not be read.
func (m Model) handleDropFallback(msg dropFallbackMsg) (tea.Model, tea.Cmd) {
	if !m.composing() {
		return m, nil
	}
	m.log.Debug("drop unreadable, pasting as text")
	return m.updateComposer(msg.paste)
}
