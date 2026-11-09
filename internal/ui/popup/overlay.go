package popup

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Overlay draws pop over base with its top-left corner at x, y.
func Overlay(base, pop string, x, y int) string {
	if pop == "" {
		return base
	}
	return lipgloss.NewCompositor(
		lipgloss.NewLayer(base),
		lipgloss.NewLayer(pop).X(x).Y(y).Z(1),
	).Render()
}

// Center returns where to draw pop so it's centered in a w × h area.
func Center(w, h int, pop string) (x, y int) {
	return max((w-lipgloss.Width(pop))/2, 0), max((h-lipgloss.Height(pop))/2, 0)
}

// Frame puts content in a bordered box of exactly w × h cells, cutting it
// to fit. It returns "" when the box has no room inside.
func Frame(content string, w, h int) string {
	box := NewHelp(nil).st.box
	innerW, innerH := w-box.GetHorizontalFrameSize(), h-box.GetVerticalFrameSize()
	if innerW < 1 || innerH < 1 {
		return ""
	}
	lines := strings.Split(content, "\n")
	lines = lines[:min(len(lines), innerH)]
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, innerW, "")
	}
	return box.Width(w).Height(h).Render(strings.Join(lines, "\n"))
}

// FrameInner is the content size inside a Frame of w × h.
func FrameInner(w, h int) (int, int) {
	box := NewHelp(nil).st.box
	return max(w-box.GetHorizontalFrameSize(), 0), max(h-box.GetVerticalFrameSize(), 0)
}
