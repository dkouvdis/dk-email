package ui

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/dkouvdis/dk-email/internal/mail"
	"github.com/dkouvdis/dk-email/internal/mail/mailtest"
)

const goodDraft = "To: Alice <alice@example.com>\nCc: bob@example.com\nBcc:\nSubject: Lunch\n\nShall we?\nSecond line.\n"

// composeFixture is a started model where Personal can send through the
// returned sender and Work can't.
func composeFixture(t *testing.T) (Model, *mailtest.Sender) {
	t.Helper()
	t.Setenv("TMPDIR", t.TempDir())
	s := &mailtest.Sender{}
	f := newFixture()
	f.personalSender = s
	m := started(t, f, 160, 30)
	m.accts[0].Email, m.accts[0].FromName = "me@example.com", "Me Myself"
	m.editorCmd = func(path string) *exec.Cmd { return exec.Command("true", path) }
	return m, s
}

// keyCmd presses k and returns the model and the command it produced.
func keyCmd(t *testing.T, m Model, k string) (Model, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(press(k))
	return next.(Model), cmd
}

// edited is c followed by the editor returning text.
func edited(t *testing.T, m Model, text string) Model {
	t.Helper()
	m, cmd := keyCmd(t, m, "c")
	if cmd == nil || m.draft == nil {
		t.Fatalf("c didn't start an edit (cmd %v, draft %v, status %q)", cmd != nil, m.draft, m.status)
	}
	return send(t, m, editorDoneMsg{text: text, orig: m.draft.orig})
}

// yes presses y and delivers the send result back to the model.
func yes(t *testing.T, m Model) Model {
	t.Helper()
	m, cmd := keyCmd(t, m, "y")
	if cmd == nil {
		t.Fatalf("y started nothing (status %q)", m.status)
	}
	return send(t, m, cmd())
}

func TestCompose_UnchangedFileIsDiscardedSilently(t *testing.T) {
	m, s := composeFixture(t)
	m, _ = keyCmd(t, m, "c")

	m = send(t, m, editorDoneMsg{text: m.draft.orig, orig: m.draft.orig})

	if m.showConfirm || m.draft != nil || m.status != "Draft discarded" || len(s.Sent()) != 0 {
		t.Errorf("popup %v, draft %v, status %q, sent %d; want a silent discard", m.showConfirm, m.draft, m.status, len(s.Sent()))
	}
	if strings.Contains(plain(m), "Send message?") {
		t.Error("confirm popup drawn")
	}
}

func TestCompose_EmptyFileIsDiscarded(t *testing.T) {
	m, s := composeFixture(t)

	m = edited(t, m, " \n\t\n")

	if m.showConfirm || m.draft != nil || m.status != "Draft discarded" || len(s.Sent()) != 0 {
		t.Errorf("popup %v, draft %v, status %q; want a discard", m.showConfirm, m.draft, m.status)
	}
}

func TestCompose_TemplateHasAccountFromAndIsSentFromIt(t *testing.T) {
	m, s := composeFixture(t)

	m = edited(t, m, goodDraft)
	if !m.showConfirm {
		t.Fatalf("no confirm popup; status %q", m.status)
	}
	view := plain(m)
	for _, want := range []string{"Send message?", "Me Myself <me@example.com>", "Alice <alice@example.com>", "bob@example.com", "Lunch", "Shall we?", "Second line.", "y send · e edit · d discard"} {
		if !strings.Contains(view, want) {
			t.Errorf("popup missing %q:\n%s", want, view)
		}
	}
	m = yes(t, m)

	sent := s.Sent()
	if len(sent) != 1 {
		t.Fatalf("sent %d drafts, want 1", len(sent))
	}
	d := sent[0]
	if d.From != (mail.Address{Name: "Me Myself", Email: "me@example.com"}) ||
		len(d.To) != 1 || d.To[0].Email != "alice@example.com" ||
		len(d.Cc) != 1 || d.Cc[0].Email != "bob@example.com" ||
		d.Subject != "Lunch" || d.Body != "Shall we?\nSecond line.\n" {
		t.Errorf("draft = %+v", d)
	}
	if m.draft != nil || m.sending || m.showConfirm || m.status != "Sent to alice@example.com (+1)" || m.statusErr {
		t.Errorf("draft %v, sending %v, popup %v, status %q (err %v)", m.draft, m.sending, m.showConfirm, m.status, m.statusErr)
	}
}

func TestCompose_PopupKeepsTheViewTheSizeOfTheTerminal(t *testing.T) {
	m, _ := composeFixture(t)
	m = edited(t, m, goodDraft)

	view := m.View().Content
	if w, h := lipgloss.Width(view), lipgloss.Height(view); w != 160 || h != 30 {
		t.Errorf("view is %dx%d, want 160x30", w, h)
	}
}

func TestCompose_SendingStateWhileInFlight(t *testing.T) {
	m, _ := composeFixture(t)
	m = edited(t, m, goodDraft)

	m, cmd := keyCmd(t, m, "y")

	if !m.sending || m.showConfirm || m.status != "Sending…" || cmd == nil {
		t.Errorf("sending %v, popup %v, status %q", m.sending, m.showConfirm, m.status)
	}
	m, qcmd := keyCmd(t, m, "q")
	if qcmd != nil || m.status != "Sending…" {
		t.Errorf("q while sending: quit cmd %v, status %q; want blocked", qcmd != nil, m.status)
	}
	m, ccmd := keyCmd(t, m, "c")
	if ccmd != nil || m.status != "Sending…" || m.editorPath != "" {
		t.Errorf("c while sending: cmd %v, status %q", ccmd != nil, m.status)
	}
	if _, cmd := m.Update(press("ctrl+c")); cmd == nil {
		t.Error("ctrl+c should still quit while sending")
	}
}

func TestCompose_EditReopensEditorWithCurrentText(t *testing.T) {
	m, _ := composeFixture(t)
	m = edited(t, m, goodDraft)

	m, cmd := keyCmd(t, m, "e")

	if cmd == nil || m.editorPath == "" || m.showConfirm {
		t.Fatalf("e: cmd %v, editor %q, popup %v; want the editor open", cmd != nil, m.editorPath, m.showConfirm)
	}
	data, err := os.ReadFile(m.editorPath)
	if err != nil || string(data) != goodDraft {
		t.Errorf("editor file = %q, %v; want the current text", data, err)
	}
	// Leaving it unchanged still returns to the popup.
	m = send(t, m, editorDoneMsg{text: goodDraft, orig: goodDraft})
	if !m.showConfirm || m.draft == nil {
		t.Errorf("popup %v, draft %v; want the popup back", m.showConfirm, m.draft)
	}
	_ = m.Close()
}

func TestCompose_ReeditedTextIsWhatGetsSent(t *testing.T) {
	m, s := composeFixture(t)
	m = edited(t, m, goodDraft)
	m, _ = keyCmd(t, m, "e")
	m = send(t, m, editorDoneMsg{text: "To: z@example.com\nSubject: Changed\n\nnew body", orig: goodDraft})

	m = yes(t, m)

	got := s.Sent()
	if len(got) != 1 || got[0].To[0].Email != "z@example.com" || got[0].Subject != "Changed" || got[0].Body != "new body" {
		t.Errorf("sent = %+v", got)
	}
}

func TestCompose_ReeditEmptiedFileDiscards(t *testing.T) {
	m, _ := composeFixture(t)
	m = edited(t, m, goodDraft)
	m, _ = keyCmd(t, m, "e")

	m = send(t, m, editorDoneMsg{text: "\n", orig: goodDraft})

	if m.draft != nil || m.showConfirm || m.status != "Draft discarded" {
		t.Errorf("draft %v, popup %v, status %q", m.draft, m.showConfirm, m.status)
	}
}

func TestCompose_EditorErrorOnFirstEditDiscards(t *testing.T) {
	m, _ := composeFixture(t)
	m, _ = keyCmd(t, m, "c")

	m = send(t, m, editorDoneMsg{err: errors.New("editor failed: exit status 1")})

	if m.draft != nil || m.showConfirm || !m.statusErr || !strings.Contains(m.status, "editor failed") {
		t.Errorf("draft %v, popup %v, status %q (err %v)", m.draft, m.showConfirm, m.status, m.statusErr)
	}
}

func TestCompose_EditorErrorOnReeditKeepsTextAndReopensPopup(t *testing.T) {
	m, s := composeFixture(t)
	m = edited(t, m, goodDraft)
	m, _ = keyCmd(t, m, "e")

	m = send(t, m, editorDoneMsg{err: errors.New("editor failed: exit status 1")})

	if m.draft == nil || m.draft.text != goodDraft || !m.showConfirm || !m.statusErr {
		t.Fatalf("draft %v, popup %v, status %q (err %v)", m.draft, m.showConfirm, m.status, m.statusErr)
	}
	m = yes(t, m)
	if got := s.Sent(); len(got) != 1 || got[0].Subject != "Lunch" {
		t.Errorf("sent = %+v, want the text from before the failed edit", got)
	}
}

func TestCompose_DiscardDropsTheDraft(t *testing.T) {
	m, s := composeFixture(t)
	m = edited(t, m, goodDraft)

	m, _ = keyCmd(t, m, "d")

	if m.draft != nil || m.showConfirm || m.status != "Draft discarded" || len(s.Sent()) != 0 {
		t.Errorf("draft %v, popup %v, status %q", m.draft, m.showConfirm, m.status)
	}
	if _, cmd := keyCmd(t, m, "c"); cmd == nil {
		t.Error("c should work again after a discard")
	}
}

func TestCompose_EscAndQKeepThePopup(t *testing.T) {
	m, s := composeFixture(t)
	m = edited(t, m, goodDraft)

	for _, k := range []string{"esc", "q", "x", "enter", "c", "?", "a"} {
		var cmd tea.Cmd
		m, cmd = keyCmd(t, m, k)
		if cmd != nil || !m.showConfirm || m.draft == nil || m.showHelp || m.showPicker {
			t.Fatalf("%q: cmd %v, popup %v, draft %v; the popup should swallow it", k, cmd != nil, m.showConfirm, m.draft)
		}
	}
	if len(s.Sent()) != 0 {
		t.Error("something was sent")
	}
	if _, cmd := m.Update(press("ctrl+c")); cmd == nil {
		t.Error("ctrl+c should quit even with the popup open")
	}
}

func TestCompose_QIsBlockedWhileADraftIsOpen(t *testing.T) {
	m, _ := composeFixture(t)
	m = edited(t, m, goodDraft)
	m, _ = keyCmd(t, m, "e") // popup closed, editor "running"
	if m.draft == nil || m.showConfirm {
		t.Fatal("setup: want a draft with no popup")
	}

	m, cmd := keyCmd(t, m, "q")

	if cmd != nil || m.status != "Finish or discard the draft first" {
		t.Errorf("quit cmd %v, status %q; want q blocked", cmd != nil, m.status)
	}
	_ = m.Close()
}

func TestCompose_ParseErrorDisablesSend(t *testing.T) {
	m, s := composeFixture(t)
	m = edited(t, m, "Too: a@example.com\nSubject: x\n\nbody")
	if !m.showConfirm {
		t.Fatal("no popup")
	}
	view := plain(m)
	if !strings.Contains(view, `unknown header "Too"`) || !strings.Contains(view, "e edit · d discard") || strings.Contains(view, "y send") {
		t.Errorf("popup should show the error and no y:\n%s", view)
	}

	m, cmd := keyCmd(t, m, "y")

	if cmd != nil || m.sending || !m.showConfirm || len(s.Sent()) != 0 {
		t.Errorf("y with a parse error: cmd %v, sending %v, popup %v", cmd != nil, m.sending, m.showConfirm)
	}
	// e fixes it.
	m, _ = keyCmd(t, m, "e")
	m = send(t, m, editorDoneMsg{text: goodDraft, orig: "x"})
	m = yes(t, m)
	if len(s.Sent()) != 1 {
		t.Errorf("sent %d after fixing, want 1", len(s.Sent()))
	}
}

func TestCompose_FailureReopensPopupAndRetrySendsOnce(t *testing.T) {
	m, s := composeFixture(t)
	s.FailWith(errors.New("smtp: connection refused"))
	m = edited(t, m, goodDraft)

	m = yes(t, m)

	if m.sending || m.draft == nil || !m.showConfirm || len(s.Sent()) != 1 {
		t.Fatalf("sending %v, draft %v, popup %v, sent %d", m.sending, m.draft, m.showConfirm, len(s.Sent()))
	}
	if view := plain(m); !strings.Contains(view, "Send failed: smtp: connection refused") || !strings.Contains(view, "Lunch") {
		t.Errorf("popup should show the error and the draft:\n%s", view)
	}

	m = yes(t, m)

	got := s.Sent()
	if len(got) != 2 || got[0].Subject != got[1].Subject || got[1].To[0].Email != "alice@example.com" {
		t.Errorf("sent = %+v; want the same draft twice", got)
	}
	if m.draft != nil || m.showConfirm || m.status != "Sent to alice@example.com (+1)" {
		t.Errorf("draft %v, popup %v, status %q", m.draft, m.showConfirm, m.status)
	}
}

func TestCompose_AfterDeliveryWarnsAndDoesNotRetry(t *testing.T) {
	m, s := composeFixture(t)
	s.FailWith(fmt.Errorf("sent, but couldn't save a copy to Sent: disk full: %w", mail.ErrAfterDelivery))
	m = edited(t, m, goodDraft)

	m = yes(t, m)

	if m.draft != nil || m.showConfirm || m.sending {
		t.Errorf("draft %v, popup %v, sending %v; want everything closed", m.draft, m.showConfirm, m.sending)
	}
	if !m.statusErr || !strings.HasPrefix(m.status, "Sent, but couldn't save a copy to Sent") {
		t.Errorf("status %q (err %v), want the capitalised warning", m.status, m.statusErr)
	}
	if len(s.Sent()) != 1 {
		t.Errorf("sent %d times", len(s.Sent()))
	}
	if _, cmd := keyCmd(t, m, "y"); cmd != nil {
		t.Error("y after delivery started another send")
	}
}

func TestCompose_CBlockedDuringADraft(t *testing.T) {
	m, _ := composeFixture(t)
	m = edited(t, m, goodDraft)
	m, _ = keyCmd(t, m, "e") // draft open, no popup

	m, cmd := keyCmd(t, m, "c")

	if cmd != nil || m.status != "Finish or discard the draft first" {
		t.Errorf("cmd %v, status %q", cmd != nil, m.status)
	}
	_ = m.Close()
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
	if cmd != nil || m.draft != nil || m.editorPath != "" || m.status != want || m.statusErr {
		t.Errorf("cmd %v, draft %v, editor %q, status %q (err %v)", cmd != nil, m.draft, m.editorPath, m.status, m.statusErr)
	}
}

func TestCompose_CtrlRStillRefreshes(t *testing.T) {
	m, _ := composeFixture(t)
	m = edited(t, m, goodDraft)
	m, _ = keyCmd(t, m, "e")

	m, cmd := keyCmd(t, m, "ctrl+r")

	if cmd == nil {
		t.Errorf("ctrl+r with a draft open did nothing; status %q", m.status)
	}
	_ = m.Close()
}

func TestCompose_RealStubEditorRoundTrip(t *testing.T) {
	m, s := composeFixture(t)
	m.editorCmd = func(path string) *exec.Cmd {
		return exec.Command("sh", "-c", `printf 'To: x@example.com\nSubject: Hi\n\nfrom the stub\n' > "$1"`, "--", path)
	}
	m, _ = keyCmd(t, m, "c")
	path := m.editorPath
	if err := m.editorCmd(path).Run(); err != nil {
		t.Fatal(err)
	}

	m = send(t, m, finishEditor(path, m.draft.orig, nil))
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("draft file left behind (stat err %v)", err)
	}
	m = yes(t, m)

	got := s.Sent()
	if len(got) != 1 || got[0].To[0].Email != "x@example.com" || got[0].Body != "from the stub\n" {
		t.Errorf("sent = %+v", got)
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
