package ui

import (
	"strings"
	"testing"

	"github.com/dkouvdis/dk-email/internal/mail"
)

func TestListMode_EnterOpensReaderPopup(t *testing.T) {
	m := started(t, newFixture(), 70, 24)

	m = pressSettled(t, m, "enter")

	if !m.readerPopup || m.focus != PaneList {
		t.Fatalf("popup %v focus %v, want the reader popup over the list", m.readerPopup, m.focus)
	}
	v := plain(m)
	for _, want := range []string{"From: Bob", "Body of Re: Plan from Bob.", "esc back"} {
		if !strings.Contains(v, want) {
			t.Errorf("popup view missing %q:\n%s", want, v)
		}
	}
}

func TestListMode_PopupKeysGoToReader(t *testing.T) {
	m := started(t, newFixture(), 70, 24)
	m = pressSettled(t, m, "enter")
	listCursor, _ := m.list.CursorUID()

	m = pressSettled(t, m, "space")
	if !strings.Contains(plain(m), "Body of Plan from Alice.") {
		t.Errorf("space in the popup didn't expand the conversation:\n%s", plain(m))
	}
	m = pressSettled(t, m, "j")
	if uid, _ := m.list.CursorUID(); uid != listCursor {
		t.Error("j in the popup moved the list cursor")
	}

	m = pressSettled(t, m, "?")
	if !m.showHelp || !m.readerPopup {
		t.Fatal("? should open help over the reader popup")
	}
	m = pressSettled(t, m, "esc")
	if m.showHelp || !m.readerPopup {
		t.Error("esc should close help first and keep the reader popup")
	}
	m = pressSettled(t, m, "esc")
	if m.readerPopup {
		t.Error("second esc didn't close the reader popup")
	}
	if uid, _ := m.list.CursorUID(); uid != listCursor {
		t.Error("closing the popup moved the list cursor")
	}
}

func TestListMode_ReaderPopupStaysOpenAcrossResize(t *testing.T) {
	m := started(t, newFixture(), 130, 30)
	m = pressSettled(t, m, "enter")
	if !m.readerPopup {
		t.Fatal("enter in narrow mode didn't open the reader popup")
	}

	for _, w := range []int{70, 130} {
		m = send(t, m, size(w, 30))
		if !m.readerPopup || !strings.Contains(plain(m), "Body of Re: Plan from Bob.") {
			t.Errorf("%d cols: the reader popup didn't stay open:\n%s", w, plain(m))
		}
	}
}

func TestListMode_ResizeKeepsChosenModeAndCursor(t *testing.T) {
	tests := []struct {
		name string
		keys []string
		want Kind
	}{
		{"split chosen", nil, KindNarrow},
		{"list chosen", []string{"v"}, KindList},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := started(t, newFixture(), 130, 30)
			m = pressSettled(t, m, "j") // cursor on the Lunch thread
			for _, k := range tt.keys {
				m = pressSettled(t, m, k)
			}
			before, _ := m.list.CursorUID()

			m = send(t, m, size(70, 30))
			if m.panes.Kind != KindList {
				t.Fatalf("70 cols: kind %v, want list", m.panes.Kind)
			}
			m = pressSettled(t, m, "v") // ignored below 80 columns
			m = send(t, m, size(130, 30))

			if m.panes.Kind != tt.want {
				t.Errorf("back at 130 cols: kind %v, want %v", m.panes.Kind, tt.want)
			}
			if after, _ := m.list.CursorUID(); after != before || after == mail.UID(0) {
				t.Errorf("cursor moved from %d to %d", before, after)
			}
		})
	}
}

func TestListMode_OpeningAnotherFolderClosesPopup(t *testing.T) {
	m := started(t, newFixture(), 70, 24)
	m = pressSettled(t, m, "enter")

	m = pressSettled(t, m, "a")
	for _, k := range []string{"j", "j", "j", "j", "enter"} { // Personal › Receipts
		m = pressSettled(t, m, k)
	}

	if m.readerPopup || m.read.IsOpen() || m.open.folder != "Receipts" {
		t.Errorf("popup %v, reader open %v, folder %q; want Receipts with no popup", m.readerPopup, m.read.IsOpen(), m.open.folder)
	}
}

func TestListMode_QClosesPopupsTopFirst(t *testing.T) {
	m := started(t, newFixture(), 70, 24)
	m = pressSettled(t, m, "enter")
	m = pressSettled(t, m, "?")

	next, cmd := m.Update(press("q"))
	m = next.(Model)
	if cmd != nil || m.showHelp || !m.readerPopup {
		t.Fatalf("first q: help %v reader %v quit %v; want only help closed", m.showHelp, m.readerPopup, cmd != nil)
	}
	next, cmd = m.Update(press("q"))
	m = next.(Model)
	if cmd != nil || m.readerPopup {
		t.Fatalf("second q: reader %v quit %v; want the reader popup closed", m.readerPopup, cmd != nil)
	}
	if _, cmd = m.Update(press("q")); cmd == nil {
		t.Error("third q with no popup didn't quit")
	}
}
