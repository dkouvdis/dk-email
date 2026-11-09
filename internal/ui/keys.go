package ui

import "charm.land/bubbles/v2/key"

// KeyMap is the fixed v1 keymap from the spec.
type KeyMap struct {
	Up, Down, Top, Bottom  key.Binding
	Left, Right            key.Binding
	NextPane, PrevPane     key.Binding
	Open, Expand           key.Binding
	ToggleRead, Star       key.Binding
	Archive, Delete, Move  key.Binding
	Refresh                key.Binding
	Picker, Connect        key.Binding
	ToggleView, Help, Back key.Binding
	Quit                   key.Binding
}

func DefaultKeyMap() KeyMap {
	b := func(keys []string, help, desc string) key.Binding {
		return key.NewBinding(key.WithKeys(keys...), key.WithHelp(help, desc))
	}
	return KeyMap{
		Up:         b([]string{"k", "up"}, "k/↑", "up"),
		Down:       b([]string{"j", "down"}, "j/↓", "down"),
		Top:        b([]string{"g"}, "g", "top"),
		Bottom:     b([]string{"G"}, "G", "bottom"),
		Left:       b([]string{"h", "left"}, "h/←", "left pane"),
		Right:      b([]string{"l", "right"}, "l/→", "right pane"),
		NextPane:   b([]string{"tab"}, "tab", "next pane"),
		PrevPane:   b([]string{"shift+tab"}, "shift+tab", "previous pane"),
		Open:       b([]string{"enter"}, "enter", "open"),
		Expand:     b([]string{"space"}, "space", "expand"),
		ToggleRead: b([]string{"u"}, "u", "read/unread"),
		Star:       b([]string{"s"}, "s", "star"),
		Archive:    b([]string{"e"}, "e", "archive"),
		Delete:     b([]string{"d"}, "d", "delete"),
		Move:       b([]string{"m"}, "m", "move"),
		Refresh:    b([]string{"r"}, "r", "refresh"),
		Picker:     b([]string{"a"}, "a", "accounts"),
		Connect:    b([]string{"A"}, "A", "connect account"),
		ToggleView: b([]string{"v"}, "v", "split/list"),
		Help:       b([]string{"?"}, "?", "help"),
		Back:       b([]string{"esc"}, "esc", "back"),
		Quit:       b([]string{"q", "ctrl+c"}, "q", "quit"),
	}
}

// FullHelp groups every key for the ? popup.
func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Top, k.Bottom, k.Open, k.Expand},
		{k.NextPane, k.PrevPane, k.Left, k.Right, k.Picker, k.ToggleView},
		{k.ToggleRead, k.Star, k.Archive, k.Delete, k.Move, k.Refresh},
		{k.Connect, k.Help, k.Back, k.Quit},
	}
}

// hints are the keys shown in the hint bar for the focused pane, most
// important first; the bar truncates from the end. ? and q are added by the
// bar itself so they're never cut off.
func (k KeyMap) hints(focus Pane, kind Kind) []key.Binding {
	keys := []key.Binding{k.Up, k.Down, k.Open}
	if focus == PaneReader {
		keys = keys[:2]
	}
	if kind != KindWide {
		keys = append(keys, k.Picker)
	}
	switch focus {
	case PaneList:
		keys = append(keys, k.Expand, k.ToggleRead, k.Star, k.Archive, k.Delete, k.Move)
	case PaneReader:
		keys = append(keys, k.ToggleRead, k.Star, k.Archive, k.Delete, k.Move)
	}
	if kind != KindList {
		keys = append(keys, k.NextPane)
	}
	return append(keys, k.ToggleView)
}
