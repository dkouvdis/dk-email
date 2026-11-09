package popup

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
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
	got := ansi.Strip(Frame("hello\nworld", 12, 5))

	lines := strings.Split(got, "\n")
	if len(lines) != 5 || ansi.StringWidth(lines[0]) != 12 || !strings.Contains(got, "hello") {
		t.Errorf("Frame =\n%s\nwant a 12×5 box", got)
	}
	if Frame("x", 3, 2) != "" {
		t.Error("a frame too small for content should render nothing")
	}
}
