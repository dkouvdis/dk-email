// Package popup renders the centered popups drawn over the main layout.
package popup

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Section is a titled group of keys in the help popup.
type Section struct {
	Title string
	Keys  []key.Binding
}

// Border, padding, header, the blank lines around the content, and footer.
const (
	helpChromeRows = 2 + 4
	helpChromeCols = 2 + 2
	keyDescGap     = 3
	minHelpInner   = 36
)

type helpLine struct {
	title    string // a section heading when set
	key, dsc string
}

type helpStyles struct {
	box, title, badge, section, key, desc, dim, thumb lipgloss.Style
}

// Help is the scrollable key reference opened with ?. Its size follows the
// content, capped by SetSize.
type Help struct {
	lines      []helpLine
	keyW, dscW int
	offset     int
	maxW, maxH int
	st         helpStyles
	scroll     struct{ up, down, pgUp, pgDown, top, bottom key.Binding }
}

func NewHelp(sections []Section) Help {
	h := Help{st: helpStyles{
		box:     lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("63")).Padding(0, 1),
		title:   lipgloss.NewStyle().Bold(true),
		badge:   lipgloss.NewStyle().Background(lipgloss.Color("63")).Foreground(lipgloss.Color("231")).Padding(0, 1),
		section: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("111")),
		key:     lipgloss.NewStyle().Foreground(lipgloss.Color("183")),
		desc:    lipgloss.NewStyle(),
		dim:     lipgloss.NewStyle().Foreground(lipgloss.Color("244")),
		thumb:   lipgloss.NewStyle().Foreground(lipgloss.Color("63")),
	}}
	h.scroll.up = key.NewBinding(key.WithKeys("k", "up"))
	h.scroll.down = key.NewBinding(key.WithKeys("j", "down"))
	h.scroll.pgUp = key.NewBinding(key.WithKeys("pgup", "ctrl+u"))
	h.scroll.pgDown = key.NewBinding(key.WithKeys("pgdown", "ctrl+d"))
	h.scroll.top = key.NewBinding(key.WithKeys("g", "home"))
	h.scroll.bottom = key.NewBinding(key.WithKeys("G", "end"))

	for i, s := range sections {
		if i > 0 {
			h.lines = append(h.lines, helpLine{})
		}
		h.lines = append(h.lines, helpLine{title: s.Title})
		for _, b := range s.Keys {
			l := helpLine{key: b.Help().Key, dsc: b.Help().Desc}
			h.keyW = max(h.keyW, ansi.StringWidth(l.key))
			h.dscW = max(h.dscW, ansi.StringWidth(l.dsc))
			h.lines = append(h.lines, l)
		}
	}
	return h
}

// SetSize caps the popup at maxW × maxH cells, e.g. on terminal resize.
func (h *Help) SetSize(maxW, maxH int) {
	h.maxW, h.maxH = maxW, maxH
	h.clamp()
}

// Reset scrolls back to the top, for when the popup is reopened.
func (h *Help) Reset() { h.offset = 0 }

// Update scrolls on navigation keys and ignores everything else.
func (h Help) Update(msg tea.KeyPressMsg) Help {
	page := max(h.viewH(), 1)
	switch {
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
	return h
}

func (h *Help) clamp() {
	h.offset = min(h.offset, len(h.lines)-h.viewH())
	h.offset = max(h.offset, 0)
}

// viewH is how many content lines fit.
func (h Help) viewH() int {
	return max(min(len(h.lines), h.maxH-helpChromeRows), 0)
}

func (h Help) scrollable() bool { return len(h.lines) > h.viewH() }

func (h Help) footer() string {
	if h.scrollable() {
		return "scroll j/k/↑↓/pgup/pgdn · close esc/q"
	}
	return "close esc/q"
}

// View renders the popup, or "" when there's no room for it.
func (h Help) View() string {
	vh := h.viewH()
	contentW := 2 + h.keyW + keyDescGap + h.dscW
	innerW := max(contentW+2, ansi.StringWidth(h.footer()), minHelpInner) // +2 for the scrollbar
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
		rows[i] = pad(ansi.Truncate(h.renderLine(h.lines[h.offset+i]), bodyW, "…"), bodyW) + " " + bar[i]
	}

	content := []string{ansi.Truncate(header, innerW, "")}
	content = append(content, "")
	content = append(content, rows...)
	content = append(content, "", h.st.dim.Render(ansi.Truncate(h.footer(), innerW, "…")))
	return h.st.box.Width(innerW + helpChromeCols).Render(strings.Join(content, "\n"))
}

func (h Help) renderLine(l helpLine) string {
	switch {
	case l.title != "":
		return h.st.section.Render(l.title)
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
