// Package popup renders the centered popups drawn over the main layout.
package popup

import (
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Help renders the full key reference in a bordered box of exactly
// width × height cells, or "" when there's no room for it.
func Help(groups [][]key.Binding, width, height int) string {
	if width < 10 || height < 5 {
		return ""
	}
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("63")).
		Padding(0, 1).
		Width(width).
		Height(height)
	innerW := width - box.GetHorizontalFrameSize()
	innerH := height - box.GetVerticalFrameSize()

	h := help.New()
	h.SetWidth(innerW)
	h.FullSeparator = "   "

	title := lipgloss.NewStyle().Bold(true).Render("Keys")
	footer := lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Render("esc or ? to close")
	body := h.FullHelpView(groups)

	content := lipgloss.JoinVertical(lipgloss.Left, title, "", body)
	content = clip(content, innerW, innerH-1)
	content = lipgloss.PlaceVertical(innerH-1, lipgloss.Top, content)
	return box.Render(lipgloss.JoinVertical(lipgloss.Left, content, footer))
}

// clip cuts s to at most w columns and h lines so the box never grows.
func clip(s string, w, h int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > h {
		lines = lines[:max(h, 0)]
	}
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, w, "…")
	}
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}
