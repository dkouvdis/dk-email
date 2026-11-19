// Package ui is the root Bubble Tea model: layout, focus, keys, the status
// line, the hint bar and popups.
package ui

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/dkouvdis/dk-email/internal/config"
	"github.com/dkouvdis/dk-email/internal/download"
	"github.com/dkouvdis/dk-email/internal/mail"
	"github.com/dkouvdis/dk-email/internal/notify"
	"github.com/dkouvdis/dk-email/internal/ui/popup"
	"github.com/dkouvdis/dk-email/internal/ui/reader"
	"github.com/dkouvdis/dk-email/internal/ui/sidebar"
	"github.com/dkouvdis/dk-email/internal/ui/theme"
	"github.com/dkouvdis/dk-email/internal/ui/threadlist"
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
)

func (p Pane) String() string {
	switch p {
	case PaneSidebar:
		return "sidebar"
	case PaneList:
		return "list"
	}
	return fmt.Sprintf("pane(%d)", int(p))
}

type Model struct {
	ui     config.UI
	notify config.Notify
	// inbox counts new INBOX mail per account from server results.
	inbox   *notify.Tracker
	tmux    bool // inside tmux, so OSC 9 needs passthrough
	accts   []config.Account
	connect Connector
	// boxes holds each account's mailbox once connected, by account index.
	boxes []mail.Mailbox
	// senders is parallel to boxes; nil means sending isn't set up.
	senders []mail.Sender
	side    sidebar.Model
	open    openFolder
	// every schedules the next refresh tick; tests replace it.
	every   func(time.Duration) tea.Cmd
	refresh refreshState
	// writes counts flag changes and moves started; inflight counts those
	// still waiting for the server. A refresh uses them to spot results
	// that could undo an optimistic change.
	writes, inflight int
	list             threadlist.Model
	search           listSearch
	read             reader.Model

	log           *slog.Logger
	keys          KeyMap
	theme         theme.Theme
	styles        styles
	helpPop       popup.Help
	width, height int
	mode          Mode
	panes         Panes
	focus         Pane
	showHelp      bool
	showPicker    bool
	// readerPopup shows the reader in a popup over the list (list mode).
	readerPopup bool
	picker      popup.Picker[pick]
	// The connect popup: connectFor is the account a password is being
	// entered for, or -1 for a new account.
	onboard     Onboarding
	showConnect bool
	connectPop  popup.Connect
	connectFor  int
	connectGen  int
	// connectCancel stops the connect or edit test now running, if any.
	connectCancel context.CancelFunc
	// editFor is the account the connect popup is editing, or -1.
	editFor int
	// ids gives each account a number that never changes or repeats, so a
	// slow connect or refresh result still finds its account after another
	// one was deleted (indexes shift) or this one was edited.
	ids           []int
	nextID        int
	needsPassword map[int]bool
	// moveTargets is set while the picker chooses where to move them.
	moveTargets []mail.Summary
	// pickingAttachment is set while the picker chooses an attachment to open.
	pickingAttachment bool
	// files saves and opens attachments.
	files     fileOpener
	status    string
	statusErr bool
	// draft is the message being composed, nil when there is none; sending
	// is set while it is being delivered and saving while it is being saved
	// to Drafts. The composer popup edits draft.
	draft   *draftState
	sending bool
	saving  bool
	// cancelSave ends the save in flight.
	cancelSave context.CancelFunc
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
	th := theme.New(cfg.Colors)
	m := Model{
		ui:            cfg.UI,
		notify:        cfg.Notify,
		inbox:         notify.NewTracker(),
		tmux:          os.Getenv("TMUX") != "",
		accts:         slices.Clone(cfg.Accounts),
		connect:       connect,
		boxes:         make([]mail.Mailbox, len(cfg.Accounts)),
		senders:       make([]mail.Sender, len(cfg.Accounts)),
		side:          sidebar.New(th, names),
		open:          openFolder{account: -1},
		every:         tick,
		needsPassword: map[int]bool{},
		connectFor:    -1,
		editFor:       -1,
		list:          threadlist.New(th, time.Now),
		read:          reader.New(th, time.Now),
		log:           log,
		keys:          keys,
		theme:         th,
		styles:        newStyles(th),
		helpPop:       popup.NewHelp(th, keys.HelpSections()),
		mode:          mode,
		focus:         PaneList,
		files:         fileOpener{dir: download.Dir, canOpen: func() bool { return false }},
	}
	m.search.input = textinput.New()
	m.search.input.Prompt = "/ "
	m.search.input.Placeholder = "from:  subject:  is:unread  since:  -word"
	for range names {
		m.ids = append(m.ids, m.newID())
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
	cmds := []tea.Cmd{m.side.Tick(), m.every(m.ui.RefreshInterval.Duration)}
	for i, a := range m.accts {
		m.log.Debug("connecting", "account", a.Name)
		cmds = append(cmds, connectAccount(m.connect, i, m.ids[i], a))
	}
	return tea.Batch(cmds...)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.resize(msg.Width, msg.Height)
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	case tea.MouseClickMsg:
		return m.handleClick(tea.Mouse(msg))
	case tea.MouseWheelMsg:
		return m.handleWheel(tea.Mouse(msg))
	case tea.PasteMsg:
		switch {
		case m.composing():
			if m.draft.pop.TakesPaste() {
				if paths, ok := parseDropped(msg.Content, runtime.GOOS); ok {
					m.log.Debug("paste looks like a drop", "files", len(paths))
					return m, readDropped(paths, msg)
				}
			}
			return m.updateComposer(msg)
		case m.showConnect:
			m.connectPop = m.connectPop.Paste(msg)
		case m.showHelp:
			m.helpPop = m.helpPop.Paste(msg)
		case m.showPicker:
			m.picker = m.picker.Paste(msg)
		case m.search.input.Focused():
			content := strings.Join(strings.Fields(msg.Content), " ")
			m.search.input, _ = m.search.input.Update(tea.PasteMsg{Content: content})
		case m.focus == PaneSidebar:
			m.side = m.side.Paste(msg)
		}
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
	case searchMsg:
		return m.handleSearch(msg)
	case threadlist.LoadMoreMsg:
		return m.loadMore(msg.Before)
	case messageMsg:
		return m.handleMessage(msg)
	case flagMsg:
		return m.handleFlag(msg)
	case moveMsg:
		return m.handleMove(msg)
	case refreshTickMsg:
		return m.handleTick()
	case refreshMsg:
		return m.handleRefresh(msg)
	case connectedMsg:
		return m.handleConnected(msg)
	case signInProgressMsg:
		return m.handleSignInProgress(msg)
	case editedMsg:
		return m.handleEdited(msg)
	case removedMsg:
		return m.handleRemoved(msg)
	case sentMsg:
		return m.handleSent(msg)
	case draftSavedMsg:
		return m.handleDraftSaved(msg)
	case replyFetchedMsg:
		return m.handleReplyFetched(msg)
	case draftOpenedMsg:
		return m.handleDraftOpened(msg)
	case attachmentMsg:
		return m.handleAttachment(msg)
	case completedMsg:
		return m.handleCompleted(msg)
	case attachReadMsg:
		return m.handleAttachRead(msg)
	case droppedMsg:
		return m.handleDropped(msg)
	case dropFallbackMsg:
		return m.handleDropFallback(msg)
	default:
		// Whatever else the composer's inputs wait for, such as a clipboard read.
		if m.composing() {
			return m.updateComposer(msg)
		}
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
	m.panes = Layout(m.width, m.height, m.mode)
	m.helpPop.SetSize(m.panes.Popup.W, m.panes.Popup.H)
	m.picker.SetSize(m.panes.Popup.W, m.panes.Popup.H)
	m.connectPop.SetSize(m.panes.Popup.W, m.panes.Popup.H)
	if m.composing() {
		m.draft.pop.SetSize(m.panes.Popup.W, m.panes.Popup.H)
	}
	frame := m.styles.pane.GetVerticalFrameSize()
	m.side = m.side.SetSize(max(m.panes.Sidebar.W-m.styles.pane.GetHorizontalFrameSize(), 0), max(m.panes.Sidebar.H-frame, 0))
	m.list = m.list.SetHeight(max(m.panes.List.H-frame-listHeaderRows, 0))
	m.read = m.read.SetSize(popup.FrameInner(m.panes.Popup.W, m.panes.Popup.H))
	if !slices.Contains(m.visiblePanes(), m.focus) {
		m.focus = PaneList
	}
}

func (m Model) visiblePanes() []Pane {
	if m.panes.Kind == KindNarrow {
		return []Pane{PaneSidebar, PaneList}
	}
	return []Pane{PaneList}
}

func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, m.keys.ForceQuit) {
		m.log.Debug("quit")
		return m, tea.Quit
	}
	// The composer takes every key, so nothing behind it can act.
	if m.composing() {
		return m.updateComposer(msg)
	}
	// The connect form takes every key, so q can be typed into it.
	if m.showConnect {
		return m.handleConnectKey(msg)
	}
	// Help sees every key, so q and ? can be typed into its filter.
	if m.showHelp {
		var r popup.HelpResult
		m.helpPop, r = m.helpPop.Update(msg)
		if r == popup.HelpClosed {
			m.showHelp = false
			m.log.Debug("popup closed", "popup", "help")
		}
		return m, nil
	}
	// A folder filter takes every key too, q included.
	if m.showPicker && m.picker.Filtering() {
		return m.handlePickerKey(msg)
	}
	if m.focus == PaneSidebar && m.side.Filtering() && !m.popupOpen() {
		var cmd tea.Cmd
		m.side, cmd = m.side.Update(msg)
		return m, cmd
	}
	// The list search prompt takes every key as well.
	if m.search.input.Focused() && !m.popupOpen() {
		return m.handleSearchKey(msg)
	}
	// q closes the topmost popup before it quits.
	if key.Matches(msg, m.keys.Quit) {
		if m.closeTopPopup() {
			return m, nil
		}
		m.log.Debug("quit")
		return m, tea.Quit
	}
	if m.statusErr {
		m.status, m.statusErr = "", false
	}
	if m.showPicker {
		return m.handlePickerKey(msg)
	}
	if m.readerPopup {
		switch {
		case key.Matches(msg, m.keys.Back):
			m.readerPopup = false
			m.log.Debug("popup closed", "popup", "reader")
			return m, nil
		case key.Matches(msg, m.keys.Help):
			m.showHelp = true
			m.helpPop.Reset()
			return m, nil
		case key.Matches(msg, m.keys.Picker):
			return m.openPicker()
		case key.Matches(msg, m.keys.Compose):
			return m.startCompose()
		case key.Matches(msg, m.keys.Reply):
			return m.startReply(false)
		case key.Matches(msg, m.keys.ReplyAll):
			return m.startReply(true)
		case key.Matches(msg, m.keys.Forward):
			return m.startForward()
		case key.Matches(msg, m.keys.OpenAttachment):
			return m.openAttachment()
		case m.isTriageKey(msg):
			return m.triage(msg)
		}
		m.read, _ = m.read.Update(msg)
		return m, m.fetchMissing()
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
	case key.Matches(msg, m.keys.Picker):
		return m.openPicker()
	case key.Matches(msg, m.keys.Refresh):
		m, retry := m.retryFailed()
		var rerun tea.Cmd
		if m.searching() {
			m, rerun = m.startSearch(m.search.query)
		}
		m, refresh := m.startRefresh(true)
		return m, tea.Batch(retry, rerun, refresh)
	case key.Matches(msg, m.keys.Connect):
		return m.openConnect()
	case key.Matches(msg, m.keys.EditAccount):
		return m.openEdit()
	case key.Matches(msg, m.keys.Compose) && m.open.account >= 0:
		return m.startCompose()
	case m.focus == PaneList && key.Matches(msg, m.keys.Search):
		return m.openSearch()
	case m.focus == PaneList && m.searching() && key.Matches(msg, m.keys.Back):
		return m.clearSearch()
	case m.focus == PaneList && key.Matches(msg, m.keys.Reply):
		return m.startReply(false)
	case m.focus == PaneList && key.Matches(msg, m.keys.ReplyAll):
		return m.startReply(true)
	case m.focus == PaneList && key.Matches(msg, m.keys.Forward):
		return m.startForward()
	case m.focus == PaneSidebar:
		var cmd tea.Cmd
		m.side, cmd = m.side.Update(msg)
		return m, cmd
	case m.focus == PaneList && m.isTriageKey(msg):
		return m.triage(msg)
	case m.focus == PaneList && key.Matches(msg, m.keys.Open):
		return m.openThread()
	case m.focus == PaneList:
		var cmd tea.Cmd
		m.list, cmd = m.list.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m Model) popupOpen() bool {
	return m.showHelp || m.showPicker || m.readerPopup || m.composing()
}

// closeTopPopup closes the popup drawn on top, if any: help or the picker,
// then the reader popup beneath them.
func (m *Model) closeTopPopup() bool {
	var name string
	switch {
	case m.showHelp:
		m.showHelp, name = false, "help"
	case m.showPicker:
		m.showPicker, m.moveTargets, m.pickingAttachment, name = false, nil, false, "picker"
	case m.readerPopup:
		m.readerPopup, name = false, "reader"
	default:
		return false
	}
	m.log.Debug("popup closed", "popup", name)
	return true
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

// windowTitle is the terminal title: the app name plus the INBOX unread total.
func (m Model) windowTitle() string {
	if n := m.side.InboxUnread(); n > 0 {
		return fmt.Sprintf("dk-email (%d)", n)
	}
	return "dk-email"
}

func (m Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	if m.notify.Title {
		v.WindowTitle = m.windowTitle()
	}
	// Popups ignore clicks, so release the mouse to the terminal while one is
	// open: it can then open hyperlinks in the message (cmd/ctrl+click), and
	// in the alt screen it turns the wheel into ↑/↓, which still scrolls.
	if !m.popupOpen() {
		v.MouseMode = tea.MouseModeCellMotion
	}
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
	cols = append(cols, m.renderPane(PaneList, p.List, m.listView))
	panes := lipgloss.JoinHorizontal(lipgloss.Top, cols...)
	if m.popupOpen() || m.showConnect {
		panes = m.theme.Dim(panes)
	}
	screen := lipgloss.JoinVertical(lipgloss.Left,
		panes,
		m.renderStatus(),
		m.renderHints(),
	)

	if m.readerPopup {
		p := m.panes.Popup
		w, h := popup.FrameInner(p.W, p.H)
		screen = popup.Overlay(screen, popup.Frame(m.theme, m.read.View(w, h), p.W, p.H), p.X, p.Y)
	}
	if m.composing() {
		p := m.panes.Popup
		screen = popup.Overlay(screen, m.draft.pop.View(), p.X, p.Y)
	}
	pop := ""
	switch {
	case m.showHelp:
		pop = m.helpPop.View()
	case m.showPicker:
		pop = m.picker.View()
	case m.showConnect:
		pop = m.connectPop.View()
	}
	x, y := popup.Center(m.width, m.height-footerRows, pop)
	screen = popup.Overlay(screen, pop, x, y)
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

// unreadNote is " · N unread" for the open folder, or "" when it has none
// or is Trash, whose count isn't shown.
func (m Model) unreadNote() string {
	f, ok := m.side.Folder(m.open.account, m.open.folder)
	if !ok || f.Unread == 0 || f.Role == mail.RoleTrash {
		return ""
	}
	return m.styles.dim.Render(fmt.Sprintf(" · %d unread", f.Unread))
}

// listHeaderRows is the folder header above the thread rows.
const listHeaderRows = 1

// listView shows the open folder's name above the thread rows, plus its
// account in list mode where there's no sidebar.
func (m Model) listView(w, h int) string {
	var title string
	note := m.unreadNote()
	if m.searching() {
		note = m.styles.dim.Render(fmt.Sprintf(" · search: %s (%d)", m.search.query.Raw, m.search.total))
	}
	switch {
	case m.search.input.Focused():
		in := m.search.input
		in.SetWidth(max(w-lipgloss.Width(in.Prompt), 1))
		title = in.View()
	case m.open.account >= 0 && m.panes.Kind == KindNarrow:
		title = m.styles.title.Render(m.open.label) + note
	case m.open.account >= 0:
		title = m.styles.title.Render(m.accts[m.open.account].Name) + m.styles.dim.Render(" › "+m.open.label) + note
	case len(m.accts) > 0:
		title = m.styles.title.Render("Threads")
	default:
		title = m.styles.title.Render("No account")
	}

	var body string
	switch {
	case m.open.account < 0 && m.side.Connecting():
		body = m.styles.dim.Render("Connecting…")
	case m.open.account < 0:
		body = m.styles.dim.Render("Threads appear here once a folder is open.")
	case m.open.loading:
		body = m.styles.dim.Render("Loading…")
	case m.searching() && m.list.Count() == 0:
		body = m.styles.dim.Render("No messages match.")
	default:
		body = m.list.View(w, h-listHeaderRows)
	}
	return title + "\n" + body
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

// renderHints is the hint bar. The top-most popup shows its own keys, since
// ?, q and the like may type text or mean something else there; the reader
// popup and the panes also end with Help: ? | Quit: q (Close: q in a popup).
func (m Model) renderHints() string {
	switch {
	case m.showConnect:
		return m.fitHints(m.connectPop.Hints(), nil)
	case m.showHelp:
		return m.fitHints(m.helpPop.Hints(), nil)
	case m.showPicker:
		return m.fitHints(m.picker.Hints(), nil)
	case m.composing():
		return m.fitHints(m.draft.pop.Hints(), nil)
	}
	quit := m.keys.Quit
	if m.popupOpen() {
		quit = key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "close"))
	}
	return m.fitHints(m.keys.hints(m.focus, m.readerPopup, m.panes.Kind), []key.Binding{m.keys.Help, quit})
}

// fitHints fits as many hints as the width allows, lazygit style
// ("Archive: e | Delete: d"), marks a cut with …, and always keeps tail.
func (m Model) fitHints(hints, tail []key.Binding) string {
	sepW := ansi.StringWidth(popup.HintSep)
	tailText := popup.HintLine(tail)
	room, trail := m.width, 0
	if len(tail) > 0 {
		room -= ansi.StringWidth(tailText)
		trail = sepW
	}

	var head []string
	used := 0
	for i, b := range hints {
		last := i == len(hints)-1
		item := popup.HintItem(b)
		w := ansi.StringWidth(item)
		if !last || len(tail) > 0 {
			w += sepW
		}
		// Unless this is the last key, leave space for a "…" marker.
		reserve := 0
		if !last {
			reserve = ansi.StringWidth("…") + trail
		}
		if used+w+reserve > room {
			head = append(head, "…")
			break
		}
		head = append(head, item)
		used += w
	}
	if len(tail) > 0 {
		head = append(head, tailText)
	}
	line := ansi.Truncate(strings.Join(head, popup.HintSep), m.width, "")
	return m.styles.hint.Render(line + strings.Repeat(" ", max(m.width-ansi.StringWidth(line), 0)))
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
