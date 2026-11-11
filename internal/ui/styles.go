package ui

import (
	"charm.land/lipgloss/v2"

	"github.com/dkouvdis/dk-email/internal/ui/theme"
)

type styles struct {
	pane, paneFocused lipgloss.Style
	title, dim        lipgloss.Style
	status, statusErr lipgloss.Style
	hint              lipgloss.Style
}

func newStyles(th theme.Theme) styles {
	pane := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(th.BorderColor).
		Padding(0, 1)
	return styles{
		pane:        pane,
		paneFocused: pane.BorderForeground(th.AccentColor),
		title:       lipgloss.NewStyle().Bold(true),
		dim:         th.Muted,
		status:      th.Muted,
		statusErr:   th.Error,
		hint:        th.Hint,
	}
}
