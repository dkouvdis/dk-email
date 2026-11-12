package popup

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/charmbracelet/x/ansi"
	"github.com/dkouvdis/dk-email/internal/mail"
	"github.com/dkouvdis/dk-email/internal/ui/theme"
)

const (
	confirmInnerW      = 72
	confirmPreviewRows = 8
	minConfirmInner    = 30
	minConfirmHeight   = 10
	// maxConfirmAttachments is the most rows the attachment list takes,
	// "+n more" included.
	maxConfirmAttachments = 5
	// minConfirmPreview is the body rows kept when attachments compete for room.
	minConfirmPreview = 2
)

// ConfirmResult is what a key press asks of the model.
type ConfirmResult int

const (
	ConfirmNone ConfirmResult = iota
	ConfirmSend
	ConfirmEdit
	ConfirmDiscard
)

// Confirm is the popup shown after the editor closes: a summary of the
// draft with y send, e edit and d discard. Esc and q do nothing, so a draft
// is only ever dropped on purpose.
type Confirm struct {
	draft       mail.Draft
	attachments []string
	parseErr    error
	sendErr     error
	maxW, maxH  int
	st          helpStyles
	keys        struct{ send, edit, discard key.Binding }
}

// NewConfirm shows d. attachments describe the files the message carries,
// each already formatted, e.g. "report.pdf (1.2 MB)". parseErr is why the
// edited file couldn't be read, which disables sending; sendErr is why the
// last attempt to send d failed.
func NewConfirm(th theme.Theme, d mail.Draft, attachments []string, parseErr, sendErr error) Confirm {
	c := Confirm{draft: d, attachments: attachments, parseErr: parseErr, sendErr: sendErr, st: newHelpStyles(th)}
	c.keys.send = key.NewBinding(key.WithKeys("y"))
	c.keys.edit = key.NewBinding(key.WithKeys("e"))
	c.keys.discard = key.NewBinding(key.WithKeys("d"))
	return c
}

// SetSize caps the popup at maxW × maxH cells.
func (c *Confirm) SetSize(maxW, maxH int) { c.maxW, c.maxH = maxW, maxH }

// Update maps a key to a result. Every other key is swallowed, and y does
// nothing while the draft couldn't be parsed.
func (c Confirm) Update(msg tea.KeyPressMsg) (Confirm, ConfirmResult) {
	switch {
	case key.Matches(msg, c.keys.send) && c.parseErr == nil:
		return c, ConfirmSend
	case key.Matches(msg, c.keys.edit):
		return c, ConfirmEdit
	case key.Matches(msg, c.keys.discard):
		return c, ConfirmDiscard
	}
	return c, ConfirmNone
}

// View renders the popup, or "" when there's no room for it.
func (c Confirm) View() string {
	innerW := min(confirmInnerW, c.maxW-helpChromeCols)
	if innerW < minConfirmInner || c.maxH < minConfirmHeight {
		return ""
	}
	fit := func(s string) string { return ansi.Truncate(s, innerW, "…") }

	title := c.st.title.Render("Send message?")
	head := []string{fit(title), ""}
	field := func(name, value string) {
		head = append(head, c.st.dim.Render(fmt.Sprintf("%-*s", labelW, name))+" "+ansi.Truncate(value, innerW-labelW-1, "…"))
	}
	field("From", c.draft.From.String())
	field("To", joinAddresses(c.draft.To))
	if len(c.draft.Cc) > 0 {
		field("Cc", joinAddresses(c.draft.Cc))
	}
	if len(c.draft.Bcc) > 0 {
		field("Bcc", joinAddresses(c.draft.Bcc))
	}
	field("Subject", c.draft.Subject)

	var tail []string
	if c.parseErr != nil || c.sendErr != nil {
		tail = append(tail, "")
	}
	if c.parseErr != nil {
		tail = append(tail, c.alert("Can't send: "+c.parseErr.Error(), innerW)...)
		tail = append(tail, c.st.dim.Render("Press e to fix it."))
	}
	if c.sendErr != nil {
		tail = append(tail, c.alert("Send failed: "+c.sendErr.Error(), innerW)...)
	}
	footer := "y send · e edit · d discard"
	if c.parseErr != nil {
		footer = "e edit · d discard"
	}
	tail = append(tail, "", c.st.dim.Render(fit(footer)))

	// head and tail are fixed; the attachments and the body preview share
	// what room is left.
	free := c.maxH - 2 - len(head) - len(tail) - 1
	atts := c.attachmentRows(free - minConfirmPreview)
	room := min(confirmPreviewRows+1, free-len(atts))
	lines := append([]string{}, head...)
	lines = append(lines, atts...)
	lines = append(lines, "")
	lines = append(lines, c.preview(max(room, 0), innerW)...)
	lines = append(lines, tail...)
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, innerW, "")
	}
	return c.st.box.Width(innerW + helpChromeCols).Render(strings.Join(lines, "\n"))
}

// attachmentRows lists the attachments in at most room rows (and at most
// maxConfirmAttachments): a label row, then one row each, the last being
// "+n more" when they don't all fit. With no room for the list it is just the
// label and a count.
func (c Confirm) attachmentRows(room int) []string {
	n := len(c.attachments)
	if n == 0 || room < 1 {
		return nil
	}
	label := c.st.dim.Render("Attachments")
	limit := min(maxConfirmAttachments, room-1)
	if limit < 1 {
		return []string{label + c.st.dim.Render(fmt.Sprintf(" (%d)", n))}
	}
	shown := c.attachments
	if n > limit {
		shown = shown[:limit-1]
	}
	rows := []string{label}
	for _, a := range shown {
		rows = append(rows, "  "+strings.NewReplacer("\r", " ", "\n", " ").Replace(a))
	}
	if more := n - len(shown); more > 0 {
		rows = append(rows, c.st.dim.Render(fmt.Sprintf("  +%d more", more)))
	}
	return rows
}

// preview is the body cut to n rows; when it's longer, the last row is ….
func (c Confirm) preview(n, w int) []string {
	body := strings.TrimRight(strings.ReplaceAll(c.draft.Body, "\r", ""), "\n \t")
	if strings.TrimSpace(body) == "" {
		return []string{c.st.dim.Render("(empty message)")}
	}
	all := strings.Split(strings.TrimLeft(body, "\n"), "\n")
	// A lone … row would say nothing, so very short previews just stop.
	more := len(all) > n && n >= 3
	if more {
		all = all[:n-1]
	} else {
		all = all[:min(len(all), n)]
	}
	var out []string
	for _, l := range all {
		out = append(out, ansi.Truncate(strings.ReplaceAll(l, "\t", "    "), w, "…"))
	}
	if more {
		out = append(out, c.st.dim.Render("…"))
	}
	return out
}

// alert wraps text to w columns in the error style.
func (c Confirm) alert(text string, w int) []string {
	rows := strings.Split(lipgloss.NewStyle().Width(w).Render(text), "\n")
	for i, r := range rows {
		rows[i] = c.st.alert.Render(strings.TrimRight(r, " "))
	}
	return rows
}

func joinAddresses(l []mail.Address) string {
	s := make([]string, len(l))
	for i, a := range l {
		s[i] = a.String()
	}
	return strings.Join(s, ", ")
}
