package popup

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func bind(k, desc string) key.Binding {
	return key.NewBinding(key.WithKeys(k), key.WithHelp(k, desc))
}

// sections has 3 titles + 12 keys + 2 blank separators = 17 lines.
func sections() []Section {
	var a, b, c []key.Binding
	for i := range 4 {
		a = append(a, bind("a"+string(rune('0'+i)), "first group"))
		b = append(b, bind("b"+string(rune('0'+i)), "second group"))
		c = append(c, bind("c"+string(rune('0'+i)), "third group"))
	}
	return []Section{{"one", a}, {"two", b}, {"three", c}}
}

func keyMsg(k string) tea.KeyPressMsg {
	switch k {
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "pgdown":
		return tea.KeyPressMsg{Code: tea.KeyPgDown}
	case "pgup":
		return tea.KeyPressMsg{Code: tea.KeyPgUp}
	}
	return tea.KeyPressMsg{Code: []rune(k)[0], Text: k}
}

func TestHelp_SizesToContentWhenItFits(t *testing.T) {
	h := NewHelp(sections())
	h.SetSize(200, 100)

	view := h.View()
	plain := ansi.Strip(view)

	if w := lipgloss.Width(view); w > 60 {
		t.Errorf("width = %d, want a compact popup (≤ 60) when there's room", w)
	}
	// 17 content lines + border, header, blank, blank, footer.
	if got := lipgloss.Height(view); got != 17+6 {
		t.Errorf("height = %d, want %d", got, 17+6)
	}
	for _, want := range []string{"Keys", "esc close", "one", "three", "c3", "third group", "close esc/?"} {
		if !strings.Contains(plain, want) {
			t.Errorf("view missing %q:\n%s", want, plain)
		}
	}
	if strings.Contains(plain, "scroll") || strings.Contains(plain, "┃") {
		t.Errorf("view shows scrolling though everything fits:\n%s", plain)
	}
}

func TestHelp_ClampsToMaxSizeAndScrolls(t *testing.T) {
	h := NewHelp(sections())
	h.SetSize(50, 12) // 6 rows of content
	view := h.View()

	if w, ht := lipgloss.Width(view), lipgloss.Height(view); w > 50 || ht != 12 {
		t.Fatalf("size = %dx%d, want at most 50 wide and exactly 12 high", w, ht)
	}
	plain := ansi.Strip(view)
	if !strings.Contains(plain, "scroll j/k") || !strings.Contains(plain, "┃") {
		t.Errorf("overflowing view lacks scroll hint or scrollbar:\n%s", plain)
	}
	if strings.Contains(plain, "c3") {
		t.Error("last key visible before scrolling")
	}

	steps := []struct {
		key  string
		want int
	}{
		{"j", 1}, {"down", 2}, {"k", 1}, {"up", 0}, {"up", 0},
		{"pgdown", 6}, {"pgdown", 11}, {"j", 11}, {"pgup", 5},
		{"G", 11}, {"g", 0},
	}
	for _, s := range steps {
		h = h.Update(keyMsg(s.key))
		if h.offset != s.want {
			t.Fatalf("after %s: offset = %d, want %d", s.key, h.offset, s.want)
		}
	}

	h = h.Update(keyMsg("G"))
	if plain := ansi.Strip(h.View()); !strings.Contains(plain, "c3") || strings.Contains(plain, "a0") {
		t.Errorf("bottom view should show the last key and not the first:\n%s", plain)
	}
}

func TestHelp_ResetAndResizeKeepOffsetValid(t *testing.T) {
	h := NewHelp(sections())
	h.SetSize(50, 12)
	h = h.Update(keyMsg("G"))

	h.SetSize(50, 100)
	if h.offset != 0 {
		t.Errorf("offset = %d after growing to fit everything, want 0", h.offset)
	}
	h.SetSize(50, 12)
	h = h.Update(keyMsg("G"))
	h.Reset()
	if h.offset != 0 {
		t.Errorf("offset = %d after Reset, want 0", h.offset)
	}
}

func TestHelp_TooSmallRendersNothing(t *testing.T) {
	h := NewHelp(sections())
	for _, s := range [][2]int{{0, 0}, {20, 30}, {50, 6}} {
		h.SetSize(s[0], s[1])
		if v := h.View(); v != "" {
			t.Errorf("SetSize(%d, %d): view = %q, want empty", s[0], s[1], v)
		}
	}
}

func TestHelp_LongDescriptionsAreTruncatedNotWrapped(t *testing.T) {
	h := NewHelp([]Section{{"x", []key.Binding{bind("z", strings.Repeat("long ", 30))}}})
	h.SetSize(40, 20)

	view := h.View()

	if w := lipgloss.Width(view); w != 40 {
		t.Errorf("width = %d, want clamped to 40", w)
	}
	if ht := lipgloss.Height(view); ht != 2+6 {
		t.Errorf("height = %d, want %d (no wrapping)", ht, 2+6)
	}
}
