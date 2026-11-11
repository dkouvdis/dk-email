package ui

import (
	"strconv"
	"testing"
)

func TestLayout_WidthsPerModeAndBreakpoint(t *testing.T) {
	const h = 30
	body := h - 2

	tests := []struct {
		width           int
		mode            Mode
		want            Kind
		sidebarW, listW int
	}{
		{60, ModeSplit, KindList, 0, 60},
		{79, ModeSplit, KindList, 0, 79},
		{80, ModeSplit, KindNarrow, 32, 48},
		{200, ModeSplit, KindNarrow, 32, 168},

		{60, ModeList, KindList, 0, 60},
		{80, ModeList, KindList, 0, 80},
		{200, ModeList, KindList, 0, 200},
	}
	for _, tt := range tests {
		t.Run(tt.mode.String()+"/"+strconv.Itoa(tt.width), func(t *testing.T) {
			got := Layout(tt.width, h, tt.mode)

			if got.Kind != tt.want {
				t.Errorf("Kind = %v, want %v", got.Kind, tt.want)
			}
			if got.Sidebar.W != tt.sidebarW || got.List.W != tt.listW {
				t.Errorf("widths sidebar/list = %d/%d, want %d/%d", got.Sidebar.W, got.List.W, tt.sidebarW, tt.listW)
			}
			// Panes sit side by side, left to right, filling the body.
			x := 0
			for _, r := range []Rect{got.Sidebar, got.List} {
				if r.W == 0 {
					continue
				}
				if r.X != x || r.Y != 0 || r.H != body {
					t.Errorf("pane %+v, want X=%d Y=0 H=%d", r, x, body)
				}
				x += r.W
			}
			if x != tt.width {
				t.Errorf("panes fill %d columns, want the full width %d", x, tt.width)
			}
			if got.Status != (Rect{0, h - 2, tt.width, 1}) || got.Hints != (Rect{0, h - 1, tt.width, 1}) {
				t.Errorf("status %+v / hints %+v, want the last two rows", got.Status, got.Hints)
			}
		})
	}
}

func TestLayout_PopupIsCenteredAt90Percent(t *testing.T) {
	got := Layout(100, 42, ModeList)

	if want := (Rect{X: 5, Y: 2, W: 90, H: 36}); got.Popup != want {
		t.Errorf("Popup = %+v, want %+v", got.Popup, want)
	}
}

func TestLayout_TinyTerminalHasNoNegativeSizes(t *testing.T) {
	for _, size := range [][2]int{{0, 0}, {1, 1}, {10, 2}, {5, 3}} {
		got := Layout(size[0], size[1], ModeSplit)
		for _, r := range []Rect{got.Sidebar, got.List, got.Status, got.Hints, got.Popup} {
			if r.W < 0 || r.H < 0 || r.X < 0 || r.Y < 0 {
				t.Errorf("Layout(%d, %d) has negative rect %+v", size[0], size[1], r)
			}
		}
	}
}
