package ui

import "testing"

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
