package ui

import (
	"strconv"
	"testing"

	"github.com/dkouvdis/dk-email/internal/config"
)

func TestLayout_WidthsPerModeAndBreakpoint(t *testing.T) {
	ui := config.Default().UI // wide_breakpoint 120, list_width 35
	const h = 30
	body := h - 2

	tests := []struct {
		width                    int
		mode                     Mode
		want                     Kind
		sidebarW, listW, readerW int
	}{
		{60, ModeSplit, KindList, 0, 60, 0},
		{79, ModeSplit, KindList, 0, 79, 0},
		{80, ModeSplit, KindNarrow, 0, 28, 52},
		{119, ModeSplit, KindNarrow, 0, 41, 78},
		{120, ModeSplit, KindWide, 24, 33, 63},
		{200, ModeSplit, KindWide, 24, 61, 115},

		{60, ModeList, KindList, 0, 60, 0},
		{79, ModeList, KindList, 0, 79, 0},
		{80, ModeList, KindList, 0, 80, 0},
		{119, ModeList, KindList, 0, 119, 0},
		{120, ModeList, KindList, 0, 120, 0},
		{200, ModeList, KindList, 0, 200, 0},
	}
	for _, tt := range tests {
		t.Run(tt.mode.String()+"/"+strconv.Itoa(tt.width), func(t *testing.T) {
			got := Layout(tt.width, h, tt.mode, ui)

			if got.Kind != tt.want {
				t.Errorf("Kind = %v, want %v", got.Kind, tt.want)
			}
			if got.Sidebar.W != tt.sidebarW || got.List.W != tt.listW || got.Reader.W != tt.readerW {
				t.Errorf("widths sidebar/list/reader = %d/%d/%d, want %d/%d/%d",
					got.Sidebar.W, got.List.W, got.Reader.W, tt.sidebarW, tt.listW, tt.readerW)
			}
			if sum := got.Sidebar.W + got.List.W + got.Reader.W; sum != tt.width {
				t.Errorf("pane widths sum to %d, want the full width %d", sum, tt.width)
			}
			// Panes sit side by side, left to right, filling the body.
			x := 0
			for _, r := range []Rect{got.Sidebar, got.List, got.Reader} {
				if r.W == 0 {
					continue
				}
				if r.X != x || r.Y != 0 || r.H != body {
					t.Errorf("pane %+v, want X=%d Y=0 H=%d", r, x, body)
				}
				x += r.W
			}
			if got.Status != (Rect{0, h - 2, tt.width, 1}) || got.Hints != (Rect{0, h - 1, tt.width, 1}) {
				t.Errorf("status %+v / hints %+v, want the last two rows", got.Status, got.Hints)
			}
		})
	}
}

func TestLayout_UsesConfiguredBreakpointAndListWidth(t *testing.T) {
	ui := config.UI{WideBreakpoint: 140, ListWidth: 50}

	narrow := Layout(139, 30, ModeSplit, ui)
	wide := Layout(140, 30, ModeSplit, ui)

	if narrow.Kind != KindNarrow || narrow.List.W != 69 {
		t.Errorf("139 cols: %v list %d, want narrow with list 69 (50%%)", narrow.Kind, narrow.List.W)
	}
	if wide.Kind != KindWide {
		t.Errorf("140 cols: %v, want wide", wide.Kind)
	}
}

func TestLayout_PopupIsCenteredAt90Percent(t *testing.T) {
	got := Layout(100, 42, ModeList, config.Default().UI)

	if want := (Rect{X: 5, Y: 2, W: 90, H: 36}); got.Popup != want {
		t.Errorf("Popup = %+v, want %+v", got.Popup, want)
	}
}

func TestLayout_TinyTerminalHasNoNegativeSizes(t *testing.T) {
	for _, size := range [][2]int{{0, 0}, {1, 1}, {10, 2}, {5, 3}} {
		got := Layout(size[0], size[1], ModeSplit, config.Default().UI)
		for _, r := range []Rect{got.Sidebar, got.List, got.Reader, got.Status, got.Hints, got.Popup} {
			if r.W < 0 || r.H < 0 || r.X < 0 || r.Y < 0 {
				t.Errorf("Layout(%d, %d) has negative rect %+v", size[0], size[1], r)
			}
		}
	}
}
