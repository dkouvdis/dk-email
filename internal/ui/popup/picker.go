package popup

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
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

// Picker is a popup list of grouped items, chosen with enter. / filters
// the items by label.
type Picker[T any] struct {
	title      string
	groups     []Group[T]
	refs       []itemRef // selectable items matching the filter, in display order
	all        int       // lines without a filter, which sets the height
	cursor     int
	offset     int
	maxW, maxH int
	filter     textinput.Model
	keys       struct{ up, down, top, bottom, choose, cancel, filter, arrowUp, arrowDown key.Binding }
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
	p.keys.filter = key.NewBinding(key.WithKeys("/"))
	p.keys.arrowUp = key.NewBinding(key.WithKeys("up"))
	p.keys.arrowDown = key.NewBinding(key.WithKeys("down"))

	p.filter = textinput.New()
	p.filter.Prompt = "/ "
	p.filter.Placeholder = "press / to filter folders"

	p.rebuild()
	for i, r := range p.refs {
		if groups[r.group].Items[r.item].Current {
			p.cursor = i
		}
	}
	p.all = len(p.lines(1))
	return p
}

// matches reports whether an item's label contains the filter, ignoring case.
func (p Picker[T]) matches(it Item[T]) bool {
	q := strings.ToLower(strings.TrimSpace(p.filter.Value()))
	return q == "" || strings.Contains(strings.ToLower(it.Label), q)
}

func (p Picker[T]) filtered() bool { return strings.TrimSpace(p.filter.Value()) != "" }

// rebuild lists the matching items, keeping the cursor on the same item if
// it still matches, else moving it to the first match.
func (p *Picker[T]) rebuild() {
	var prev *itemRef
	if p.cursor < len(p.refs) {
		prev = &p.refs[p.cursor]
	}
	var refs []itemRef
	cursor := 0
	for g, grp := range p.groups {
		for i, it := range grp.Items {
			if !p.matches(it) {
				continue
			}
			if prev != nil && *prev == (itemRef{g, i}) {
				cursor = len(refs)
			}
			refs = append(refs, itemRef{g, i})
		}
	}
	p.refs, p.cursor = refs, cursor
	p.scrollToCursor()
}

// Filtering reports whether the filter input has focus, so every key,
// q included, goes to it.
func (p Picker[T]) Filtering() bool { return p.filter.Focused() }

// Paste types pasted text into the filter while it has focus.
func (p Picker[T]) Paste(msg tea.PasteMsg) Picker[T] {
	if !p.filter.Focused() {
		return p
	}
	p.filter, _ = p.filter.Update(tea.PasteMsg{Content: singleLine(msg.Content)})
	p.rebuild()
	return p
}

func (p *Picker[T]) clearFilter() {
	p.filter.Reset()
	p.filter.Blur()
	p.rebuild()
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

// Update handles a key. While filtering, keys type into the filter, ↑/↓
// move between matches, enter chooses and esc clears the filter; otherwise
// / starts a filter and esc closes.
func (p Picker[T]) Update(msg tea.KeyPressMsg) (Picker[T], Result) {
	if p.filter.Focused() {
		switch {
		case key.Matches(msg, p.keys.cancel):
			p.clearFilter()
			return p, Pending
		case key.Matches(msg, p.keys.choose):
			if len(p.refs) == 0 {
				return p, Pending
			}
			return p, Chosen
		case key.Matches(msg, p.keys.arrowUp):
			p.cursor--
		case key.Matches(msg, p.keys.arrowDown):
			p.cursor++
		default:
			before := p.filter.Value()
			p.filter, _ = p.filter.Update(msg)
			if p.filter.Value() != before {
				p.rebuild()
			}
			return p, Pending
		}
		p.cursor = min(max(p.cursor, 0), max(len(p.refs)-1, 0))
		p.scrollToCursor()
		return p, Pending
	}

	switch {
	case key.Matches(msg, p.keys.filter):
		p.filter.Focus()
		return p, Pending
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
	header bool // a group heading, e.g. an account name
}

// lines lists the groups and their matching items. While filtering,
// groups with no match, and their notes, are left out.
func (p Picker[T]) lines(width int) []pickerLine {
	var out []pickerLine
	n := 0
	for _, grp := range p.groups {
		var items []pickerLine
		for _, it := range grp.Items {
			if !p.matches(it) {
				continue
			}
			isCursor := n == p.cursor
			items = append(items, pickerLine{text: p.itemRow(it, isCursor, width), cursor: isCursor})
			n++
		}
		if p.filtered() && len(items) == 0 {
			continue
		}
		if len(out) > 0 {
			out = append(out, pickerLine{})
		}
		out = append(out, pickerLine{text: p.st.section.Render(grp.Name), header: true})
		if grp.Note != "" && !p.filtered() {
			out = append(out, pickerLine{text: "  " + p.st.dim.Render(grp.Note)})
		}
		out = append(out, items...)
	}
	if len(out) == 0 && p.filtered() {
		out = []pickerLine{{text: p.st.dim.Render("No folders match")}}
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

const (
	pickerFooter       = "/ filter · j/k move · enter open · esc/q close"
	pickerFilterFooter = "↑/↓ move · enter open · esc clear"
)

func (p Picker[T]) footer() string {
	if p.filter.Focused() {
		return pickerFilterFooter
	}
	return pickerFooter
}

// contentW is the inner width the items need, before capping. It fits
// both footers, so the box keeps its width while filtering.
func (p Picker[T]) contentW() int {
	w := max(ansi.StringWidth(pickerFooter), ansi.StringWidth(pickerFilterFooter), minHelpInner)
	for _, g := range p.groups {
		w = max(w, ansi.StringWidth(g.Name), 2+ansi.StringWidth(g.Note))
		for _, it := range g.Items {
			w = max(w, 2+ansi.StringWidth(it.Label)+1+len(fmt.Sprint(it.Count)))
		}
	}
	return w
}

func (p Picker[T]) innerW() int { return min(p.contentW(), p.maxW-helpChromeCols) }

// viewH is how many item lines fit. It's based on the unfiltered list so
// the box keeps its size while filtering.
func (p Picker[T]) viewH() int {
	return max(min(p.all, p.maxH-helpChromeRows-helpFilterRows), 0)
}

func (p *Picker[T]) scrollToCursor() {
	vh := p.viewH()
	if vh <= 0 {
		return
	}
	lines := p.lines(max(p.innerW(), 1))
	row, header := 0, 0
	for i, l := range lines {
		if l.header {
			header = i
		}
		if l.cursor {
			row = i
			break
		}
	}
	// Show the cursor's group heading too whenever both fit.
	top := row
	if row-header < vh {
		top = header
	}
	if top < p.offset {
		p.offset = top
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

	f := p.filter
	f.SetWidth(max(innerW-ansi.StringWidth(f.Prompt)-1, 1))
	filterLine := ansi.Truncate(f.View(), innerW, "…")
	if !f.Focused() && f.Value() == "" {
		filterLine = p.st.dim.Render(ansi.Truncate(f.Prompt+f.Placeholder, innerW, "…"))
	}

	lines := p.lines(innerW)
	content := []string{ansi.Truncate(header, innerW, ""), filterLine, ""}
	for i := range vh {
		line := ""
		if p.offset+i < len(lines) {
			line = ansi.Truncate(lines[p.offset+i].text, innerW, "")
		}
		content = append(content, line)
	}
	content = append(content, "", p.st.dim.Render(ansi.Truncate(p.footer(), innerW, "…")))
	return p.st.box.Width(innerW + helpChromeCols).Render(strings.Join(content, "\n"))
}
