package ui

import (
	"context"
	"errors"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/dkouvdis/dk-email/internal/mail"
)

const (
	messageTimeout = 30 * time.Second
	flagTimeout    = 30 * time.Second
)

type messageMsg struct {
	account int
	folder  string
	uid     mail.UID
	msg     mail.Message
	err     error
}

type flagMsg struct {
	account int
	folder  string
	uids    []mail.UID
	flag    mail.Flag
	on      bool
	err     error
}

func loadMessage(mb mail.Mailbox, account int, folder string, uid mail.UID) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), messageTimeout)
		defer cancel()
		msg, err := mb.Message(ctx, folder, uid)
		return messageMsg{account: account, folder: folder, uid: uid, msg: msg, err: err}
	}
}

func setFlag(mb mail.Mailbox, account int, folder string, uids []mail.UID, f mail.Flag, on bool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), flagTimeout)
		defer cancel()
		err := mb.SetFlag(ctx, folder, uids, f, on)
		return flagMsg{account: account, folder: folder, uids: uids, flag: f, on: on, err: err}
	}
}

// openThread shows the thread under the list cursor in the reader. On a
// message row inside an expanded thread, that message is the one expanded.
func (m Model) openThread() (Model, tea.Cmd) {
	th, uid, ok := m.list.Selected()
	if !ok || m.open.account < 0 || m.open.loading {
		return m, nil
	}
	m.read = m.read.Open(m.open.account, m.open.folder, th, uid)
	m.focus = PaneReader
	m.log.Debug("open thread", "messages", th.Len())
	return m, m.fetchMissing()
}

// fetchMissing loads bodies the reader is waiting for.
func (m *Model) fetchMissing() tea.Cmd {
	uids := m.read.Missing()
	if len(uids) == 0 || m.open.account < 0 {
		return nil
	}
	m.read = m.read.MarkLoading(uids)
	mb := m.boxes[m.open.account]
	cmds := make([]tea.Cmd, 0, len(uids))
	for _, uid := range uids {
		cmds = append(cmds, loadMessage(mb, m.open.account, m.open.folder, uid))
	}
	return tea.Batch(cmds...)
}

func (m Model) handleMessage(msg messageMsg) (Model, tea.Cmd) {
	if !m.read.Showing(msg.account, msg.folder, msg.uid) {
		return m, nil
	}
	if errors.Is(msg.err, mail.ErrFolderChanged) {
		return m.reloadChanged(msg.account, msg.folder)
	}
	m.read = m.read.SetMessage(msg.uid, msg.msg, msg.err)
	if msg.err != nil {
		m.setError(fmt.Errorf("open message: %w", msg.err))
		return m, nil
	}
	m.log.Debug("message loaded", "attachments", len(msg.msg.Attachments), "html", msg.msg.BodyFromHTML)
	if !m.read.Unread(msg.uid) {
		return m, nil
	}
	return m, setFlag(m.boxes[msg.account], msg.account, msg.folder, []mail.UID{msg.uid}, mail.FlagSeen, true)
}

func (m Model) handleFlag(msg flagMsg) (Model, tea.Cmd) {
	if msg.err != nil {
		// Real IMAP can't set flags until T16; reading stays read-only until then.
		if errors.Is(msg.err, errors.ErrUnsupported) {
			m.log.Debug("set flag unsupported", "flag", msg.flag)
			return m, nil
		}
		m.setError(fmt.Errorf("mark %s: %w", msg.flag, msg.err))
		return m, nil
	}
	if msg.flag != mail.FlagSeen {
		return m, nil
	}
	if m.open.account == msg.account && m.open.folder == msg.folder {
		m.list = m.list.SetSeen(msg.uids, msg.on)
	}
	if m.read.IsOpen() {
		m.read = m.read.SetSeen(msg.uids, msg.on)
	}
	delta := len(msg.uids)
	if msg.on {
		delta = -delta
	}
	m.side = m.side.AdjustUnread(msg.account, msg.folder, delta)
	return m, nil
}
