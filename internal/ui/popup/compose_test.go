package popup

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/dkouvdis/dk-email/internal/ui/theme"
)

var ctrlS = tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}

func newTestCompose(v ComposeValues, focus ComposeField) Compose {
	c := NewCompose(theme.Default(), "Reply · Re: Lunch? · me@x.com", v, focus)
	c.SetSize(80, 24)
	return c
}

// press feeds keys ("ctrl+s" or anything ck knows) to c and returns the last result.
func press(c Compose, ks ...string) (Compose, ComposeResult) {
	var r ComposeResult
	for _, k := range ks {
		msg := ck(k)
		if k == "ctrl+s" {
			msg = ctrlS
		}
		c, r, _ = c.Update(msg)
	}
	return c, r
}

func composeType(c Compose, s string) Compose {
	for _, r := range s {
		c, _ = press(c, string(r))
	}
	return c
}

func TestCompose_FocusCycle(t *testing.T) {
	c := newTestCompose(ComposeValues{}, ComposeTo)
	want := []ComposeField{ComposeCc, ComposeBcc, ComposeSubject, ComposeBody, ComposeTo}
	for _, f := range want {
		c, _ = press(c, "tab")
		if c.focus != f {
			t.Fatalf("tab went to %d, want %d", c.focus, f)
		}
	}
	for _, f := range []ComposeField{ComposeBody, ComposeSubject, ComposeBcc, ComposeCc, ComposeTo, ComposeBody} {
		c, _ = press(c, "shift+tab")
		if c.focus != f {
			t.Fatalf("shift+tab went to %d, want %d", c.focus, f)
		}
	}
	c, _ = press(c, "tab")
	if c.focus != ComposeTo || c.Values().Body != "" {
		t.Errorf("tab in the body should move focus without inserting; focus %d body %q", c.focus, c.Values().Body)
	}
}

func TestCompose_EnterInHeaderMovesFocus(t *testing.T) {
	c := newTestCompose(ComposeValues{}, ComposeTo)
	c = composeType(c, "a@b.c")
	c, _ = press(c, "enter")
	if c.focus != ComposeCc || c.Values().To != "a@b.c" {
		t.Errorf("focus %d, values %+v", c.focus, c.Values())
	}
	c = newTestCompose(ComposeValues{Body: "x"}, ComposeBody)
	c, _ = press(c, "enter")
	if c.focus != ComposeBody || c.Values().Body != "\nx" {
		t.Errorf("enter in the body: focus %d, body %q", c.focus, c.Values().Body)
	}
}

func TestCompose_Typing(t *testing.T) {
	c := newTestCompose(ComposeValues{Subject: "Hi"}, ComposeSubject)
	c = composeType(c, "!")
	if got := c.Values().Subject; got != "Hi!" {
		t.Errorf("subject = %q; the cursor should start at the end", got)
	}
}

func TestCompose_BodyFocusStartsAtTop(t *testing.T) {
	quote := "\n\nOn Monday, Bob wrote:\n> lunch?"
	c := newTestCompose(ComposeValues{Body: quote}, ComposeBody)
	if c.body.Line() != 0 || c.body.Column() != 0 {
		t.Fatalf("cursor at %d,%d, want 0,0", c.body.Line(), c.body.Column())
	}
	c = composeType(c, "Hi")
	if got := c.Values().Body; got != "Hi"+quote {
		t.Errorf("body = %q", got)
	}
}

func TestCompose_LongBodyKept(t *testing.T) {
	body := strings.TrimSuffix(strings.Repeat("> quoted line\n", 500), "\n")
	c := newTestCompose(ComposeValues{Body: body}, ComposeBody)
	if got := c.Values().Body; got != body {
		t.Errorf("a 500-line body was cut to %d bytes of %d", len(got), len(body))
	}
	if c.Dirty() {
		t.Error("an untouched body is not dirty")
	}
}

func TestCompose_Send(t *testing.T) {
	c := newTestCompose(ComposeValues{}, ComposeTo)
	if _, r := press(c, "ctrl+s"); r != ComposeSend {
		t.Errorf("ctrl+s = %v, want send", r)
	}
}

func TestCompose_Dirty(t *testing.T) {
	c := newTestCompose(ComposeValues{To: "a@b.c", Body: "text"}, ComposeBody)
	if c.Dirty() {
		t.Error("a fresh composer is not dirty")
	}
	c = composeType(c, "x")
	if !c.Dirty() {
		t.Error("typing should make it dirty")
	}
	c, _ = press(c, "backspace")
	if c.Dirty() {
		t.Error("deleting back to the start should make it clean again")
	}
}

func TestCompose_EscClean(t *testing.T) {
	c := newTestCompose(ComposeValues{To: "a@b.c"}, ComposeTo)
	if c, r := press(c, "esc"); r != ComposeClose || c.Prompting() {
		t.Errorf("esc on a clean composer = %v, prompting %v", r, c.Prompting())
	}
}

func dirty(save bool) Compose {
	c := newTestCompose(ComposeValues{}, ComposeSubject)
	c.EnableSave(save)
	c = composeType(c, "x")
	c, _ = press(c, "esc")
	return c
}

func TestCompose_Prompt(t *testing.T) {
	for _, tc := range []struct {
		name      string
		save      bool
		key       string
		want      ComposeResult
		prompting bool
	}{
		{"discard", true, "d", ComposeDiscard, true},
		{"discard without save", false, "d", ComposeDiscard, true},
		{"keep editing", true, "e", ComposeNone, false},
		{"keep editing without save", false, "e", ComposeNone, false},
		{"save", true, "s", ComposeSave, false},
		{"esc saves", true, "esc", ComposeSave, false},
		{"s without save", false, "s", ComposeNone, true},
		{"esc without save", false, "esc", ComposeNone, true},
		{"other keys", true, "x", ComposeNone, true},
		{"enter", true, "enter", ComposeNone, true},
		{"tab", true, "tab", ComposeNone, true},
		{"ctrl+s", true, "ctrl+s", ComposeNone, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := dirty(tc.save)
			if !c.Prompting() {
				t.Fatal("esc on a dirty composer should open the prompt")
			}
			c, r := press(c, tc.key)
			if r != tc.want || c.Prompting() != tc.prompting {
				t.Errorf("result %v prompting %v; want %v, %v", r, c.Prompting(), tc.want, tc.prompting)
			}
			if c.Values().Subject != "x" || c.focus != ComposeSubject {
				t.Errorf("the prompt changed text or focus: %+v focus %d", c.Values(), c.focus)
			}
		})
	}
}

func TestCompose_PromptFooter(t *testing.T) {
	for save, want := range map[bool]string{
		true:  "Save draft?  Save: s | Discard: d | Keep editing: e | Save & close: esc",
		false: "Discard draft?  Discard: d | Keep editing: e",
	} {
		c := dirty(save)
		if v := ansi.Strip(c.View()); !strings.Contains(v, want) {
			t.Errorf("save=%v: view lacks %q:\n%s", save, want, v)
		}
	}
	c, _ := press(dirty(true), "e")
	if v := ansi.Strip(c.View()); strings.Contains(v, "draft?") || strings.Contains(v, "ctrl+s") {
		t.Errorf("after keep editing the footer should be blank:\n%s", v)
	}
}

func TestCompose_Hints(t *testing.T) {
	normal := newTestCompose(ComposeValues{}, ComposeTo)
	busy := normal
	busy.SetBusy("Sending…")
	noSave := dirty(false)
	for name, tc := range map[string]struct {
		c    Compose
		want string
	}{
		"normal":    {normal, "Send: ctrl+s | Next field: tab | Previous field: shift+tab | Close: esc"},
		"prompting": {dirty(true), "Save: s | Discard: d | Keep editing: e | Save & close: esc"},
		"no save":   {noSave, "Discard: d | Keep editing: e"},
		"busy":      {busy, "Quit now: ctrl+c"},
	} {
		if got := hintText(tc.c.Hints()); got != tc.want {
			t.Errorf("%s: hints = %q, want %q", name, got, tc.want)
		}
	}
}

func TestCompose_Busy(t *testing.T) {
	c := newTestCompose(ComposeValues{}, ComposeTo)
	c.SetError("bad address")
	c.SetBusy("Sending…")
	if v := ansi.Strip(c.View()); !strings.Contains(v, "Sending…") || strings.Contains(v, "bad address") {
		t.Errorf("busy should replace the error:\n%s", v)
	}
	for _, k := range []string{"a", "tab", "esc", "ctrl+s", "enter"} {
		c2, r := press(c, k)
		if r != ComposeNone || c2.focus != ComposeTo || c2.Values().To != "" || c2.Prompting() {
			t.Errorf("key %q while busy: result %v, focus %d, values %+v", k, r, c2.focus, c2.Values())
		}
	}
	c, _, _ = c.Update(tea.PasteMsg{Content: "x"})
	if c.Values().To != "" {
		t.Error("paste while busy should be ignored")
	}
	c.SetBusy("")
	if _, r := press(c, "ctrl+s"); r != ComposeSend {
		t.Errorf("ctrl+s after busy = %v, want send", r)
	}
}

func TestCompose_BusyClosesPrompt(t *testing.T) {
	c := dirty(true)
	c.SetBusy("Saving…")
	c.SetBusy("")
	if c.Prompting() {
		t.Error("the prompt should not return after a save attempt")
	}
}

func TestCompose_Paste(t *testing.T) {
	c := newTestCompose(ComposeValues{}, ComposeTo)
	c, _, _ = c.Update(tea.PasteMsg{Content: "a@b.c\nd@e.f\r\ng@h.i\n"})
	if got := c.Values().To; got != "a@b.c d@e.f g@h.i" {
		t.Errorf("header paste = %q", got)
	}
	c, _ = press(c, "shift+tab")
	c, _, _ = c.Update(tea.PasteMsg{Content: "one\ntwo\n\nthree"})
	if got := c.Values().Body; got != "one\ntwo\n\nthree" {
		t.Errorf("body paste = %q", got)
	}
}

func TestCompose_Error(t *testing.T) {
	c := newTestCompose(ComposeValues{}, ComposeTo)
	footer := func() string {
		lines := strings.Split(ansi.Strip(c.View()), "\n")
		return strings.Trim(lines[len(lines)-2], "│ ")
	}
	if got := footer(); got != "" {
		t.Errorf("footer = %q, want it blank (the keys are in the hint bar)", got)
	}
	c.SetError("no recipients")
	if got := footer(); got != "no recipients" {
		t.Errorf("footer = %q, want the error", got)
	}
	c.SetError("")
	if got := footer(); got != "" {
		t.Errorf("footer = %q after clearing the error, want it blank", got)
	}
}

func TestCompose_View(t *testing.T) {
	long := strings.Repeat("a long wrapped line of text ", 20)
	c := newTestCompose(ComposeValues{To: "a@b.c", Subject: "日本語のサブジェクト", Body: strings.Repeat(long+"\n", 80)}, ComposeBody)
	c.SetError(strings.Repeat("an error that is far too long ", 10))
	for _, sz := range [][2]int{{80, 24}, {120, 40}, {30, 10}, {24, 9}, {50, 30}} {
		c.SetSize(sz[0], sz[1])
		lines := strings.Split(ansi.Strip(c.View()), "\n")
		if len(lines) != sz[1] {
			t.Errorf("%v: %d lines", sz, len(lines))
		}
		for i, l := range lines {
			if w := ansi.StringWidth(l); w != sz[0] {
				t.Errorf("%v: line %d is %d wide: %q", sz, i, w, l)
			}
		}
	}
	for _, sz := range [][2]int{{0, 0}, {80, 8}, {23, 24}, {80, 2}} {
		c.SetSize(sz[0], sz[1])
		if v := c.View(); v != "" {
			t.Errorf("%v: want no view, got\n%s", sz, v)
		}
	}
	c.SetSize(80, 9)
	if c.View() == "" {
		t.Error("a 1-row body should still render")
	}
}

func TestCompose_ViewShowsFields(t *testing.T) {
	c := newTestCompose(ComposeValues{To: "a@b.c", Cc: "c@d.e", Subject: "Lunch?", Body: "hello"}, ComposeBody)
	v := ansi.Strip(c.View())
	for _, want := range []string{"Reply · Re: Lunch? · me@x.com", "To", "a@b.c", "Cc", "c@d.e", "Bcc", "Subject", "Lunch?", "hello"} {
		if !strings.Contains(v, want) {
			t.Errorf("view lacks %q:\n%s", want, v)
		}
	}
}

func TestCompose_ResizeKeepsText(t *testing.T) {
	c := newTestCompose(ComposeValues{Body: "keep me"}, ComposeBody)
	c.SetSize(40, 12)
	c.SetSize(100, 30)
	if c.Values().Body != "keep me" || c.Dirty() {
		t.Errorf("resizing changed the values: %+v", c.Values())
	}
}

func BenchmarkComposeTypeLongBody(b *testing.B) {
	line := strings.Repeat("quoted text ", 6)[:70]
	body := strings.TrimSuffix(strings.Repeat("> "+line+"\n", 5000), "\n")
	c := NewCompose(theme.Default(), "Reply", ComposeValues{Body: body}, ComposeBody)
	c.SetSize(160, 45)
	c.View()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c, _, _ = c.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
		_ = c.View()
	}
}
