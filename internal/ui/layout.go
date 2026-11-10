package ui

import "fmt"

const (
	// listForcedBelow is the width under which list + popup mode is forced.
	listForcedBelow = 80
	sidebarWidth    = 24
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

// Kind is the layout actually used, after applying the breakpoint.
type Kind int

const (
	// KindNarrow is the folders sidebar beside the thread list.
	KindNarrow Kind = iota
	// KindList is the thread list alone.
	KindList
)

func (k Kind) String() string {
	switch k {
	case KindNarrow:
		return "narrow"
	case KindList:
		return "list"
	}
	return fmt.Sprintf("kind(%d)", int(k))
}

// Rect is a screen area in cells. A zero width means the pane is hidden.
type Rect struct{ X, Y, W, H int }

// Panes is where everything goes for one terminal size. Conversations are
// always read in the popup.
type Panes struct {
	Kind          Kind
	Sidebar, List Rect
	Status, Hints Rect
	// Popup is the centered area used by popups in any kind.
	Popup Rect
}

// Layout places the panes for a width × height terminal. It is pure so the
// breakpoint can be table-tested.
func Layout(width, height int, mode Mode) Panes {
	width, height = max(width, 0), max(height, 0)
	body := max(height-footerRows, 0)

	p := Panes{
		Status: Rect{0, max(height-2, 0), width, min(height, 1)},
		Hints:  Rect{0, max(height-1, 0), width, min(max(height-1, 0), 1)},
	}

	if mode == ModeList || width < listForcedBelow {
		p.Kind = KindList
		p.List = Rect{0, 0, width, body}
	} else {
		p.Kind = KindNarrow
		p.Sidebar = Rect{0, 0, sidebarWidth, body}
		p.List = Rect{sidebarWidth, 0, width - sidebarWidth, body}
	}

	pw, ph := width*popupPercent/100, body*popupPercent/100
	p.Popup = Rect{(width - pw) / 2, (body - ph) / 2, pw, ph}
	return p
}
