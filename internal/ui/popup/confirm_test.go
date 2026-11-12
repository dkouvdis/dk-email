package popup

import (
	"errors"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/dkouvdis/dk-email/internal/mail"
	"github.com/dkouvdis/dk-email/internal/ui/theme"
)

func testDraft() mail.Draft {
	return mail.Draft{
		From:    mail.Address{Name: "Me", Email: "me@example.com"},
		To:      []mail.Address{{Name: "Alice", Email: "alice@example.com"}},
		Cc:      []mail.Address{{Email: "bob@example.com"}},
		Subject: "Lunch",
		Body:    "Shall we?\nSecond line.\n",
	}
}

func newTestConfirm(parseErr, sendErr error) Confirm {
	c := NewConfirm(theme.Default(), testDraft(), parseErr, sendErr)
	c.SetSize(100, 30)
	return c
}

func TestConfirm_ShowsDraftAndFooter(t *testing.T) {
	view := ansi.Strip(newTestConfirm(nil, nil).View())

	for _, want := range []string{"From", "Me <me@example.com>", "To", "Alice <alice@example.com>", "Cc", "bob@example.com", "Subject", "Lunch", "Shall we?", "Second line.", "y send · e edit · d discard"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "Bcc") {
		t.Errorf("an empty Bcc is shown:\n%s", view)
	}
}

func TestConfirm_ShowsBccWhenPresent(t *testing.T) {
	d := testDraft()
	d.Bcc = []mail.Address{{Email: "hidden@example.com"}}
	c := NewConfirm(theme.Default(), d, nil, nil)
	c.SetSize(100, 30)

	if view := ansi.Strip(c.View()); !strings.Contains(view, "Bcc") || !strings.Contains(view, "hidden@example.com") {
		t.Errorf("view missing Bcc:\n%s", view)
	}
}

func TestConfirm_ParseErrorDisablesSend(t *testing.T) {
	c := newTestConfirm(errors.New(`unknown header "Too" on line 1`), nil)

	view := ansi.Strip(c.View())
	if !strings.Contains(view, `Can't send: unknown header "Too"`) || !strings.Contains(view, "e edit · d discard") || strings.Contains(view, "y send") {
		t.Errorf("view should show the error, hint e and no y:\n%s", view)
	}
	if _, r := c.Update(keyMsg("y")); r != ConfirmNone {
		t.Errorf("y with a parse error = %v, want ConfirmNone", r)
	}
	if _, r := c.Update(keyMsg("e")); r != ConfirmEdit {
		t.Errorf("e = %v, want ConfirmEdit", r)
	}
}

func TestConfirm_ShowsSendError(t *testing.T) {
	c := newTestConfirm(nil, errors.New("smtp: connection refused"))

	view := ansi.Strip(c.View())
	if !strings.Contains(view, "Send failed: smtp: connection refused") || !strings.Contains(view, "y send") {
		t.Errorf("view should show the error and keep y:\n%s", view)
	}
	if _, r := c.Update(keyMsg("y")); r != ConfirmSend {
		t.Errorf("y = %v, want ConfirmSend (retry)", r)
	}
}

func TestConfirm_Keys(t *testing.T) {
	tests := []struct {
		k    string
		want ConfirmResult
	}{
		{"y", ConfirmSend},
		{"e", ConfirmEdit},
		{"d", ConfirmDiscard},
		{"esc", ConfirmNone},
		{"q", ConfirmNone},
		{"enter", ConfirmNone},
		{"x", ConfirmNone},
	}
	for _, tt := range tests {
		t.Run(tt.k, func(t *testing.T) {
			if _, got := newTestConfirm(nil, nil).Update(keyMsg(tt.k)); got != tt.want {
				t.Errorf("%q = %v, want %v", tt.k, got, tt.want)
			}
		})
	}
}

func TestConfirm_BodyPreviewIsCutAndTruncated(t *testing.T) {
	d := testDraft()
	var lines []string
	for i := range 20 {
		lines = append(lines, "line "+string(rune('a'+i)))
	}
	lines[0] = strings.Repeat("w", 200)
	d.Body = strings.Join(lines, "\n")
	c := NewConfirm(theme.Default(), d, nil, nil)
	c.SetSize(100, 40)

	view := c.View()
	plain := ansi.Strip(view)

	if !strings.Contains(plain, "line b") || strings.Contains(plain, "line n") || !strings.Contains(plain, "…") {
		t.Errorf("preview should stop after about 8 lines with …:\n%s", plain)
	}
	if w := lipgloss.Width(view); w > 72+helpChromeCols {
		t.Errorf("width = %d; long lines should be truncated", w)
	}
}

func TestConfirm_EmptyBodyAndSmallSize(t *testing.T) {
	d := testDraft()
	d.Body = ""
	c := NewConfirm(theme.Default(), d, nil, nil)
	c.SetSize(100, 30)
	if view := ansi.Strip(c.View()); !strings.Contains(view, "(empty message)") {
		t.Errorf("view missing the empty-body note:\n%s", view)
	}

	c.SetSize(100, 12)
	if h := lipgloss.Height(c.View()); h > 12 || h == 0 {
		t.Errorf("height = %d, want it to fit 12 rows", h)
	}
	c.SetSize(20, 5)
	if c.View() != "" {
		t.Error("a popup with no room should render nothing")
	}
}
