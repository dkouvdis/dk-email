package popup

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/charmbracelet/x/ansi"
	"github.com/dkouvdis/dk-email/internal/ui/theme"
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
func Frame(th theme.Theme, content string, w, h int) string {
	return FrameTitled(th, "", content, w, h)
}

// FrameTitled is Frame with title drawn into the top border, cut with … to
// keep the border its full width. An empty title gives a plain Frame.
func FrameTitled(th theme.Theme, title, content string, w, h int) string {
	box := frameBox(th)
	innerW, innerH := w-box.GetHorizontalFrameSize(), h-box.GetVerticalFrameSize()
	if innerW < 1 || innerH < 1 {
		return ""
	}
	lines := strings.Split(content, "\n")
	lines = lines[:min(len(lines), innerH)]
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, innerW, "")
	}
	out := box.Width(w).Height(h).Render(strings.Join(lines, "\n"))
	title = strings.TrimSpace(strings.NewReplacer("\r", " ", "\n", " ").Replace(title))
	if title == "" {
		return out
	}
	// "╭─ " + title + " " + dashes + "╮"
	title = ansi.Truncate(title, w-5, "…")
	if title == "" {
		return out
	}
	b := lipgloss.RoundedBorder()
	edge := lipgloss.NewStyle().Foreground(th.AccentColor)
	dashes := strings.Repeat(b.Top, max(w-5-ansi.StringWidth(title), 0))
	top := edge.Render(b.TopLeft+b.Top+" ") + th.Accent.Bold(true).Inline(true).Render(title) + edge.Render(" "+dashes+b.TopRight)
	_, rest, _ := strings.Cut(out, "\n")
	return top + "\n" + rest
}

// FrameInner is the content size inside a Frame of w × h.
func FrameInner(w, h int) (int, int) {
	box := frameBox(theme.Theme{})
	return max(w-box.GetHorizontalFrameSize(), 0), max(h-box.GetVerticalFrameSize(), 0)
}
