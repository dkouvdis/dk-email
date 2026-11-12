package ui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/dkouvdis/dk-email/internal/mail"
	"github.com/dkouvdis/dk-email/internal/mail/mailtest"
)

func tickSettled(t *testing.T, m Model) Model {
	t.Helper()
	next, cmd := m.Update(refreshTickMsg{})
	return settle(t, next.(Model), cmd)
}

func TestRefresh_TickMergesNewMailAndKeepsCursor(t *testing.T) {
	f := newFixture()
	m := started(t, f, 160, 30)
	m = pressSettled(t, m, "j") // cursor on Lunch
	m = pressSettled(t, m, "space")

	f.personal.AddMessage("INBOX", msg("Zed", "Brand new", fixedNow, false, "z1"))
	f.work.AddMessage("INBOX", msg("Boss", "Another", fixedNow, false, "w2"))
	m = tickSettled(t, m)

	if !strings.Contains(plain(m), "Brand new") {
		t.Errorf("new mail not shown after a tick:\n%s", plain(m))
	}
	if uid, _ := m.list.CursorUID(); uid != 2 || !m.list.OnThreadRow() {
		t.Errorf("cursor on %d (thread row %v), want it still on Lunch (2)", uid, m.list.OnThreadRow())
	}
	if !strings.Contains(plain(m), "Carol") || strings.Count(plain(m), "Lunch?") != 2 {
		t.Errorf("Lunch should still be expanded:\n%s", plain(m))
	}
	if unread(m, "INBOX") != 3 {
		t.Errorf("Personal Inbox unread = %d, want 3", unread(m, "INBOX"))
	}
	if f, _ := m.side.Folder(1, "INBOX"); f.Unread != 2 {
		t.Errorf("Work Inbox unread = %d, want 2 (every account refreshes)", f.Unread)
	}
	if got := summaryCalls(f.work); len(got) != 0 {
		t.Errorf("Work Summaries called %v; only the open folder refreshes its messages", got)
	}
}

func TestRefresh_CtrlRRefreshesNowWithStatus(t *testing.T) {
	f := newFixture()
	m := started(t, f, 160, 30)
	f.personal.AddMessage("INBOX", msg("Zed", "Brand new", fixedNow, false, "z1"))

	if _, cmd := m.Update(press("r")); cmd != nil {
		t.Error("r started a refresh; it only refreshes on ctrl+r")
	}
	next, cmd := m.Update(press("ctrl+r"))
	m = next.(Model)
	if m.status != "Refreshing…" || cmd == nil {
		t.Fatalf("status %q, cmd %v; want a refresh under way", m.status, cmd != nil)
	}
	if _, again := m.Update(press("ctrl+r")); again != nil {
		t.Error("a second ctrl+r started an overlapping refresh")
	}
	scheduled := 0
	m.every = func(time.Duration) tea.Cmd { scheduled++; return nil }
	if _, again := m.Update(refreshTickMsg{}); again != nil || scheduled != 1 {
		t.Errorf("a tick during a refresh: cmd %v, scheduled %d; want no overlap but the next tick scheduled", again != nil, scheduled)
	}

	m = settle(t, m, cmd)
	if m.status != "" || !strings.Contains(plain(m), "Brand new") {
		t.Errorf("status %q after refresh; view:\n%s", m.status, plain(m))
	}
}

func TestRefresh_TickReschedules(t *testing.T) {
	m := started(t, newFixture(), 160, 30)
	scheduled := 0
	m.every = func(d time.Duration) tea.Cmd {
		if d != 120*time.Second {
			t.Errorf("tick every %v, want refresh_interval 120s", d)
		}
		scheduled++
		return nil
	}

	_ = tickSettled(t, m)

	if scheduled != 1 {
		t.Errorf("next tick scheduled %d times, want 1", scheduled)
	}
}

func TestRefresh_FailureShowsAndNextTickRetries(t *testing.T) {
	f := newFixture()
	m := started(t, f, 160, 30)
	f.personal.Fail(mailtest.MethodSummaries, errors.New("connection reset"))
	f.work.Fail(mailtest.MethodFolders, errors.New("LIST timed out"))

	m = tickSettled(t, m)
	if !m.statusErr || !(strings.Contains(m.status, "refresh Personal/Inbox: connection reset") || strings.Contains(m.status, "refresh Work: LIST timed out")) {
		t.Errorf("status = %q, want a refresh error", m.status)
	}
	if m.list.Count() != 3 || !strings.Contains(plain(m), "Quarterly") && !strings.Contains(plain(m), "Inbox") {
		t.Error("a failed refresh changed the list")
	}

	f.personal.Fail(mailtest.MethodSummaries, nil)
	f.work.Fail(mailtest.MethodFolders, nil)
	f.personal.AddMessage("INBOX", msg("Zed", "Brand new", fixedNow, false, "z1"))
	m = tickSettled(t, m)
	if !strings.Contains(plain(m), "Brand new") {
		t.Errorf("the next tick didn't retry:\n%s", plain(m))
	}
}

func TestRefresh_ResultsDuringAWriteAreDropped(t *testing.T) {
	f := newFixture()
	m := started(t, f, 160, 30)

	// Archive the Plan thread but hold the server's answer back.
	next, moveCmd := m.Update(press("e"))
	m = next.(Model)
	// A refresh that started before the move landed sees the old INBOX.
	next, refreshCmd := m.Update(refreshTickMsg{})
	m = next.(Model)
	refresh := refreshCmd().(tea.BatchMsg)
	for _, c := range refresh {
		if c == nil {
			continue
		}
		if r, ok := c().(refreshMsg); ok {
			r.summaries = append(r.summaries, f.personal.Messages("INBOX")[0].Summary)
			next, _ = m.Update(r)
			m = next.(Model)
		}
	}

	if strings.Contains(plain(m), "Re: Plan") || strings.Contains(plain(m), "Plan  ") {
		t.Errorf("a stale refresh brought the archived thread back:\n%s", plain(m))
	}
	m = settle(t, m, moveCmd)
	if m.status != "Archived 2 messages." {
		t.Errorf("status = %q", m.status)
	}
}

func TestRefresh_FolderResetReloads(t *testing.T) {
	f := newFixture()
	m := started(t, f, 160, 30)
	f.personal.Fail(mailtest.MethodSummaries, mail.ErrFolderChanged)

	next, cmd := m.Update(refreshTickMsg{})
	m = next.(Model)
	for _, c := range cmd().(tea.BatchMsg) {
		if c == nil {
			continue
		}
		if r, ok := c().(refreshMsg); ok {
			f.personal.Fail(mailtest.MethodSummaries, nil)
			next, reload := m.Update(r)
			m = settle(t, next.(Model), reload)
		}
	}

	if !strings.Contains(m.status, "Inbox changed on the server; reloaded.") || m.list.Count() != 3 {
		t.Errorf("status %q, %d messages; want a reload", m.status, m.list.Count())
	}
}

func TestRefresh_UsesNonZeroBeforeToCatchFolderResets(t *testing.T) {
	f := newFixture()
	m := started(t, f, 160, 30)

	_ = tickSettled(t, m)

	got := calls(f.personal, mailtest.MethodSummaries)
	if len(got) != 2 {
		t.Fatalf("Summaries calls = %d, want the initial load and the refresh", len(got))
	}
	if refreshBefore == 0 {
		t.Error("refreshBefore must be non-zero so Summaries checks UIDVALIDITY")
	}
}

func TestRefresh_NoAccountsConnected(t *testing.T) {
	m := send(t, newModel(nil), size(160, 30))

	next, cmd := m.Update(press("r"))

	if cmd != nil || next.(Model).status == "Refreshing…" {
		t.Error("r with nothing connected should do nothing")
	}
}
