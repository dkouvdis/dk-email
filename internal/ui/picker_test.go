package ui

import (
	tea "charm.land/bubbletea/v2"
	"errors"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
	"time"
)

func TestPicker_ListModeAOpensPickerWithEveryAccount(t *testing.T) {
	m := started(t, newFixture(), 70, 30)

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
	m := started(t, f, 70, 30)

	// Personal: Inbox, Sent Mail, All Mail, Trash, Receipts; then Work: Inbox.
	for _, k := range []string{"a", "j", "j", "j", "j", "j", "enter"} {
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
	m := started(t, f, 70, 30)

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

func TestPicker_KeysDontLeakAndQClosesIt(t *testing.T) {
	m := started(t, newFixture(), 70, 30)
	m = pressSettled(t, m, "a")

	m = pressSettled(t, m, "tab")
	if m.focus != PaneList {
		t.Errorf("tab moved focus to %v while the picker was open", m.focus)
	}
	m = pressSettled(t, m, "q")
	if m.showPicker {
		t.Error("q didn't close the picker")
	}
}

func TestPicker_NarrowModeFocusesSidebar(t *testing.T) {
	m := started(t, newFixture(), 160, 30)

	m = pressSettled(t, m, "a")
	if m.showPicker || m.focus != PaneSidebar {
		t.Errorf("narrow a: picker %v focus %v, want the sidebar focused", m.showPicker, m.focus)
	}

	m = pressSettled(t, m, "l")
	m = pressSettled(t, m, "enter")
	m = pressSettled(t, m, "a")
	if m.readerPopup || m.showPicker || m.focus != PaneSidebar {
		t.Errorf("a while reading: popup %v picker %v focus %v, want the popup closed and the sidebar focused", m.readerPopup, m.showPicker, m.focus)
	}
}

func TestPicker_ShowsWhyAnAccountHasNoFolders(t *testing.T) {
	f := newFixture()
	f.errs["Work"] = errors.New("login failed\nHint: long advice")
	m := started(t, f, 70, 30)

	m = pressSettled(t, m, "a")

	if v := plain(m); !strings.Contains(v, "login failed") || strings.Contains(v, "long advice") {
		t.Errorf("picker should show the error's first line only:\n%s", v)
	}
}

func TestHeader_UnreadCountFollowsReading(t *testing.T) {
	f := newFixture()
	m := started(t, f, 70, 30)
	if !strings.Contains(plain(m), "Personal › Inbox · 2 unread") {
		t.Fatalf("header lacks the unread count:\n%s", plain(m))
	}

	m = pressSettled(t, m, "enter")
	m = pressSettled(t, m, "esc")

	if !strings.Contains(plain(m), "Personal › Inbox · 1 unread") {
		t.Errorf("header count didn't follow reading:\n%s", plain(m))
	}
}

func TestHeader_TrashShowsNoUnreadCount(t *testing.T) {
	f := newFixture()
	f.personal.AddMessage("[Gmail]/Trash", msg("Spam", "Junk", fixedNow.Add(-time.Hour), false, "t1"))
	m := started(t, f, 70, 30)

	m, cmd := m.openFolder(0, "[Gmail]/Trash")
	m = settle(t, m, cmd)

	v := plain(m)
	if !strings.Contains(v, "Personal › Trash") || strings.Contains(v, " unread") {
		t.Errorf("Trash header should name the folder without a count:\n%s", v)
	}
}

func TestFolderFilter_SidebarTakesEveryKeyAndOpensTheMatch(t *testing.T) {
	f := newFixture()
	m := started(t, f, 160, 30)

	for _, k := range []string{"a", "/", "q", "v", "r", "?"} {
		m = pressSettled(t, m, k)
	}
	if !m.side.Filtering() || m.showHelp || m.mode != ModeSplit {
		t.Fatalf("global keys leaked out of the sidebar filter: filtering %v help %v mode %v", m.side.Filtering(), m.showHelp, m.mode)
	}
	if !strings.Contains(plain(m), "/ qvr?") {
		t.Errorf("filter didn't get the typed keys:\n%s", plain(m))
	}

	m = send(t, m, press("esc"), press("/"), tea.PasteMsg{Content: "rece"})
	m = pressSettled(t, m, "enter")

	if m.open.folder != "Receipts" || m.side.Filtering() {
		t.Errorf("open %q, filtering %v; want Receipts opened and the filter cleared", m.open.folder, m.side.Filtering())
	}
}

func TestFolderFilter_PickerTakesEveryKey(t *testing.T) {
	f := newFixture()
	m := started(t, f, 70, 30)

	m = pressSettled(t, m, "a")
	m = pressSettled(t, m, "/")
	m = send(t, m, tea.PasteMsg{Content: "inb"})
	m = pressSettled(t, m, "q")
	if !m.showPicker || !m.picker.Filtering() {
		t.Fatalf("q closed the picker while filtering (open %v)", m.showPicker)
	}
	if !strings.Contains(plain(m), "No folders match") {
		t.Errorf("typed q didn't reach the filter:\n%s", plain(m))
	}

	m = pressSettled(t, m, "esc")
	m = pressSettled(t, m, "/")
	for _, k := range []string{"r", "e", "c"} {
		m = pressSettled(t, m, k)
	}
	m = pressSettled(t, m, "enter")
	if m.showPicker || m.open.folder != "Receipts" {
		t.Errorf("picker %v open %q, want Receipts opened", m.showPicker, m.open.folder)
	}
}

func TestFolderFilter_KeyIsInHelpAndSidebarHints(t *testing.T) {
	m := started(t, newFixture(), 160, 30)

	m = pressSettled(t, m, "a")
	if h := ansi.Strip(m.renderHints()); !strings.Contains(h, "Filter: /") {
		t.Errorf("sidebar hints = %q, want Filter: /", h)
	}
	for _, k := range []string{"?", "/", "f", "o", "l", "d"} {
		m = pressSettled(t, m, k)
	}
	if !strings.Contains(plain(m), "filter folders") {
		t.Errorf("help popup doesn't list the folder filter:\n%s", plain(m))
	}
}
