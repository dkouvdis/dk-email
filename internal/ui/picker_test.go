package ui

import (
	"errors"
	"strings"
	"testing"
)

func TestPicker_NarrowAOpensPickerWithEveryAccount(t *testing.T) {
	m := started(t, newFixture(), 100, 30)

	m = pressSettled(t, m, "a")

	if !m.showPicker {
		t.Fatal("a didn't open the picker")
	}
	v := plain(m)
	for _, want := range []string{"Go to folder", "Personal", "Receipts", "Work", "Archive", "▸ Inbox"} {
		if !strings.Contains(v, want) {
			t.Errorf("picker missing %q:\n%s", want, v)
		}
	}
}

func TestPicker_ChoosingAnotherAccountSwitchesAndLoads(t *testing.T) {
	f := newFixture()
	m := started(t, f, 100, 30)

	// Personal: Inbox, Sent Mail, All Mail, Receipts; then Work: Inbox.
	for _, k := range []string{"a", "j", "j", "j", "j", "enter"} {
		m = pressSettled(t, m, k)
	}

	if m.showPicker {
		t.Error("picker still open after enter")
	}
	if m.open.account != 1 || m.open.folder != "INBOX" || m.list.Count() != 1 {
		t.Fatalf("open = %+v with %d messages, want Work's Inbox", m.open, m.list.Count())
	}
	if !strings.Contains(plain(m), "Work › Inbox · 1 unread") {
		t.Errorf("header doesn't match the new selection:\n%s", plain(m))
	}
	if got := summaryCalls(f.work); len(got) != 1 || got[0] != "INBOX" {
		t.Errorf("Work Summaries calls = %v", got)
	}
}

func TestPicker_EscAndReselectChangeNothing(t *testing.T) {
	f := newFixture()
	m := started(t, f, 100, 30)

	m = pressSettled(t, m, "a")
	m = pressSettled(t, m, "j")
	m = pressSettled(t, m, "esc")
	if m.showPicker || m.open.folder != "INBOX" || m.open.account != 0 {
		t.Errorf("esc changed the selection: open %+v, picker %v", m.open, m.showPicker)
	}

	m = pressSettled(t, m, "a")
	m = pressSettled(t, m, "enter") // the current folder
	if got := summaryCalls(f.personal); len(got) != 1 {
		t.Errorf("choosing the open folder reloaded it: %v", got)
	}
}

func TestPicker_KeysDontLeakAndQuitWorks(t *testing.T) {
	m := started(t, newFixture(), 100, 30)
	m = pressSettled(t, m, "a")

	m = pressSettled(t, m, "tab")
	if m.focus != PaneList {
		t.Errorf("tab moved focus to %v while the picker was open", m.focus)
	}
	if _, cmd := m.Update(press("q")); cmd == nil {
		t.Error("q didn't quit with the picker open")
	}
}

func TestPicker_WideModeFocusesSidebar(t *testing.T) {
	m := started(t, newFixture(), 160, 30)

	m = pressSettled(t, m, "a")

	if m.showPicker || m.focus != PaneSidebar {
		t.Errorf("wide a: picker %v focus %v, want the sidebar focused", m.showPicker, m.focus)
	}
}

func TestPicker_ShowsWhyAnAccountHasNoFolders(t *testing.T) {
	f := newFixture()
	f.errs["Work"] = errors.New("login failed\nHint: long advice")
	m := started(t, f, 100, 30)

	m = pressSettled(t, m, "a")

	if v := plain(m); !strings.Contains(v, "login failed") || strings.Contains(v, "long advice") {
		t.Errorf("picker should show the error's first line only:\n%s", v)
	}
}

func TestHeader_UnreadCountFollowsReading(t *testing.T) {
	f := newFixture()
	m := started(t, f, 100, 30)
	if !strings.Contains(plain(m), "Personal › Inbox · 2 unread") {
		t.Fatalf("header lacks the unread count:\n%s", plain(m))
	}

	m = pressSettled(t, m, "enter")

	if !strings.Contains(plain(m), "Personal › Inbox · 1 unread") {
		t.Errorf("header count didn't follow reading:\n%s", plain(m))
	}
}
