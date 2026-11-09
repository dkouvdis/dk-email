package popup

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Item is one choice in a Picker.
type Item[T any] struct {
	Label string
	Count int // shown right-aligned when > 0, e.g. unread mail
	Value T
	// Current marks the item already in use; the cursor starts on it.
	Current bool
}

// Group is a heading with its items, e.g. an account and its folders. Note
// is shown under the heading, e.g. why an account has no folders.
type Group[T any] struct {
	Name  string
	Note  string
	Items []Item[T]
}

// Result is what a key press did to a Picker.
type Result int

const (
	Pending Result = iota
	Chosen
	Cancelled
)

// Picker is a popup list of grouped items, chosen with enter.
type Picker[T any] struct {
	title      string
	groups     []Group[T]
	refs       []itemRef // selectable items in display order
	cursor     int
	offset     int
	maxW, maxH int
	keys       struct{ up, down, top, bottom, choose, cancel key.Binding }
	st         helpStyles
	cursorSt   lipgloss.Style
}

type itemRef struct{ group, item int }

func NewPicker[T any](title string, groups []Group[T]) Picker[T] {
	p := Picker[T]{
		title:    title,
		groups:   groups,
		st:       NewHelp(nil).st,
		cursorSt: lipgloss.NewStyle().Background(lipgloss.Color("63")).Foreground(lipgloss.Color("231")),
	}
	p.keys.up = key.NewBinding(key.WithKeys("k", "up"))
	p.keys.down = key.NewBinding(key.WithKeys("j", "down"))
	p.keys.top = key.NewBinding(key.WithKeys("g", "home"))
	p.keys.bottom = key.NewBinding(key.WithKeys("G", "end"))
	p.keys.choose = key.NewBinding(key.WithKeys("enter"))
	p.keys.cancel = key.NewBinding(key.WithKeys("esc"))
	for g, grp := range groups {
		for i, it := range grp.Items {
			if it.Current {
				p.cursor = len(p.refs)
			}
			p.refs = append(p.refs, itemRef{g, i})
		}
	}
	return p
}

// SetSize caps the popup at maxW × maxH cells.
func (p *Picker[T]) SetSize(maxW, maxH int) {
	p.maxW, p.maxH = maxW, maxH
	p.scrollToCursor()
}

// Selected returns the item under the cursor.
func (p Picker[T]) Selected() (T, bool) {
	if len(p.refs) == 0 {
		var zero T
		return zero, false
	}
	r := p.refs[p.cursor]
	return p.groups[r.group].Items[r.item].Value, true
}

func (p Picker[T]) Update(msg tea.KeyPressMsg) (Picker[T], Result) {
	switch {
	case key.Matches(msg, p.keys.cancel):
		return p, Cancelled
	case key.Matches(msg, p.keys.choose):
		if len(p.refs) == 0 {
			return p, Pending
		}
		return p, Chosen
	case len(p.refs) == 0:
		return p, Pending
	case key.Matches(msg, p.keys.up):
		p.cursor--
	case key.Matches(msg, p.keys.down):
		p.cursor++
	case key.Matches(msg, p.keys.top):
		p.cursor = 0
	case key.Matches(msg, p.keys.bottom):
		p.cursor = len(p.refs) - 1
	}
	p.cursor = min(max(p.cursor, 0), len(p.refs)-1)
	p.scrollToCursor()
	return p, Pending
}

type pickerLine struct {
	text   string
	cursor bool
}

func (p Picker[T]) lines(width int) []pickerLine {
	var out []pickerLine
	n := 0
	for g, grp := range p.groups {
		if g > 0 {
			out = append(out, pickerLine{})
		}
		out = append(out, pickerLine{text: p.st.section.Render(grp.Name)})
		if grp.Note != "" {
			out = append(out, pickerLine{text: "  " + p.st.dim.Render(grp.Note)})
		}
		for _, it := range grp.Items {
			isCursor := n == p.cursor
			out = append(out, pickerLine{text: p.itemRow(it, isCursor, width), cursor: isCursor})
			n++
		}
	}
	return out
}

func (p Picker[T]) itemRow(it Item[T], cursor bool, width int) string {
	marker := "  "
	switch {
	case cursor:
		marker = "▸ "
	case it.Current:
		marker = "▶ "
	}
	count := ""
	if it.Count > 0 {
		count = fmt.Sprint(it.Count)
	}
	label := ansi.Truncate(it.Label, max(width-2-len(count)-1, 1), "…")
	gap := strings.Repeat(" ", max(width-2-ansi.StringWidth(label)-len(count), 1))
	row := marker + label + gap + count
	if cursor {
		return p.cursorSt.Render(ansi.Truncate(row, width, ""))
	}
	return marker + label + gap + p.st.key.Render(count)
}

func (p Picker[T]) footer() string { return "j/k move · enter open · esc/q close" }

// contentW is the inner width the items need, before capping.
func (p Picker[T]) contentW() int {
	w := max(ansi.StringWidth(p.footer()), minHelpInner)
	for _, g := range p.groups {
		w = max(w, ansi.StringWidth(g.Name), 2+ansi.StringWidth(g.Note))
		for _, it := range g.Items {
			w = max(w, 2+ansi.StringWidth(it.Label)+1+len(fmt.Sprint(it.Count)))
		}
	}
	return w
}

func (p Picker[T]) innerW() int { return min(p.contentW(), p.maxW-helpChromeCols) }

func (p Picker[T]) viewH() int {
	return max(min(len(p.lines(max(p.innerW(), 1))), p.maxH-helpChromeRows), 0)
}

func (p *Picker[T]) scrollToCursor() {
	vh := p.viewH()
	if vh <= 0 {
		return
	}
	lines := p.lines(max(p.innerW(), 1))
	row := 0
	for i, l := range lines {
		if l.cursor {
			row = i
		}
	}
	if row < p.offset {
		p.offset = row
	}
	if row >= p.offset+vh {
		p.offset = row - vh + 1
	}
	p.offset = max(min(p.offset, len(lines)-vh), 0)
}

// View renders the popup, or "" when there's no room for it.
func (p Picker[T]) View() string {
	innerW, vh := p.innerW(), p.viewH()
	if vh < 1 || innerW < 20 {
		return ""
	}
	badge := p.st.badge.Render("esc close")
	title := p.st.title.Render(p.title)
	header := title + strings.Repeat(" ", max(innerW-lipgloss.Width(title)-lipgloss.Width(badge), 1)) + badge

	lines := p.lines(innerW)
	content := []string{ansi.Truncate(header, innerW, ""), ""}
	for _, l := range lines[p.offset : p.offset+vh] {
		content = append(content, ansi.Truncate(l.text, innerW, ""))
	}
	content = append(content, "", p.st.dim.Render(ansi.Truncate(p.footer(), innerW, "…")))
	return p.st.box.Width(innerW + helpChromeCols).Render(strings.Join(content, "\n"))
}
