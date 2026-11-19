package ui

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/dkouvdis/dk-email/internal/compose"
	"github.com/dkouvdis/dk-email/internal/mail"
	"github.com/dkouvdis/dk-email/internal/ui/popup"
	"github.com/dkouvdis/dk-email/internal/ui/reader"
)

const (
	// saveTimeout bounds saving a draft, which is IMAP only.
	saveTimeout = 15 * time.Second
	// sendTimeout bounds a whole send: SMTP plus the IMAP steps around it.
	sendTimeout = 30 * time.Second
)

const (
	statusFinishDraft   = "Finish or discard the draft first"
	statusSending       = "Sending…"
	statusSaving        = "Saving…"
	statusSaved         = "Draft saved"
	statusCancelling    = "Cancelling…"
	statusSaveCancelled = "Save cancelled"
	statusDiscarded     = "Draft discarded"
)

// draftState is the message being composed, from the first key press until
// it is sent or closed. It lives only in memory.
type draftState struct {
	account int
	// base is the draft the composer was filled from; every send parses the
	// fields against it so what they don't carry (From, threading, the
	// original of a forward) survives.
	base mail.Draft
	// from are the addresses the composer's From row cycles through.
	from []mail.Address
	// titlePrefix is the title up to the From address.
	titlePrefix string
	// loading is set while the message being replied to is fetched, before
	// the composer opens. It holds the slot so no other draft starts.
	loading bool
	// pop is the composer, an overlay: opening and closing it never touches
	// the view beneath.
	pop popup.Compose
}

// composing reports whether the composer is open.
func (m Model) composing() bool { return m.draft != nil && !m.draft.loading }

// sentMsg reports the outcome of sending a draft.
type sentMsg struct {
	account int
	draft   mail.Draft
	err     error
	took    time.Duration
}

// draftSavedMsg reports the outcome of saving a draft to the Drafts folder.
type draftSavedMsg struct {
	account int
	draft   mail.Draft
	err     error
	took    time.Duration
}

// drafter is account's way of saving drafts, if it has one.
func (m Model) drafter(account int) (mail.Drafter, bool) {
	if account < 0 || account >= len(m.senders) {
		return nil, false
	}
	d, ok := m.senders[account].(mail.Drafter)
	return d, ok
}

// draftAccount checks a new draft can start from account and returns it.
// When it can't, it sets the status that says why, or leaves it alone if
// there is nothing to say.
func (m *Model) draftAccount(account int) (int, bool) {
	switch {
	case m.sending:
		m.setStatus(statusSending)
		return 0, false
	case m.saving:
		m.setStatus(statusSaving)
		return 0, false
	case m.draft != nil:
		m.setStatus(statusFinishDraft)
		return 0, false
	case account < 0:
		return 0, false
	}
	if m.boxes[account] == nil {
		m.setStatus(fmt.Sprintf("%s isn't connected.", m.accts[account].Name))
		return 0, false
	}
	if m.senders[account] == nil {
		m.setStatus(fmt.Sprintf("Sending isn't set up for %s: add smtp_host to its config", m.accts[account].Name))
		return 0, false
	}
	return account, true
}

// identities are the addresses account sends as: its email with from_name,
// then its aliases, each named by its own name or else from_name.
func (m Model) identities(account int) []mail.Address {
	a := m.accts[account]
	out := []mail.Address{{Name: a.FromName, Email: a.Email}}
	for _, al := range a.Aliases {
		name := al.Name
		if name == "" {
			name = a.FromName
		}
		out = append(out, mail.Address{Name: name, Email: al.Email})
	}
	return out
}

// activeAccount is the account the sidebar cursor is in, the one the
// sidebar shows undimmed, or the open folder's account when the cursor is
// on no folder.
func (m Model) activeAccount() int {
	if account, _ := m.side.Cursor(); account >= 0 {
		return account
	}
	return m.open.account
}

// startCompose opens an empty message from the active account.
func (m Model) startCompose() (Model, tea.Cmd) {
	account, ok := m.draftAccount(m.activeAccount())
	if !ok {
		return m, nil
	}
	return m.startDraft(account, compose.New(m.identities(account)[0]), composeNew)
}

// startDraft opens base in the composer. A reply starts in the body, above
// its quote, and so does a reopened draft that has recipients; everything
// else starts in To.
func (m Model) startDraft(account int, base mail.Draft, kind composeKind) (Model, tea.Cmd) {
	fields := compose.FieldsOf(base)
	title := kind.action()
	if fields.Subject != "" {
		title += " · " + fields.Subject
	}
	choices := m.identities(account)
	if e := base.From.Email; e != "" && indexOfAddr(choices, e) < 0 {
		choices = append(choices, base.From)
	}
	selected := max(indexOfAddr(choices, base.From.Email), 0)
	prefix := title
	title += " · " + choices[selected].Email
	focus := popup.ComposeTo
	if kind == composeReply || kind == composeReplyAll || (kind == composeDraft && len(recipients(base)) > 0) {
		focus = popup.ComposeBody
	}
	pop := popup.NewCompose(m.theme, title, popup.ComposeValues(fields), focus)
	labels := make([]string, len(choices))
	for i, a := range choices {
		labels[i] = a.String()
	}
	pop.SetFrom(labels, selected)
	pop.InitAttachments(attachmentLabels(base.Attachments))
	pop.SetSize(m.panes.Popup.W, m.panes.Popup.H)
	_, canSave := m.drafter(account)
	pop.EnableSave(canSave)
	m.draft = &draftState{account: account, base: base, from: choices, titlePrefix: prefix, pop: pop}
	m.log.Debug("popup opened", "popup", "compose", "action", kind.String())
	return m, nil
}

// attachmentLabels are the composer's attachment items: "name (12 KB)".
func attachmentLabels(files []mail.File) []string {
	out := make([]string, len(files))
	for i, f := range files {
		out[i] = f.Name + " (" + reader.FormatSize(int64(len(f.Data))) + ")"
	}
	return out
}

// indexOfAddr is the index of the address with email in list, compared
// case-insensitively, or -1.
func indexOfAddr(list []mail.Address, email string) int {
	for i, a := range list {
		if email != "" && strings.EqualFold(a.Email, email) {
			return i
		}
	}
	return -1
}

// selectedFrom is the address picked in the composer's From row.
func (d *draftState) selectedFrom() mail.Address {
	if i := d.pop.FromIndex(); i >= 0 && i < len(d.from) {
		return d.from[i]
	}
	return d.base.From
}

// closeDraft closes the composer, returning to the view beneath it.
func (m *Model) closeDraft() {
	m.draft = nil
	m.log.Debug("popup closed", "popup", "compose")
}

// updateComposer gives msg to the composer and acts on what it asks for.
func (m Model) updateComposer(msg tea.Msg) (Model, tea.Cmd) {
	d := m.draft
	var res popup.ComposeResult
	var cmd tea.Cmd
	d.pop, res, cmd = d.pop.Update(msg)
	d.pop.SetTitle(d.titlePrefix + " · " + d.selectedFrom().Email)
	switch res {
	case popup.ComposeSend:
		return m.startSend()
	case popup.ComposeSave:
		return m.startSave()
	case popup.ComposeCancel:
		if m.saving && m.cancelSave != nil {
			m.cancelSave()
			m.setStatus(statusCancelling)
		}
	case popup.ComposeRemoveAttachment:
		// A copy, so the files a send or save already holds stay as they were.
		if i, files := d.pop.AttachmentIndex(), d.base.Attachments; i >= 0 && i < len(files) {
			d.base.Attachments = slices.Delete(slices.Clone(files), i, i+1)
			d.pop.SetAttachments(attachmentLabels(d.base.Attachments))
			m.log.Debug("attachment removed", "left", len(d.base.Attachments))
		}
	case popup.ComposeComplete, popup.ComposeAttachFile:
		return m, tea.Batch(cmd, m.attachPrompt(res))
	case popup.ComposeClose:
		m.closeDraft()
	case popup.ComposeDiscard:
		m.closeDraft()
		m.status, m.statusErr = statusDiscarded, false
	}
	return m, cmd
}

// startSend parses the composer's fields and sends them in the background,
// leaving the composer open and locked meanwhile. A parse error is shown in
// the composer, which stays editable.
func (m Model) startSend() (Model, tea.Cmd) {
	d := m.draft
	sender := m.senders[d.account]
	if sender == nil {
		d.pop.SetError("Sending isn't set up for this account")
		return m, nil
	}
	draft, err := compose.Fields(d.pop.Values()).Draft(d.base)
	if err != nil {
		d.pop.SetError(err.Error())
		return m, nil
	}
	draft.From = d.selectedFrom()
	d.pop.SetBusy(statusSending)
	m.sending = true
	m.status, m.statusErr = statusSending, false
	m.log.Debug("send started", "account", m.accts[d.account].Name, "recipients", len(recipients(draft)))
	return m, sendDraft(sender, d.account, draft)
}

// startSave parses the composer's fields, which may lack recipients, and
// saves them to Drafts in the background, leaving the composer open and
// locked meanwhile. A parse error is shown in the composer, which stays
// editable.
func (m Model) startSave() (Model, tea.Cmd) {
	d := m.draft
	drafter, ok := m.drafter(d.account)
	if !ok {
		d.pop.SetError("Saving drafts isn't set up for this account")
		return m, nil
	}
	draft, err := compose.Fields(d.pop.Values()).DraftForSave(d.base)
	if err != nil {
		d.pop.SetError(err.Error())
		return m, nil
	}
	draft.From = d.selectedFrom()
	d.pop.SetBusy(statusSaving)
	d.pop.SetCancellable(true)
	ctx, cancel := context.WithTimeout(context.Background(), saveTimeout)
	m.cancelSave = cancel
	m.saving = true
	m.status, m.statusErr = statusSaving, false
	m.log.Debug("draft save started", "account", m.accts[d.account].Name, "recipients", len(recipients(draft)), "replaces", draft.DraftOf != nil)
	return m, saveDraft(ctx, cancel, drafter, d.account, draft)
}

// saveDraft runs in a tea.Cmd. ctx ends after saveTimeout, so a dead
// connection fails the save instead of hanging it, or when esc cancels it.
func saveDraft(ctx context.Context, cancel context.CancelFunc, dr mail.Drafter, account int, d mail.Draft) tea.Cmd {
	return func() tea.Msg {
		start := time.Now()
		defer cancel()
		err := dr.SaveDraft(ctx, d)
		return draftSavedMsg{account: account, draft: d, err: err, took: time.Since(start)}
	}
}

func (m Model) handleDraftSaved(msg draftSavedMsg) (tea.Model, tea.Cmd) {
	m.saving = false
	m.cancelSave = nil
	if msg.account >= 0 && msg.account < len(m.accts) {
		m.log.Debug("draft save finished", "account", m.accts[msg.account].Name, "ok", msg.err == nil, "recipients", len(recipients(msg.draft)), "took", msg.took.Round(time.Millisecond))
	}
	switch {
	case msg.err == nil:
		m.closeDraft()
		m.status, m.statusErr = statusSaved, false
	case errors.Is(msg.err, mail.ErrDraftSaved):
		// Saved, so there's nothing to retry.
		m.closeDraft()
		m.status, m.statusErr = capitalize(msg.err.Error()), true
	case errors.Is(msg.err, context.Canceled) && m.composing():
		m.draft.pop.SetBusy("")
		m.status, m.statusErr = statusSaveCancelled, false
		return m, nil
	case !m.composing():
		m.setError(msg.err)
		return m, nil
	default:
		m.draft.pop.SetBusy("")
		m.draft.pop.SetError("Save failed: " + msg.err.Error())
		m.status, m.statusErr = "", false
		return m, nil
	}
	return m.refreshSoon(msg.account)
}

// sendDraft runs in a tea.Cmd. It gives up after sendTimeout; past delivery
// that is reported like any later failure.
func sendDraft(s mail.Sender, account int, d mail.Draft) tea.Cmd {
	return func() tea.Msg {
		start := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
		defer cancel()
		err := s.Send(ctx, d)
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
		m.closeDraft()
		m.status, m.statusErr = sentStatus(msg.draft), false
	case errors.Is(msg.err, mail.ErrAfterDelivery):
		// Delivered, so there's nothing to retry.
		m.closeDraft()
		m.status, m.statusErr = capitalize(msg.err.Error()), true
	case !m.composing():
		m.setError(msg.err)
		return m, nil
	default:
		m.draft.pop.SetBusy("")
		m.draft.pop.SetError("Send failed: " + msg.err.Error())
		m.status, m.statusErr = "", false
		return m, nil
	}
	if msg.draft.DraftOf != nil {
		// The saved copy this replaced is gone from Drafts.
		return m.refreshSoon(msg.account)
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
