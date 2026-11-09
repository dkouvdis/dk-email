// Package ui is the root Bubble Tea model: layout, focus, keys, the status
// line, the hint bar and popups. The thread list and reader are placeholders
// until T10–T12.
package ui

import (
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/dkouvdis/dk-email/internal/config"
	"github.com/dkouvdis/dk-email/internal/mail"
	"github.com/dkouvdis/dk-email/internal/ui/popup"
	"github.com/dkouvdis/dk-email/internal/ui/sidebar"
)

// Below this size only a "too small" notice is drawn.
const (
	minWidth  = 20
	minHeight = 6
)

// Pane identifies a focusable pane.
type Pane int

const (
	PaneSidebar Pane = iota
	PaneList
	PaneReader
)

func (p Pane) String() string {
	switch p {
	case PaneSidebar:
		return "sidebar"
	case PaneList:
		return "list"
	case PaneReader:
		return "reader"
	}
	return fmt.Sprintf("pane(%d)", int(p))
}

type Model struct {
	ui      config.UI
	accts   []config.Account
	connect Connector
	// boxes holds each account's mailbox once connected, by account index.
	boxes []mail.Mailbox
	side  sidebar.Model
	open  openFolder

	log           *slog.Logger
	keys          KeyMap
	styles        styles
	help          help.Model
	helpPop       popup.Help
	width, height int
	mode          Mode
	panes         Panes
	focus         Pane
	showHelp      bool
	status        string
	statusErr     bool
}

// New builds the root model. Init connects every account with connect. log
// receives metadata only and may be nil.
func New(cfg config.Config, connect Connector, log *slog.Logger) Model {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	mode := ModeSplit
	if cfg.UI.DefaultLayout == config.LayoutList {
		mode = ModeList
	}
	names := make([]string, 0, len(cfg.Accounts))
	for _, a := range cfg.Accounts {
		names = append(names, a.Name)
	}
	keys := DefaultKeyMap()
	m := Model{
		ui:      cfg.UI,
		accts:   slices.Clone(cfg.Accounts),
		connect: connect,
		boxes:   make([]mail.Mailbox, len(cfg.Accounts)),
		side:    sidebar.New(names),
		open:    openFolder{account: -1},
		log:     log,
		keys:    keys,
		styles:  defaultStyles(),
		help:    help.New(),
		helpPop: popup.NewHelp(keys.HelpSections()),
		mode:    mode,
		focus:   PaneList,
	}
	if len(names) == 0 {
		m.status = "No accounts configured yet."
	}
	log.Debug("start", "accounts", len(names), "mode", mode)
	return m
}

// Init connects every account at once; each reports back on its own.
func (m Model) Init() tea.Cmd {
	if len(m.accts) == 0 || m.connect == nil {
		return nil
	}
	cmds := []tea.Cmd{m.side.Tick()}
	for i, a := range m.accts {
		m.log.Debug("connecting", "account", a.Name)
		cmds = append(cmds, connectAccount(m.connect, i, a))
	}
	return tea.Batch(cmds...)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.resize(msg.Width, msg.Height)
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.side, cmd = m.side.Update(msg)
		return m, cmd
	case accountMsg:
		return m.handleAccount(msg)
	case sidebar.SelectedMsg:
		return m.openFolder(msg.Account, msg.Folder)
	case summariesMsg:
		return m.handleSummaries(msg)
	}
	return m, nil
}

func (m *Model) resize(w, h int) {
	prev, first := m.panes.Kind, m.width == 0 && m.height == 0
	m.width, m.height = w, h
	m.relayout()
	if first || m.panes.Kind != prev {
		m.log.Debug("layout", "kind", m.panes.Kind, "width", w, "height", h)
	}
}

func (m *Model) relayout() {
	m.panes = Layout(m.width, m.height, m.mode, m.ui)
	m.helpPop.SetSize(m.panes.Popup.W, m.panes.Popup.H)
	m.side = m.side.SetHeight(max(m.panes.Sidebar.H-m.styles.pane.GetVerticalFrameSize(), 0))
	if !slices.Contains(m.visiblePanes(), m.focus) {
		m.focus = PaneList
	}
}

func (m Model) visiblePanes() []Pane {
	switch m.panes.Kind {
	case KindWide:
		return []Pane{PaneSidebar, PaneList, PaneReader}
	case KindNarrow:
		return []Pane{PaneList, PaneReader}
	}
	return []Pane{PaneList}
}

func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, m.keys.Quit) {
		m.log.Debug("quit")
		return m, tea.Quit
	}
	if m.statusErr {
		m.status, m.statusErr = "", false
	}
	if m.showHelp {
		if key.Matches(msg, m.keys.Back, m.keys.Help) {
			m.showHelp = false
			m.log.Debug("popup closed", "popup", "help")
			return m, nil
		}
		m.helpPop = m.helpPop.Update(msg)
		return m, nil
	}

	switch {
	case key.Matches(msg, m.keys.Help):
		m.showHelp = true
		m.helpPop.Reset()
		m.log.Debug("popup opened", "popup", "help")
	case key.Matches(msg, m.keys.NextPane):
		m.moveFocus(1, true)
	case key.Matches(msg, m.keys.PrevPane):
		m.moveFocus(-1, true)
	case key.Matches(msg, m.keys.Right):
		m.moveFocus(1, false)
	case key.Matches(msg, m.keys.Left):
		m.moveFocus(-1, false)
	case key.Matches(msg, m.keys.ToggleView):
		m.toggleMode()
	case m.focus == PaneSidebar:
		var cmd tea.Cmd
		m.side, cmd = m.side.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *Model) moveFocus(step int, wrap bool) {
	panes := m.visiblePanes()
	i := slices.Index(panes, m.focus) + step
	switch {
	case wrap:
		i = (i + len(panes)) % len(panes)
	case i < 0 || i >= len(panes):
		return
	}
	if panes[i] != m.focus {
		m.focus = panes[i]
		m.log.Debug("focus", "pane", m.focus)
	}
}

func (m *Model) toggleMode() {
	if m.width < listForcedBelow {
		m.setStatus(fmt.Sprintf("List mode is forced below %d columns.", listForcedBelow))
		return
	}
	if m.mode == ModeSplit {
		m.mode = ModeList
	} else {
		m.mode = ModeSplit
	}
	m.relayout()
	m.log.Debug("mode", "mode", m.mode, "kind", m.panes.Kind)
}

func (m Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	return v
}

func (m Model) render() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}
	if m.width < minWidth || m.height < minHeight {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, "Terminal too small")
	}

	p := m.panes
	var cols []string
	if p.Sidebar.W > 0 {
		cols = append(cols, m.renderPane(PaneSidebar, p.Sidebar, m.side.View))
	}
	cols = append(cols, m.renderPane(PaneList, p.List, lines(m.listLines())))
	if p.Reader.W > 0 {
		cols = append(cols, m.renderPane(PaneReader, p.Reader, lines(m.readerLines())))
	}
	screen := lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.JoinHorizontal(lipgloss.Top, cols...),
		m.renderStatus(),
		m.renderHints(),
	)

	if m.showHelp {
		if pop := m.helpPop.View(); pop != "" {
			body := m.height - footerRows
			x := max((m.width-lipgloss.Width(pop))/2, 0)
			y := max((body-lipgloss.Height(pop))/2, 0)
			screen = lipgloss.NewCompositor(
				lipgloss.NewLayer(screen),
				lipgloss.NewLayer(pop).X(x).Y(y).Z(1),
			).Render()
		}
	}
	return screen
}

// renderPane draws a bordered pane whose content is rendered for its inner size.
func (m Model) renderPane(pane Pane, r Rect, content func(w, h int) string) string {
	style := m.styles.pane
	if pane == m.focus {
		style = m.styles.paneFocused
	}
	innerW := max(r.W-style.GetHorizontalFrameSize(), 0)
	innerH := max(r.H-style.GetVerticalFrameSize(), 0)
	body := fitLines(strings.Split(content(innerW, innerH), "\n"), innerW, innerH)
	return style.Width(r.W).Height(r.H).Render(body)
}

func lines(l []string) func(w, h int) string {
	return func(int, int) string { return strings.Join(l, "\n") }
}

// listLines shows the account › folder header outside wide mode, where
// there's no sidebar to show it.
func (m Model) listLines() []string {
	title := m.styles.title.Render("Threads")
	if m.panes.Kind != KindWide {
		switch {
		case m.open.account >= 0:
			title = m.styles.title.Render(m.accts[m.open.account].Name) + m.styles.dim.Render(" › "+m.open.label)
		case len(m.accts) > 0:
			title = m.styles.title.Render(m.accts[0].Name) + m.styles.dim.Render(" › no folder")
		default:
			title = m.styles.title.Render("No account")
		}
	}
	body := "Threads appear here once a folder is open."
	switch {
	case m.open.account < 0:
	case m.open.loading:
		body = "Loading…"
	default:
		body = fmt.Sprintf("%d messages loaded.", len(m.open.summaries))
	}
	return []string{title, "", m.styles.dim.Render(body)}
}

func (m Model) readerLines() []string {
	return []string{m.styles.title.Render("Message"), "", m.styles.dim.Render("No message selected.")}
}

func (m Model) renderStatus() string {
	right := m.panes.Kind.String()
	if m.mode == ModeSplit && m.panes.Kind == KindList {
		right += " (forced)"
	}
	right = m.styles.dim.Render(right)
	style := m.styles.status
	if m.statusErr {
		style = m.styles.statusErr
	}
	left := ansi.Truncate(style.Render(m.status), m.width-lipgloss.Width(right)-1, "…")
	gap := max(m.width-lipgloss.Width(left)-lipgloss.Width(right), 0)
	return left + strings.Repeat(" ", gap) + right
}

// renderHints fits as many of the focused pane's keys as the width allows,
// marks a cut with …, and always ends with ? help • q quit.
func (m Model) renderHints() string {
	st := m.help.Styles
	sep := st.ShortSeparator.Render(m.help.ShortSeparator)
	item := func(b key.Binding) string {
		return st.ShortKey.Render(b.Help().Key) + " " + st.ShortDesc.Render(b.Help().Desc)
	}
	tail := item(m.keys.Help) + sep + item(m.keys.Quit)
	room := m.width - lipgloss.Width(tail)

	var head []string
	used := 0
	hints := m.keys.hints(m.focus, m.panes.Kind)
	for i, b := range hints {
		w := lipgloss.Width(item(b)) + lipgloss.Width(sep)
		// Unless this is the last key, leave space for a "… • " marker.
		reserve := 0
		if i < len(hints)-1 {
			reserve = 1 + lipgloss.Width(sep)
		}
		if used+w+reserve > room {
			head = append(head, st.Ellipsis.Render("…"))
			break
		}
		head = append(head, item(b))
		used += w
	}
	line := strings.Join(append(head, tail), sep)
	line = ansi.Truncate(line, m.width, "")
	return line + strings.Repeat(" ", max(m.width-lipgloss.Width(line), 0))
}

// setStatus shows progress or info. It never replaces an error, which stays
// until the next key press dismisses it.
func (m *Model) setStatus(s string) {
	if !m.statusErr {
		m.status = s
	}
}

// setError shows err in the status line. Only its text is shown, which
// never includes passwords (see secret and imap).
func (m *Model) setError(err error) {
	m.status, m.statusErr = err.Error(), true
}

// fitLines cuts content to w columns and h lines so a pane never grows.
func fitLines(lines []string, w, h int) string {
	if len(lines) > h {
		lines = lines[:max(h, 0)]
	}
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = ansi.Truncate(l, max(w, 0), "…")
	}
	return strings.Join(out, "\n")
}
