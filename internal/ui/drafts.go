package ui

import (
	"context"
	"errors"
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/dkouvdis/dk-email/internal/mail"
)

const statusOpeningDraft = "Opening draft…"

// draftOpenedMsg carries a saved draft read back for editing.
type draftOpenedMsg struct {
	account int
	folder  string
	draft   mail.Draft
	err     error
}

func fetchDraft(dr mail.Drafter, account int, ref mail.MessageRef) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), messageTimeout)
		defer cancel()
		d, err := dr.OpenDraft(ctx, ref)
		return draftOpenedMsg{account: account, folder: ref.Folder, draft: d, err: err}
	}
}

// openDraft reads the saved draft uid in the open folder and, when it
// arrives, opens it in the composer. The draft slot is held meanwhile.
func (m Model) openDraft(uid mail.UID) (Model, tea.Cmd) {
	account, ok := m.draftAccount(m.open.account)
	if !ok {
		return m, nil
	}
	dr, ok := m.drafter(account)
	if !ok {
		return m, nil
	}
	m.draft = &draftState{account: account, loading: true}
	m.setStatus(statusOpeningDraft)
	m.log.Debug("compose: fetching draft", "account", m.accts[account].Name)
	return m, fetchDraft(dr, account, mail.MessageRef{Folder: m.open.folder, UID: uid})
}

func (m Model) handleDraftOpened(msg draftOpenedMsg) (Model, tea.Cmd) {
	if d := m.draft; d == nil || !d.loading || d.account != msg.account {
		return m, nil
	}
	if msg.err != nil {
		m.draft = nil
		if errors.Is(msg.err, mail.ErrFolderChanged) && m.open.account == msg.account && m.open.folder == msg.folder {
			return m.reloadChanged(msg.account, msg.folder)
		}
		m.setError(fmt.Errorf("open draft: %w", msg.err))
		return m, nil
	}
	m.setStatus("")
	d := msg.draft
	d.ReplyTo = nil
	if d.From == (mail.Address{}) {
		d.From = m.identities(msg.account)[0]
	}
	return m.startDraft(msg.account, d, composeDraft)
}
