// Package threadlist is the thread list pane. It re-threads every loaded
// summary whenever a page arrives, and tracks the cursor and expanded
// threads by message UID so both survive re-threading.
package threadlist

import (
	"cmp"
	"maps"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/dkouvdis/dk-email/internal/mail"
	"github.com/dkouvdis/dk-email/internal/thread"
)

// LoadMoreMsg asks the root model for the page of messages older than Before.
type LoadMoreMsg struct{ Before mail.UID }

type keys struct{ up, down, top, bottom, expand key.Binding }

// cursor points at a thread row (message false; uid is any message in the
// thread) or at a message row inside an expanded thread.
type cursor struct {
	uid     mail.UID
	message bool
}

type row struct {
	thread int
	msg    int // index into the thread's messages, or -1 for the thread row
}

type Model struct {
	now      func() time.Time
	sums     []mail.Summary
	threads  []thread.Thread
	expanded map[mail.UID]bool // a thread is expanded if any of its UIDs is here
	cursor   cursor
	offset   int
	height   int
	more     bool // the server may have older messages
	loading  bool
	keys     keys
	st       rowStyles
}

// New returns an empty list. now is used for relative dates.
func New(now func() time.Time) Model {
	return Model{
		now:      now,
		expanded: map[mail.UID]bool{},
		keys: keys{
			up:     key.NewBinding(key.WithKeys("k", "up")),
			down:   key.NewBinding(key.WithKeys("j", "down")),
			top:    key.NewBinding(key.WithKeys("g", "home")),
			bottom: key.NewBinding(key.WithKeys("G", "end")),
			expand: key.NewBinding(key.WithKeys("space")),
		},
		st: defaultRowStyles(),
	}
}

// SetSummaries replaces the list with a newly opened folder's first page:
// cursor at the top, every thread collapsed. more says whether older pages
// may exist.
func (m Model) SetSummaries(sums []mail.Summary, more bool) Model {
	m.sums = dedupe(nil, sums)
	m.threads = thread.Build(m.sums)
	m.expanded = map[mail.UID]bool{}
	m.offset = 0
	m.more, m.loading = more, false
	m.cursor = cursor{}
	if len(m.threads) > 0 {
		m.cursor.uid = m.threads[0].Latest().UID
	}
	return m
}

// AddPage merges an older page and re-threads everything, keeping the
// cursor and expanded threads where they were.
func (m Model) AddPage(sums []mail.Summary, more bool) Model {
	m.sums = dedupe(m.sums, sums)
	m.threads = thread.Build(m.sums)
	m.more, m.loading = more, false
	m.ensureVisible()
	return m
}

// SetSeen updates messages' read state after a flag change, keeping the
// cursor and expanded threads.
func (m Model) SetSeen(uids []mail.UID, seen bool) Model {
	return m.update(uids, func(s *mail.Summary) { s.Seen = seen })
}

// SetFlagged updates messages' starred state, keeping the cursor.
func (m Model) SetFlagged(uids []mail.UID, flagged bool) Model {
	return m.update(uids, func(s *mail.Summary) { s.Flagged = flagged })
}

func (m Model) update(uids []mail.UID, fn func(*mail.Summary)) Model {
	sums := slices.Clone(m.sums)
	for i := range sums {
		if slices.Contains(uids, sums[i].UID) {
			fn(&sums[i])
		}
	}
	m.sums = sums
	m.threads = thread.Build(m.sums)
	return m
}

// Lookup returns the loaded summaries for uids, in uids order, skipping
// any that aren't loaded.
func (m Model) Lookup(uids []mail.UID) []mail.Summary {
	var out []mail.Summary
	for _, uid := range uids {
		if i := slices.IndexFunc(m.sums, func(s mail.Summary) bool { return s.UID == uid }); i >= 0 {
			out = append(out, m.sums[i])
		}
	}
	return out
}

// Remove drops messages, e.g. after archiving. If the cursor's row goes,
// the cursor moves to the thread that took its place (the next one), or
// the new last thread.
func (m Model) Remove(uids []mail.UID) Model {
	rows := m.rows()
	at := max(m.cursorRow(rows), 0)
	threadAt := 0
	if at < len(rows) {
		threadAt = rows[at].thread
	}

	m.sums = slices.DeleteFunc(slices.Clone(m.sums), func(s mail.Summary) bool { return slices.Contains(uids, s.UID) })
	m.threads = thread.Build(m.sums)

	if _, ok := m.CursorUID(); ok && !slices.Contains(uids, m.cursor.uid) {
		m.ensureVisible()
		return m
	}
	m.cursor = cursor{}
	if len(m.threads) > 0 {
		m.cursor.uid = m.threads[min(threadAt, len(m.threads)-1)].Latest().UID
	}
	m.ensureVisible()
	return m
}

// Merge applies a refreshed newest page: new messages are added, loaded
// ones take the page's flags, and loaded messages within the page's UID
// range that it no longer holds (moved or deleted elsewhere) are dropped.
// Older pages are kept. The cursor and expanded threads stay put; if the
// cursor's message is gone it moves like Remove.
func (m Model) Merge(page []mail.Summary) Model {
	if len(page) == 0 {
		return m
	}
	lowest := slices.MinFunc(page, func(a, b mail.Summary) int { return cmp.Compare(a.UID, b.UID) }).UID
	fresh := make(map[mail.UID]mail.Summary, len(page))
	for _, s := range page {
		fresh[s.UID] = s
	}
	var gone []mail.UID
	sums := slices.Clone(m.sums)
	for i, s := range sums {
		switch f, ok := fresh[s.UID]; {
		case ok:
			sums[i] = f
		case s.UID >= lowest:
			gone = append(gone, s.UID)
		}
	}
	m.sums = dedupe(sums, page)
	m.threads = thread.Build(m.sums)
	if len(m.threads) > 0 && m.cursor.uid == 0 {
		m.cursor.uid = m.threads[0].Latest().UID
	}
	if len(gone) > 0 {
		return m.Remove(gone)
	}
	m.ensureVisible()
	return m
}

// Restore puts removed messages back, e.g. when a move fails.
func (m Model) Restore(sums []mail.Summary) Model {
	m.sums = dedupe(m.sums, sums)
	m.threads = thread.Build(m.sums)
	m.ensureVisible()
	return m
}

// PageFailed ends a page load that failed, so reaching the end retries.
func (m Model) PageFailed() Model {
	m.loading = false
	return m
}

func (m Model) LoadingMore() bool { return m.loading }

// Count is the number of loaded messages.
func (m Model) Count() int { return len(m.sums) }

// SetHeight tells the pane how many rows it has, for scrolling.
func (m Model) SetHeight(h int) Model {
	m.height = h
	m.ensureVisible()
	return m
}

// CursorUID is the UID of the row under the cursor: the thread's latest
// message on a thread row.
func (m Model) CursorUID() (mail.UID, bool) {
	rows := m.rows()
	i := m.cursorRow(rows)
	if i < 0 {
		return 0, false
	}
	return m.rowSummary(rows[i]).UID, true
}

// OnThreadRow reports whether the cursor is on a thread row rather than a
// message inside an expanded thread.
func (m Model) OnThreadRow() bool {
	rows := m.rows()
	i := m.cursorRow(rows)
	return i >= 0 && rows[i].msg < 0
}

// Selected returns the thread under the cursor and, on a message row, that
// message's UID (0 on a thread row).
func (m Model) Selected() (thread.Thread, mail.UID, bool) {
	rows := m.rows()
	i := m.cursorRow(rows)
	if i < 0 {
		return thread.Thread{}, 0, false
	}
	r := rows[i]
	if r.msg < 0 {
		return m.threads[r.thread], 0, true
	}
	return m.threads[r.thread], m.threads[r.thread].Messages[r.msg].UID, true
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	rows := m.rows()
	if len(rows) == 0 {
		return m, nil
	}
	i := max(m.cursorRow(rows), 0)
	switch {
	case key.Matches(k, m.keys.up):
		i--
	case key.Matches(k, m.keys.down):
		i++
	case key.Matches(k, m.keys.top):
		i = 0
	case key.Matches(k, m.keys.bottom):
		i = len(rows) - 1
	case key.Matches(k, m.keys.expand):
		m.toggle(rows[i])
		m.ensureVisible()
		return m, nil
	default:
		return m, nil
	}
	i = min(max(i, 0), len(rows)-1)
	m.cursor = cursor{uid: m.rowSummary(rows[i]).UID, message: rows[i].msg >= 0}
	m.ensureVisible()

	if i == len(rows)-1 && m.more && !m.loading {
		m.loading = true
		before := m.oldest()
		return m, func() tea.Msg { return LoadMoreMsg{Before: before} }
	}
	return m, nil
}

// Click puts the cursor on the row on the given line (0 is the top line
// shown). It reports whether a row is there and whether the cursor was
// already on it, so a second click can open it.
func (m Model) Click(line int) (next Model, hit, again bool) {
	rows := m.rows()
	cur := m.cursorRow(rows)
	offset := clampOffset(m.offset, cur, m.height, m.totalLines())
	i := offset + line
	if line < 0 || line >= m.height || i >= len(rows) {
		return m, false, false
	}
	if i == cur {
		return m, true, true
	}
	m.cursor = cursor{uid: m.rowSummary(rows[i]).UID, message: rows[i].msg >= 0}
	m.offset = offset
	m.ensureVisible()
	return m, true, false
}

// toggle expands a collapsed thread, or collapses the thread under the
// cursor (from its row or any of its messages) and puts the cursor on it.
func (m *Model) toggle(r row) {
	th := m.threads[r.thread]
	m.expanded = maps.Clone(m.expanded)
	if r.msg < 0 && !m.isExpanded(th) {
		m.expanded[th.Latest().UID] = true
		return
	}
	for _, s := range th.Messages {
		delete(m.expanded, s.UID)
	}
	m.cursor = cursor{uid: th.Latest().UID}
}

func (m Model) isExpanded(th thread.Thread) bool {
	return slices.ContainsFunc(th.Messages, func(s mail.Summary) bool { return m.expanded[s.UID] })
}

func (m Model) rows() []row {
	rows := make([]row, 0, len(m.threads))
	for i, th := range m.threads {
		rows = append(rows, row{thread: i, msg: -1})
		if m.isExpanded(th) {
			for j := range th.Messages {
				rows = append(rows, row{thread: i, msg: j})
			}
		}
	}
	return rows
}

func (m Model) rowSummary(r row) mail.Summary {
	th := m.threads[r.thread]
	if r.msg < 0 {
		return th.Latest()
	}
	return th.Messages[r.msg]
}

// cursorRow finds the cursor's row. A message row whose thread is no longer
// expanded falls back to the thread row; an unknown UID falls back to the top.
func (m Model) cursorRow(rows []row) int {
	if len(rows) == 0 {
		return -1
	}
	threadRow := -1
	for i, r := range rows {
		th := m.threads[r.thread]
		if r.msg < 0 {
			if slices.ContainsFunc(th.Messages, func(s mail.Summary) bool { return s.UID == m.cursor.uid }) {
				if !m.cursor.message {
					return i
				}
				threadRow = i
			}
			continue
		}
		if m.cursor.message && th.Messages[r.msg].UID == m.cursor.uid {
			return i
		}
	}
	return max(threadRow, 0)
}

func (m Model) oldest() mail.UID {
	if len(m.sums) == 0 {
		return 0
	}
	return slices.MinFunc(m.sums, func(a, b mail.Summary) int { return cmp.Compare(a.UID, b.UID) }).UID
}

func (m *Model) ensureVisible() {
	if m.height <= 0 {
		return
	}
	m.offset = clampOffset(m.offset, m.cursorRow(m.rows()), m.height, m.totalLines())
}

func (m Model) totalLines() int {
	n := len(m.rows())
	if m.loading {
		n++
	}
	return n
}

// View renders at most height lines, each exactly width cells wide.
func (m Model) View(width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	if len(m.threads) == 0 {
		return m.st.dim.Render(ansi.Truncate("No messages.", width, "…"))
	}
	rows := m.rows()
	cur := m.cursorRow(rows)
	offset := clampOffset(m.offset, cur, height, m.totalLines())
	end := min(offset+height, len(rows))

	now := m.now()
	visible := rows[offset:end]
	data := make([]rowData, len(visible))
	dateW := 0
	for i, r := range visible {
		data[i] = m.rowData(r, now)
		dateW = max(dateW, ansi.StringWidth(data[i].date))
	}

	lines := make([]string, 0, height)
	for i, d := range data {
		lines = append(lines, m.st.render(d, width, dateW, offset+i == cur))
	}
	if m.loading && len(lines) < height {
		lines = append(lines, m.st.dim.Render(pad("Loading more…", width)))
	}
	return strings.Join(lines, "\n")
}

func (m Model) rowData(r row, now time.Time) rowData {
	th := m.threads[r.thread]
	if r.msg >= 0 {
		s := th.Messages[r.msg]
		return rowData{
			unread: !s.Seen, starred: s.Flagged, message: true,
			sender: senderName(s), subject: subjectText(s), date: formatDate(s.Date, now),
		}
	}
	latest := th.Latest()
	return rowData{
		unread: th.Unread() > 0, starred: th.Flagged(),
		sender: senderName(latest), subject: subjectText(latest),
		count: th.Len(), date: formatDate(latest.Date, now),
	}
}

func clampOffset(offset, cursor, height, total int) int {
	if cursor >= 0 {
		if cursor < offset {
			offset = cursor
		}
		if cursor >= offset+height {
			offset = cursor - height + 1
		}
	}
	offset = min(offset, max(total-height, 0))
	return max(offset, 0)
}

// dedupe appends the summaries in add whose UIDs aren't already in have,
// into a new slice so earlier Model copies are never modified.
func dedupe(have, add []mail.Summary) []mail.Summary {
	seen := make(map[mail.UID]bool, len(have)+len(add))
	out := make([]mail.Summary, 0, len(have)+len(add))
	for _, s := range slices.Concat(have, add) {
		if !seen[s.UID] {
			seen[s.UID] = true
			out = append(out, s)
		}
	}
	return out
}
