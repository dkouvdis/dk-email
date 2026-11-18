package popup

import (
	"fmt"
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/charmbracelet/x/ansi"
	"github.com/dkouvdis/dk-email/internal/ui/theme"
)

// ComposeResult is what a key press did to the composer.
type ComposeResult int

const (
	ComposeNone ComposeResult = iota
	ComposeSend
	ComposeClose   // esc with nothing changed
	ComposeDiscard // d in the close prompt
	ComposeSave    // s or esc in the close prompt, when saving is enabled
	ComposeCancel  // esc while busy with work that can be cancelled
	// ComposeRemoveAttachment is d, backspace or delete on the Attachments
	// row; the UI drops the one AttachmentIndex names.
	ComposeRemoveAttachment
	// ComposeComplete is tab in the attach prompt; the UI completes the path
	// and answers with SetAttachInput.
	ComposeComplete
	// ComposeAttachFile is enter in the attach prompt with a path typed; the
	// UI reads the file and answers with CloseAttach or SetAttachError.
	ComposeAttachFile
)

// ComposeField is a field of the composer, in focus order.
type ComposeField int

const (
	ComposeTo ComposeField = iota
	ComposeCc
	ComposeBcc
	ComposeSubject
	ComposeBody
	composeFields
	// ComposeFrom is the From row, which sits before To in focus order and
	// exists only with two or more choices (see SetFrom).
	ComposeFrom
	// ComposeAttachments is the Attachments row, after Subject in focus order,
	// present only while there are attachments (see InitAttachments).
	ComposeAttachments
)

// ComposeValues is the text of every field.
type ComposeValues struct{ To, Cc, Bcc, Subject, Body string }

const (
	composeLabelW = 9 // "Subject" and the gap after it
	// attachLabelW is "Attach file:" and the gap after it.
	attachLabelW = 13
	composeRows  = 6 // the 4 header rows, the rule and the footer around the body
	// composeErrLines is the most rows an error may take.
	composeErrLines = 3
)

var composeLabels = [ComposeBody]string{"To", "Cc", "Bcc", "Subject"}

// Compose is the message composer: header fields above a body editor.
type Compose struct {
	headers [ComposeBody]textinput.Model
	body    textarea.Model
	focus   ComposeField
	initial ComposeValues

	// from are the labels of the addresses to send as; the row shows with 2+.
	from        []string
	fromSel     int
	initialFrom int
	// fromOpen is set while the dropdown of choices is open; fromHi is its
	// highlighted choice and fromTop the first one shown.
	fromOpen        bool
	fromHi, fromTop int

	// atts are the labels of the attachments; the row shows while there are
	// any. initialAtts is the list Dirty compares with and attSel the
	// selected one.
	atts, initialAtts []string
	attSel            int

	// attaching is set while the attach prompt is open. attachIn is its input,
	// attachChoices the completions listed under it and attachErr why the last
	// attempt failed.
	attaching     bool
	attachIn      textinput.Model
	attachChoices []string
	attachErr     string

	title     string
	w, h      int
	err, busy string
	prompting bool
	canSave   bool
	// cancellable is set while the busy work can be cancelled with esc.
	cancellable bool
	th          theme.Theme
}

// NewCompose is a composer with v filled in. focus is the field to start in.
// With the body focused the cursor starts at the top, so a reply is written
// above its quote; otherwise every cursor starts at the end of its text.
func NewCompose(th theme.Theme, title string, v ComposeValues, focus ComposeField) Compose {
	c := Compose{title: title, th: th}
	plain := lipgloss.NewStyle()
	for i, val := range []string{v.To, v.Cc, v.Bcc, v.Subject} {
		in := textinput.New()
		in.Prompt = ""
		st := in.Styles()
		st.Focused.Text, st.Blurred.Text = plain, plain
		st.Cursor.Color, st.Cursor.Blink = nil, false
		in.SetStyles(st)
		in.SetValue(val)
		in.CursorEnd()
		c.headers[i] = in
	}

	c.attachIn = textinput.New()
	c.attachIn.Prompt = ""
	c.attachIn.Placeholder = "path, tab completes"
	ast := c.attachIn.Styles()
	ast.Focused.Text, ast.Blurred.Text = plain, plain
	ast.Cursor.Color, ast.Cursor.Blink = nil, false
	c.attachIn.SetStyles(ast)

	ta := textarea.New()
	ta.Prompt = ""
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	ta.MaxHeight = 0
	ta.MaxWidth = 0
	st := ta.Styles()
	for _, s := range []*textarea.StyleState{&st.Focused, &st.Blurred} {
		s.Text, s.CursorLine, s.EndOfBuffer, s.Base = plain, plain, plain, plain
	}
	st.Cursor.Color, st.Cursor.Blink = nil, false
	ta.SetStyles(st)
	ta.SetValue(v.Body)
	if focus == ComposeBody {
		ta.MoveToBegin()
	}
	c.body = ta

	c.setFocus(focus)
	c.initial = c.Values()
	return c
}

// SetSize sets the size of the whole popup, border included.
func (c *Compose) SetSize(w, h int) {
	c.w, c.h = w, h
	c.layout()
}

// layout sizes the inputs to the popup. The body gives up a row for every
// extra row the footer takes, so its cursor stays in view.
func (c *Compose) layout() {
	innerW, innerH := FrameInner(c.w, c.h)
	for i := range c.headers {
		c.headers[i].SetWidth(max(innerW-composeLabelW-1, 1))
	}
	c.attachIn.SetWidth(max(innerW-attachLabelW-1, 1))
	c.body.SetWidth(max(innerW, 1))
	c.body.SetHeight(max(innerH-composeRows-c.extraRows()-(len(c.footerLines(innerW))-1), 1))
}

// SetFrom sets the From choices (already formatted labels) and the selected
// one, which becomes the baseline for Dirty. With fewer than two choices the
// row is hidden.
func (c *Compose) SetFrom(choices []string, selected int) {
	c.from = choices
	if selected < 0 || selected >= len(choices) {
		selected = 0
	}
	c.fromSel, c.initialFrom = selected, selected
	if !c.hasFrom() && c.focus == ComposeFrom {
		c.setFocus(ComposeTo)
	}
	c.layout()
}

// FromIndex is the selected From choice.
func (c Compose) FromIndex() int { return c.fromSel }

// InitAttachments sets the attachment labels (already formatted), which
// become the baseline for Dirty. With none the row is hidden.
func (c *Compose) InitAttachments(labels []string) {
	c.atts, c.initialAtts = slices.Clone(labels), slices.Clone(labels)
	c.attSel = 0
	c.layout()
}

// SetAttachments replaces the attachment labels without touching the
// baseline for Dirty. Focus leaves the row when the last one goes.
func (c *Compose) SetAttachments(labels []string) {
	c.atts = slices.Clone(labels)
	c.attSel = max(min(c.attSel, len(c.atts)-1), 0)
	if len(c.atts) == 0 && c.focus == ComposeAttachments {
		c.setFocus(ComposeBody)
	}
	c.layout()
}

// AttachmentIndex is the selected attachment.
func (c Compose) AttachmentIndex() int { return c.attSel }

// SetAttachInput sets the attach prompt's text, with the cursor at the end,
// and lists choices under it when there are two or more.
func (c *Compose) SetAttachInput(value string, choices []string) {
	c.attachIn.SetValue(value)
	c.attachIn.CursorEnd()
	c.attachChoices, c.attachErr = nil, ""
	if len(choices) >= 2 {
		c.attachChoices = slices.Clone(choices)
	}
}

// SetAttachError shows msg under the attach prompt, which stays open.
func (c *Compose) SetAttachError(msg string) {
	c.attachErr, c.attachChoices = msg, nil
}

// CloseAttach closes the attach prompt.
func (c *Compose) CloseAttach() {
	c.attaching = false
	c.attachIn.Blur()
	c.attachChoices, c.attachErr = nil, ""
	c.layout()
}

// Attaching reports whether the attach prompt is open.
func (c Compose) Attaching() bool { return c.attaching }

// AttachPath is what is typed in the attach prompt, trimmed.
func (c Compose) AttachPath() string { return strings.TrimSpace(c.attachIn.Value()) }

// SetTitle replaces the popup title.
func (c *Compose) SetTitle(t string) { c.title = t }

func (c Compose) hasFrom() bool { return len(c.from) >= 2 }

// extraRows is how many rows the From and Attachments rows add to the layout.
func (c Compose) extraRows() int {
	n := 0
	if c.hasFrom() {
		n++
	}
	if len(c.atts) > 0 {
		n++
	}
	return n
}

// SetError shows msg in the footer. An empty msg clears it.
func (c *Compose) SetError(msg string) {
	c.err = msg
	c.layout()
}

// SetBusy shows label in the footer and ignores keys until it's set to "".
func (c *Compose) SetBusy(label string) {
	c.busy = label
	if label != "" {
		c.err = ""
		c.prompting = false
		c.attaching = false
	} else {
		c.cancellable = false
	}
	c.layout()
}

// SetCancellable sets whether esc cancels the busy work. SetBusy("") clears it.
func (c *Compose) SetCancellable(on bool) { c.cancellable = on }

// EnableSave sets whether the close prompt offers saving a draft.
func (c *Compose) EnableSave(on bool) { c.canSave = on }

// Values is the current text of every field, as typed.
func (c Compose) Values() ComposeValues {
	return ComposeValues{
		To:      c.headers[ComposeTo].Value(),
		Cc:      c.headers[ComposeCc].Value(),
		Bcc:     c.headers[ComposeBcc].Value(),
		Subject: c.headers[ComposeSubject].Value(),
		Body:    c.body.Value(),
	}
}

// Dirty reports whether any field differs from where it started.
func (c Compose) Dirty() bool {
	return c.Values() != c.initial || c.fromSel != c.initialFrom || !slices.Equal(c.atts, c.initialAtts)
}

// Prompting reports whether the close prompt is showing.
func (c Compose) Prompting() bool { return c.prompting }

// TakesPaste reports whether a paste would be acted on: it is not while busy
// or while the close prompt or the From choices are open.
func (c Compose) TakesPaste() bool { return c.busy == "" && !c.prompting && !c.fromOpen }

func (c *Compose) setFocus(f ComposeField) {
	c.focus = f
	if f == ComposeFrom || f == ComposeAttachments {
		for i := range c.headers {
			c.headers[i].Blur()
		}
		c.body.Blur()
		return
	}
	for i := range c.headers {
		if ComposeField(i) == f {
			c.headers[i].Focus()
		} else {
			c.headers[i].Blur()
		}
	}
	if f == ComposeBody {
		c.body.Focus()
	} else {
		c.body.Blur()
	}
}

func (c *Compose) move(step int) {
	order := []ComposeField{ComposeTo, ComposeCc, ComposeBcc, ComposeSubject, ComposeBody}
	if c.hasFrom() {
		order = append([]ComposeField{ComposeFrom}, order...)
	}
	if len(c.atts) > 0 {
		order = slices.Insert(order, slices.Index(order, ComposeSubject)+1, ComposeAttachments)
	}
	for i, f := range order {
		if f == c.focus {
			c.setFocus(order[(i+step+len(order))%len(order)])
			return
		}
	}
}

// cycleFrom moves the From selection by step, wrapping.
func (c *Compose) cycleFrom(step int) {
	n := len(c.from)
	c.fromSel = (c.fromSel + step + n) % n
}

// forward gives msg to the focused input.
func (c *Compose) forward(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	if c.focus == ComposeFrom || c.focus == ComposeAttachments {
		return nil
	}
	if c.focus == ComposeBody {
		c.body, cmd = c.body.Update(msg)
	} else {
		c.headers[c.focus], cmd = c.headers[c.focus].Update(msg)
	}
	return cmd
}

// Update handles keys and pastes; any other message goes to the focused input.
func (c Compose) Update(msg tea.Msg) (Compose, ComposeResult, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if c.busy != "" {
			if c.cancellable && msg.String() == "esc" {
				return c, ComposeCancel, nil
			}
			return c, ComposeNone, nil
		}
		if c.prompting {
			return c.updatePrompt(msg)
		}
		if c.attaching {
			return c.updateAttach(msg)
		}
		if c.fromOpen {
			return c.updateDropdown(msg), ComposeNone, nil
		}
		return c.updateKey(msg)
	case tea.PasteMsg:
		if c.busy != "" || c.prompting || c.fromOpen {
			return c, ComposeNone, nil
		}
		if c.attaching {
			c.attachChoices, c.attachErr = nil, ""
			msg.Content = headerLine(msg.Content)
			var cmd tea.Cmd
			c.attachIn, cmd = c.attachIn.Update(msg)
			return c, ComposeNone, cmd
		}
		if c.focus != ComposeBody {
			msg.Content = headerLine(msg.Content)
		}
		return c, ComposeNone, c.forward(msg)
	}
	if c.attaching {
		var cmd tea.Cmd
		c.attachIn, cmd = c.attachIn.Update(msg)
		return c, ComposeNone, cmd
	}
	return c, ComposeNone, c.forward(msg)
}

// updateDropdown handles keys while the From choices are open; everything
// but moving, picking and closing is ignored.
func (c Compose) updateDropdown(msg tea.KeyPressMsg) Compose {
	switch msg.String() {
	case "up", "k":
		c.fromHi = max(c.fromHi-1, 0)
	case "down", "j":
		c.fromHi = min(c.fromHi+1, len(c.from)-1)
	case "enter":
		c.fromSel, c.fromOpen = c.fromHi, false
	case "esc":
		c.fromOpen = false
	}
	return c
}

func (c Compose) updateKey(msg tea.KeyPressMsg) (Compose, ComposeResult, tea.Cmd) {
	switch msg.String() {
	case "ctrl+s":
		return c, ComposeSend, nil
	case "ctrl+o":
		c.attaching = true
		c.attachIn.SetValue("")
		c.attachChoices, c.attachErr = nil, ""
		c.layout()
		return c, ComposeNone, c.attachIn.Focus()
	case "esc":
		if !c.Dirty() {
			return c, ComposeClose, nil
		}
		c.prompting = true
		return c, ComposeNone, nil
	case "tab":
		c.move(1)
		return c, ComposeNone, nil
	case "shift+tab":
		c.move(-1)
		return c, ComposeNone, nil
	case "enter":
		if c.focus == ComposeFrom {
			c.fromOpen, c.fromHi = true, c.fromSel
			return c, ComposeNone, nil
		}
		if c.focus != ComposeBody {
			c.move(1)
			return c, ComposeNone, nil
		}
	case "d", "backspace", "delete":
		if c.focus == ComposeAttachments {
			return c, ComposeRemoveAttachment, nil
		}
	case "left", "right", "space":
		if c.focus == ComposeAttachments {
			if msg.String() == "left" {
				c.attSel = max(c.attSel-1, 0)
			} else if msg.String() == "right" {
				c.attSel = min(c.attSel+1, len(c.atts)-1)
			}
			return c, ComposeNone, nil
		}
		if c.focus == ComposeFrom {
			step := 1
			if msg.String() == "left" {
				step = -1
			}
			c.cycleFrom(step)
			return c, ComposeNone, nil
		}
	}
	return c, ComposeNone, c.forward(msg)
}

func (c Compose) updatePrompt(msg tea.KeyPressMsg) (Compose, ComposeResult, tea.Cmd) {
	switch msg.String() {
	case "d":
		return c, ComposeDiscard, nil
	case "e":
		c.prompting = false
	case "s", "esc":
		if c.canSave {
			c.prompting = false
			return c, ComposeSave, nil
		}
	}
	return c, ComposeNone, nil
}

// updateAttach handles keys while the attach prompt is open; it takes every
// key but the three it acts on.
func (c Compose) updateAttach(msg tea.KeyPressMsg) (Compose, ComposeResult, tea.Cmd) {
	switch msg.String() {
	case "esc":
		c.CloseAttach()
		return c, ComposeNone, nil
	case "tab":
		return c, ComposeComplete, nil
	case "enter":
		if c.AttachPath() == "" {
			return c, ComposeNone, nil
		}
		return c, ComposeAttachFile, nil
	}
	c.attachChoices, c.attachErr = nil, ""
	var cmd tea.Cmd
	c.attachIn, cmd = c.attachIn.Update(msg)
	return c, ComposeNone, cmd
}

// headerLine makes pasted text fit a single-line field.
func headerLine(s string) string {
	s = strings.Trim(s, "\r\n")
	return strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(s)
}

// View draws the composer at exactly the size given to SetSize, or returns ""
// when that's too small to use.
func (c Compose) View() string {
	innerW, innerH := FrameInner(c.w, c.h)
	if innerW < 20 || innerH <= composeRows+c.extraRows() {
		return ""
	}
	lines := make([]string, 0, innerH)
	if c.hasFrom() {
		label := fmt.Sprintf("%-*s", composeLabelW, "From")
		if c.focus == ComposeFrom {
			label = c.th.Accent.Render(label)
		} else {
			label = c.th.Muted.Render(label)
		}
		addr := ansi.Truncate(c.from[c.fromSel], max(innerW-composeLabelW-2, 1), "…")
		if c.focus == ComposeFrom {
			addr = c.th.Cursor.Render(addr + " ▾")
		} else {
			addr += c.th.Muted.Render(" ▾")
		}
		lines = append(lines, label+addr)
	}
	for i := range c.headers {
		label := fmt.Sprintf("%-*s", composeLabelW, composeLabels[i])
		if ComposeField(i) == c.focus {
			label = c.th.Accent.Render(label)
		} else {
			label = c.th.Muted.Render(label)
		}
		lines = append(lines, label+c.headers[i].View())
		if ComposeField(i) == ComposeSubject && len(c.atts) > 0 {
			lines = append(lines, c.attachmentsRow(innerW))
		}
	}
	rule := c.th.Muted.Render(strings.Repeat("─", innerW))
	if c.focus == ComposeBody {
		rule = c.th.Accent.Render(strings.Repeat("─", innerW))
	}
	lines = append(lines, rule)
	lines = append(lines, strings.Split(c.body.View(), "\n")...)

	footer := c.footerLines(innerW)
	if c.fromOpen {
		// Everything behind the list recedes, as behind any popup.
		for i := range lines {
			lines[i] = c.th.Dim(lines[i])
		}
		for i := range footer {
			footer[i] = c.th.Dim(footer[i])
		}
		lines = c.overlayDropdown(lines, innerW, innerH-len(footer))
	}
	body := lines[:min(len(lines), innerH-len(footer))]
	for len(body) < innerH-len(footer) {
		body = append(body, "")
	}
	body = append(body, footer...)
	return FrameTitled(c.th, c.title, strings.Join(body, "\n"), c.w, c.h)
}

// attachmentsRow is the Attachments row. When the items don't fit, the
// window starts at the selected one, after "… " if others come before it,
// and is cut off with "…".
func (c Compose) attachmentsRow(w int) string {
	label := fmt.Sprintf("%-*s", composeLabelW, "Attach")
	focused := c.focus == ComposeAttachments
	if focused {
		label = c.th.Accent.Render(label)
	} else {
		label = c.th.Muted.Render(label)
	}
	avail := max(w-composeLabelW, 1)
	render := func(from int) string {
		parts := make([]string, 0, len(c.atts)-from)
		for i := from; i < len(c.atts); i++ {
			if focused && i == c.attSel {
				parts = append(parts, c.th.Cursor.Render(c.atts[i]))
			} else {
				parts = append(parts, c.atts[i])
			}
		}
		return strings.Join(parts, "  ")
	}
	items := render(0)
	if ansi.StringWidth(items) > avail {
		items = render(c.attSel)
		if c.attSel > 0 {
			items = c.th.Muted.Render("… ") + items
		}
		items = ansi.Truncate(items, avail, "…")
	}
	return label + items
}

// overlayDropdown draws the From choices in a bordered box over the rows
// under the From row, starting on the row right under it at
// the value column. It scrolls so the highlighted choice stays visible; rows
// is how many body rows there are above the footer.
func (c *Compose) overlayDropdown(lines []string, w, rows int) []string {
	const top = 1 // the From row
	frameH := 2   // border rows
	n := max(min(len(c.from), rows-top-frameH, len(lines)-top-frameH), 1)
	if c.fromHi < c.fromTop {
		c.fromTop = c.fromHi
	}
	if c.fromHi >= c.fromTop+n {
		c.fromTop = c.fromHi - n + 1
	}
	avail := max(w-composeLabelW, 5)
	longest := 0
	for _, ch := range c.from {
		longest = max(longest, ansi.StringWidth(ch))
	}
	boxW := min(longest+4, avail)
	innerW := boxW - 4
	items := make([]string, n)
	for i := range items {
		idx := c.fromTop + i
		text := ansi.Truncate(c.from[idx], innerW, "…")
		text += strings.Repeat(" ", max(innerW-ansi.StringWidth(text), 0))
		if idx == c.fromHi {
			text = c.th.Cursor.Render(text)
		}
		items[i] = text
	}
	box := strings.Split(Frame(c.th, strings.Join(items, "\n"), boxW, n+frameH), "\n")
	for i, l := range box {
		if top+i < len(lines) {
			lines[top+i] = splice(lines[top+i], l, composeLabelW)
		}
	}
	return lines
}

// splice draws over on line starting at column col, keeping what line has to
// the left and right of it.
func splice(line, over string, col int) string {
	left := ansi.Truncate(line, col, "")
	left += strings.Repeat(" ", max(col-ansi.StringWidth(left), 0))
	return left + over + ansi.TruncateLeft(line, col+ansi.StringWidth(over), "")
}

// footerLines are the last rows: the attach prompt, the close prompt, an error or the busy
// label. The keys themselves are in the hint bar (see Hints); with nothing to
// say the row stays blank so the body doesn't move. An error wraps onto up to
// composeErrLines rows, fewer when the popup is short.
func (c Compose) footerLines(w int) []string {
	switch {
	case c.attaching:
		line := c.th.Accent.Render(fmt.Sprintf("%-*s", attachLabelW, "Attach file:")) + c.attachIn.View()
		under := ""
		switch {
		case c.attachErr != "":
			under = c.th.Error.Render(ansi.Truncate(oneLine(c.attachErr), w, "…"))
		case len(c.attachChoices) > 0:
			under = c.th.Muted.Render(ansi.Truncate(strings.Join(c.attachChoices, "  "), w, "…"))
		}
		return []string{ansi.Truncate(line, w, "…"), under}
	case c.prompting:
		return []string{ansi.Truncate(c.th.Cursor.Padding(0, 1).Render(c.promptQuestion()+"  "+HintLine(c.Hints())), w, "…")}
	case c.err != "":
		_, innerH := FrameInner(c.w, c.h)
		n := min(composeErrLines, max(1, innerH-composeRows-c.extraRows()))
		lines := strings.Split(ansi.Wrap(oneLine(c.err), w, ""), "\n")
		if len(lines) > n {
			lines = lines[:n]
			lines[n-1] = ansi.Truncate(lines[n-1]+" …", w, "…")
		}
		for i, l := range lines {
			lines[i] = c.th.Error.Render(l)
		}
		return lines
	case c.busy != "":
		return []string{ansi.Truncate(c.th.Muted.Render(c.busy), w, "…")}
	}
	return []string{""}
}

func (c Compose) promptQuestion() string {
	if c.canSave {
		return "Save draft?"
	}
	return "Discard draft?"
}

// Hints are the keys that currently work, for the hint bar.
func (c Compose) Hints() []key.Binding {
	h := func(k, desc string) key.Binding { return key.NewBinding(key.WithKeys(k), key.WithHelp(k, desc)) }
	switch {
	case c.busy != "" && c.cancellable:
		return []key.Binding{h("esc", "cancel"), h("ctrl+c", "quit now")}
	case c.busy != "":
		return []key.Binding{h("ctrl+c", "quit now")}
	case c.prompting && c.canSave:
		return []key.Binding{h("s", "save"), h("d", "discard"), h("e", "keep editing"), h("esc", "save & close")}
	case c.prompting:
		return []key.Binding{h("d", "discard"), h("e", "keep editing")}
	}
	if c.attaching {
		return []key.Binding{h("tab", "complete"), h("enter", "attach"), h("esc", "cancel")}
	}
	if c.fromOpen {
		return []key.Binding{h("↑/↓", "move"), h("enter", "pick"), h("esc", "close")}
	}
	keys := []key.Binding{h("ctrl+s", "send"), h("ctrl+o", "attach"), h("tab", "next field"), h("shift+tab", "previous field"), h("esc", "close")}
	if c.focus == ComposeAttachments {
		keys = append([]key.Binding{h("←/→", "choose"), h("d", "remove")}, keys...)
	}
	if c.focus == ComposeFrom {
		keys = append([]key.Binding{h("enter", "choose"), h("←/→", "switch")}, keys...)
	}
	return keys
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
