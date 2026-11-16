// Package scrollbar draws a one-column vertical scrollbar.
package scrollbar

import (
	"charm.land/lipgloss/v2"
)

// Render returns one cell per visible row: a thumb where the view is, blank elsewhere, or all blank when everything fits.
func Render(thumb lipgloss.Style, height, total, offset int) []string {
	if height <= 0 {
		return nil
	}
	bar := make([]string, height)
	for i := range bar {
		bar[i] = " "
	}
	if total <= height {
		return bar
	}
	size := max(height*height/total, 1)
	offset = max(min(offset, total-height), 0)
	pos := offset * (height - size) / (total - height)
	cell := thumb.Render("┃")
	for i := pos; i < pos+size && i < height; i++ {
		bar[i] = cell
	}
	return bar
}
