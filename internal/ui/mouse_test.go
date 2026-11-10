package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func click(x, y int) tea.MouseClickMsg { return tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft} }

func wheel(x, y int, down bool) tea.MouseWheelMsg {
	b := tea.MouseWheelUp
	if down {
		b = tea.MouseWheelDown
	}
	return tea.MouseWheelMsg{X: x, Y: y, Button: b}
}

func sendSettled(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()
	next, cmd := m.Update(msg)
	return settle(t, next.(Model), cmd)
}

// At 160×30 the sidebar's inner rows start at y=1: Personal (1), Inbox (2),
// Sent Mail, All Mail, Trash, Receipts (6), blank, Work (8), Inbox (9). The
// thread list starts at x=24 with its header at y=1 and threads from y=2.
func TestMouse_ClickFolderOpensIt(t *testing.T) {
	f := newFixture()
	m := started(t, f, 160, 30)

	m = sendSettled(t, m, click(5, 1)) // the Personal header
	if m.open.folder != "INBOX" || m.open.account != 0 {
		t.Fatalf("clicking an account name opened %d/%q", m.open.account, m.open.folder)
	}

	m = sendSettled(t, m, click(5, 6))
	if m.open.account != 0 || m.open.folder != "Receipts" || m.focus != PaneSidebar {
		t.Errorf("open %d/%q focus %v, want Personal's Receipts opened from the sidebar", m.open.account, m.open.folder, m.focus)
	}
	m = sendSettled(t, m, click(5, 9))
	if m.open.account != 1 || m.open.folder != "INBOX" {
		t.Errorf("open %d/%q, want Work's Inbox", m.open.account, m.open.folder)
	}
}

func TestMouse_ClickThreadSelectsThenOpens(t *testing.T) {
	m := started(t, newFixture(), 160, 30)
	m = pressSettled(t, m, "tab") // focus the sidebar, to see the click move it

	m = sendSettled(t, m, click(60, 3))
	th, _, _ := m.list.Selected()
	if th.Latest().Subject != "Lunch?" || m.readerPopup || m.focus != PaneList {
		t.Fatalf("first click: selected %q popup %v focus %v, want Lunch? selected and nothing opened", th.Latest().Subject, m.readerPopup, m.focus)
	}

	m = sendSettled(t, m, click(60, 3))
	if !m.readerPopup || !m.read.IsOpen() {
		t.Error("second click on the selected thread didn't open it")
	}

	m = sendSettled(t, m, click(60, 2))
	if th, _, _ := m.list.Selected(); th.Latest().Subject != "Lunch?" {
		t.Error("a click behind the reader popup changed the selection")
	}
}

func TestMouse_ClickInListModeAndBelowTheRows(t *testing.T) {
	m := started(t, newFixture(), 70, 24)

	m = sendSettled(t, m, click(10, 3))
	if th, _, _ := m.list.Selected(); th.Latest().Subject != "Lunch?" {
		t.Errorf("list-mode click selected %q, want Lunch?", th.Latest().Subject)
	}
	m = sendSettled(t, m, click(10, 15))
	if th, _, _ := m.list.Selected(); th.Latest().Subject != "Lunch?" || m.readerPopup {
		t.Error("clicking below the last thread changed something")
	}
}

func TestMouse_WheelScrollsButNeverOpens(t *testing.T) {
	m := started(t, newFixture(), 160, 30)

	m = sendSettled(t, m, wheel(60, 5, true))
	if th, _, _ := m.list.Selected(); th.Latest().Subject != "Lunch?" || m.readerPopup {
		t.Errorf("wheel down over the list: selected %q popup %v", th.Latest().Subject, m.readerPopup)
	}

	m = sendSettled(t, m, wheel(5, 5, true))
	if _, f := m.side.Cursor(); f != "[Gmail]/Sent Mail" || m.open.folder != "INBOX" || m.focus != PaneSidebar {
		t.Errorf("wheel over the sidebar: cursor %q open %q focus %v, want the cursor moved and nothing opened", f, m.open.folder, m.focus)
	}

	m = pressSettled(t, m, "l")
	m = pressSettled(t, m, "enter")
	m = sendSettled(t, m, wheel(60, 5, true))
	if th, _, _ := m.list.Selected(); !m.readerPopup || th.Latest().Subject != "Lunch?" {
		t.Error("the wheel with the reader popup open should scroll the popup, not the list")
	}
}

func TestMouse_Enabled(t *testing.T) {
	m := started(t, newFixture(), 160, 30)
	if m.View().MouseMode != tea.MouseModeCellMotion {
		t.Errorf("MouseMode = %v, want cell motion", m.View().MouseMode)
	}
}
