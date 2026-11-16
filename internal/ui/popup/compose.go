package popup

import (
	"fmt"
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
)

// ComposeValues is the text of every field.
type ComposeValues struct{ To, Cc, Bcc, Subject, Body string }

const (
	composeLabelW = 9 // "Subject" and the gap after it
	composeRows   = 6 // the 4 header rows, the rule and the footer around the body
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
	c.body.SetWidth(max(innerW, 1))
	c.body.SetHeight(max(innerH-composeRows-(len(c.footerLines(innerW))-1), 1))
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
func (c Compose) Dirty() bool { return c.Values() != c.initial }

// Prompting reports whether the close prompt is showing.
func (c Compose) Prompting() bool { return c.prompting }

func (c *Compose) setFocus(f ComposeField) {
	c.focus = f
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
	c.setFocus((c.focus + ComposeField(step) + composeFields) % composeFields)
}

// forward gives msg to the focused input.
func (c *Compose) forward(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
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
		return c.updateKey(msg)
	case tea.PasteMsg:
		if c.busy != "" || c.prompting {
			return c, ComposeNone, nil
		}
		if c.focus != ComposeBody {
			msg.Content = headerLine(msg.Content)
		}
		return c, ComposeNone, c.forward(msg)
	}
	return c, ComposeNone, c.forward(msg)
}

func (c Compose) updateKey(msg tea.KeyPressMsg) (Compose, ComposeResult, tea.Cmd) {
	switch msg.String() {
	case "ctrl+s":
		return c, ComposeSend, nil
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
		if c.focus != ComposeBody {
			c.move(1)
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

// headerLine makes pasted text fit a single-line field.
func headerLine(s string) string {
	s = strings.Trim(s, "\r\n")
	return strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(s)
}

// View draws the composer at exactly the size given to SetSize, or returns ""
// when that's too small to use.
func (c Compose) View() string {
	innerW, innerH := FrameInner(c.w, c.h)
	if innerW < 20 || innerH <= composeRows {
		return ""
	}
	lines := make([]string, 0, innerH)
	for i := range c.headers {
		label := fmt.Sprintf("%-*s", composeLabelW, composeLabels[i])
		if ComposeField(i) == c.focus {
			label = c.th.Accent.Render(label)
		} else {
			label = c.th.Muted.Render(label)
		}
		lines = append(lines, label+c.headers[i].View())
	}
	rule := c.th.Muted.Render(strings.Repeat("─", innerW))
	if c.focus == ComposeBody {
		rule = c.th.Accent.Render(strings.Repeat("─", innerW))
	}
	lines = append(lines, rule)
	lines = append(lines, strings.Split(c.body.View(), "\n")...)

	footer := c.footerLines(innerW)
	body := lines[:min(len(lines), innerH-len(footer))]
	for len(body) < innerH-len(footer) {
		body = append(body, "")
	}
	body = append(body, footer...)
	return FrameTitled(c.th, c.title, strings.Join(body, "\n"), c.w, c.h)
}

// footerLines are the last rows: the close prompt, an error or the busy
// label. The keys themselves are in the hint bar (see Hints); with nothing to
// say the row stays blank so the body doesn't move. An error wraps onto up to
// composeErrLines rows, fewer when the popup is short.
func (c Compose) footerLines(w int) []string {
	switch {
	case c.prompting:
		return []string{ansi.Truncate(c.th.Cursor.Padding(0, 1).Render(c.promptQuestion()+"  "+HintLine(c.Hints())), w, "…")}
	case c.err != "":
		_, innerH := FrameInner(c.w, c.h)
		n := min(composeErrLines, max(1, innerH-composeRows))
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
	return []key.Binding{h("ctrl+s", "send"), h("tab", "next field"), h("shift+tab", "previous field"), h("esc", "close")}
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
