package scrollbar

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func thumbs(bar []string) string { return strings.Join(bar, "") }

func TestRender_FitsIsBlank(t *testing.T) {
	st := lipgloss.NewStyle()
	if got := thumbs(Render(st, 4, 4, 0)); got != "    " {
		t.Errorf("fits: %q", got)
	}
	if got := thumbs(Render(st, 4, 2, 0)); got != "    " {
		t.Errorf("shorter than view: %q", got)
	}
}

func TestRender_Positions(t *testing.T) {
	st := lipgloss.NewStyle()
	if got := thumbs(Render(st, 4, 8, 0)); got != "┃┃  " {
		t.Errorf("top: %q", got)
	}
	if got := thumbs(Render(st, 4, 8, 4)); got != "  ┃┃" {
		t.Errorf("bottom: %q", got)
	}
	if got := thumbs(Render(st, 4, 8, 99)); got != "  ┃┃" {
		t.Errorf("offset past the end: %q", got)
	}
	if got := thumbs(Render(st, 4, 8, -3)); got != "┃┃  " {
		t.Errorf("negative offset: %q", got)
	}
}

func TestRender_ThumbAtLeastOne(t *testing.T) {
	st := lipgloss.NewStyle()
	bar := Render(st, 5, 1000, 0)
	if strings.Count(thumbs(bar), "┃") != 1 || bar[0] != "┃" {
		t.Errorf("tiny thumb: %q", thumbs(bar))
	}
	bar = Render(st, 5, 1000, 995)
	if bar[4] != "┃" {
		t.Errorf("thumb not on last row at bottom: %q", thumbs(bar))
	}
}

func TestRender_NoHeight(t *testing.T) {
	if Render(lipgloss.NewStyle(), 0, 10, 0) != nil {
		t.Error("height 0 should be nil")
	}
}
