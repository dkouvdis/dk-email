package ui

import (
	"testing"

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
