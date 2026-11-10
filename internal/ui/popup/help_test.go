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
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
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
	// 17 content lines + border, header, filter, blank, blank, footer.
	if got := lipgloss.Height(view); got != 17+7 {
		t.Errorf("height = %d, want %d", got, 17+7)
	}
	for _, want := range []string{"Keys", "esc close", "/ press / to filter", "one", "three", "c3", "third group", "/ filter · esc/q close"} {
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
	h.SetSize(50, 12) // 5 rows of content
	view := h.View()

	if w, ht := lipgloss.Width(view), lipgloss.Height(view); w > 50 || ht != 12 {
		t.Fatalf("size = %dx%d, want at most 50 wide and exactly 12 high", w, ht)
	}
	plain := ansi.Strip(view)
	if !strings.Contains(plain, "j/k/pgup/pgdn scroll") || !strings.Contains(plain, "┃") {
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
		{"pgdown", 5}, {"pgdown", 10}, {"pgdown", 12}, {"j", 12}, {"pgup", 7},
		{"G", 12}, {"g", 0},
	}
	for _, s := range steps {
		h, _ = h.Update(keyMsg(s.key))
		if h.offset != s.want {
			t.Fatalf("after %s: offset = %d, want %d", s.key, h.offset, s.want)
		}
	}

	h, _ = h.Update(keyMsg("G"))
	if plain := ansi.Strip(h.View()); !strings.Contains(plain, "c3") || strings.Contains(plain, "a0") {
		t.Errorf("bottom view should show the last key and not the first:\n%s", plain)
	}
}

func TestHelp_ResetAndResizeKeepOffsetValid(t *testing.T) {
	h := NewHelp(sections())
	h.SetSize(50, 12)
	h, _ = h.Update(keyMsg("G"))

	h.SetSize(50, 100)
	if h.offset != 0 {
		t.Errorf("offset = %d after growing to fit everything, want 0", h.offset)
	}
	h.SetSize(50, 12)
	h, _ = h.Update(keyMsg("G"))
	h.Reset()
	if h.offset != 0 {
		t.Errorf("offset = %d after Reset, want 0", h.offset)
	}
}

func TestHelp_TooSmallRendersNothing(t *testing.T) {
	h := NewHelp(sections())
	for _, s := range [][2]int{{0, 0}, {20, 30}, {50, 7}} {
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
	if ht := lipgloss.Height(view); ht != 2+7 {
		t.Errorf("height = %d, want %d (no wrapping)", ht, 2+7)
	}
}

func typeText(h Help, text string) Help {
	for _, r := range text {
		h, _ = h.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return h
}

func TestHelp_FilterNarrowsAndHidesEmptySections(t *testing.T) {
	h := NewHelp(sections())
	h.SetSize(80, 40)
	size := lipgloss.Height(h.View())

	h, r := h.Update(keyMsg("/"))
	if !h.Filtering() || r != HelpOpen {
		t.Fatal("/ didn't focus the filter")
	}
	h = typeText(h, "B2")

	plain := ansi.Strip(h.View())
	if !strings.Contains(plain, "b2") || strings.Contains(plain, "a0") || strings.Contains(plain, "c3") {
		t.Errorf("filter b2 shows the wrong keys:\n%s", plain)
	}
	if strings.Contains(plain, "one") || strings.Contains(plain, "three") || !strings.Contains(plain, "two") {
		t.Errorf("sections without matches should be hidden:\n%s", plain)
	}
	if !strings.Contains(plain, "enter keep · esc clear") {
		t.Errorf("footer doesn't explain the filter keys:\n%s", plain)
	}
	if got := lipgloss.Height(h.View()); got != size {
		t.Errorf("height changed from %d to %d while filtering", size, got)
	}

	h = typeText(h, "x")
	plain = ansi.Strip(h.View())
	if !strings.Contains(plain, "No keys match") || lipgloss.Height(h.View()) != size {
		t.Errorf("no matches should say so and keep the size:\n%s", plain)
	}

	h, _ = h.Update(keyMsg("backspace"))
	if !strings.Contains(ansi.Strip(h.View()), "b2") {
		t.Error("backspace didn't widen the filter again")
	}

	// Descriptions match too, case-insensitively.
	h.Reset()
	h, _ = h.Update(keyMsg("/"))
	h = typeText(h, "THIRD")
	if plain := ansi.Strip(h.View()); !strings.Contains(plain, "c0") || strings.Contains(plain, "a0") {
		t.Errorf("description filter failed:\n%s", plain)
	}
}

func TestHelp_FilterTakesEveryPrintableKey(t *testing.T) {
	h := NewHelp(sections())
	h.SetSize(80, 40)
	h, _ = h.Update(keyMsg("/"))

	for _, k := range []string{"q", "?", "j", "G", "/"} {
		var r HelpResult
		h, r = h.Update(keyMsg(k))
		if r != HelpOpen {
			t.Errorf("%s closed the popup while filtering", k)
		}
	}
	if got := h.filter.Value(); got != "q?jG/" {
		t.Errorf("filter = %q, want every key typed in", got)
	}
}

func TestHelp_EscClearsThenCloses(t *testing.T) {
	h := NewHelp(sections())
	h.SetSize(80, 40)

	h, _ = h.Update(keyMsg("/"))
	h = typeText(h, "b")
	h, r := h.Update(keyMsg("esc"))
	if r != HelpOpen || h.Filtering() || h.filter.Value() != "" || !strings.Contains(ansi.Strip(h.View()), "a0") {
		t.Fatal("first esc should clear the filter and leave the input")
	}
	if _, r = h.Update(keyMsg("esc")); r != HelpClosed {
		t.Error("second esc should close the popup")
	}

	// enter keeps the filter; then esc clears it before closing.
	h, _ = h.Update(keyMsg("/"))
	h = typeText(h, "b")
	h, _ = h.Update(keyMsg("enter"))
	if h.Filtering() || h.filter.Value() != "b" {
		t.Fatal("enter should leave the input and keep the filter")
	}
	if h, _ = h.Update(keyMsg("j")); h.Filtering() {
		t.Error("j should scroll, not type, after enter")
	}
	h, r = h.Update(keyMsg("esc"))
	if r != HelpOpen || h.filter.Value() != "" {
		t.Error("esc with a kept filter should clear it first")
	}
	for _, k := range []string{"q", "?"} {
		if _, r = h.Update(keyMsg(k)); r != HelpClosed {
			t.Errorf("%s with no filter should close the popup", k)
		}
	}
}

func TestHelp_FilterResetsScroll(t *testing.T) {
	h := NewHelp(sections())
	h.SetSize(50, 12)
	h, _ = h.Update(keyMsg("G"))

	h, _ = h.Update(keyMsg("/"))
	h = typeText(h, "a")

	if h.offset != 0 {
		t.Errorf("offset = %d after filtering, want 0", h.offset)
	}
}

func TestHelp_PasteIntoFilter(t *testing.T) {
	h := NewHelp(sections())
	h.SetSize(80, 40)
	if h.Paste(tea.PasteMsg{Content: "b2"}).filter.Value() != "" {
		t.Error("paste without the filter focused should do nothing")
	}

	h, _ = h.Update(keyMsg("/"))
	h = h.Paste(tea.PasteMsg{Content: "b2\n"})

	if plain := ansi.Strip(h.View()); !strings.Contains(plain, "b2") || strings.Contains(plain, "a0") {
		t.Errorf("pasted filter not applied:\n%s", plain)
	}
}
