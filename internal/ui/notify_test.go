package ui

import (
	"errors"
	"strings"
	"testing"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"github.com/dkouvdis/dk-email/internal/mail/mailtest"
)

func TestWindowTitle_CountsInboxUnreadAcrossAccounts(t *testing.T) {
	m := started(t, newFixture(), 160, 30)

	if got := m.View().WindowTitle; got != "dk-email (3)" {
		t.Errorf("WindowTitle = %q, want %q", got, "dk-email (3)")
	}
}

func TestWindowTitle_DropsWhenMessageRead(t *testing.T) {
	m := started(t, newFixture(), 160, 30)

	m = pressSettled(t, m, "u") // marks the Plan thread read: one unread fewer

	if got := m.View().WindowTitle; got != "dk-email (2)" {
		t.Errorf("WindowTitle = %q, want %q", got, "dk-email (2)")
	}
}

func TestWindowTitle_PlainWithoutUnread(t *testing.T) {
	m := started(t, newFixture(), 160, 30)
	for i, a := range m.side.Accounts() {
		for _, f := range a.Folders {
			m.side = m.side.AdjustUnread(i, f.Name, -f.Unread)
		}
	}

	if got := m.View().WindowTitle; got != "dk-email" {
		t.Errorf("WindowTitle = %q, want %q", got, "dk-email")
	}
}

func TestWindowTitle_EmptyWhenTitleOff(t *testing.T) {
	m := started(t, newFixture(), 160, 30)
	m.notify.Title = false

	if got := m.View().WindowTitle; got != "" {
		t.Errorf("WindowTitle = %q, want empty", got)
	}
}

func TestNewMail_TickCountsMailDeliveredToEachInbox(t *testing.T) {
	f := newFixture()
	m := started(t, f, 160, 30)
	f.personal.AddMessage("INBOX", msg("Zed", "One", fixedNow, false, "z1"))
	f.personal.AddMessage("INBOX", msg("Zed", "Two", fixedNow, false, "z2"))

	m = tickSettled(t, m)

	if got := m.refresh.newMail[m.ids[0]]; got != 2 {
		t.Errorf("Personal newMail = %d, want 2", got)
	}
	if got := m.refresh.newMail[m.ids[1]]; got != 0 {
		t.Errorf("Work newMail = %d, want 0", got)
	}
}

func TestNewMail_TickWithoutNewMailCountsNothing(t *testing.T) {
	m := started(t, newFixture(), 160, 30)

	m = tickSettled(t, m)

	if len(m.refresh.newMail) != 0 {
		t.Errorf("newMail = %v, want empty", m.refresh.newMail)
	}
}

func TestNewMail_MovingIntoInboxIsNotNewMail(t *testing.T) {
	f := newFixture()
	m := started(t, f, 160, 30)
	opened, cmd := m.openFolder(0, "Receipts")
	m = settle(t, opened, cmd)

	m = pressSettled(t, m, "m")
	m = pressSettled(t, m, "enter") // the first folder: Inbox
	if c := lastCall(t, f.personal, mailtest.MethodMove); c.To != "INBOX" {
		t.Fatalf("Move to %q, want INBOX", c.To)
	}
	m = tickSettled(t, m)

	if len(m.refresh.newMail) != 0 {
		t.Errorf("newMail = %v, want empty after a move into INBOX", m.refresh.newMail)
	}
}

func TestNewMail_ReadMailDeliveredToInboxIsNotNewMail(t *testing.T) {
	f := newFixture()
	m := started(t, f, 160, 30)
	f.personal.AddMessage("INBOX", msg("Zed", "Restored", fixedNow, true, "z1"))

	m, raw := tickRaw(t, m)

	if len(m.refresh.newMail) != 0 || len(raw) != 0 {
		t.Errorf("newMail = %v, raw = %q; want no alert for read mail", m.refresh.newMail, raw)
	}
}

func TestNewMail_UnreadDeliveryCountsNextToReadOne(t *testing.T) {
	f := newFixture()
	m := started(t, f, 160, 30)
	f.personal.AddMessage("INBOX", msg("Zed", "Restored", fixedNow, true, "z1"))
	f.personal.AddMessage("INBOX", msg("Zed", "Fresh", fixedNow, false, "z2"))

	m = tickSettled(t, m)

	if got := m.refresh.newMail[m.ids[0]]; got != 1 {
		t.Errorf("Personal newMail = %d, want 1", got)
	}
}

func TestNewMail_MovingReadMessageIntoInboxDoesNotHideNewMail(t *testing.T) {
	f := newFixture()
	m := started(t, f, 160, 30)
	f.personal.AddMessage("[Gmail]/Trash", msg("Old", "Binned", fixedNow, true, "b1"))
	opened, cmd := m.openFolder(0, "[Gmail]/Trash")
	m = settle(t, opened, cmd)
	m = pressSettled(t, m, "m")
	m = pressSettled(t, m, "enter") // the first folder: Inbox
	if c := lastCall(t, f.personal, mailtest.MethodMove); c.To != "INBOX" {
		t.Fatalf("Move to %q, want INBOX", c.To)
	}
	f.personal.AddMessage("INBOX", msg("Zed", "Fresh", fixedNow, false, "z1"))

	m = tickSettled(t, m)

	if got := m.refresh.newMail[m.ids[0]]; got != 1 {
		t.Errorf("Personal newMail = %d, want 1 (the read move must not hide it)", got)
	}
}

func TestNewMail_UIDGapFromMoveIsNotNewMail(t *testing.T) {
	f := newFixture()
	m := started(t, f, 160, 30)
	f.personal.AddMessage("[Gmail]/Trash", msg("Old", "Binned", fixedNow, true, "b1"))
	f.personal.SkipUIDs("INBOX", 4) // the server leaves a gap in INBOX's UIDs
	opened, cmd := m.openFolder(0, "[Gmail]/Trash")
	m = settle(t, opened, cmd)

	m = pressSettled(t, m, "m")
	m = pressSettled(t, m, "enter") // the first folder: Inbox
	if c := lastCall(t, f.personal, mailtest.MethodMove); c.To != "INBOX" {
		t.Fatalf("Move to %q, want INBOX", c.To)
	}
	m = tickSettled(t, m)

	if len(m.refresh.newMail) != 0 {
		t.Errorf("newMail = %v, want empty after a move into INBOX that left a UID gap", m.refresh.newMail)
	}
}

// moveHeld starts a move of a Receipts message into INBOX but doesn't run it
// yet: it returns the model and the command that performs it on the server.
func moveHeld(t *testing.T, f *fixture) (Model, tea.Cmd) {
	t.Helper()
	m := started(t, f, 160, 30)
	opened, cmd := m.openFolder(0, "Receipts")
	m = settle(t, opened, cmd)
	m = pressSettled(t, m, "m")
	next, held := m.Update(press("enter"))
	if held == nil {
		t.Fatal("moving into INBOX returned no command")
	}
	return next.(Model), held
}

func TestNewMail_RefreshDuringMoveDoesNotCountTheMove(t *testing.T) {
	f := newFixture()
	m, held := moveHeld(t, f)

	m = tickSettled(t, m) // stale: the move hasn't reached the server
	if len(m.refresh.newMail) != 0 {
		t.Errorf("newMail = %v during the move, want empty", m.refresh.newMail)
	}
	m = settle(t, m, held)
	m = tickSettled(t, m)

	if len(m.refresh.newMail) != 0 {
		t.Errorf("newMail = %v after the move, want empty", m.refresh.newMail)
	}
}

func TestNewMail_MailDeliveredDuringStaleRoundIsCountedNext(t *testing.T) {
	f := newFixture()
	m, held := moveHeld(t, f)
	f.personal.AddMessage("INBOX", msg("Zed", "One", fixedNow, false, "z1"))

	m = tickSettled(t, m) // stale
	if len(m.refresh.newMail) != 0 {
		t.Errorf("newMail = %v in the stale round, want empty", m.refresh.newMail)
	}
	m = settle(t, m, held)
	m = tickSettled(t, m)

	if got := m.refresh.newMail[m.ids[0]]; got != 1 {
		t.Errorf("Personal newMail = %d, want 1", got)
	}
}

// tickRaw runs one refresh tick to completion like tickSettled and returns
// the model with every tea.RawMsg the commands produced.
func tickRaw(t *testing.T, m Model) (Model, []string) {
	t.Helper()
	next, cmd := m.Update(refreshTickMsg{})
	m = next.(Model)
	var raw []string
	queue := []tea.Cmd{cmd}
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		switch msg := c().(type) {
		case nil, spinner.TickMsg:
		case tea.BatchMsg:
			queue = append(queue, msg...)
		case tea.RawMsg:
			s, _ := msg.Msg.(string)
			raw = append(raw, s)
		default:
			next, nc := m.Update(msg)
			m = next.(Model)
			queue = append(queue, nc)
		}
	}
	return m, raw
}

func addNewMail(f *fixture) {
	f.personal.AddMessage("INBOX", msg("Zed", "One", fixedNow, false, "z1"))
	f.personal.AddMessage("INBOX", msg("Zed", "Two", fixedNow, false, "z2"))
	f.work.AddMessage("INBOX", msg("Boss", "Three", fixedNow, false, "w3"))
}

func TestAnnounce_NewMailWritesOneBellAndDesktopNotification(t *testing.T) {
	f := newFixture()
	m := started(t, f, 160, 30)
	addNewMail(f)

	_, raw := tickRaw(t, m)

	if len(raw) != 1 {
		t.Fatalf("got %d raw writes %q, want 1", len(raw), raw)
	}
	for _, want := range []string{"dk-email: 2 new in Personal, 1 in Work", "\a", "\x1b]9;"} {
		if !strings.Contains(raw[0], want) {
			t.Errorf("raw write %q missing %q", raw[0], want)
		}
	}
}

func TestAnnounce_SilentWithoutNewMail(t *testing.T) {
	m := started(t, newFixture(), 160, 30)

	if _, raw := tickRaw(t, m); len(raw) != 0 {
		t.Errorf("raw writes %q, want none", raw)
	}
}

func TestAnnounce_SilentWhenBellAndDesktopOff(t *testing.T) {
	f := newFixture()
	m := started(t, f, 160, 30)
	m.notify.Bell = false
	m.notify.Desktop = false
	addNewMail(f)

	if _, raw := tickRaw(t, m); len(raw) != 0 {
		t.Errorf("raw writes %q, want none", raw)
	}
}

func TestAnnounce_WrapsForTmux(t *testing.T) {
	f := newFixture()
	m := started(t, f, 160, 30)
	m.tmux = true
	addNewMail(f)

	_, raw := tickRaw(t, m)

	if len(raw) != 1 || !strings.HasPrefix(raw[0], "\x1bPtmux;") {
		t.Errorf("raw writes %q, want one starting with the tmux passthrough", raw)
	}
}

// focus gives the window focus.
func focus(m Model) Model {
	next, _ := m.Update(tea.FocusMsg{})
	return next.(Model)
}

func TestAnnounce_SilentWhileFocused(t *testing.T) {
	f := newFixture()
	m := focus(started(t, f, 160, 30)) // Personal's INBOX is open
	addNewMailTo(f, "personal")

	m, raw := tickRaw(t, m)
	if len(raw) != 0 {
		t.Fatalf("focused: raw writes %q, want none", raw)
	}

	next, _ := m.Update(tea.BlurMsg{})
	m = next.(Model)
	addNewMailTo(f, "personal")
	if _, raw := tickRaw(t, m); len(raw) != 1 {
		t.Errorf("blurred: got %d raw writes %q, want 1", len(raw), raw)
	}
}

func addNewMailTo(f *fixture, which string) {
	if which == "personal" {
		f.personal.AddMessage("INBOX", msg("Zed", "One", fixedNow, false, "z1"))
		f.personal.AddMessage("INBOX", msg("Zed", "Two", fixedNow, false, "z2"))
		return
	}
	f.work.AddMessage("INBOX", msg("Boss", "Three", fixedNow, false, "w3"))
}

func TestAnnounce_KeyPressCountsAsFocus(t *testing.T) {
	f := newFixture()
	m := started(t, f, 160, 30)
	addNewMailTo(f, "personal")
	m2, raw := tickRaw(t, m)
	if len(raw) != 1 {
		t.Fatalf("unfocused: got %d raw writes %q, want 1", len(raw), raw)
	}

	m2 = pressSettled(t, m2, "j")
	addNewMailTo(f, "personal")
	if _, raw := tickRaw(t, m2); len(raw) != 0 {
		t.Errorf("after key press: raw writes %q, want none", raw)
	}
}

func TestAnnounce_MailSeenWhileFocusedNeverAlertsLater(t *testing.T) {
	f := newFixture()
	m := focus(started(t, f, 160, 30))
	addNewMailTo(f, "personal")
	m, raw := tickRaw(t, m)
	if len(raw) != 0 {
		t.Fatalf("focused: raw writes %q, want none", raw)
	}

	next, _ := m.Update(tea.BlurMsg{})
	m = next.(Model)
	if _, raw := tickRaw(t, m); len(raw) != 0 {
		t.Errorf("after blur with no new mail: raw writes %q, want none", raw)
	}
}

func TestAnnounce_FocusedOtherAccountStillAlerts(t *testing.T) {
	f := newFixture()
	m := focus(started(t, f, 160, 30))
	addNewMailTo(f, "work")

	m, raw := tickRaw(t, m)

	if len(raw) != 1 || !strings.Contains(raw[0], "dk-email: 1 new in Work") || strings.Contains(raw[0], "Personal") {
		t.Fatalf("raw writes %q, want one naming only Work", raw)
	}
	if m.status != "New mail: 1 new in Work" || m.notice != m.status {
		t.Errorf("status = %q (notice %q), want the Work notice", m.status, m.notice)
	}
}

func TestAnnounce_FocusedOpenInboxOnlyIsSilentWithoutNotice(t *testing.T) {
	f := newFixture()
	m := focus(started(t, f, 160, 30))
	addNewMailTo(f, "personal")

	m, raw := tickRaw(t, m)

	if len(raw) != 0 || m.notice != "" || strings.Contains(m.status, "New mail") {
		t.Errorf("raw = %q, status = %q (notice %q); want silence", raw, m.status, m.notice)
	}
}

func TestAnnounce_FocusedNonInboxOpenStillAlerts(t *testing.T) {
	f := newFixture()
	m := started(t, f, 160, 30)
	opened, cmd := m.openFolder(0, "Receipts")
	m = focus(settle(t, opened, cmd))
	addNewMailTo(f, "personal")

	m, raw := tickRaw(t, m)

	if len(raw) != 1 || !strings.Contains(raw[0], "2 new in Personal") {
		t.Fatalf("raw writes %q, want one for Personal", raw)
	}
	if m.status != "New mail: 2 new in Personal" || m.notice != m.status {
		t.Errorf("status = %q (notice %q), want the Personal notice", m.status, m.notice)
	}
}

func TestAnnounce_BlurredOpenInboxAlertsAndNotes(t *testing.T) {
	f := newFixture()
	m := started(t, f, 160, 30)
	next, _ := m.Update(tea.BlurMsg{})
	m = next.(Model)
	addNewMailTo(f, "personal")

	m, raw := tickRaw(t, m)

	if len(raw) != 1 || m.status != "New mail: 2 new in Personal" || m.notice != m.status {
		t.Errorf("raw = %q, status = %q (notice %q); want alert and notice", raw, m.status, m.notice)
	}
}

func TestAnnounce_NoticeShownWhenBellAndDesktopOff(t *testing.T) {
	f := newFixture()
	m := started(t, f, 160, 30)
	m.notify.Bell = false
	m.notify.Desktop = false
	addNewMailTo(f, "personal")

	m, raw := tickRaw(t, m)

	if len(raw) != 0 || m.status != "New mail: 2 new in Personal" || m.notice != m.status {
		t.Errorf("raw = %q, status = %q (notice %q); want a notice only", raw, m.status, m.notice)
	}
}

func TestNotice_KeyPressClearsIt(t *testing.T) {
	m := started(t, newFixture(), 160, 30)
	m.setNotice("New mail: 1 new in Work")

	m = pressSettled(t, m, "j")

	if m.status != "" || m.notice != "" {
		t.Errorf("status = %q (notice %q), want cleared", m.status, m.notice)
	}
}

func TestNotice_NeverReplacesError(t *testing.T) {
	m := started(t, newFixture(), 160, 30)
	m.setError(errors.New("boom"))

	m.setNotice("New mail: 1 new in Work")

	if m.status != "boom" || !m.statusErr || m.notice != "" {
		t.Errorf("status = %q (err %v, notice %q), want the error kept", m.status, m.statusErr, m.notice)
	}
}

func TestNotice_SetStatusReplacesIt(t *testing.T) {
	m := started(t, newFixture(), 160, 30)
	m.setNotice("New mail: 1 new in Work")

	m.setStatus("Saved.")

	if m.status != "Saved." || m.status == m.notice {
		t.Errorf("status = %q (notice %q), want plain Saved.", m.status, m.notice)
	}
}

func TestNotice_DirectStatusAssignmentRendersPlain(t *testing.T) {
	m := started(t, newFixture(), 160, 30)
	m.setNotice("New mail: 1 new in Work")
	m.status = "Sent."

	got := m.renderStatus()
	if want := m.styles.status.Render("Sent."); !strings.Contains(got, want) {
		t.Errorf("status line %q lacks the normal style %q", got, want)
	}
}

func TestNotice_RenderedInNoticeStyle(t *testing.T) {
	m := started(t, newFixture(), 160, 30)
	m.setNotice("New mail: 1 new in Work")
	got := m.renderStatus()
	if want := m.styles.statusNote.Render(m.status); !strings.Contains(got, want) {
		t.Errorf("status line %q lacks the notice style %q", got, want)
	}
	if plain := m.styles.status.Render(m.status); strings.Contains(got, plain) {
		t.Errorf("status line %q uses the dim style", got)
	}
}

func TestView_ReportsFocusOnlyWhenAlerting(t *testing.T) {
	m := started(t, newFixture(), 160, 30)
	if !m.View().ReportFocus {
		t.Error("ReportFocus false by default, want true")
	}
	m.notify.Bell = false
	m.notify.Desktop = false
	if m.View().ReportFocus {
		t.Error("ReportFocus true with bell and desktop off, want false")
	}
}
