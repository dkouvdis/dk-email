package ui

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/dkouvdis/dk-email/internal/compose"
	"github.com/dkouvdis/dk-email/internal/mail"
	"github.com/dkouvdis/dk-email/internal/render"
)

const statusLoadingReply = "Loading message…"

// composeKind is what a draft built from an existing message does with it.
type composeKind int

const (
	composeNew composeKind = iota
	composeReply
	composeReplyAll
	composeForward
	composeDraft // a saved draft reopened
)

func (k composeKind) String() string {
	return [...]string{"new message", "reply", "reply all", "forward", "draft"}[k]
}

// action is k's name in the composer's title.
func (k composeKind) action() string {
	return [...]string{"New message", "Reply", "Reply all", "Forward", "Draft"}[k]
}

// replyFetchedMsg carries the message a reply, reply-all or forward is
// waiting for.
type replyFetchedMsg struct {
	account int
	folder  string
	kind    composeKind
	msg     mail.Message
	files   []mail.File // a forward's attachments, from the same bytes as msg
	err     error
}

func fetchReply(mb mail.Mailbox, account int, folder string, uid mail.UID, kind composeKind) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), messageTimeout)
		defer cancel()
		msg, err := mb.Message(ctx, folder, uid)
		return replyFetchedMsg{account: account, folder: folder, kind: kind, msg: msg, err: err}
	}
}

// fetchForward fetches the raw message and derives both the message and its
// attachments from the same bytes, so they match by construction.
func fetchForward(mb mail.Mailbox, account int, folder string, uid mail.UID) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), messageTimeout)
		defer cancel()
		res := replyFetchedMsg{account: account, folder: folder, kind: composeForward}
		raw, err := mb.Raw(ctx, folder, uid)
		if err != nil {
			res.err = err
			return res
		}
		msg, err := render.Parse(bytes.NewReader(raw))
		if err != nil {
			res.err = err
			return res
		}
		msg.UID = uid
		parts, err := render.Parts(raw)
		if err != nil {
			res.err = err
			return res
		}
		res.msg = msg
		for _, p := range parts {
			res.files = append(res.files, mail.File{Name: p.Filename, ContentType: p.ContentType, Data: p.Data})
		}
		return res
	}
}

// actionTarget is the message r, R and f act on. In the reader it is the
// message the reader was opened on from an expanded row, else the thread's
// latest. On the list it is the selected message on an expanded row, else the
// newest message of the thread under the cursor.
func (m Model) actionTarget() (mail.Summary, bool) {
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

// startReply drafts a reply, or a reply-all, to the target message.
func (m Model) startReply(all bool) (Model, tea.Cmd) {
	if all {
		return m.startFromTarget(composeReplyAll)
	}
	return m.startFromTarget(composeReply)
}

// startForward drafts a forward of the target message.
func (m Model) startForward() (Model, tea.Cmd) {
	return m.startFromTarget(composeForward)
}

// startFromTarget drafts kind from the target message. A reply takes the
// message from the reader when it already has it; a forward always fetches the
// raw message, since the reader has no bytes to copy attachments from. The
// draft slot is held meanwhile.
func (m Model) startFromTarget(kind composeKind) (Model, tea.Cmd) {
	account, ok := m.draftAccount(m.open.account)
	if !ok {
		return m, nil
	}
	target, ok := m.actionTarget()
	if !ok || m.boxes[account] == nil {
		return m, nil
	}
	folder := m.open.folder
	if kind != composeForward && m.read.Showing(account, folder, target.UID) {
		if msg, ok := m.read.Message(target.UID); ok {
			return m.startMessageDraft(account, folder, msg, nil, kind)
		}
	}
	m.draft = &draftState{account: account, loading: true}
	m.setStatus(statusLoadingReply)
	m.log.Debug("compose: fetching message", "account", m.accts[account].Name, "action", kind.String())
	if kind == composeForward {
		return m, fetchForward(m.boxes[account], account, folder, target.UID)
	}
	return m, fetchReply(m.boxes[account], account, folder, target.UID, kind)
}

// startMessageDraft opens the composer on kind's draft for msg. A forward
// carries the original's attachments, files, in the draft.
func (m Model) startMessageDraft(account int, folder string, msg mail.Message, files []mail.File, kind composeKind) (Model, tea.Cmd) {
	mine := m.identities(account)
	switch kind {
	case composeReplyAll:
		return m.startDraft(account, compose.ReplyAll(msg, folder, mine), kind)
	case composeForward:
		d := compose.Forward(msg, folder, mine)
		d.Attachments = files
		return m.startDraft(account, d, kind)
	}
	return m.startDraft(account, compose.Reply(msg, folder, mine), kind)
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
	return m.startMessageDraft(msg.account, msg.folder, msg.msg, msg.files, msg.kind)
}
