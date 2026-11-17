package ui

import (
	"errors"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/dkouvdis/dk-email/internal/mail"
	"github.com/dkouvdis/dk-email/internal/mail/mailtest"
	"github.com/dkouvdis/dk-email/internal/search"
)

func typeText(t *testing.T, m Model, text string) Model {
	t.Helper()
	for _, r := range text {
		k := string(r)
		if r == ' ' {
			k = "space"
		}
		m = send(t, m, press(k))
	}
	return m
}

// searched opens the prompt, types raw and presses enter, settling the results.
func searched(t *testing.T, m Model, raw string) Model {
	t.Helper()
	m = send(t, m, press("/"))
	m = typeText(t, m, raw)
	return pressSettled(t, m, "enter")
}

func TestSearch_SlashFocusesPromptAndTypesEveryKey(t *testing.T) {
	f := newFixture()
	m := started(t, f, 160, 30)

	m = send(t, m, press("/"))
	if !m.search.input.Focused() || !strings.Contains(plain(m), "/ ") {
		t.Fatal("/ didn't open the search prompt")
	}
	m = typeText(t, m, "qjk?")
	if m.search.input.Value() != "qjk?" {
		t.Errorf("prompt = %q, want qjk?", m.search.input.Value())
	}
	if !strings.Contains(plain(m), "/ qjk?") {
		t.Errorf("header doesn't show the prompt:\n%s", plain(m))
	}
	if m.showHelp {
		t.Error("? opened help instead of being typed")
	}

	m = send(t, m, press("esc"))
	if m.search.input.Focused() || m.searching() || m.search.input.Value() != "" {
		t.Error("esc didn't close the prompt without searching")
	}
	if got := calls(f.personal, mailtest.MethodSearch); len(got) != 0 {
		t.Errorf("esc searched: %v", got)
	}
}

func TestSearch_EnterSearchesOpenFolder(t *testing.T) {
	f := newFixture()
	m := started(t, f, 160, 30)

	m = searched(t, m, "plan")

	if c := lastCall(t, f.personal, mailtest.MethodSearch); c.Folder != "INBOX" || c.Query != "plan" {
		t.Errorf("Search call = %+v, want INBOX / plan", c)
	}
	if m.search.input.Focused() || !m.searching() {
		t.Error("prompt still focused or search not active")
	}
	v := plain(m)
	if !strings.Contains(v, "search: plan (2)") {
		t.Errorf("header lacks search note:\n%s", v)
	}
	if strings.Contains(v, "Lunch?") || !strings.Contains(v, "Plan") {
		t.Errorf("results not shown:\n%s", v)
	}
	if m.list.Count() != 2 || m.status != "" {
		t.Errorf("count %d, status %q; want 2 results and a cleared status", m.list.Count(), m.status)
	}
}

func TestSearch_SlashOnSidebarStillFiltersFolders(t *testing.T) {
	m := started(t, newFixture(), 90, 30) // narrow: sidebar visible
	m = send(t, m, press("h"))
	if m.focus != PaneSidebar {
		t.Fatal("focus not on the sidebar")
	}
	m = send(t, m, press("/"))
	if !m.side.Filtering() || m.search.input.Focused() {
		t.Error("/ on the sidebar should filter folders, not open the list search")
	}
}

func TestSearch_EscReopensFolder(t *testing.T) {
	f := newFixture()
	m := started(t, f, 160, 30)
	m = searched(t, m, "plan")
	before := len(summaryCalls(f.personal))

	m = pressSettled(t, m, "esc")

	if m.searching() || strings.Contains(plain(m), "search:") {
		t.Errorf("search still shown:\n%s", plain(m))
	}
	if len(summaryCalls(f.personal)) != before+1 || m.list.Count() != 3 {
		t.Errorf("folder not reloaded: summaries calls %d, count %d", len(summaryCalls(f.personal)), m.list.Count())
	}
}

func TestSearch_EmptyEnterClearsActiveSearch(t *testing.T) {
	m := started(t, newFixture(), 160, 30)
	m = searched(t, m, "plan")
	m = send(t, m, press("/"))
	for range "plan" {
		m = send(t, m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	}
	m = pressSettled(t, m, "enter")
	if m.searching() {
		t.Error("an empty query should clear the search")
	}
}

func TestSearch_NoMatches(t *testing.T) {
	m := started(t, newFixture(), 160, 30)
	m = searched(t, m, "zzzznothing")
	if !strings.Contains(plain(m), "No messages match.") || !strings.Contains(plain(m), "(0)") {
		t.Errorf("empty result not shown:\n%s", plain(m))
	}
}

func TestSearch_StaleResultsAreDropped(t *testing.T) {
	f := newFixture()
	m := started(t, f, 160, 30)
	m = send(t, m, press("/"))
	m = typeText(t, m, "plan")
	next, cmd := m.Update(press("enter"))
	m = next.(Model)
	stale := cmd().(searchMsg)

	// Replaced by a newer search.
	m, cmd = m.startSearch(search.Parse("lunch"))
	m = send(t, m, stale)
	if !m.open.loading || m.list.Count() != 3 {
		t.Error("a replaced search's results were applied")
	}
	m = settle(t, m, cmd)
	if m.list.Count() != 1 {
		t.Fatalf("lunch search shows %d, want 1", m.list.Count())
	}

	// After opening another folder.
	m, cmd = m.startSearch(search.Parse("shop"))
	late := cmd().(searchMsg)
	opened, ocmd := m.openFolder(0, "Receipts")
	m = settle(t, opened, ocmd)
	m = send(t, m, late)
	if m.searching() || m.list.Count() != 1 || strings.Contains(plain(m), "search:") {
		t.Error("a search result for a left folder was applied")
	}
}

func TestSearch_PagesWithSearchNotSummaries(t *testing.T) {
	f := bulkFixture(450)
	m := started(t, f, 160, 30)
	opened, cmd := m.openFolder(0, "Bulk")
	m = settle(t, opened, cmd)
	summaries := len(summaryCalls(f.personal))

	m = searched(t, m, "bulk")
	if m.list.Count() != 200 || m.search.total != 450 {
		t.Fatalf("first page %d, total %d; want 200 and 450", m.list.Count(), m.search.total)
	}
	next, cmd := m.Update(press("G"))
	m = next.(Model)
	if cmd == nil {
		t.Fatal("G on the last row didn't ask for another page")
	}
	m = settle(t, m, cmd)
	if m.list.Count() != 400 {
		t.Errorf("after paging: %d, want 400", m.list.Count())
	}
	if got := calls(f.personal, mailtest.MethodSearch); len(got) != 2 {
		t.Errorf("%d Search calls, want 2", len(got))
	}
	if len(summaryCalls(f.personal)) != summaries {
		t.Error("paging a search called Summaries")
	}
}

func TestSearch_RefreshLeavesResultsAlone(t *testing.T) {
	f := newFixture()
	m := started(t, f, 160, 30)
	m = searched(t, m, "plan")
	summaries := len(summaryCalls(f.personal))

	f.personal.AddMessage("INBOX", msg("Zed", "Brand new", fixedNow, false, "z1"))
	m = tickSettled(t, m)

	if len(summaryCalls(f.personal)) != summaries {
		t.Error("a refresh fetched the open folder while searching")
	}
	if m.list.Count() != 2 || strings.Contains(plain(m), "Brand new") {
		t.Errorf("refresh changed the results:\n%s", plain(m))
	}
}

func TestSearch_RefreshStartedBeforeSearchDoesNotMerge(t *testing.T) {
	f := newFixture()
	m := started(t, f, 160, 30)
	next, refreshCmd := m.Update(refreshTickMsg{})
	m = next.(Model)

	m = searched(t, m, "plan")
	f.personal.AddMessage("INBOX", msg("Zed", "Brand new", fixedNow, false, "z1"))
	for _, c := range refreshCmd().(tea.BatchMsg) {
		if c == nil {
			continue
		}
		next, _ = m.Update(c())
		m = next.(Model)
	}

	if m.list.Count() != 2 || strings.Contains(plain(m), "Brand new") {
		t.Errorf("a refresh from before the search merged into results:\n%s", plain(m))
	}
}

func TestSearch_CtrlRRerunsSearch(t *testing.T) {
	f := newFixture()
	m := started(t, f, 160, 30)
	m = searched(t, m, "plan")
	f.personal.AddMessage("INBOX", msg("Zed", "Plan B", fixedNow, false, "z1"))

	m = pressSettled(t, m, "ctrl+r")

	if got := calls(f.personal, mailtest.MethodSearch); len(got) != 2 {
		t.Errorf("%d Search calls, want 2", len(got))
	}
	if m.list.Count() != 3 || !strings.Contains(plain(m), "search: plan (3)") {
		t.Errorf("search not refreshed:\n%s", plain(m))
	}
}

func TestSearch_FirstPageErrorFallsBackToFolder(t *testing.T) {
	f := newFixture()
	m := started(t, f, 160, 30)
	f.personal.Fail(mailtest.MethodSearch, errors.New("SEARCH timed out"))

	m = searched(t, m, "plan")

	if m.searching() || m.list.Count() != 3 {
		t.Errorf("searching %v, count %d; want the folder's 3 messages back", m.searching(), m.list.Count())
	}
	if !m.statusErr || !strings.Contains(m.status, "search Personal/Inbox: SEARCH timed out") {
		t.Errorf("status = %q", m.status)
	}
}

func TestSearch_FailedRefinementKeepsPreviousSearch(t *testing.T) {
	f := newFixture()
	m := started(t, f, 160, 30)
	m = searched(t, m, "plan")
	f.personal.Fail(mailtest.MethodSearch, errors.New("SEARCH timed out"))
	m = searched(t, m, "lunch")

	if !m.searching() || m.search.query.Raw != "plan" || m.list.Count() != 2 {
		t.Fatalf("query %q, count %d; want the plan search kept", m.search.query.Raw, m.list.Count())
	}
	if v := plain(m); !strings.Contains(v, "search: plan (2)") {
		t.Errorf("header lost the previous search:\n%s", v)
	}
	if !m.statusErr || !strings.Contains(m.status, "SEARCH timed out") {
		t.Errorf("status = %q, want the error", m.status)
	}
	summaries := len(summaryCalls(f.personal))
	m = tickSettled(t, m)
	if len(summaryCalls(f.personal)) != summaries || m.list.Count() != 2 {
		t.Error("a refresh touched the previous search's results")
	}
}

func TestSearch_TriageActsOnResults(t *testing.T) {
	f := newFixture()
	m := started(t, f, 160, 30)
	m = searched(t, m, "lunch")

	m = pressSettled(t, m, "s")

	c := lastCall(t, f.personal, mailtest.MethodSetFlag)
	if c.Folder != "INBOX" || c.Flag != mail.FlagFlagged || !slices.Equal(c.UIDs, []mail.UID{2}) {
		t.Errorf("SetFlag = %+v, want flagged on INBOX UID 2", c)
	}
}

func TestSearch_HelpAndHintsListIt(t *testing.T) {
	m := send(t, New(testConfig(), nil, nil), size(200, 30))
	if h := ansi.Strip(m.renderHints()); !strings.Contains(h, "search: /") && !strings.Contains(strings.ToLower(h), "search: /") {
		t.Errorf("list hints = %q, want search", h)
	}
	found := false
	for _, sec := range m.keys.HelpSections() {
		for _, b := range sec.Keys {
			found = found || (b.Help().Key == "/" && b.Help().Desc == "search")
		}
	}
	if !found {
		t.Error("help doesn't list / search")
	}
}
