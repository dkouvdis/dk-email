package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/dkouvdis/dk-email/internal/compose"
	"github.com/dkouvdis/dk-email/internal/mail"
	"github.com/dkouvdis/dk-email/internal/ui/popup"
)

const (
	statusFinishDraft = "Finish or discard the draft first"
	statusSending     = "Sending…"
	statusDiscarded   = "Draft discarded"
)

// draftState is the message being composed, from the first editor launch
// until it is sent or discarded. It lives only in memory.
type draftState struct {
	account int
	// base is the draft the editor file was built from; every parse starts
	// from it so fields the file doesn't carry (From, threading) survive.
	base mail.Draft
	// text is the file as last read back from the editor. It stays empty
	// until the confirm popup first opens, which is how a re-edit is told
	// apart from the first edit.
	text string
	// orig is the template the editor first opened with.
	orig string
	// parsed is text parsed against base. It is what gets sent.
	parsed   mail.Draft
	parseErr error
	sendErr  error
	// loading is set while the message being replied to is fetched, before
	// any editor has opened. It holds the slot so no other draft starts.
	loading bool
}

func (d *draftState) reedit() bool { return d.text != "" }

// sentMsg reports the outcome of sending a draft.
type sentMsg struct {
	account int
	draft   mail.Draft
	err     error
	took    time.Duration
}

// draftAccount is the account a new draft would be sent from. When none can
// start, it sets the status that says why, or leaves it alone if there is
// nothing to say.
func (m *Model) draftAccount() (int, bool) {
	switch {
	case m.sending:
		m.setStatus(statusSending)
		return 0, false
	case m.draft != nil:
		m.setStatus(statusFinishDraft)
		return 0, false
	case m.open.account < 0:
		return 0, false
	}
	account := m.open.account
	if m.senders[account] == nil {
		m.setStatus(fmt.Sprintf("Sending isn't set up for %s: add smtp_host to its config", m.accts[account].Name))
		return 0, false
	}
	return account, true
}

// identity is the From address of account's messages.
func (m Model) identity(account int) mail.Address {
	a := m.accts[account]
	return mail.Address{Name: a.FromName, Email: a.Email}
}

// startCompose opens an empty message from the open folder's account.
func (m Model) startCompose() (Model, tea.Cmd) {
	account, ok := m.draftAccount()
	if !ok {
		return m, nil
	}
	return m.startDraft(account, compose.New(m.identity(account)))
}

// startDraft opens base in the editor as the draft being composed.
func (m Model) startDraft(account int, base mail.Draft) (Model, tea.Cmd) {
	text := compose.Format(base)
	m.draft = &draftState{account: account, base: base, orig: text}
	m, cmd := m.startEditor(text)
	if cmd == nil {
		m.draft = nil // the editor never started; the error is in the status
	}
	return m, cmd
}

// discardDraft drops the draft and closes the confirm popup.
func (m *Model) discardDraft() {
	m.draft, m.showConfirm = nil, false
	m.status, m.statusErr = statusDiscarded, false
	m.log.Debug("draft discarded")
}

// openConfirm shows the confirm popup for the current draft.
func (m *Model) openConfirm() {
	d := m.draft
	m.confirmPop = popup.NewConfirm(m.theme, d.parsed, d.parseErr, d.sendErr)
	m.confirmPop.SetSize(m.panes.Popup.W, m.panes.Popup.H)
	m.showConfirm = true
	m.log.Debug("popup opened", "popup", "confirm", "parse error", d.parseErr != nil, "send error", d.sendErr != nil)
}

// handleEditorDone is where the edited draft arrives: an unchanged or empty
// file is discarded, anything else is parsed and put to the confirm popup.
func (m Model) handleEditorDone(msg editorDoneMsg) (tea.Model, tea.Cmd) {
	m.editorPath = ""
	d := m.draft
	if d == nil {
		if msg.err != nil {
			m.setError(msg.err)
		}
		return m, nil
	}
	if msg.err != nil {
		m.setError(msg.err)
		if d.reedit() {
			m.openConfirm() // keep the text from before the failed edit
		} else {
			m.draft = nil
		}
		return m, nil
	}
	if strings.TrimSpace(msg.text) == "" || (!d.reedit() && msg.text == d.orig) {
		m.discardDraft()
		return m, nil
	}
	d.text, d.sendErr = msg.text, nil
	d.parsed, d.parseErr = compose.Parse(msg.text, d.base)
	m.openConfirm()
	return m, nil
}

func (m Model) handleConfirmKey(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	var r popup.ConfirmResult
	m.confirmPop, r = m.confirmPop.Update(msg)
	switch r {
	case popup.ConfirmSend:
		return m.startSend()
	case popup.ConfirmEdit:
		m.showConfirm = false
		m.draft.sendErr = nil
		m, cmd := m.startEditor(m.draft.text)
		if cmd == nil {
			m.openConfirm() // the editor never started; the error is in the status
		}
		return m, cmd
	case popup.ConfirmDiscard:
		m.discardDraft()
	}
	return m, nil
}

// startSend closes the popup and sends the parsed draft in the background.
func (m Model) startSend() (Model, tea.Cmd) {
	d := m.draft
	sender := m.senders[d.account]
	if sender == nil {
		d.sendErr = errors.New("sending isn't set up for this account")
		m.openConfirm()
		return m, nil
	}
	d.sendErr = nil
	m.showConfirm, m.sending = false, true
	m.status, m.statusErr = statusSending, false
	m.log.Debug("send started", "account", m.accts[d.account].Name, "recipients", len(recipients(d.parsed)))
	return m, sendDraft(sender, d.account, d.parsed)
}

// sendDraft runs in a tea.Cmd. The transport enforces its own timeout.
func sendDraft(s mail.Sender, account int, d mail.Draft) tea.Cmd {
	return func() tea.Msg {
		start := time.Now()
		err := s.Send(context.Background(), d)
		return sentMsg{account: account, draft: d, err: err, took: time.Since(start)}
	}
}

func (m Model) handleSent(msg sentMsg) (tea.Model, tea.Cmd) {
	m.sending = false
	if msg.account >= 0 && msg.account < len(m.accts) {
		m.log.Debug("send finished", "account", m.accts[msg.account].Name, "ok", msg.err == nil, "recipients", len(recipients(msg.draft)), "took", msg.took.Round(time.Millisecond))
	}
	switch {
	case msg.err == nil:
		m.draft = nil
		m.status, m.statusErr = sentStatus(msg.draft), false
	case errors.Is(msg.err, mail.ErrAfterDelivery):
		// Delivered, so there's nothing to retry.
		m.draft = nil
		m.status, m.statusErr = capitalize(msg.err.Error()), true
	case m.draft == nil:
		m.setError(msg.err)
	default:
		m.draft.sendErr = msg.err
		m.status, m.statusErr = "Send failed", false
		m.openConfirm()
	}
	return m, nil
}

// recipients are the To, Cc and Bcc addresses in that order, without
// repeats (compared case-insensitively).
func recipients(d mail.Draft) []string {
	var out []string
	seen := map[string]bool{}
	for _, l := range [][]mail.Address{d.To, d.Cc, d.Bcc} {
		for _, a := range l {
			k := strings.ToLower(a.Email)
			if !seen[k] {
				seen[k] = true
				out = append(out, a.Email)
			}
		}
	}
	return out
}

// sentStatus is "Sent to <first>", plus " (+n)" for the others.
func sentStatus(d mail.Draft) string {
	rcpts := recipients(d)
	if len(rcpts) == 0 {
		return "Sent"
	}
	s := "Sent to " + rcpts[0]
	if n := len(rcpts) - 1; n > 0 {
		s += fmt.Sprintf(" (+%d)", n)
	}
	return s
}

func capitalize(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	return string(unicode.ToUpper(r)) + s[n:]
}
