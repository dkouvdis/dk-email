package popup

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/dkouvdis/dk-email/internal/ui/theme"
)

func TestOverlay(t *testing.T) {
	base := strings.Repeat(strings.Repeat(".", 10)+"\n", 4) + strings.Repeat(".", 10)

	got := ansi.Strip(Overlay(base, "ab\ncd", 3, 1))

	want := "..........\n...ab.....\n...cd.....\n..........\n.........."
	if got != want {
		t.Errorf("Overlay =\n%s\nwant\n%s", got, want)
	}
	if Overlay(base, "", 0, 0) != base {
		t.Error("an empty popup should leave the base unchanged")
	}
}

func TestCenter(t *testing.T) {
	if x, y := Center(100, 40, "xxxx\nxxxx"); x != 48 || y != 19 {
		t.Errorf("Center = %d, %d; want 48, 19", x, y)
	}
	if x, y := Center(2, 1, "xxxx\nxxxx"); x != 0 || y != 0 {
		t.Errorf("Center of an oversized popup = %d, %d; want 0, 0", x, y)
	}
}

func TestFrame(t *testing.T) {
	got := ansi.Strip(Frame(theme.Default(), "hello\nworld", 12, 5))

	lines := strings.Split(got, "\n")
	if len(lines) != 5 || ansi.StringWidth(lines[0]) != 12 || !strings.Contains(got, "hello") {
		t.Errorf("Frame =\n%s\nwant a 12×5 box", got)
	}
	if Frame(theme.Default(), "x", 3, 2) != "" {
		t.Error("a frame too small for content should render nothing")
	}
}

func TestFrameTitled(t *testing.T) {
	th := theme.Default()
	check := func(title string, w, h int) []string {
		t.Helper()
		lines := strings.Split(ansi.Strip(FrameTitled(th, title, "body", w, h)), "\n")
		if len(lines) != h {
			t.Fatalf("title %q: %d lines, want %d", title, len(lines), h)
		}
		for i, l := range lines {
			if got := ansi.StringWidth(l); got != w {
				t.Errorf("title %q: line %d is %d wide, want %d: %q", title, i, got, w, l)
			}
		}
		return lines
	}

	if top := check("Reply · Re: Lunch?", 30, 4)[0]; top != "╭─ Reply · Re: Lunch? ───────╮" {
		t.Errorf("top = %q", top)
	}
	if top := check("Reply · Re: Lunch? · me@example.com", 20, 4)[0]; top != "╭─ Reply · Re: Lu… ╮" {
		t.Errorf("truncated top = %q", top)
	}
	if top := check("日本語のタイトルです", 17, 4)[0]; !strings.Contains(top, "…") {
		t.Errorf("wide title not truncated: %q", top)
	}
	check("日本語", 20, 4)
	check("日本語日本語日本語日本語", 21, 4)
	check("title", 5, 3)
	check("title", 6, 3)

	if got, want := FrameTitled(th, "", "x\ny", 12, 5), Frame(th, "x\ny", 12, 5); got != want {
		t.Errorf("empty title differs from Frame:\n%s\n%s", got, want)
	}
	if FrameTitled(th, "title", "x", 3, 2) != "" {
		t.Error("a frame with no room inside should render nothing")
	}
}
