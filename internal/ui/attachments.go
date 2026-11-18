package ui

import (
	"context"
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/dkouvdis/dk-email/internal/download"
	"github.com/dkouvdis/dk-email/internal/mail"
	"github.com/dkouvdis/dk-email/internal/render"
	"github.com/dkouvdis/dk-email/internal/ui/popup"
	"github.com/dkouvdis/dk-email/internal/ui/reader"
)

// fileOpener saves and opens attachments; cmd/dk-email sets the opener, tests
// replace all of it.
type fileOpener struct {
	dir     func() (string, error)
	open    func(path string) error
	canOpen func() bool
}

// WithOpener sets how saved attachments are opened, and whether this system
// can open them at all. Without it attachments are saved but never opened.
func (m Model) WithOpener(open func(path string) error, canOpen func() bool) Model {
	m.files.open, m.files.canOpen = open, canOpen
	return m
}

// attachmentMsg is the result of saving, and maybe opening, one attachment.
type attachmentMsg struct {
	name, path string
	// runnable means the file can run code, so it was saved but not opened.
	runnable, opened bool
	// openErr is why opening failed; err is why saving did.
	openErr, err error
}

// openAttachment opens the only attachment of the loaded messages in the
// reader, or lists them all in the picker.
func (m Model) openAttachment() (Model, tea.Cmd) {
	var groups []popup.Group[pick]
	count := 0
	for _, msg := range m.read.Loaded() {
		if len(msg.Attachments) == 0 {
			continue
		}
		g := popup.Group[pick]{Name: msg.From.DisplayName() + " · " + msg.Date.Format("2 Jan 2006 15:04")}
		for i, a := range msg.Attachments {
			g.Items = append(g.Items, popup.Item[pick]{
				Label: a.Name + "  " + reader.FormatSize(a.Size),
				Value: pick{account: m.open.account, folder: m.open.folder, uid: msg.UID, part: i, name: a.Name},
			})
		}
		groups = append(groups, g)
		count += len(g.Items)
	}
	switch count {
	case 0:
		m.setStatus("No attachments.")
		return m, nil
	case 1:
		return m.saveAttachment(groups[0].Items[0].Value)
	}
	m.picker = popup.NewPicker(m.theme, "Open attachment", groups)
	m.picker.SetSize(m.panes.Popup.W, m.panes.Popup.H)
	m.showPicker = true
	m.pickingAttachment = true
	m.log.Debug("popup opened", "popup", "attachment picker", "attachments", count)
	return m, nil
}

// saveAttachment fetches the message, saves the part and opens it unless it
// can run code.
func (m Model) saveAttachment(p pick) (Model, tea.Cmd) {
	if m.boxes[p.account] == nil {
		m.setError(fmt.Errorf("Couldn't save %s: the account isn't connected", p.name))
		return m, nil
	}
	m.setStatus("Saving " + p.name + "…")
	return m, saveAttachment(m.boxes[p.account], m.files, p)
}

func saveAttachment(mb mail.Mailbox, files fileOpener, p pick) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), messageTimeout)
		defer cancel()
		res := attachmentMsg{name: p.name}
		raw, err := mb.Raw(ctx, p.folder, p.uid)
		if err != nil {
			res.err = err
			return res
		}
		parts, err := render.Parts(raw)
		if err != nil {
			res.err = err
			return res
		}
		if p.part >= len(parts) || parts[p.part].Filename != p.name {
			res.err = fmt.Errorf("the message changed; open it again")
			return res
		}
		dir, err := files.dir()
		if err != nil {
			res.err = err
			return res
		}
		res.path, err = download.Save(dir, parts[p.part].Filename, parts[p.part].Data)
		if err != nil {
			res.err = err
			return res
		}
		res.runnable = download.Runnable(res.path)
		if !res.runnable && files.canOpen() {
			if res.openErr = files.open(res.path); res.openErr == nil {
				res.opened = true
			}
		}
		return res
	}
}

func (m Model) handleAttachment(msg attachmentMsg) (Model, tea.Cmd) {
	if msg.err != nil {
		m.setError(fmt.Errorf("Couldn't save %s: %w", msg.name, msg.err))
		return m, nil
	}
	m.log.Debug("attachment saved", "opened", msg.opened, "runnable", msg.runnable, "open failed", msg.openErr != nil)
	p := tilde(msg.path)
	switch {
	case msg.runnable:
		m.setStatus("Saved " + p + " — not opened: it can run code.")
	case msg.openErr != nil:
		m.setError(fmt.Errorf("Saved %s, but couldn't open it: %w", p, msg.openErr))
	case msg.opened:
		m.setStatus("Saved and opened " + p + ".")
	default:
		m.setStatus("Saved " + p + " — no app to open it here.")
	}
	return m, nil
}

// tilde shortens a path in the home directory to start with ~.
func tilde(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" || !strings.HasPrefix(path, home+string(os.PathSeparator)) {
		return path
	}
	return "~" + path[len(home):]
}
