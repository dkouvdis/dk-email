package ui

import (
	"fmt"

	"github.com/dkouvdis/dk-email/internal/config"
)

const (
	// listForcedBelow is the width under which list + popup mode is forced.
	listForcedBelow = 80
	sidebarWidth    = 24
	// wideListPercent is the thread list's share of the space beside the sidebar.
	wideListPercent = 35
	popupPercent    = 90
	// Status line and hint bar.
	footerRows = 2
)

// Mode is the layout the user asked for with default_layout or v.
type Mode int

const (
	ModeSplit Mode = iota
	ModeList
)

func (m Mode) String() string {
	if m == ModeList {
		return "list"
	}
	return "split"
}

// Kind is the layout actually used, after applying the breakpoints.
type Kind int

const (
	KindWide Kind = iota
	KindNarrow
	KindList
)

func (k Kind) String() string {
	switch k {
	case KindWide:
		return "wide"
	case KindNarrow:
		return "narrow"
	case KindList:
		return "list"
	}
	return fmt.Sprintf("kind(%d)", int(k))
}

// Rect is a screen area in cells. A zero width means the pane is hidden.
type Rect struct{ X, Y, W, H int }

// Panes is where everything goes for one terminal size.
type Panes struct {
	Kind                  Kind
	Sidebar, List, Reader Rect
	Status, Hints         Rect
	// Popup is the centered area used by popups in any kind.
	Popup Rect
}

// Layout places the panes for a width × height terminal. It is pure so the
// breakpoints can be table-tested.
func Layout(width, height int, mode Mode, ui config.UI) Panes {
	width, height = max(width, 0), max(height, 0)
	body := max(height-footerRows, 0)

	p := Panes{
		Status: Rect{0, max(height-2, 0), width, min(height, 1)},
		Hints:  Rect{0, max(height-1, 0), width, min(max(height-1, 0), 1)},
	}

	switch {
	case mode == ModeList || width < listForcedBelow:
		p.Kind = KindList
		p.List = Rect{0, 0, width, body}
	case width >= ui.WideBreakpoint:
		p.Kind = KindWide
		rest := width - sidebarWidth
		listW := rest * wideListPercent / 100
		p.Sidebar = Rect{0, 0, sidebarWidth, body}
		p.List = Rect{sidebarWidth, 0, listW, body}
		p.Reader = Rect{sidebarWidth + listW, 0, rest - listW, body}
	default:
		p.Kind = KindNarrow
		listW := width * ui.ListWidth / 100
		p.List = Rect{0, 0, listW, body}
		p.Reader = Rect{listW, 0, width - listW, body}
	}

	pw, ph := width*popupPercent/100, body*popupPercent/100
	p.Popup = Rect{(width - pw) / 2, (body - ph) / 2, pw, ph}
	return p
}
