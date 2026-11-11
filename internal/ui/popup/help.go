// Package popup renders the centered popups drawn over the main layout.
package popup

import (
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/charmbracelet/x/ansi"
	"github.com/dkouvdis/dk-email/internal/ui/theme"
)

// Section is a titled group of keys in the help popup.
type Section struct {
	Title string
	Keys  []key.Binding
}

// Border, padding, header, the blank lines around the content, and footer.
// The help popup adds a filter row under its header.
const (
	helpChromeRows = 2 + 4
	helpFilterRows = 1
	helpChromeCols = 2 + 2
	keyDescGap     = 3
	minHelpInner   = 36
)

type helpLine struct {
	title    string // a section heading when set
	key, dsc string
	note     string // a dimmed message such as "No keys match"
}

type helpStyles struct {
	box, title, badge, section, key, desc, dim, thumb, alert lipgloss.Style
}

// Help is the scrollable key reference opened with ?. Its size follows the
// full key list, capped by SetSize, and stays the same while filtering.
type Help struct {
	sections   []Section
	all        int        // lines without a filter, which sets the height
	lines      []helpLine // lines matching the filter
	filter     textinput.Model
	keyW, dscW int
	offset     int
	maxW, maxH int
	st         helpStyles
	scroll     struct{ up, down, pgUp, pgDown, top, bottom key.Binding }
	keys       struct{ filter, close, clear, keep key.Binding }
}

// HelpResult says whether a key press closed the help popup.
type HelpResult int

const (
	HelpOpen HelpResult = iota
	HelpClosed
)

func newHelpStyles(th theme.Theme) helpStyles {
	return helpStyles{
		box:     frameBox(th),
		title:   lipgloss.NewStyle().Bold(true),
		badge:   th.Cursor.Padding(0, 1),
		section: th.Unread.Bold(true),
		key:     th.Key,
		desc:    lipgloss.NewStyle(),
		dim:     th.Muted,
		thumb:   th.Accent,
		alert:   th.Error,
	}
}

// frameBox is the bordered box every popup is drawn in.
func frameBox(th theme.Theme) lipgloss.Style {
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(th.AccentColor).Padding(0, 1)
}

func NewHelp(th theme.Theme, sections []Section) Help {
	h := Help{st: newHelpStyles(th)}
	h.scroll.up = key.NewBinding(key.WithKeys("k", "up"))
	h.scroll.down = key.NewBinding(key.WithKeys("j", "down"))
	h.scroll.pgUp = key.NewBinding(key.WithKeys("pgup", "ctrl+u"))
	h.scroll.pgDown = key.NewBinding(key.WithKeys("pgdown", "ctrl+d"))
	h.scroll.top = key.NewBinding(key.WithKeys("g", "home"))
	h.scroll.bottom = key.NewBinding(key.WithKeys("G", "end"))
	h.keys.filter = key.NewBinding(key.WithKeys("/"))
	h.keys.close = key.NewBinding(key.WithKeys("esc", "q", "?"))
	h.keys.clear = key.NewBinding(key.WithKeys("esc"))
	h.keys.keep = key.NewBinding(key.WithKeys("enter"))

	h.filter = textinput.New()
	h.filter.Prompt = "/ "
	h.filter.Placeholder = "press / to filter by key or description"

	h.sections = sections
	for _, s := range sections {
		for _, b := range s.Keys {
			h.keyW = max(h.keyW, ansi.StringWidth(b.Help().Key))
			h.dscW = max(h.dscW, ansi.StringWidth(b.Help().Desc))
		}
	}
	h.rebuild()
	h.all = len(h.lines)
	return h
}

// rebuild lists the keys whose key or description contains the filter,
// case-insensitively, hiding sections with no match.
func (h *Help) rebuild() {
	q := strings.ToLower(strings.TrimSpace(h.filter.Value()))
	h.lines = nil
	for _, s := range h.sections {
		var keys []helpLine
		for _, b := range s.Keys {
			k, d := b.Help().Key, b.Help().Desc
			if q == "" || strings.Contains(strings.ToLower(k), q) || strings.Contains(strings.ToLower(d), q) {
				keys = append(keys, helpLine{key: k, dsc: d})
			}
		}
		if len(keys) == 0 {
			continue
		}
		if len(h.lines) > 0 {
			h.lines = append(h.lines, helpLine{})
		}
		h.lines = append(h.lines, helpLine{title: s.Title})
		h.lines = append(h.lines, keys...)
	}
	if len(h.lines) == 0 {
		h.lines = []helpLine{{note: "No keys match"}}
	}
	h.offset = 0
}

// Paste types pasted text into the filter while it has focus.
func (h Help) Paste(msg tea.PasteMsg) Help {
	if !h.filter.Focused() {
		return h
	}
	before := h.filter.Value()
	h.filter, _ = h.filter.Update(tea.PasteMsg{Content: singleLine(msg.Content)})
	if h.filter.Value() != before {
		h.rebuild()
	}
	return h
}

// Filtering reports whether the filter input has focus, so every key,
// q and ? included, goes to it.
func (h Help) Filtering() bool { return h.filter.Focused() }

// SetSize caps the popup at maxW × maxH cells, e.g. on terminal resize.
func (h *Help) SetSize(maxW, maxH int) {
	h.maxW, h.maxH = maxW, maxH
	h.clamp()
}

// Reset clears the filter and scrolls back to the top, for when the popup
// is reopened.
func (h *Help) Reset() {
	h.filter.Reset()
	h.filter.Blur()
	h.rebuild()
}

// Update handles a key: typing into the filter while it has focus,
// otherwise / to filter, scrolling, and esc, q or ? to close. esc first
// clears a filter; only with none left does it close the popup.
func (h Help) Update(msg tea.KeyPressMsg) (Help, HelpResult) {
	if h.filter.Focused() {
		switch {
		case key.Matches(msg, h.keys.clear):
			h.Reset()
		case key.Matches(msg, h.keys.keep):
			h.filter.Blur()
		default:
			before := h.filter.Value()
			h.filter, _ = h.filter.Update(msg)
			if h.filter.Value() != before {
				h.rebuild()
			}
		}
		return h, HelpOpen
	}

	page := max(h.viewH(), 1)
	switch {
	case key.Matches(msg, h.keys.filter):
		h.filter.Focus()
		return h, HelpOpen
	case key.Matches(msg, h.keys.clear) && h.filter.Value() != "":
		h.Reset()
		return h, HelpOpen
	case key.Matches(msg, h.keys.close):
		return h, HelpClosed
	case key.Matches(msg, h.scroll.up):
		h.offset--
	case key.Matches(msg, h.scroll.down):
		h.offset++
	case key.Matches(msg, h.scroll.pgUp):
		h.offset -= page
	case key.Matches(msg, h.scroll.pgDown):
		h.offset += page
	case key.Matches(msg, h.scroll.top):
		h.offset = 0
	case key.Matches(msg, h.scroll.bottom):
		h.offset = len(h.lines)
	}
	h.clamp()
	return h, HelpOpen
}

func (h *Help) clamp() {
	h.offset = min(h.offset, len(h.lines)-h.viewH())
	h.offset = max(h.offset, 0)
}

// viewH is how many content lines fit. It's based on the unfiltered list
// so the box keeps its size while filtering.
func (h Help) viewH() int {
	return max(min(h.all, h.maxH-helpChromeRows-helpFilterRows), 0)
}

func (h Help) scrollable() bool { return len(h.lines) > h.viewH() }

func (h Help) footer() string {
	switch {
	case h.filter.Focused():
		return "enter keep · esc clear"
	case h.scrollable():
		return "/ filter · j/k/pgup/pgdn scroll · esc/q close"
	}
	return "/ filter · esc/q close"
}

// View renders the popup, or "" when there's no room for it.
func (h Help) View() string {
	vh := h.viewH()
	contentW := 2 + h.keyW + keyDescGap + h.dscW
	innerW := max(contentW+2, minHelpInner) // +2 for the scrollbar
	innerW = min(innerW, h.maxW-helpChromeCols)
	if vh < 1 || innerW < 20 {
		return ""
	}

	badge := h.st.badge.Render("esc close")
	title := h.st.title.Render("Keys")
	header := title + strings.Repeat(" ", max(innerW-lipgloss.Width(title)-lipgloss.Width(badge), 1)) + badge

	bodyW := innerW - 2
	bar := h.scrollbar(vh)
	rows := make([]string, vh)
	for i := range vh {
		line := ""
		if h.offset+i < len(h.lines) {
			line = h.renderLine(h.lines[h.offset+i])
		}
		rows[i] = pad(ansi.Truncate(line, bodyW, "…"), bodyW) + " " + bar[i]
	}

	f := h.filter
	f.SetWidth(max(innerW-ansi.StringWidth(f.Prompt)-1, 1))
	filterLine := ansi.Truncate(f.View(), innerW, "…")
	if !f.Focused() && f.Value() == "" {
		filterLine = h.st.dim.Render(ansi.Truncate(f.Prompt+f.Placeholder, innerW, "…"))
	}

	content := []string{ansi.Truncate(header, innerW, ""), filterLine}
	content = append(content, "")
	content = append(content, rows...)
	content = append(content, "", h.st.dim.Render(ansi.Truncate(h.footer(), innerW, "…")))
	return h.st.box.Width(innerW + helpChromeCols).Render(strings.Join(content, "\n"))
}

func (h Help) renderLine(l helpLine) string {
	switch {
	case l.title != "":
		return h.st.section.Render(l.title)
	case l.note != "":
		return h.st.dim.Render(l.note)
	case l.key == "" && l.dsc == "":
		return ""
	}
	return "  " + h.st.key.Render(pad(l.key, h.keyW)) + strings.Repeat(" ", keyDescGap) + h.st.desc.Render(l.dsc)
}

// scrollbar returns one cell per visible row: a thumb where the view is,
// blank elsewhere, or all blank when everything fits.
func (h Help) scrollbar(vh int) []string {
	bar := make([]string, vh)
	for i := range bar {
		bar[i] = " "
	}
	if !h.scrollable() {
		return bar
	}
	total := len(h.lines)
	thumb := max(vh*vh/total, 1)
	pos := 0
	if total > vh {
		pos = h.offset * (vh - thumb) / (total - vh)
	}
	for i := pos; i < pos+thumb && i < vh; i++ {
		bar[i] = h.st.thumb.Render("┃")
	}
	return bar
}

func pad(s string, w int) string {
	return s + strings.Repeat(" ", max(w-ansi.StringWidth(s), 0))
}
