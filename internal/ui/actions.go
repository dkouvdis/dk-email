package ui

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/dkouvdis/dk-email/internal/mail"
	"github.com/dkouvdis/dk-email/internal/ui/sidebar"
)

const moveTimeout = 60 * time.Second

// flagMsg reports a flag change that was already applied to the UI.
type flagMsg struct {
	account int
	folder  string
	uids    []mail.UID
	flag    mail.Flag
	on      bool
	err     error
}

// moveMsg reports a move whose messages were already removed from the UI.
// removed and unread are what's needed to undo that if the move failed.
type moveMsg struct {
	account  int
	from, to string
	uids     []mail.UID
	removed  []mail.Summary
	unread   int
	verb     string // "archive", "delete" or "move", for messages
	label    string // the destination's display name
	err      error
}

func setFlag(mb mail.Mailbox, account int, folder string, uids []mail.UID, f mail.Flag, on bool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), flagTimeout)
		defer cancel()
		err := mb.SetFlag(ctx, folder, uids, f, on)
		return flagMsg{account: account, folder: folder, uids: uids, flag: f, on: on, err: err}
	}
}

func moveMessages(mb mail.Mailbox, msg moveMsg) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), moveTimeout)
		defer cancel()
		msg.err = mb.Move(ctx, msg.from, msg.uids, msg.to)
		return msg
	}
}

// isTriageKey reports the u/s/e/d/m keys.
func (m Model) isTriageKey(msg tea.KeyPressMsg) bool {
	return key.Matches(msg, m.keys.ToggleRead, m.keys.Star, m.keys.Archive, m.keys.Delete, m.keys.Move)
}

// triage runs u, s, e or d on the target messages: the whole thread on a
// collapsed thread row, otherwise the selected message.
func (m Model) triage(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	targets := m.targets()
	if len(targets) == 0 {
		return m, nil
	}
	uids := make([]mail.UID, len(targets))
	for i, s := range targets {
		uids[i] = s.UID
	}
	switch {
	case key.Matches(msg, m.keys.ToggleRead):
		anyUnread := slices.ContainsFunc(targets, func(s mail.Summary) bool { return !s.Seen })
		return m.changeFlag(m.open.account, m.open.folder, targets, mail.FlagSeen, anyUnread)
	case key.Matches(msg, m.keys.Star):
		anyStarred := slices.ContainsFunc(targets, func(s mail.Summary) bool { return s.Flagged })
		return m.changeFlag(m.open.account, m.open.folder, targets, mail.FlagFlagged, !anyStarred)
	case key.Matches(msg, m.keys.Archive):
		return m.moveTo(targets, "archive")
	case key.Matches(msg, m.keys.Delete):
		return m.moveTo(targets, "delete")
	case key.Matches(msg, m.keys.Move):
		return m.openMovePicker(targets)
	}
	return m, nil
}

// targets are the messages a triage key acts on.
func (m Model) targets() []mail.Summary {
	if m.open.account < 0 || m.open.loading {
		return nil
	}
	if m.readerPopup || m.focus == PaneReader {
		if uid := m.read.Current(); uid != 0 {
			return m.list.Lookup([]mail.UID{uid})
		}
		return nil
	}
	if m.focus != PaneList {
		return nil
	}
	th, uid, ok := m.list.Selected()
	switch {
	case !ok:
		return nil
	case uid != 0:
		return m.list.Lookup([]mail.UID{uid})
	}
	return th.Messages
}

// changeFlag sets f to on for every target not already in that state,
// updating the UI first and the server in the background.
func (m Model) changeFlag(account int, folder string, targets []mail.Summary, f mail.Flag, on bool) (Model, tea.Cmd) {
	var uids []mail.UID
	for _, s := range targets {
		current := s.Seen
		if f == mail.FlagFlagged {
			current = s.Flagged
		}
		if current != on {
			uids = append(uids, s.UID)
		}
	}
	if len(uids) == 0 || m.boxes[account] == nil {
		return m, nil
	}
	m.applyFlag(account, folder, uids, f, on)
	m.writes++
	m.inflight++
	m.log.Debug("set flag", "flag", f, "on", on, "count", len(uids))
	return m, setFlag(m.boxes[account], account, folder, uids, f, on)
}

func (m *Model) applyFlag(account int, folder string, uids []mail.UID, f mail.Flag, on bool) {
	inView := m.open.account == account && m.open.folder == folder
	switch f {
	case mail.FlagSeen:
		if inView {
			m.list = m.list.SetSeen(uids, on)
			m.read = m.read.SetSeen(uids, on)
		}
		delta := len(uids)
		if on {
			delta = -delta
		}
		m.side = m.side.AdjustUnread(account, folder, delta)
	case mail.FlagFlagged:
		if inView {
			m.list = m.list.SetFlagged(uids, on)
			m.read = m.read.SetFlagged(uids, on)
		}
	}
}

func (m Model) handleFlag(msg flagMsg) (Model, tea.Cmd) {
	m.inflight--
	if msg.err == nil {
		return m, nil
	}
	m.applyFlag(msg.account, msg.folder, msg.uids, msg.flag, !msg.on)
	switch {
	// A Mailbox that can't change flags isn't worth an error on every open.
	case errors.Is(msg.err, errors.ErrUnsupported):
		m.log.Debug("set flag unsupported", "flag", msg.flag)
		return m, nil
	case errors.Is(msg.err, mail.ErrFolderChanged):
		return m.reloadChanged(msg.account, msg.folder)
	}
	m.setError(fmt.Errorf("%s: %w", flagVerb(msg.flag, msg.on), msg.err))
	return m, nil
}

func flagVerb(f mail.Flag, on bool) string {
	switch {
	case f == mail.FlagSeen && on:
		return "mark as read"
	case f == mail.FlagSeen:
		return "mark as unread"
	case on:
		return "star"
	}
	return "unstar"
}

// moveTo archives or deletes targets: removes them from the UI at once and
// moves them on the server in the background. Nothing is ever expunged
// for good: delete moves to Trash, and both are disabled with an
// explanation when the account has no such folder.
func (m Model) moveTo(targets []mail.Summary, verb string) (Model, tea.Cmd) {
	account, from := m.open.account, m.open.folder
	name := m.accts[account].Name
	folders := m.side.Accounts()[account].Folders

	var to string
	var err error
	if verb == "archive" {
		to, err = mail.ArchiveFolder(m.accts[account].ArchiveFolder, folders)
	} else {
		to, err = mail.TrashFolder(folders)
	}
	switch {
	case err != nil:
		m.setError(fmt.Errorf("can't %s in %s: %w", verb, name, err))
		return m, nil
	case to == from && verb == "archive":
		m.setStatus("Already in the archive folder.")
		return m, nil
	case to == from:
		m.setStatus("Already in Trash; dk-email never deletes mail permanently.")
		return m, nil
	}
	label := to
	if f, ok := m.side.Folder(account, to); ok {
		label = sidebar.Label(f)
	}
	return m.startMove(targets, to, label, verb)
}

// startMove removes targets from the UI at once and moves them to the
// folder to on the server in the background.
func (m Model) startMove(targets []mail.Summary, to, label, verb string) (Model, tea.Cmd) {
	account, from := m.open.account, m.open.folder
	if m.boxes[account] == nil || to == from {
		return m, nil
	}
	msg := moveMsg{account: account, from: from, to: to, verb: verb, label: label, removed: targets}
	for _, s := range targets {
		msg.uids = append(msg.uids, s.UID)
		if !s.Seen {
			msg.unread++
		}
	}
	m.list = m.list.Remove(msg.uids)
	m.read = m.read.Remove(msg.uids)
	if !m.read.IsOpen() {
		m.readerPopup = false
		if m.focus == PaneReader {
			m.focus = PaneList
		}
	}
	m.side = m.side.AdjustUnread(account, from, -msg.unread)
	m.writes++
	m.inflight++
	m.setStatus(progressive(verb) + "…")
	m.log.Debug("move", "verb", verb, "count", len(msg.uids), "role", m.side.Role(account, to))
	return m, moveMessages(m.boxes[account], msg)
}

func (m Model) handleMove(msg moveMsg) (Model, tea.Cmd) {
	m.inflight--
	if msg.err == nil {
		done := fmt.Sprintf("%s %s", past(msg.verb), plural(len(msg.uids), "message"))
		if msg.verb == "move" {
			done += " to " + msg.label
		}
		m.setStatus(done + ".")
		return m, nil
	}
	if m.open.account == msg.account && m.open.folder == msg.from {
		m.list = m.list.Restore(msg.removed)
	}
	m.side = m.side.AdjustUnread(msg.account, msg.from, msg.unread)
	if errors.Is(msg.err, mail.ErrFolderChanged) {
		return m.reloadChanged(msg.account, msg.from)
	}
	m.setError(fmt.Errorf("%s: %w", msg.verb, msg.err))
	return m, nil
}

func progressive(verb string) string {
	switch verb {
	case "archive":
		return "Archiving"
	case "move":
		return "Moving"
	}
	return "Deleting"
}

func past(verb string) string {
	switch verb {
	case "archive":
		return "Archived"
	case "move":
		return "Moved"
	}
	return "Deleted"
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
