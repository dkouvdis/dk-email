package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/dkouvdis/dk-email/internal/mail"
	"github.com/dkouvdis/dk-email/internal/search"
	"github.com/dkouvdis/dk-email/internal/ui/threadlist"
)

const searchTimeout = 60 * time.Second

// listSearch is the thread list's server search: input is the "/ " prompt
// while it's being typed, query the search shown in the list (Raw == "" when
// none). gen drops results of a search that was replaced or cleared. prev and
// prevTotal and prevList are what the list showed before the search in flight,
// restored if its first page fails. writes and dirty are Model.writes when the
// search started and whether a write was already in flight then: a first page
// that arrives after either may predate an optimistic flag or move, so it is
// fetched again instead of shown.
type listSearch struct {
	input     textinput.Model
	query     mail.SearchQuery
	total     int
	prev      mail.SearchQuery
	prevTotal int
	prevList  threadlist.Model
	writes    int
	dirty     bool
	gen       int
}

// searchMsg is a page of search results; before is 0 for the first page.
type searchMsg struct {
	account   int
	folder    string
	gen       int
	before    mail.UID
	summaries []mail.Summary
	total     int
	err       error
}

// searching reports whether the list shows search results.
func (m Model) searching() bool { return m.search.query.Raw != "" }

func runSearch(mb mail.Mailbox, account int, folder string, q mail.SearchQuery, before mail.UID, gen int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), searchTimeout)
		defer cancel()
		sums, total, err := mb.Search(ctx, folder, q, before, pageSize)
		return searchMsg{account: account, folder: folder, gen: gen, before: before, summaries: sums, total: total, err: err}
	}
}

// openSearch focuses the prompt, holding the active query so it can be edited.
func (m Model) openSearch() (Model, tea.Cmd) {
	if m.focus != PaneList || m.open.account < 0 || m.open.loading {
		return m, nil
	}
	m.search.input.SetValue(m.search.query.Raw)
	m.search.input.CursorEnd()
	return m, m.search.input.Focus()
}

func (m Model) handleSearchKey(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Open):
		raw := strings.TrimSpace(m.search.input.Value())
		m.search.input.Blur()
		if raw == "" {
			if m.searching() {
				return m.clearSearch()
			}
			return m, nil
		}
		return m.startSearch(search.Parse(raw))
	case key.Matches(msg, m.keys.Back):
		m.search.input.Blur()
		m.search.input.Reset()
		return m, nil
	}
	var cmd tea.Cmd
	m.search.input, cmd = m.search.input.Update(msg)
	return m, cmd
}

// startSearch runs q on the open folder. The list is cleared so no key acts
// on threads the user can't see; what it showed is kept in prevList, so a
// failed first page can fall back to it.
func (m Model) startSearch(q mail.SearchQuery) (Model, tea.Cmd) {
	if m.open.account < 0 || m.boxes[m.open.account] == nil {
		return m, nil
	}
	m.search.gen++
	// A search already in flight keeps the fallback it saved: the list is
	// empty now, so there is nothing newer to remember.
	if !(m.open.loading && m.searching()) {
		m.search.prev, m.search.prevTotal = m.search.query, m.search.total
		m.search.prevList = m.list
		if m.list.LoadingMore() {
			m.search.prevList = m.list.PageFailed()
		}
	}
	m.list = m.list.SetSummaries(nil, false)
	m.search.writes, m.search.dirty = m.writes, m.inflight > 0
	m.search.query = q
	m.search.total = 0
	m.open.loading = true
	m.read = m.read.Close()
	m.readerPopup = false
	m.setStatus("Searching…")
	m.log.Debug("search", "terms", len(q.Terms))
	return m, runSearch(m.boxes[m.open.account], m.open.account, m.open.folder, q, 0, m.search.gen)
}

// resetSearch forgets the search and closes its prompt.
func (m *Model) resetSearch() {
	m.search.gen++
	m.search.query, m.search.total = mail.SearchQuery{}, 0
	m.search.prev, m.search.prevTotal = mail.SearchQuery{}, 0
	m.search.prevList = threadlist.Model{}
	m.search.input.Reset()
	m.search.input.Blur()
}

// clearSearch goes back to the folder's threads.
func (m Model) clearSearch() (Model, tea.Cmd) {
	return m.openFolder(m.open.account, m.open.folder)
}

func (m Model) handleSearch(msg searchMsg) (Model, tea.Cmd) {
	if msg.gen != m.search.gen || m.open.account != msg.account || m.open.folder != msg.folder {
		return m, nil
	}
	paging := msg.before != 0
	if paging != m.list.LoadingMore() || (!paging && !m.open.loading) {
		return m, nil
	}
	name := m.accts[msg.account].Name
	if !paging && msg.err == nil && (m.search.dirty || m.writes != m.search.writes) {
		// A write may not be reflected in these results yet; ask again.
		m.search.writes, m.search.dirty = m.writes, m.inflight > 0
		return m, runSearch(m.boxes[msg.account], msg.account, msg.folder, m.search.query, 0, m.search.gen)
	}
	m.open.loading = false
	more := len(msg.summaries) < msg.total

	switch {
	case paging && errors.Is(msg.err, mail.ErrFolderChanged):
		m, cmd := m.reloadChanged(msg.account, msg.folder)
		m.open.notice = fmt.Sprintf("%s changed on the server; reloaded and search cleared.", m.open.label)
		return m, cmd
	case msg.err != nil:
		if paging {
			m.list = m.list.PageFailed()
		} else {
			m.search.query, m.search.total = m.search.prev, m.search.prevTotal
			m.list = m.search.prevList
			m.relayout() // the window may have been resized meanwhile
		}
		m.setError(fmt.Errorf("search %s/%s: %w", name, m.open.label, msg.err))
		return m, nil
	case paging:
		m.list = m.list.AddPage(msg.summaries, more)
	default:
		m.list = m.list.SetSummaries(msg.summaries, more)
		m.search.total = msg.total
		m.search.prevList = threadlist.Model{}
	}
	m.setStatus("")
	m.log.Debug("search results", "count", len(msg.summaries), "total", msg.total, "page", paging)
	return m, nil
}
