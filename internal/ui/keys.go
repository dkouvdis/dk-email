package ui

import (
	"charm.land/bubbles/v2/key"

	"github.com/dkouvdis/dk-email/internal/ui/popup"
)

// KeyMap is the fixed v1 keymap from the spec.
type KeyMap struct {
	Up, Down, Top, Bottom  key.Binding
	Left, Right            key.Binding
	NextPane, PrevPane     key.Binding
	Open, Expand           key.Binding
	ToggleRead, Star       key.Binding
	Archive, Delete, Move  key.Binding
	Refresh                key.Binding
	Reply, ReplyAll        key.Binding
	Forward, Compose       key.Binding
	Picker, Filter, Search key.Binding
	Connect, EditAccount   key.Binding
	ToggleView, Help, Back key.Binding
	Quit, ForceQuit        key.Binding
}

func DefaultKeyMap() KeyMap {
	b := func(keys []string, help, desc string) key.Binding {
		return key.NewBinding(key.WithKeys(keys...), key.WithHelp(help, desc))
	}
	return KeyMap{
		Up:          b([]string{"k", "up"}, "k/↑", "up"),
		Down:        b([]string{"j", "down"}, "j/↓", "down"),
		Top:         b([]string{"g"}, "g", "top"),
		Bottom:      b([]string{"G"}, "G", "bottom"),
		Left:        b([]string{"h", "left"}, "h/←", "left pane"),
		Right:       b([]string{"l", "right"}, "l/→", "right pane"),
		NextPane:    b([]string{"tab"}, "tab", "next pane"),
		PrevPane:    b([]string{"shift+tab"}, "shift+tab", "previous pane"),
		Open:        b([]string{"enter"}, "enter", "open"),
		Expand:      b([]string{"space"}, "space", "expand"),
		ToggleRead:  b([]string{"u"}, "u", "read/unread"),
		Star:        b([]string{"s"}, "s", "star"),
		Archive:     b([]string{"e"}, "e", "archive"),
		Delete:      b([]string{"d"}, "d", "delete"),
		Move:        b([]string{"m"}, "m", "move"),
		Refresh:     b([]string{"ctrl+r"}, "ctrl+r", "refresh"),
		Reply:       b([]string{"r"}, "r", "reply"),
		ReplyAll:    b([]string{"R"}, "R", "reply all"),
		Forward:     b([]string{"f"}, "f", "forward"),
		Compose:     b([]string{"c"}, "c", "new message"),
		Picker:      b([]string{"a"}, "a", "accounts"),
		Filter:      b([]string{"/"}, "/", "filter folder names"),
		Search:      b([]string{"/"}, "/", "search folder"),
		Connect:     b([]string{"A"}, "A", "connect account"),
		EditAccount: b([]string{"E"}, "E", "edit account"),
		ToggleView:  b([]string{"v"}, "v", "split/list"),
		Help:        b([]string{"?"}, "?", "help"),
		Back:        b([]string{"esc"}, "esc", "back"),
		Quit:        b([]string{"q"}, "q", "quit"),
		ForceQuit:   b([]string{"ctrl+c"}, "ctrl+c", "quit now"),
	}
}

// HelpSections groups every key for the ? popup.
func (k KeyMap) HelpSections() []popup.Section {
	help := func(keys, desc string) key.Binding {
		return key.NewBinding(key.WithKeys(keys), key.WithHelp(keys, desc))
	}
	return []popup.Section{
		{Title: "Navigation", Keys: []key.Binding{k.Up, k.Down, k.Top, k.Bottom, k.Left, k.Right, k.NextPane, k.PrevPane}},
		{Title: "Threads", Keys: []key.Binding{k.Open, k.Expand, k.ToggleRead, k.Star, k.Archive, k.Delete, k.Move, k.Refresh}},
		{Title: "Search (thread list)", Keys: []key.Binding{
			help("/", "search this folder"),
			help("enter", "run the search"),
			help("esc", "clear the search"),
			help("ctrl+r", "run the search again"),
			help("from: to: cc:", "match an address"),
			help("subject: body:", "match a field"),
			help("is:unread", "also is:read, is:starred"),
			help("since: before:", "dates as YYYY-MM-DD"),
			help("-word", "exclude a term"),
			help(`"a phrase"`, "match exact words"),
		}},
		{Title: "Compose", Keys: []key.Binding{k.Reply, k.ReplyAll, k.Forward, k.Compose, help("enter", "edit draft (in Drafts)")}},
		{Title: "Composer", Keys: []key.Binding{
			help("ctrl+s", "send"),
			help("tab", "next field"),
			help("shift+tab", "previous field"),
			help("enter / ←/→", "choose From"),
			help("enter", "next field (in a header)"),
			help("esc", "close, or save / discard"),
			help("ctrl+c", "quit now"),
		}},
		{Title: "Accounts & view", Keys: []key.Binding{k.Picker, k.Filter, k.Connect, k.EditAccount, k.ToggleView}},
		{Title: "General", Keys: []key.Binding{k.Help, k.Back, k.Quit, k.ForceQuit}},
	}
}

// hints are the keys shown in the hint bar for the focused pane, or for the
// reader popup while reading, most important first; the bar truncates from
// the end. ? and q are added by the bar itself so they're never cut off.
func (k KeyMap) hints(focus Pane, reading bool, kind Kind) []key.Binding {
	if reading {
		return []key.Binding{k.Up, k.Down, k.Back, k.Expand, k.ToggleRead, k.Star, k.Archive, k.Delete, k.Move, k.Reply, k.Forward, k.Compose}
	}
	keys := []key.Binding{k.Up, k.Down, k.Open}
	if kind == KindList {
		keys = append(keys, k.Picker)
	}
	switch focus {
	case PaneList:
		keys = append(keys, k.Expand, k.ToggleRead, k.Star, k.Archive, k.Delete, k.Move, k.Reply, k.Forward, k.Compose, k.Search)
	case PaneSidebar:
		keys = append(keys, key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")))
	}
	if kind == KindNarrow {
		keys = append(keys, k.NextPane)
	}
	return append(keys, k.ToggleView)
}
