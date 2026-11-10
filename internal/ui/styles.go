package ui

import "charm.land/lipgloss/v2"

type styles struct {
	pane, paneFocused lipgloss.Style
	title, dim        lipgloss.Style
	status, statusErr lipgloss.Style
	hint              lipgloss.Style
}

func defaultStyles() styles {
	pane := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240")).
		Padding(0, 1)
	return styles{
		pane:        pane,
		paneFocused: pane.BorderForeground(lipgloss.Color("63")),
		title:       lipgloss.NewStyle().Bold(true),
		dim:         lipgloss.NewStyle().Foreground(lipgloss.Color("244")),
		status:      lipgloss.NewStyle().Foreground(lipgloss.Color("244")),
		statusErr:   lipgloss.NewStyle().Foreground(lipgloss.Color("203")),
		hint:        lipgloss.NewStyle().Foreground(lipgloss.Color("110")),
	}
}
