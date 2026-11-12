package ui

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/dkouvdis/dk-email/internal/compose"
	"github.com/dkouvdis/dk-email/internal/mail"
)

const statusLoadingReply = "Loading message…"

// replyFetchedMsg carries the message a reply is waiting for.
type replyFetchedMsg struct {
	account int
	folder  string
	all     bool
	msg     mail.Message
	err     error
}

func fetchReply(mb mail.Mailbox, account int, folder string, uid mail.UID, all bool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), messageTimeout)
		defer cancel()
		msg, err := mb.Message(ctx, folder, uid)
		return replyFetchedMsg{account: account, folder: folder, all: all, msg: msg, err: err}
	}
}

// replyTarget is the message r and R answer. In the reader it is the message
// the reader was opened on from an expanded row, else the thread's latest.
// On the list it is the selected message on an expanded row, else the
// newest message of the thread under the cursor.
func (m Model) replyTarget() (mail.Summary, bool) {
	var targets []mail.Summary
	if m.readerPopup && m.open.account >= 0 && !m.open.loading {
		if uid := m.read.ReplyTarget(); uid != 0 {
			targets = m.list.Lookup([]mail.UID{uid})
		}
	} else {
		targets = m.targets()
	}
	if len(targets) == 0 {
		return mail.Summary{}, false
	}
	return slices.MaxFunc(targets, func(a, b mail.Summary) int {
		if c := a.Date.Compare(b.Date); c != 0 {
			return c
		}
		return cmp.Compare(a.UID, b.UID)
	}), true
}

// startReply drafts a reply to the target message. The message is taken from
// the reader when it already has it, and fetched otherwise; the draft slot is
// held meanwhile.
func (m Model) startReply(all bool) (Model, tea.Cmd) {
	account, ok := m.draftAccount()
	if !ok {
		return m, nil
	}
	target, ok := m.replyTarget()
	if !ok || m.boxes[account] == nil {
		return m, nil
	}
	folder := m.open.folder
	if m.read.Showing(account, folder, target.UID) {
		if msg, ok := m.read.Message(target.UID); ok {
			return m.startDraft(account, m.replyDraft(account, folder, msg, all))
		}
	}
	m.draft = &draftState{account: account, loading: true}
	m.setStatus(statusLoadingReply)
	m.log.Debug("reply: fetching message", "account", m.accts[account].Name, "all", all)
	return m, fetchReply(m.boxes[account], account, folder, target.UID, all)
}

func (m Model) replyDraft(account int, folder string, msg mail.Message, all bool) mail.Draft {
	if all {
		return compose.ReplyAll(msg, folder, m.identity(account))
	}
	return compose.Reply(msg, folder, m.identity(account))
}

func (m Model) handleReplyFetched(msg replyFetchedMsg) (Model, tea.Cmd) {
	if d := m.draft; d == nil || !d.loading || d.account != msg.account {
		return m, nil
	}
	if msg.err != nil {
		m.draft = nil
		if errors.Is(msg.err, mail.ErrFolderChanged) && m.open.account == msg.account && m.open.folder == msg.folder {
			return m.reloadChanged(msg.account, msg.folder)
		}
		m.setError(fmt.Errorf("open message: %w", msg.err))
		return m, nil
	}
	m.setStatus("")
	return m.startDraft(msg.account, m.replyDraft(msg.account, msg.folder, msg.msg, msg.all))
}
