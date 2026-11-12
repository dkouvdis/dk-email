package ui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/dkouvdis/dk-email/internal/mail"
	"github.com/dkouvdis/dk-email/internal/mail/mailtest"
	"github.com/dkouvdis/dk-email/internal/ui/popup"
)

// composeFixture is a started model where Personal can send through the
// returned sender and Work can't.
func composeFixture(t *testing.T) (Model, *mailtest.DraftSender) {
	t.Helper()
	s := &mailtest.DraftSender{}
	m, _ := composeFixtureWith(t, s)
	return m, s
}

// composeFixtureWith is composeFixture with Personal's sender s, which may
// lack saving. It also returns the mailboxes.
func composeFixtureWith(t *testing.T, s mail.Sender) (Model, *fixture) {
	t.Helper()
	f := newFixture()
	f.personalSender = s
	m := started(t, f, 160, 30)
	m.accts[0].Email, m.accts[0].FromName = "me@example.com", "Me Myself"
	return m, f
}

// folderCalls counts the Folders calls mb has served.
func folderCalls(mb *mailtest.Fake) int {
	n := 0
	for _, c := range mb.Calls() {
		if c.Method == mailtest.MethodFolders {
			n++
		}
	}
	return n
}

// keyCmd presses k and returns the model and the command it produced.
func keyCmd(t *testing.T, m Model, k string) (Model, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(press(k))
	return next.(Model), cmd
}

// typed types text into the composer; "\n" is enter.
func typed(t *testing.T, m Model, text string) Model {
	t.Helper()
	for _, r := range text {
		if r == '\n' {
			m = send(t, m, press("enter"))
			continue
		}
		m = send(t, m, press(string(r)))
	}
	return m
}

// written opens the composer with c and fills in To, Subject and the body.
func written(t *testing.T, m Model, to, subject, body string) Model {
	t.Helper()
	m, _ = keyCmd(t, m, "c")
	if !m.composing() {
		t.Fatalf("c didn't open the composer (status %q)", m.status)
	}
	m = typed(t, m, to)
	m = send(t, m, press("tab"), press("tab"), press("tab"))
	m = typed(t, m, subject)
	m = send(t, m, press("tab"))
	return typed(t, m, body)
}

// sendKey presses ctrl+s and delivers the send result back to the model.
func sendKey(t *testing.T, m Model) Model {
	t.Helper()
	m, cmd := keyCmd(t, m, "ctrl+s")
	if cmd == nil {
		t.Fatalf("ctrl+s started nothing (status %q)", m.status)
	}
	return send(t, m, cmd())
}

func TestCompose_COpensTheComposerInTo(t *testing.T) {
	m, _ := composeFixture(t)

	m, _ = keyCmd(t, m, "c")

	if !m.composing() || m.draft.pop.Values() != (popup.ComposeValues{}) {
		t.Fatalf("draft %v; want an empty composer", m.draft)
	}
	view := plain(m)
	for _, want := range []string{"New message · me@example.com", "To", "Cc", "Bcc", "Subject"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q:\n%s", want, view)
		}
	}
	m = typed(t, m, "bob")
	if got := m.draft.pop.Values().To; got != "bob" {
		t.Errorf("To = %q, want typing to land in the focused To field", got)
	}
}

func TestCompose_HintsRowShowsComposerKeys(t *testing.T) {
	m, _ := composeFixture(t)
	m, _ = keyCmd(t, m, "c")

	if h := strings.TrimSpace(ansi.Strip(m.renderHints())); h != "ctrl+s send · tab next field · esc close" {
		t.Errorf("hints = %q", h)
	}
}

func TestCompose_ComposerSwallowsKeys(t *testing.T) {
	m, s := composeFixture(t)
	m, _ = keyCmd(t, m, "c")
	focus, mode := m.focus, m.mode
	row, _, _ := m.list.Selected()

	for _, k := range []string{"tab", "shift+tab", "q", "e", "d", "?", "a", "A", "v", "j", "ctrl+r", "c", "r", "R", "f"} {
		m, _ = keyCmd(t, m, k)
		if !m.composing() || m.showHelp || m.showPicker || m.showConnect || m.focus != focus || m.mode != mode {
			t.Fatalf("%q reached the main view (composing %v, focus %v)", k, m.composing(), m.focus)
		}
	}
	if got, _, _ := m.list.Selected(); got.Latest().UID != row.Latest().UID {
		t.Error("the list selection moved while composing")
	}
	if len(s.Sent()) != 0 || m.status == "Draft discarded" {
		t.Error("a key acted on the draft")
	}
}

func TestCompose_PasteGoesToTheComposer(t *testing.T) {
	m, _ := composeFixture(t)
	m, _ = keyCmd(t, m, "c")

	m = send(t, m, tea.PasteMsg{Content: "a@example.com"})

	if got := m.draft.pop.Values().To; got != "a@example.com" {
		t.Errorf("To = %q after a paste", got)
	}
}

func TestCompose_SendParsesTheFieldsAndSends(t *testing.T) {
	m, s := composeFixture(t)
	m = written(t, m, "Alice <alice@example.com>", "Lunch", "Shall we?\nSecond line.\n")

	m, cmd := keyCmd(t, m, "ctrl+s")

	if cmd == nil || !m.sending || !m.composing() || m.status != "Sending…" {
		t.Fatalf("cmd %v, sending %v, composing %v, status %q", cmd != nil, m.sending, m.composing(), m.status)
	}
	if view := plain(m); !strings.Contains(view, "Sending…") {
		t.Errorf("composer should say Sending…:\n%s", view)
	}
	m = send(t, m, cmd())

	sent := s.Sent()
	if len(sent) != 1 {
		t.Fatalf("sent %d drafts, want 1", len(sent))
	}
	d := sent[0]
	if d.From != (mail.Address{Name: "Me Myself", Email: "me@example.com"}) ||
		len(d.To) != 1 || d.To[0].Email != "alice@example.com" ||
		d.Subject != "Lunch" || d.Body != "Shall we?\nSecond line.\n" {
		t.Errorf("draft = %+v", d)
	}
	if m.composing() || m.sending || m.status != "Sent to alice@example.com" || m.statusErr {
		t.Errorf("composing %v, sending %v, status %q (err %v)", m.composing(), m.sending, m.status, m.statusErr)
	}
}

func TestCompose_BadAddressShowsTheErrorAndDoesNotSend(t *testing.T) {
	m, s := composeFixture(t)
	m = written(t, m, "not an address", "Hi", "body")

	m, cmd := keyCmd(t, m, "ctrl+s")

	if cmd != nil || m.sending || !m.composing() || len(s.Sent()) != 0 {
		t.Fatalf("cmd %v, sending %v, composing %v, sent %d", cmd != nil, m.sending, m.composing(), len(s.Sent()))
	}
	if view := plain(m); !strings.Contains(view, "To: invalid address list") {
		t.Errorf("composer should show the parse error:\n%s", view)
	}
	if got := m.draft.pop.Values(); got.Subject != "Hi" || got.Body != "body" {
		t.Errorf("values = %+v; editing should continue from what was typed", got)
	}
}

func TestCompose_NoRecipientsShowsTheError(t *testing.T) {
	m, s := composeFixture(t)
	m, _ = keyCmd(t, m, "c")

	m, cmd := keyCmd(t, m, "ctrl+s")

	if cmd != nil || len(s.Sent()) != 0 || !strings.Contains(plain(m), "no recipients") {
		t.Errorf("cmd %v, sent %d, view:\n%s", cmd != nil, len(s.Sent()), plain(m))
	}
}

func TestCompose_BusyComposerIgnoresKeysButCtrlCQuits(t *testing.T) {
	m, s := composeFixture(t)
	m = written(t, m, "alice@example.com", "Hi", "x")
	m, _ = keyCmd(t, m, "ctrl+s")
	before := m.draft.pop.Values()

	for _, k := range []string{"y", "esc", "tab", "q", "c", "ctrl+s"} {
		var cmd tea.Cmd
		m, cmd = keyCmd(t, m, k)
		if cmd != nil || !m.composing() || m.draft.pop.Values() != before {
			t.Fatalf("%q while sending: cmd %v, composing %v, values %+v", k, cmd != nil, m.composing(), m.draft.pop.Values())
		}
	}
	if _, cmd := m.Update(press("ctrl+c")); cmd == nil {
		t.Error("ctrl+c should still quit while sending")
	}
	if len(s.Sent()) != 0 {
		t.Error("keys while busy sent something")
	}
}

func TestCompose_CtrlCQuitsWhileComposing(t *testing.T) {
	m, _ := composeFixture(t)
	m, _ = keyCmd(t, m, "c")

	if _, cmd := m.Update(press("ctrl+c")); cmd == nil {
		t.Error("ctrl+c should quit with the composer open")
	}
}

func TestCompose_FailureKeepsTheComposerAndRetrySendsAgain(t *testing.T) {
	m, s := composeFixture(t)
	s.FailWith(errors.New("smtp: connection refused"))
	m = written(t, m, "alice@example.com", "Lunch", "Shall we?")

	m = sendKey(t, m)

	if m.sending || !m.composing() || len(s.Sent()) != 1 {
		t.Fatalf("sending %v, composing %v, sent %d", m.sending, m.composing(), len(s.Sent()))
	}
	if view := plain(m); !strings.Contains(view, "Send failed: smtp: connection refused") || !strings.Contains(view, "Lunch") {
		t.Errorf("composer should show the error and the draft:\n%s", view)
	}
	m = typed(t, m, "!") // editing resumes

	m = sendKey(t, m)

	got := s.Sent()
	if len(got) != 2 || got[0].Subject != got[1].Subject || got[1].Body != "Shall we?!" {
		t.Errorf("sent = %+v; want the draft twice, the second with the edit", got)
	}
	if m.composing() || m.status != "Sent to alice@example.com" {
		t.Errorf("composing %v, status %q", m.composing(), m.status)
	}
}

func TestCompose_AfterDeliveryClosesWithAWarningAndDoesNotRetry(t *testing.T) {
	m, s := composeFixture(t)
	s.FailWith(fmt.Errorf("sent, but couldn't save a copy to Sent: disk full: %w", mail.ErrAfterDelivery))
	m = written(t, m, "alice@example.com", "Lunch", "Shall we?")

	m = sendKey(t, m)

	if m.composing() || m.sending {
		t.Errorf("composing %v, sending %v; want everything closed", m.composing(), m.sending)
	}
	if !m.statusErr || !strings.HasPrefix(m.status, "Sent, but couldn't save a copy to Sent") {
		t.Errorf("status %q (err %v), want the capitalised warning", m.status, m.statusErr)
	}
	if len(s.Sent()) != 1 {
		t.Errorf("sent %d times", len(s.Sent()))
	}
}

func TestCompose_SendResultWithNoDraftOnlyShowsTheError(t *testing.T) {
	m, _ := composeFixture(t)

	m = send(t, m, sentMsg{err: errors.New("smtp: down")})

	if m.composing() || !m.statusErr || m.status != "smtp: down" {
		t.Errorf("composing %v, status %q (err %v)", m.composing(), m.status, m.statusErr)
	}
}

func TestCompose_EscOnAnUntouchedComposerClosesAtOnce(t *testing.T) {
	m, s := composeFixture(t)
	m, _ = keyCmd(t, m, "c")

	m, _ = keyCmd(t, m, "esc")

	if m.composing() || m.status != "" || len(s.Sent()) != 0 {
		t.Errorf("composing %v, status %q; want a silent close", m.composing(), m.status)
	}
}

func TestCompose_EscAfterTypingAsksThenDiscardOrKeepEditing(t *testing.T) {
	// Saving is off for a sender without a Drafter.
	m, f := composeFixtureWith(t, &mailtest.Sender{})
	s := f.personalSender.(*mailtest.Sender)
	m = written(t, m, "alice@example.com", "Lunch", "Shall we?")

	m, _ = keyCmd(t, m, "esc")
	if !m.composing() || !m.draft.pop.Prompting() {
		t.Fatal("esc after typing should ask")
	}
	if view := plain(m); !strings.Contains(view, "Discard draft?  d discard · e keep editing") || strings.Contains(view, " s save") {
		t.Errorf("prompt should offer only d and e:\n%s", view)
	}

	for _, k := range []string{"s", "esc"} {
		var cmd tea.Cmd
		if m, cmd = keyCmd(t, m, k); cmd != nil || !m.composing() || !m.draft.pop.Prompting() || m.saving {
			t.Fatalf("%q in a prompt without saving: cmd %v, composing %v, prompting %v", k, cmd != nil, m.composing(), m.draft.pop.Prompting())
		}
	}

	m, _ = keyCmd(t, m, "e")
	if !m.composing() || m.draft.pop.Prompting() || m.draft.pop.Values().Body != "Shall we?" {
		t.Fatalf("e should return to editing with the text intact (values %+v)", m.draft.pop.Values())
	}

	m, _ = keyCmd(t, m, "esc")
	m, _ = keyCmd(t, m, "d")
	if m.composing() || m.status != "Draft discarded" || m.statusErr || len(s.Sent()) != 0 {
		t.Errorf("composing %v, status %q (err %v)", m.composing(), m.status, m.statusErr)
	}
	if m2, cmd := keyCmd(t, m, "c"); cmd != nil || !m2.composing() {
		t.Error("c should open a new composer after a discard")
	}
}

func TestCompose_ClosingReturnsToTheListUnchanged(t *testing.T) {
	m, _ := composeFixture(t)
	m = send(t, m, press("j"))
	row, _, _ := m.list.Selected()
	focus := m.focus
	m = written(t, m, "a@example.com", "", "")

	for _, closeKeys := range [][]string{{"esc", "d"}, {"ctrl+s"}} {
		m2 := m
		for _, k := range closeKeys {
			m2, _ = keyCmd(t, m2, k)
		}
		if closeKeys[0] == "ctrl+s" {
			m2 = send(t, m2, sentMsg{draft: mail.Draft{To: []mail.Address{{Email: "a@example.com"}}}})
		}
		got, _, _ := m2.list.Selected()
		if m2.composing() || m2.readerPopup || m2.focus != focus || got.Latest().UID != row.Latest().UID {
			t.Errorf("after %v: composing %v, reader %v, focus %v, selected %v; want the list as it was", closeKeys, m2.composing(), m2.readerPopup, m2.focus, got.Latest().UID)
		}
	}
}

func TestCompose_ClosingReturnsToTheReader(t *testing.T) {
	m, _, _, u := replyFixture(t)
	m = pressSettled(t, m, "enter")
	if !m.readerPopup || m.read.Current() != u.first {
		t.Fatalf("setup: reader %v, current %d", m.readerPopup, m.read.Current())
	}
	row, _, _ := m.list.Selected()

	m, _ = keyCmd(t, m, "c")
	if !m.composing() || !m.readerPopup {
		t.Fatalf("composing %v, reader %v; the composer is an overlay", m.composing(), m.readerPopup)
	}
	m = typed(t, m, "x")
	m = send(t, m, press("esc"), press("d"))

	got, _, _ := m.list.Selected()
	if m.composing() || !m.readerPopup || m.read.Current() != u.first || got.Latest().UID != row.Latest().UID {
		t.Errorf("composing %v, reader %v, current %d, selected %v; want the reader as it was", m.composing(), m.readerPopup, m.read.Current(), got.Latest().UID)
	}
	if view := plain(m); !strings.Contains(view, "Body of o1.") || strings.Contains(view, "New message") {
		t.Errorf("view should show the reader again:\n%s", view)
	}
}

func TestCompose_ComposerIsDrawnOverTheReaderAtTheViewSize(t *testing.T) {
	m, _, _, _ := replyFixture(t)
	m = pressSettled(t, m, "enter")
	m, _ = keyCmd(t, m, "c")

	view := m.View().Content
	if w, h := lipgloss.Width(view), lipgloss.Height(view); w != 160 || h != 30 {
		t.Errorf("view is %dx%d, want 160x30", w, h)
	}
	if strings.Contains(plain(m), "Body of o1.") {
		t.Error("the reader shows through the composer")
	}
}

func TestCompose_ResizeResizesTheComposer(t *testing.T) {
	m, _ := composeFixture(t)
	m, _ = keyCmd(t, m, "c")
	before := plain(m)

	m = send(t, m, size(100, 24))

	view := m.View().Content
	if w, h := lipgloss.Width(view), lipgloss.Height(view); w != 100 || h != 24 {
		t.Errorf("view is %dx%d, want 100x24", w, h)
	}
	if after := plain(m); after == before || !strings.Contains(after, "New message · me@example.com") {
		t.Errorf("composer didn't redraw at the new size:\n%s", after)
	}
}

func TestCompose_MouseIsReleasedWhileComposing(t *testing.T) {
	m, _ := composeFixture(t)
	if m.View().MouseMode != tea.MouseModeCellMotion {
		t.Fatal("setup: the mouse should be captured on the list")
	}

	m, _ = keyCmd(t, m, "c")

	if m.View().MouseMode == tea.MouseModeCellMotion {
		t.Error("mouse still captured with the composer open")
	}
	if m2 := send(t, m, click(60, 3)); !m2.composing() || m2.readerPopup {
		t.Error("a click acted behind the composer")
	}
}

func TestCompose_NotConfiguredAccountSaysSo(t *testing.T) {
	m, _ := composeFixture(t)
	next, open := m.openFolder(1, "INBOX")
	m = settle(t, next, open)
	if m.open.account != 1 {
		t.Fatalf("open account = %d, want Work (1)", m.open.account)
	}

	m, cmd := keyCmd(t, m, "c")

	want := "Sending isn't set up for Work: add smtp_host to its config"
	if cmd != nil || m.draft != nil || m.status != want || m.statusErr {
		t.Errorf("cmd %v, draft %v, status %q (err %v)", cmd != nil, m.draft, m.status, m.statusErr)
	}
}

func TestCompose_SentStatusFormatting(t *testing.T) {
	a := func(e string) mail.Address { return mail.Address{Email: e} }
	tests := []struct {
		name string
		d    mail.Draft
		want string
	}{
		{"one", mail.Draft{To: []mail.Address{a("a@x.com")}}, "Sent to a@x.com"},
		{"cc and bcc", mail.Draft{To: []mail.Address{a("a@x.com")}, Cc: []mail.Address{a("b@x.com")}, Bcc: []mail.Address{a("c@x.com")}}, "Sent to a@x.com (+2)"},
		{"deduped case-insensitively", mail.Draft{To: []mail.Address{a("a@x.com"), a("A@X.com")}, Cc: []mail.Address{a("a@x.com")}}, "Sent to a@x.com"},
		{"bcc only", mail.Draft{Bcc: []mail.Address{a("h@x.com"), a("i@x.com")}}, "Sent to h@x.com (+1)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sentStatus(tt.d); got != tt.want {
				t.Errorf("sentStatus = %q, want %q", got, tt.want)
			}
		})
	}
}

// saveFromPrompt types a draft with no recipient but a Bcc, opens the close
// prompt and presses k, returning the model and the save command.
func saveFromPrompt(t *testing.T, m Model, k string) (Model, tea.Cmd) {
	t.Helper()
	m, _ = keyCmd(t, m, "c")
	m = send(t, m, press("tab"), press("tab"))
	m = typed(t, m, "Hidden <hidden@example.com>")
	m = send(t, m, press("tab"))
	m = typed(t, m, "Lunch")
	m = send(t, m, press("tab"))
	m = typed(t, m, "Shall we?")
	m, _ = keyCmd(t, m, "esc")
	if !m.draft.pop.Prompting() {
		t.Fatal("esc after typing should ask")
	}
	return keyCmd(t, m, k)
}

func TestCompose_PromptOffersSaveWhenTheSenderCanDraft(t *testing.T) {
	m, _ := composeFixture(t)
	m = written(t, m, "alice@example.com", "Lunch", "Shall we?")

	m, _ = keyCmd(t, m, "esc")

	if view := plain(m); !strings.Contains(view, "Save draft?  s save · d discard · e keep editing") {
		t.Errorf("prompt should offer saving:\n%s", view)
	}
}

func TestCompose_SaveKeySavesWithoutRecipientsAndClosesWithRefresh(t *testing.T) {
	m, f := composeFixtureWith(t, &mailtest.DraftSender{})
	s := f.personalSender.(*mailtest.DraftSender)
	personal, work := folderCalls(f.personal), folderCalls(f.work)

	m, cmd := saveFromPrompt(t, m, "s")

	if cmd == nil || !m.saving || !m.composing() || m.status != "Saving…" || m.draft.pop.Prompting() {
		t.Fatalf("cmd %v, saving %v, composing %v, status %q, prompting %v", cmd != nil, m.saving, m.composing(), m.status, m.draft.pop.Prompting())
	}
	if view := plain(m); !strings.Contains(view, "Saving…") {
		t.Errorf("composer should say Saving…:\n%s", view)
	}
	next, refresh := m.Update(cmd())
	m = next.(Model)

	saved := s.Saved()
	if len(saved) != 1 || len(s.Sent()) != 0 {
		t.Fatalf("saved %d, sent %d; want one save and no send", len(saved), len(s.Sent()))
	}
	d := saved[0]
	if len(d.To) != 0 || len(d.Bcc) != 1 || d.Bcc[0].Email != "hidden@example.com" || d.Subject != "Lunch" || d.Body != "Shall we?" ||
		d.From != (mail.Address{Name: "Me Myself", Email: "me@example.com"}) {
		t.Errorf("draft = %+v", d)
	}
	if m.composing() || m.saving || m.status != "Draft saved" || m.statusErr {
		t.Errorf("composing %v, saving %v, status %q (err %v)", m.composing(), m.saving, m.status, m.statusErr)
	}
	if refresh == nil {
		t.Fatal("no refresh after saving")
	}
	m = settle(t, m, refresh)
	if folderCalls(f.personal) != personal+1 || folderCalls(f.work) != work {
		t.Errorf("Folders calls: personal %d→%d, work %d→%d; want only Personal refreshed once", personal, folderCalls(f.personal), work, folderCalls(f.work))
	}
	if m.refresh.pending != 0 {
		t.Errorf("refresh pending = %d after settling", m.refresh.pending)
	}
}

func TestCompose_SecondEscSavesLikeS(t *testing.T) {
	m, f := composeFixtureWith(t, &mailtest.DraftSender{})
	s := f.personalSender.(*mailtest.DraftSender)

	m, cmd := saveFromPrompt(t, m, "esc")
	if cmd == nil || !m.saving || m.status != "Saving…" {
		t.Fatalf("cmd %v, saving %v, status %q", cmd != nil, m.saving, m.status)
	}
	m = send(t, m, cmd())

	if len(s.Saved()) != 1 || m.composing() || m.status != "Draft saved" {
		t.Errorf("saved %d, composing %v, status %q", len(s.Saved()), m.composing(), m.status)
	}
}

func TestCompose_SaveParseErrorKeepsEditing(t *testing.T) {
	m, s := composeFixture(t)
	m = written(t, m, "not an address", "Hi", "body")
	m, _ = keyCmd(t, m, "esc")

	m, cmd := keyCmd(t, m, "s")

	if cmd != nil || m.saving || !m.composing() || len(s.Saved()) != 0 {
		t.Fatalf("cmd %v, saving %v, composing %v, saved %d", cmd != nil, m.saving, m.composing(), len(s.Saved()))
	}
	if view := plain(m); !strings.Contains(view, "To: invalid address list") {
		t.Errorf("composer should show the parse error:\n%s", view)
	}
}

func TestCompose_SaveFailureKeepsTheComposerAndRetrySaves(t *testing.T) {
	m, s := composeFixture(t)
	s.FailSaveWith(errors.New("imap: connection reset"))

	m, cmd := saveFromPrompt(t, m, "s")
	m = send(t, m, cmd())

	if m.saving || !m.composing() || len(s.Saved()) != 1 {
		t.Fatalf("saving %v, composing %v, saved %d", m.saving, m.composing(), len(s.Saved()))
	}
	if view := plain(m); !strings.Contains(view, "Save failed: imap: connection reset") || !strings.Contains(view, "Lunch") {
		t.Errorf("composer should show the error and the draft:\n%s", view)
	}
	m = typed(t, m, "!") // editing resumes
	m, _ = keyCmd(t, m, "esc")
	m, cmd = keyCmd(t, m, "s")
	m = send(t, m, cmd())

	got := s.Saved()
	if len(got) != 2 || got[1].Body != "Shall we?!" || m.composing() || m.status != "Draft saved" {
		t.Errorf("saved %+v, composing %v, status %q", got, m.composing(), m.status)
	}
}

func TestCompose_SaveFailureThenDiscard(t *testing.T) {
	m, s := composeFixture(t)
	s.FailSaveWith(errors.New("imap: down"))

	m, cmd := saveFromPrompt(t, m, "s")
	m = send(t, m, cmd())
	m, _ = keyCmd(t, m, "esc")
	m, _ = keyCmd(t, m, "d")

	if m.composing() || m.status != "Draft discarded" {
		t.Errorf("composing %v, status %q", m.composing(), m.status)
	}
}

func TestCompose_DraftSavedWithFollowUpFailureClosesWithAWarning(t *testing.T) {
	m, f := composeFixtureWith(t, &mailtest.DraftSender{})
	s := f.personalSender.(*mailtest.DraftSender)
	s.FailSaveWith(fmt.Errorf("draft saved, but removing the old copy failed: gone: %w", mail.ErrDraftSaved))
	before := folderCalls(f.personal)

	m, cmd := saveFromPrompt(t, m, "s")
	next, refresh := m.Update(cmd())
	m = next.(Model)

	if m.composing() || m.saving || !m.statusErr || !strings.HasPrefix(m.status, "Draft saved, but removing the old copy failed") {
		t.Errorf("composing %v, saving %v, status %q (err %v)", m.composing(), m.saving, m.status, m.statusErr)
	}
	settle(t, m, refresh)
	if folderCalls(f.personal) != before+1 {
		t.Error("the saved copy should still be refreshed")
	}
}

func TestCompose_SaveResultWithNoDraftOnlyShowsTheError(t *testing.T) {
	m, _ := composeFixture(t)

	m = send(t, m, draftSavedMsg{err: errors.New("imap: down")})

	if m.composing() || !m.statusErr || m.status != "imap: down" {
		t.Errorf("composing %v, status %q (err %v)", m.composing(), m.status, m.statusErr)
	}
}

func TestCompose_BusySavingIgnoresKeysButCtrlCQuits(t *testing.T) {
	m, s := composeFixture(t)
	m, _ = saveFromPrompt(t, m, "s")
	before := m.draft.pop.Values()

	for _, k := range []string{"y", "esc", "s", "d", "tab", "q", "c", "ctrl+s"} {
		var cmd tea.Cmd
		m, cmd = keyCmd(t, m, k)
		if cmd != nil || !m.composing() || m.draft.pop.Values() != before {
			t.Fatalf("%q while saving: cmd %v, composing %v", k, cmd != nil, m.composing())
		}
	}
	if _, cmd := m.Update(press("ctrl+c")); cmd == nil {
		t.Error("ctrl+c should still quit while saving")
	}
	if len(s.Saved()) != 0 || len(s.Sent()) != 0 {
		t.Error("keys while busy saved or sent something")
	}
}

func TestCompose_SendingAReopenedDraftRefreshesItsAccount(t *testing.T) {
	m, f := composeFixtureWith(t, &mailtest.DraftSender{})
	m = written(t, m, "alice@example.com", "Lunch", "Shall we?")
	ref := mail.MessageRef{Folder: "Drafts", UID: 4}
	m.draft.base.DraftOf = &ref
	before := folderCalls(f.personal)

	m, cmd := keyCmd(t, m, "ctrl+s")
	next, refresh := m.Update(cmd())
	m = next.(Model)
	settle(t, m, refresh)

	if folderCalls(f.personal) != before+1 {
		t.Errorf("Folders calls %d→%d, want one refresh", before, folderCalls(f.personal))
	}
}

func TestCompose_SendingAFreshDraftDoesNotRefresh(t *testing.T) {
	m, _ := composeFixture(t)
	m = written(t, m, "alice@example.com", "Lunch", "Shall we?")

	m, cmd := keyCmd(t, m, "ctrl+s")
	_, refresh := m.Update(cmd())

	if refresh != nil {
		t.Error("a send that touched no draft started a refresh")
	}
}

func TestCompose_SaveDuringARefreshQueuesAnotherOne(t *testing.T) {
	m, f := composeFixtureWith(t, &mailtest.DraftSender{})
	m, tickCmd := m.startRefresh(false)
	if m.refresh.pending == 0 || tickCmd == nil {
		t.Fatal("setup: no refresh in flight")
	}
	m, cmd := saveFromPrompt(t, m, "s")
	next, refresh := m.Update(cmd())
	m = next.(Model)
	if refresh != nil || !m.refresh.rerun {
		t.Fatalf("refresh %v, rerun %v; want the save to queue a rerun", refresh != nil, m.refresh.rerun)
	}
	before := folderCalls(f.personal)

	m = settle(t, m, tickCmd)

	if folderCalls(f.personal) != before+2 || m.refresh.pending != 0 || m.refresh.rerun {
		t.Errorf("Folders calls %d→%d, pending %d, rerun %v; want the in-flight refresh then a rerun", before, folderCalls(f.personal), m.refresh.pending, m.refresh.rerun)
	}
}
