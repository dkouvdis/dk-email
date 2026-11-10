// Package sidebar is the folders pane: one header per account with its
// folders and unread counts underneath, a spinner while it connects, and
// the error if connecting failed.
package sidebar

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/dkouvdis/dk-email/internal/mail"
)

// SelectedMsg asks the root model to open a folder.
type SelectedMsg struct {
	Account int
	Folder  string
}

// maxErrorLines caps how much of a connect error the pane shows; the status
// line has the full text.
const maxErrorLines = 6

type state int

const (
	connecting state = iota
	ready
	failed
)

type account struct {
	name    string
	state   state
	err     error
	folders []mail.Folder
}

type ref struct {
	account int
	folder  string
}

var noRef = ref{account: -1}

type keys struct{ up, down, top, bottom, open, filter, clear, arrowUp, arrowDown key.Binding }

type styles struct {
	header, folder, unread, count, cursor, open, dim, err lipgloss.Style
}

type Model struct {
	accounts []account
	cursor   ref
	open     ref
	offset   int
	height   int
	spinner  spinner.Model
	filter   textinput.Model
	keys     keys
	st       styles
}

func New(names []string) Model {
	m := Model{
		cursor:  noRef,
		open:    noRef,
		spinner: spinner.New(spinner.WithSpinner(spinner.MiniDot)),
		keys: keys{
			up:     key.NewBinding(key.WithKeys("k", "up")),
			down:   key.NewBinding(key.WithKeys("j", "down")),
			top:    key.NewBinding(key.WithKeys("g", "home")),
			bottom: key.NewBinding(key.WithKeys("G", "end")),
			open:   key.NewBinding(key.WithKeys("enter")),
			filter: key.NewBinding(key.WithKeys("/")),
			clear:  key.NewBinding(key.WithKeys("esc")),
			// While filtering, j and k are text, so only the arrows move.
			arrowUp:   key.NewBinding(key.WithKeys("up")),
			arrowDown: key.NewBinding(key.WithKeys("down")),
		},
		st: styles{
			header: lipgloss.NewStyle().Bold(true),
			folder: lipgloss.NewStyle(),
			unread: lipgloss.NewStyle().Bold(true),
			count:  lipgloss.NewStyle().Foreground(lipgloss.Color("111")),
			cursor: lipgloss.NewStyle().Foreground(lipgloss.Color("231")).Background(lipgloss.Color("63")),
			open:   lipgloss.NewStyle().Foreground(lipgloss.Color("63")),
			dim:    lipgloss.NewStyle().Foreground(lipgloss.Color("244")),
			err:    lipgloss.NewStyle().Foreground(lipgloss.Color("203")),
		},
	}
	m.filter = textinput.New()
	m.filter.Prompt = "/ "
	for _, n := range names {
		m.accounts = append(m.accounts, account{name: n})
	}
	return m
}

// Filtering reports whether the folder filter has focus, so every key, q
// included, goes to it.
func (m Model) Filtering() bool { return m.filter.Focused() }

// Paste types pasted text into the filter while it has focus.
func (m Model) Paste(msg tea.PasteMsg) Model {
	if !m.filter.Focused() {
		return m
	}
	content := strings.Join(strings.Fields(msg.Content), " ")
	m.filter, _ = m.filter.Update(tea.PasteMsg{Content: content})
	m.refilter()
	return m
}

func (m Model) query() string { return strings.ToLower(strings.TrimSpace(m.filter.Value())) }

// matches reports whether a folder's label contains the filter, ignoring case.
func (m Model) matches(f mail.Folder) bool {
	q := m.query()
	return q == "" || strings.Contains(strings.ToLower(Label(f)), q)
}

// refilter moves the cursor to the first match if its folder no longer matches.
func (m *Model) refilter() {
	if sel := m.selectable(); len(sel) > 0 && !slices.Contains(sel, m.cursor) {
		m.cursor = sel[0]
	}
	m.offset = 0
	m.ensureVisible()
}

func (m *Model) clearFilter() {
	m.filter.Reset()
	m.filter.Blur()
	m.ensureVisible()
}

// handleFilterKey types into the filter. ↑/↓ move between matches, enter
// opens the folder under the cursor and clears the filter, esc clears it.
func (m Model) handleFilterKey(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	sel := m.selectable()
	i := slices.Index(sel, m.cursor)
	switch {
	case key.Matches(msg, m.keys.clear):
		m.clearFilter()
		return m, nil
	case key.Matches(msg, m.keys.open):
		if i < 0 {
			return m, nil
		}
		c := m.cursor
		m.clearFilter()
		return m, func() tea.Msg { return SelectedMsg{Account: c.account, Folder: c.folder} }
	case key.Matches(msg, m.keys.arrowUp):
		i--
	case key.Matches(msg, m.keys.arrowDown):
		i++
	default:
		before := m.filter.Value()
		m.filter, _ = m.filter.Update(msg)
		if m.filter.Value() != before {
			m.refilter()
		}
		return m, nil
	}
	if len(sel) > 0 {
		m.cursor = sel[min(max(i, 0), len(sel)-1)]
		m.ensureVisible()
	}
	return m, nil
}

// AddAccount adds an account at the end, already connected with folders
// (as after the connect popup), and returns its index.
func (m Model) AddAccount(name string, folders []mail.Folder) (Model, int) {
	m.accounts = append(slices.Clone(m.accounts), account{name: name, state: connecting})
	i := len(m.accounts) - 1
	return m.SetFolders(i, folders, nil), i
}

// Tick starts the connecting spinner.
func (m Model) Tick() tea.Cmd { return m.spinner.Tick }

// Connecting reports whether any account is still connecting.
func (m Model) Connecting() bool {
	return slices.ContainsFunc(m.accounts, func(a account) bool { return a.state == connecting })
}

// SetFolders records an account's connect result. Folders are ordered by
// role (Inbox, Drafts, Sent, Archive, All Mail, Trash, Junk), then by name.
// The cursor stays on the same folder if it still exists.
func (m Model) SetFolders(i int, folders []mail.Folder, err error) Model {
	if i < 0 || i >= len(m.accounts) {
		return m
	}
	a := &m.accounts[i]
	a.err = err
	if err != nil {
		a.state = failed
		a.folders = nil
	} else {
		a.state = ready
		a.folders = slices.Clone(folders)
		slices.SortStableFunc(a.folders, compareFolders)
	}
	m.accounts = slices.Clone(m.accounts)

	if !slices.Contains(m.selectable(), m.cursor) {
		m.cursor = noRef
		if sel := m.selectable(); len(sel) > 0 {
			m.cursor = sel[0]
		}
	}
	m.ensureVisible()
	return m
}

// SetOpen marks the folder whose threads are shown and moves the cursor to it.
func (m Model) SetOpen(account int, folder string) Model {
	m.open = ref{account, folder}
	if slices.Contains(m.selectable(), m.open) {
		m.cursor = m.open
	}
	m.ensureVisible()
	return m
}

// SetHeight tells the pane how many rows it has, for scrolling.
func (m Model) SetHeight(h int) Model {
	m.height = h
	m.ensureVisible()
	return m
}

// Account is a read-only view of one account, for other folder lists.
type Account struct {
	Name string
	// Note explains a missing folder list: connecting, an error, or no folders.
	Note    string
	Folders []mail.Folder // in display order
}

// Accounts lists every account with its folders in display order.
func (m Model) Accounts() []Account {
	out := make([]Account, len(m.accounts))
	for i, a := range m.accounts {
		out[i] = Account{Name: a.name, Folders: slices.Clone(a.folders)}
		switch {
		case a.state == connecting:
			out[i].Note = "connecting…"
		case a.state == failed:
			out[i].Note = SummarizeError(a.err)
		case len(a.folders) == 0:
			out[i].Note = "no folders"
		}
	}
	return out
}

// SummarizeError is the first line of err, for one-line notes.
func SummarizeError(err error) string {
	s, _, _ := strings.Cut(err.Error(), "\n")
	return s
}

// AdjustUnread changes a folder's unread count by delta, never below zero.
func (m Model) AdjustUnread(account int, name string, delta int) Model {
	if account < 0 || account >= len(m.accounts) {
		return m
	}
	m.accounts = slices.Clone(m.accounts)
	a := &m.accounts[account]
	a.folders = slices.Clone(a.folders)
	for i := range a.folders {
		if a.folders[i].Name == name {
			a.folders[i].Unread = max(a.folders[i].Unread+delta, 0)
		}
	}
	return m
}

// Role is a folder's role, or RoleNone if it isn't known.
func (m Model) Role(account int, name string) mail.Role {
	f, _ := m.Folder(account, name)
	return f.Role
}

// Folder looks up a folder by account and name.
func (m Model) Folder(account int, name string) (mail.Folder, bool) {
	if account < 0 || account >= len(m.accounts) {
		return mail.Folder{}, false
	}
	i := slices.IndexFunc(m.accounts[account].folders, func(f mail.Folder) bool { return f.Name == name })
	if i < 0 {
		return mail.Folder{}, false
	}
	return m.accounts[account].folders[i], true
}

// Cursor returns the folder under the cursor, or -1 and "" if there's none.
func (m Model) Cursor() (account int, folder string) {
	return m.cursor.account, m.cursor.folder
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		if !m.Connecting() {
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	if m.filter.Focused() {
		return m.handleFilterKey(msg)
	}
	if key.Matches(msg, m.keys.filter) {
		m.filter.Focus()
		m.ensureVisible()
		return m, nil
	}
	sel := m.selectable()
	if len(sel) == 0 {
		return m, nil
	}
	i := max(slices.Index(sel, m.cursor), 0)
	switch {
	case key.Matches(msg, m.keys.up):
		i--
	case key.Matches(msg, m.keys.down):
		i++
	case key.Matches(msg, m.keys.top):
		i = 0
	case key.Matches(msg, m.keys.bottom):
		i = len(sel) - 1
	case key.Matches(msg, m.keys.open):
		c := m.cursor
		return m, func() tea.Msg { return SelectedMsg{Account: c.account, Folder: c.folder} }
	default:
		return m, nil
	}
	m.cursor = sel[min(max(i, 0), len(sel)-1)]
	m.ensureVisible()
	return m, nil
}

// selectable lists the folders the cursor can be on: those matching the filter.
func (m Model) selectable() []ref {
	var out []ref
	for i, a := range m.accounts {
		for _, f := range a.folders {
			if m.matches(f) {
				out = append(out, ref{i, f.Name})
			}
		}
	}
	return out
}

type row struct {
	text   string
	ref    ref  // noRef for headers and status lines
	header bool // an account name
}

// rows lists each account and its folders. While a filter is typed, only
// matching folders show, under their account, and accounts without a
// match are left out.
func (m Model) rows(width int) []row {
	if m.query() != "" {
		return m.filteredRows(width)
	}
	var rows []row
	for i, a := range m.accounts {
		if i > 0 {
			rows = append(rows, row{ref: noRef})
		}
		rows = append(rows, row{text: m.st.header.Render(ansi.Truncate(a.name, width, "…")), ref: noRef, header: true})
		switch a.state {
		case connecting:
			rows = append(rows, row{text: "  " + m.spinner.View() + m.st.dim.Render(" connecting…"), ref: noRef})
		case failed:
			wrapped := lipgloss.NewStyle().Width(max(width-2, 1)).Render(a.err.Error())
			lines := strings.Split(wrapped, "\n")
			if len(lines) > maxErrorLines {
				lines = lines[:maxErrorLines]
				lines[maxErrorLines-1] = ansi.Truncate(lines[maxErrorLines-1]+"…", width-2, "…")
			}
			for _, l := range lines {
				rows = append(rows, row{text: "  " + m.st.err.Render(strings.TrimRight(l, " ")), ref: noRef})
			}
		case ready:
			if len(a.folders) == 0 {
				rows = append(rows, row{text: m.st.dim.Render("  no folders"), ref: noRef})
			}
			for _, f := range a.folders {
				r := ref{i, f.Name}
				rows = append(rows, row{text: m.folderRow(f, r, width), ref: r})
			}
		}
	}
	return rows
}

func (m Model) filteredRows(width int) []row {
	var rows []row
	for i, a := range m.accounts {
		var folders []row
		for _, f := range a.folders {
			if m.matches(f) {
				r := ref{i, f.Name}
				folders = append(folders, row{text: m.folderRow(f, r, width), ref: r})
			}
		}
		if len(folders) == 0 {
			continue
		}
		if len(rows) > 0 {
			rows = append(rows, row{ref: noRef})
		}
		rows = append(rows, row{text: m.st.header.Render(ansi.Truncate(a.name, width, "…")), ref: noRef, header: true})
		rows = append(rows, folders...)
	}
	if len(rows) == 0 {
		rows = []row{{text: m.st.dim.Render("No folders match"), ref: noRef}}
	}
	return rows
}

func (m Model) folderRow(f mail.Folder, r ref, width int) string {
	marker := "  "
	if r == m.open {
		marker = m.st.open.Render("▶ ")
	}
	count := ""
	if f.Unread > 0 {
		count = fmt.Sprint(f.Unread)
	}
	labelW := max(width-2-len(count)-1, 1)
	label := ansi.Truncate(Label(f), labelW, "…")
	gap := strings.Repeat(" ", max(width-2-ansi.StringWidth(label)-len(count), 1))

	text := label
	if f.Unread > 0 {
		text = m.st.unread.Render(label)
	}
	line := text + gap + m.st.count.Render(count)
	if r == m.cursor {
		line = m.st.cursor.Bold(f.Unread > 0).Render(label + gap + count)
	}
	return marker + line
}

// Label is how a folder is shown: "Inbox" for INBOX, otherwise the last
// part of its path, so "[Gmail]/Sent Mail" shows as "Sent Mail".
func Label(f mail.Folder) string {
	if f.Role == mail.RoleInbox {
		return "Inbox"
	}
	if f.Delimiter != "" {
		if i := strings.LastIndex(f.Name, f.Delimiter); i >= 0 && i+len(f.Delimiter) < len(f.Name) {
			return f.Name[i+len(f.Delimiter):]
		}
	}
	return f.Name
}

// View renders exactly the pane's inner area: at most height lines, each at
// most width cells wide.
func (m Model) View(width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	var lines []string
	if m.filter.Focused() {
		f := m.filter
		f.SetWidth(max(width-ansi.StringWidth(f.Prompt)-1, 1))
		lines = append(lines, ansi.Truncate(f.View(), width, "…"))
		height--
	}
	rows := m.rows(width)
	cur := cursorRow(rows, m.cursor)
	offset := clampOffset(m.offset, cur, headerRow(rows, cur), height, len(rows))
	end := min(offset+max(height, 0), len(rows))
	for _, r := range rows[offset:end] {
		lines = append(lines, ansi.Truncate(r.text, width, ""))
	}
	return strings.Join(lines, "\n")
}

func (m *Model) ensureVisible() {
	h := m.height
	if m.filter.Focused() {
		h--
	}
	if h <= 0 {
		return
	}
	rows := m.rows(1)
	cur := cursorRow(rows, m.cursor)
	m.offset = clampOffset(m.offset, cur, headerRow(rows, cur), h, len(rows))
}

func cursorRow(rows []row, c ref) int {
	if c == noRef {
		return -1
	}
	return slices.IndexFunc(rows, func(r row) bool { return r.ref == c })
}

// headerRow is the row of the account name above the cursor row, or -1.
func headerRow(rows []row, cursor int) int {
	for i := cursor; i >= 0; i-- {
		if rows[i].header {
			return i
		}
	}
	return -1
}

// clampOffset scrolls just enough to show the cursor row, and the cursor's
// account name (header) too whenever both fit.
func clampOffset(offset, cursor, header, height, total int) int {
	if cursor >= 0 {
		top := cursor
		if header >= 0 && cursor-header < height {
			top = header
		}
		if top < offset {
			offset = top
		}
		if cursor >= offset+height {
			offset = cursor - height + 1
		}
	}
	offset = min(offset, max(total-height, 0))
	return max(offset, 0)
}

var roleOrder = map[mail.Role]int{
	mail.RoleInbox:   0,
	mail.RoleDrafts:  1,
	mail.RoleSent:    2,
	mail.RoleArchive: 3,
	mail.RoleAll:     4,
	mail.RoleTrash:   5,
	mail.RoleJunk:    6,
}

func compareFolders(a, b mail.Folder) int {
	ra, okA := roleOrder[a.Role]
	rb, okB := roleOrder[b.Role]
	switch {
	case okA && okB:
		return cmp.Compare(ra, rb)
	case okA:
		return -1
	case okB:
		return 1
	}
	return cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
}
