package theme

import (
	"strings"
	"testing"

	"github.com/dkouvdis/dk-email/internal/config"
)

func TestDefault_MutedIsFaintNotAColour(t *testing.T) {
	got := Default().Muted.Render("x")

	if !strings.Contains(got, "\x1b[2m") {
		t.Errorf("muted = %q, want the faint attribute (SGR 2)", got)
	}
}

func TestDefault_CursorTextIsFixedBlack(t *testing.T) {
	got := Default().Cursor.Render("x")

	if !strings.Contains(got, "38;5;16") || !strings.Contains(got, "45") {
		t.Errorf("cursor = %q, want 256-colour black (38;5;16) on ANSI magenta", got)
	}
}

func TestNew_OverridesReplaceDefaults(t *testing.T) {
	th := New(config.Colors{Accent: "#ff0000", OnAccent: "15", Muted: "8"})

	if got := th.Cursor.Render("x"); !strings.Contains(got, "97;48;2;255;0;0m") {
		t.Errorf("cursor = %q, want bright white on #ff0000", got)
	}
	if got := th.Muted.Render("x"); !strings.Contains(got, "\x1b[90m") || strings.Contains(got, "\x1b[2m") {
		t.Errorf("muted = %q, want ANSI 8 instead of faint", got)
	}
	if got := th.Star.Render("x"); !strings.Contains(got, "\x1b[33m") {
		t.Errorf("star = %q, want the default (ANSI yellow) kept", got)
	}
}
