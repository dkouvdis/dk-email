// Package reader shows a conversation: messages stacked oldest to newest,
// the first unread (or latest) one expanded with its headers and body, and
// the rest collapsed to one line each. It never fetches; the root model
// asks Missing for bodies to load and passes them in with SetMessage.
package reader

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/charmbracelet/x/ansi"
	"github.com/dkouvdis/dk-email/internal/ui/theme"

	"github.com/dkouvdis/dk-email/internal/mail"
	"github.com/dkouvdis/dk-email/internal/render"
	"github.com/dkouvdis/dk-email/internal/thread"
)

type loadState int

const (
	notLoaded loadState = iota
	loading
	loaded
	failed
)

type body struct {
	state loadState
	msg   mail.Message
	err   error
}

type keys struct{ up, down, pgUp, pgDown, top, bottom, expandAll key.Binding }

type styles struct{ subject, label, sender, dim, dot, star, sep, err lipgloss.Style }

type Model struct {
	theme theme.Theme
	now   func() time.Time

	open     bool
	account  int
	folder   string
	th       thread.Thread
	defaults map[mail.UID]bool // expanded when the thread was opened
	expanded map[mail.UID]bool
	bodies   map[mail.UID]body

	width, height, offset int
	keys                  keys
	st                    styles
}

// New returns an empty reader. now is used for the date's time zone.
func New(th theme.Theme, now func() time.Time) Model {
	return Model{
		theme: th,
		now:   now,
		keys: keys{
			up:        key.NewBinding(key.WithKeys("k", "up")),
			down:      key.NewBinding(key.WithKeys("j", "down")),
			pgUp:      key.NewBinding(key.WithKeys("pgup", "ctrl+u")),
			pgDown:    key.NewBinding(key.WithKeys("pgdown", "ctrl+d")),
			top:       key.NewBinding(key.WithKeys("g", "home")),
			bottom:    key.NewBinding(key.WithKeys("G", "end")),
			expandAll: key.NewBinding(key.WithKeys("space")),
		},
		st: styles{
			subject: lipgloss.NewStyle().Bold(true),
			label:   th.Muted,
			sender:  lipgloss.NewStyle().Bold(true),
			dim:     th.Muted,
			dot:     th.Unread,
			star:    th.Star,
			sep:     lipgloss.NewStyle().Foreground(th.BorderColor),
			err:     th.Error,
		},
	}
}

// Open shows a thread. focus, if non-zero, is the message to expand;
// otherwise the oldest unread message is, or the latest if all are read.
func (m Model) Open(account int, folder string, th thread.Thread, focus mail.UID) Model {
	m.open, m.account, m.folder, m.th = true, account, folder, th
	m.bodies = map[mail.UID]body{}
	m.offset = 0

	expand := focus
	if expand == 0 || !m.has(expand) {
		expand = th.Latest().UID
		if i := slices.IndexFunc(th.Messages, func(s mail.Summary) bool { return !s.Seen }); i >= 0 {
			expand = th.Messages[i].UID
		}
	}
	m.defaults = map[mail.UID]bool{expand: true}
	m.expanded = maps.Clone(m.defaults)
	return m
}

// Close empties the reader, e.g. when another folder opens.
func (m Model) Close() Model {
	return New(m.theme, m.now).SetSize(m.width, m.height)
}

func (m Model) IsOpen() bool { return m.open }

// Showing reports whether uid belongs to the open thread of that folder.
func (m Model) Showing(account int, folder string, uid mail.UID) bool {
	return m.open && m.account == account && m.folder == folder && m.has(uid)
}

func (m Model) has(uid mail.UID) bool {
	return slices.ContainsFunc(m.th.Messages, func(s mail.Summary) bool { return s.UID == uid })
}

// Unread reports whether a message in the thread is unread.
func (m Model) Unread(uid mail.UID) bool {
	i := slices.IndexFunc(m.th.Messages, func(s mail.Summary) bool { return s.UID == uid })
	return i >= 0 && !m.th.Messages[i].Seen
}

// Missing lists expanded messages whose bodies haven't been requested.
func (m Model) Missing() []mail.UID {
	var out []mail.UID
	for _, s := range m.th.Messages {
		if m.expanded[s.UID] && m.bodies[s.UID].state == notLoaded {
			out = append(out, s.UID)
		}
	}
	return out
}

// MarkLoading records that bodies have been requested.
func (m Model) MarkLoading(uids []mail.UID) Model {
	m.bodies = maps.Clone(m.bodies)
	for _, uid := range uids {
		m.bodies[uid] = body{state: loading}
	}
	return m
}

// SetMessage stores a fetched body, or the error fetching it.
func (m Model) SetMessage(uid mail.UID, msg mail.Message, err error) Model {
	if !m.has(uid) {
		return m
	}
	m.bodies = maps.Clone(m.bodies)
	if err != nil {
		m.bodies[uid] = body{state: failed, err: err}
	} else {
		m.bodies[uid] = body{state: loaded, msg: msg}
	}
	return m
}

// SetSeen updates read markers after a flag change.
func (m Model) SetSeen(uids []mail.UID, seen bool) Model {
	return m.update(uids, func(s *mail.Summary) { s.Seen = seen })
}

// SetFlagged updates star markers after a flag change.
func (m Model) SetFlagged(uids []mail.UID, flagged bool) Model {
	return m.update(uids, func(s *mail.Summary) { s.Flagged = flagged })
}

func (m Model) update(uids []mail.UID, fn func(*mail.Summary)) Model {
	msgs := slices.Clone(m.th.Messages)
	for i := range msgs {
		if slices.Contains(uids, msgs[i].UID) {
			fn(&msgs[i])
		}
	}
	m.th = thread.Thread{Messages: msgs}
	return m
}

// Current is the message the reader is on: the one expanded when the
// thread opened. Triage keys in the reader act on it. It's 0 when closed.
func (m Model) Current() mail.UID {
	for _, s := range m.th.Messages {
		if m.defaults[s.UID] {
			return s.UID
		}
	}
	return 0
}

// Remove drops messages that were moved away. If the current message goes,
// the next one (or the last) becomes current; with none left the reader
// closes.
func (m Model) Remove(uids []mail.UID) Model {
	if !m.open {
		return m
	}
	cur := slices.IndexFunc(m.th.Messages, func(s mail.Summary) bool { return s.UID == m.Current() })
	msgs := slices.DeleteFunc(slices.Clone(m.th.Messages), func(s mail.Summary) bool { return slices.Contains(uids, s.UID) })
	if len(msgs) == 0 {
		return m.Close()
	}
	m.th = thread.Thread{Messages: msgs}
	if m.Current() == 0 {
		next := msgs[min(max(cur, 0), len(msgs)-1)].UID
		m.defaults = map[mail.UID]bool{next: true}
		m.expanded = maps.Clone(m.expanded)
		m.expanded[next] = true
	}
	m.offset = m.clamp(m.offset)
	return m
}

// SetSize records the pane's inner size, for wrapping and scrolling.
func (m Model) SetSize(w, h int) Model {
	m.width, m.height = w, h
	m.offset = m.clamp(m.offset)
	return m
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok || !m.open {
		return m, nil
	}
	page := max(m.height-1, 1)
	switch {
	case key.Matches(k, m.keys.up):
		m.offset--
	case key.Matches(k, m.keys.down):
		m.offset++
	case key.Matches(k, m.keys.pgUp):
		m.offset -= page
	case key.Matches(k, m.keys.pgDown):
		m.offset += page
	case key.Matches(k, m.keys.top):
		m.offset = 0
	case key.Matches(k, m.keys.bottom):
		m.offset = len(m.lines(m.width))
	case key.Matches(k, m.keys.expandAll):
		m.toggleAll()
	}
	m.offset = m.clamp(m.offset)
	return m, nil
}

// toggleAll expands every message, or goes back to the default if all are.
func (m *Model) toggleAll() {
	all := !slices.ContainsFunc(m.th.Messages, func(s mail.Summary) bool { return !m.expanded[s.UID] })
	if all {
		m.expanded = maps.Clone(m.defaults)
		return
	}
	m.expanded = map[mail.UID]bool{}
	for _, s := range m.th.Messages {
		m.expanded[s.UID] = true
	}
}

func (m Model) clamp(offset int) int {
	if m.height <= 0 {
		return 0
	}
	return max(min(offset, len(m.lines(m.width))-m.height), 0)
}

// View renders at most height lines, none wider than width.
func (m Model) View(width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	if !m.open {
		return m.st.dim.Render("No message selected.")
	}
	lines := m.lines(width)
	offset := max(min(m.offset, len(lines)-height), 0)
	lines = lines[offset:min(offset+height, len(lines))]
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, width, "")
	}
	return strings.Join(lines, "\n")
}

func (m Model) lines(width int) []string {
	if !m.open || width <= 0 {
		return nil
	}
	out := wrapStyled(m.st.subject, subjectOf(m.th.Latest()), width)
	out = append(out, "")
	for i, s := range m.th.Messages {
		if i > 0 {
			out = append(out, m.st.sep.Render(strings.Repeat("─", width)))
		}
		if m.expanded[s.UID] {
			out = append(out, m.expandedLines(s, width)...)
		} else {
			out = append(out, m.collapsedLine(s))
		}
	}
	return out
}

func (m Model) collapsedLine(s mail.Summary) string {
	dot := " "
	if !s.Seen {
		dot = m.st.dot.Render("●")
	}
	return dot + " " + m.st.sender.Render(s.From.DisplayName()) + m.star(s) + "  " + m.st.dim.Render(m.longDate(s.Date))
}

func (m Model) star(s mail.Summary) string {
	if !s.Flagged {
		return ""
	}
	return " " + m.st.star.Render("*")
}

func (m Model) expandedLines(s mail.Summary, width int) []string {
	b := m.bodies[s.UID]
	to, cc := s.To, []mail.Address(nil)
	if b.state == loaded {
		to, cc = b.msg.To, b.msg.Cc
	}

	out := m.header("From", s.From.String()+m.star(s), width)
	if len(to) > 0 {
		out = append(out, m.header("To", joinAddresses(to), width)...)
	}
	if len(cc) > 0 {
		out = append(out, m.header("Cc", joinAddresses(cc), width)...)
	}
	out = append(out, m.header("Date", m.longDate(s.Date), width)...)
	out = append(out, "")

	switch b.state {
	case notLoaded, loading:
		return append(out, m.st.dim.Render("Loading…"))
	case failed:
		return append(out, wrapStyled(m.st.err, "Couldn't load this message: "+b.err.Error(), width)...)
	}
	text := b.msg.Body
	if text == "" {
		text = "(no text)"
	}
	out = append(out, strings.Split(render.Wrap(text, width), "\n")...)
	if len(b.msg.Attachments) > 0 {
		out = append(out, "", m.st.label.Render("Attachments:"))
		for _, a := range b.msg.Attachments {
			out = append(out, "  "+a.Name+m.st.dim.Render(fmt.Sprintf("  %s, %s", a.ContentType, formatSize(a.Size))))
		}
	}
	return out
}

// header renders "Label: value" with continuation lines aligned under the value.
func (m Model) header(label, value string, width int) []string {
	prefix := fmt.Sprintf("%-5s ", label+":")
	wrapped := strings.Split(render.Wrap(value, max(width-len(prefix), 1)), "\n")
	for i := range wrapped {
		if i == 0 {
			wrapped[i] = m.st.label.Render(prefix) + wrapped[i]
		} else {
			wrapped[i] = strings.Repeat(" ", len(prefix)) + wrapped[i]
		}
	}
	return wrapped
}

func (m Model) longDate(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.In(m.now().Location()).Format("Mon 2 Jan 2006 15:04")
}

func subjectOf(s mail.Summary) string {
	if strings.TrimSpace(s.Subject) == "" {
		return "(no subject)"
	}
	return s.Subject
}

func joinAddresses(as []mail.Address) string {
	parts := make([]string, len(as))
	for i, a := range as {
		parts[i] = a.String()
	}
	return strings.Join(parts, ", ")
}

func wrapStyled(st lipgloss.Style, text string, width int) []string {
	lines := strings.Split(render.Wrap(text, width), "\n")
	for i := range lines {
		lines[i] = st.Render(lines[i])
	}
	return lines
}

func formatSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}
