package popup

import (
	"fmt"
	"strings"

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
	th        theme.Theme
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
	innerW, innerH := FrameInner(w, h)
	for i := range c.headers {
		c.headers[i].SetWidth(max(innerW-composeLabelW-1, 1))
	}
	c.body.SetWidth(max(innerW, 1))
	c.body.SetHeight(max(innerH-composeRows, 1))
}

// SetError shows msg in the footer. An empty msg clears it.
func (c *Compose) SetError(msg string) { c.err = msg }

// SetBusy shows label in the footer and ignores keys until it's set to "".
func (c *Compose) SetBusy(label string) {
	c.busy = label
	if label != "" {
		c.err = ""
		c.prompting = false
	}
}

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

	body := lines[:min(len(lines), innerH-1)]
	for len(body) < innerH-1 {
		body = append(body, "")
	}
	body = append(body, c.footer(innerW))
	return FrameTitled(c.th, c.title, strings.Join(body, "\n"), c.w, c.h)
}

func (c Compose) footer(w int) string {
	var s string
	switch {
	case c.prompting && c.canSave:
		s = c.th.Cursor.Padding(0, 1).Render("Save draft?  s save · d discard · e keep editing · esc save & close")
	case c.prompting:
		s = c.th.Cursor.Padding(0, 1).Render("Discard draft?  d discard · e keep editing")
	case c.err != "":
		s = c.th.Error.Render(oneLine(c.err))
	case c.busy != "":
		s = c.th.Muted.Render(c.busy)
	default:
		s = c.th.Muted.Render("ctrl+s send · tab next field · esc close")
	}
	return ansi.Truncate(s, w, "…")
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
