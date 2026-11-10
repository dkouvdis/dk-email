package popup

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func pickerKey(k string) tea.KeyPressMsg {
	switch k {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	}
	return tea.KeyPressMsg{Code: []rune(k)[0], Text: k}
}

func typeKeys[T any](p Picker[T], text string) Picker[T] {
	for _, r := range text {
		p, _ = p.Update(pickerKey(string(r)))
	}
	return p
}

func groups() []Group[string] {
	return []Group[string]{
		{Name: "Personal", Items: []Item[string]{
			{Label: "Inbox", Count: 2, Value: "p/inbox"},
			{Label: "Sent Mail", Value: "p/sent"},
			{Label: "Receipts", Count: 1, Value: "p/receipts", Current: true},
		}},
		{Name: "Broken", Note: "login failed"},
		{Name: "Work", Items: []Item[string]{
			{Label: "Inbox", Count: 1, Value: "w/inbox"},
		}},
	}
}

func TestPicker_StartsOnCurrentAndMovesAcrossGroups(t *testing.T) {
	p := NewPicker("Go to folder", groups())
	p.SetSize(80, 30)

	if v, _ := p.Selected(); v != "p/receipts" {
		t.Fatalf("starts on %q, want the current folder", v)
	}
	steps := []struct{ key, want string }{
		{"j", "w/inbox"}, // skips the Broken group, which has no folders
		{"j", "w/inbox"},
		{"g", "p/inbox"},
		{"down", "p/sent"},
		{"k", "p/inbox"},
		{"k", "p/inbox"},
		{"G", "w/inbox"},
	}
	for _, s := range steps {
		var r Result
		p, r = p.Update(pickerKey(s.key))
		if r != Pending {
			t.Fatalf("%s returned %v, want Pending", s.key, r)
		}
		if v, _ := p.Selected(); v != s.want {
			t.Fatalf("after %s on %q, want %q", s.key, v, s.want)
		}
	}
}

func TestPicker_EnterChoosesAndEscCancels(t *testing.T) {
	p := NewPicker("Go to folder", groups())

	_, r := p.Update(pickerKey("enter"))
	if r != Chosen {
		t.Errorf("enter = %v, want Chosen", r)
	}
	_, r = p.Update(pickerKey("esc"))
	if r != Cancelled {
		t.Errorf("esc = %v, want Cancelled", r)
	}
	_, r = p.Update(pickerKey("x"))
	if r != Pending {
		t.Errorf("x = %v, want Pending", r)
	}
}

func TestPicker_Empty(t *testing.T) {
	p := NewPicker("Go to folder", []Group[string]{{Name: "Personal", Note: "connecting…"}})
	p.SetSize(80, 30)

	if _, ok := p.Selected(); ok {
		t.Error("Selected() ok with no items")
	}
	if _, r := p.Update(pickerKey("enter")); r != Pending {
		t.Errorf("enter with nothing to choose = %v, want Pending", r)
	}
	if !strings.Contains(ansi.Strip(p.View()), "connecting…") {
		t.Errorf("note not shown:\n%s", ansi.Strip(p.View()))
	}
}

func TestPicker_View(t *testing.T) {
	p := NewPicker("Go to folder", groups())
	p.SetSize(80, 30)
	p, _ = p.Update(pickerKey("g"))

	view := p.View()
	plain := ansi.Strip(view)

	for _, want := range []string{"Go to folder", "esc close", "Personal", "Broken", "login failed", "Work", "▸ Inbox", "▶ Receipts", "enter open"} {
		if !strings.Contains(plain, want) {
			t.Errorf("view missing %q:\n%s", want, plain)
		}
	}
	lines := strings.Split(plain, "\n")
	if !rowEndsWith(lines, "Inbox", "2") || !rowEndsWith(lines, "Receipts", "1") {
		t.Errorf("unread counts not right-aligned on their rows:\n%s", plain)
	}
	if w := lipgloss.Width(view); w > 60 {
		t.Errorf("width = %d, want a compact popup", w)
	}
}

func rowEndsWith(lines []string, label, count string) bool {
	for _, l := range lines {
		l = strings.TrimSuffix(strings.TrimRight(l, " "), "│")
		if strings.Contains(l, label) && strings.HasSuffix(strings.TrimRight(l, " "), " "+count) {
			return true
		}
	}
	return false
}

func TestPicker_ScrollsAndClamps(t *testing.T) {
	var items []Item[int]
	for i := range 40 {
		items = append(items, Item[int]{Label: "folder", Value: i})
	}
	p := NewPicker("Go to folder", []Group[int]{{Name: "Personal", Items: items}})
	p.SetSize(50, 14)

	p, _ = p.Update(pickerKey("G"))
	view := p.View()

	if h := lipgloss.Height(view); h != 14 {
		t.Errorf("height = %d, want clamped to 14", h)
	}
	if v, _ := p.Selected(); v != 39 {
		t.Errorf("selected %d, want 39", v)
	}
	if !strings.Contains(ansi.Strip(view), "▸ folder") {
		t.Errorf("cursor row not visible after G:\n%s", ansi.Strip(view))
	}
	p.SetSize(10, 3)
	if p.View() != "" {
		t.Error("tiny picker should render nothing")
	}
}

func TestPicker_SlashFiltersFoldersByLabel(t *testing.T) {
	p := NewPicker("Go to folder", groups())
	p.SetSize(80, 30)

	p = typeKeys(p, "/INB")

	if !p.Filtering() {
		t.Fatal("/ didn't start filtering")
	}
	plain := ansi.Strip(p.View())
	if strings.Contains(plain, "Receipts") || strings.Contains(plain, "Sent Mail") || strings.Contains(plain, "Broken") {
		t.Errorf("non-matching folders or empty accounts still shown:\n%s", plain)
	}
	if !strings.Contains(plain, "/ INB") || !strings.Contains(plain, "Personal") || !strings.Contains(plain, "Work") {
		t.Errorf("filter or matching accounts missing:\n%s", plain)
	}
	if v, _ := p.Selected(); v != "p/inbox" {
		t.Errorf("cursor on %q, want the first match once the current folder is filtered out", v)
	}

	p, _ = p.Update(pickerKey("down"))
	if v, _ := p.Selected(); v != "w/inbox" {
		t.Errorf("↓ while filtering = %q, want the next match", v)
	}
	p, r := p.Update(pickerKey("enter"))
	if v, _ := p.Selected(); r != Chosen || v != "w/inbox" {
		t.Errorf("enter while filtering = %v %q, want Chosen w/inbox", r, v)
	}
}

func TestPicker_FilterTakesEveryKeyAndEscClearsFirst(t *testing.T) {
	p := NewPicker("Go to folder", groups())
	p.SetSize(80, 30)

	p = typeKeys(p, "/jkq") // j, k and q are text while filtering
	if !strings.Contains(ansi.Strip(p.View()), "No folders match") {
		t.Errorf("no-match message missing:\n%s", ansi.Strip(p.View()))
	}
	if _, ok := p.Selected(); ok {
		t.Error("Selected() ok with no matches")
	}
	if _, r := p.Update(pickerKey("enter")); r != Pending {
		t.Errorf("enter with no matches = %v, want Pending", r)
	}

	p, r := p.Update(pickerKey("esc"))
	if r != Pending || p.Filtering() || !strings.Contains(ansi.Strip(p.View()), "Receipts") {
		t.Errorf("first esc = %v (filtering %v), want the filter cleared and the popup open", r, p.Filtering())
	}
	if _, r = p.Update(pickerKey("esc")); r != Cancelled {
		t.Errorf("second esc = %v, want Cancelled", r)
	}
}

func TestPicker_PasteIntoFilter(t *testing.T) {
	p := NewPicker("Go to folder", groups())
	p.SetSize(80, 30)

	p = p.Paste(tea.PasteMsg{Content: "rece"})
	if p.Filtering() {
		t.Error("paste started a filter without /")
	}
	p, _ = p.Update(pickerKey("/"))
	p = p.Paste(tea.PasteMsg{Content: "rece\nipts"})

	if v, _ := p.Selected(); v != "p/receipts" || strings.Contains(ansi.Strip(p.View()), "Inbox") {
		t.Errorf("paste didn't filter to Receipts: %q\n%s", v, ansi.Strip(p.View()))
	}
}

func TestPicker_SizeStaysPutWhileFiltering(t *testing.T) {
	p := NewPicker("Go to folder", groups())
	p.SetSize(80, 30)
	beforeW, beforeH := lipgloss.Size(p.View())

	for _, k := range []string{"/", "w", "o", "r", "k", "-", "x"} {
		p, _ = p.Update(pickerKey(k))
		if w, h := lipgloss.Size(p.View()); w != beforeW || h != beforeH {
			t.Fatalf("after %q the popup is %dx%d, want it to stay %dx%d", k, w, h, beforeW, beforeH)
		}
	}
}

func TestPicker_ScrollingUpShowsTheAccountName(t *testing.T) {
	group := func(name string) Group[string] {
		g := Group[string]{Name: name}
		for i := range 5 {
			g.Items = append(g.Items, Item[string]{Label: fmt.Sprintf("%s folder %d", name, i), Value: fmt.Sprint(name, i)})
		}
		return g
	}
	p := NewPicker("Go to folder", []Group[string]{group("Personal"), group("Work"), group("Proton")})
	p.SetSize(50, 6+1+6) // chrome, filter row and 6 item lines

	p, _ = p.Update(pickerKey("G"))
	p, _ = p.Update(pickerKey("g"))

	lines := strings.Split(ansi.Strip(p.View()), "\n")
	if first := strings.Trim(lines[4], "│ "); first != "Personal" {
		t.Errorf("first item line = %q, want the Personal heading:\n%s", first, strings.Join(lines, "\n"))
	}
}

func TestPicker_NestedAndDisabledItems(t *testing.T) {
	p := NewPicker("Go to folder", []Group[string]{{Name: "Personal", Items: []Item[string]{
		{Label: "Inbox", Value: "INBOX", Path: "Inbox"},
		{Label: "Work", Value: "Work", Path: "Work", Disabled: true},
		{Label: "Clients", Value: "Work/Clients", Path: "Work/Clients", Depth: 1},
		{Label: "Acme", Value: "Work/Clients/Acme", Path: "Work/Clients/Acme", Depth: 2, Count: 3},
	}}})
	p.SetSize(80, 30)

	lines := strings.Split(ansi.Strip(p.View()), "\n")
	for _, want := range []string{"│   Work", "│     Clients", "│       Acme"} {
		if !slices.ContainsFunc(lines, func(l string) bool { return strings.HasPrefix(l, want) }) {
			t.Errorf("no line starting %q:\n%s", want, strings.Join(lines, "\n"))
		}
	}
	if !rowEndsWith(lines, "Acme", "3") {
		t.Errorf("nested count not right-aligned:\n%s", strings.Join(lines, "\n"))
	}

	p, _ = p.Update(pickerKey("j"))
	if v, _ := p.Selected(); v != "Work/Clients" {
		t.Errorf("j moved to %q, want to skip the disabled Work", v)
	}

	p = typeKeys(p, "/acme")
	if plain := ansi.Strip(p.View()); !strings.Contains(plain, "▸ Work/Clients/Acme") || strings.Contains(plain, "  Work\n") {
		t.Errorf("while filtering, want the match with its path:\n%s", plain)
	}
	q := NewPicker("x", p.groups)
	q.SetSize(80, 30)
	q = typeKeys(q, "/work")
	if !strings.Contains(ansi.Strip(q.View()), "No folders match") {
		t.Errorf("a disabled item shouldn't match:\n%s", ansi.Strip(q.View()))
	}
}
