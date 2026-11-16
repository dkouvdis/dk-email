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

func TestCompose_EscCancelsOnlyCancellableBusyWork(t *testing.T) {
	c := newTestCompose(ComposeValues{}, ComposeTo)
	c.SetBusy("Saving…")
	if _, r := press(c, "esc"); r != ComposeNone {
		t.Errorf("esc while busy = %v, want none", r)
	}
	hints := func(c Compose) string {
		var b strings.Builder
		for _, h := range c.Hints() {
			b.WriteString(h.Help().Desc + ";")
		}
		return b.String()
	}
	if h := hints(c); strings.Contains(h, "cancel") {
		t.Errorf("hints = %q, want no cancel", h)
	}
	c.SetCancellable(true)
	if _, r := press(c, "esc"); r != ComposeCancel {
		t.Errorf("esc while busy and cancellable = %v, want cancel", r)
	}
	if h := hints(c); !strings.Contains(h, "cancel") {
		t.Errorf("hints = %q, want cancel", h)
	}
	c.SetBusy("")
	if _, r := press(c, "esc"); r == ComposeCancel {
		t.Error("SetBusy(\"\") should clear cancellable")
	}
}

func TestCompose_LongErrorWrapsAtAFixedSize(t *testing.T) {
	c := NewCompose(theme.Default(), "New", ComposeValues{}, ComposeTo)
	c.SetSize(50, 14)
	height := func() int { return len(strings.Split(c.View(), "\n")) }
	before := height()

	msg := "Send failed: sending: send via smtp.gmail.com:465: connect: dial tcp: lookup smtp.gmail.com: no such host"
	c.SetError(msg)
	v := ansi.Strip(c.View())
	flat := strings.Join(strings.Fields(strings.NewReplacer("│", " ").Replace(v)), " ")
	for _, w := range strings.Fields(msg) {
		if !strings.Contains(flat, w) {
			t.Errorf("word %q missing from the view:\n%s", w, v)
		}
	}
	if strings.Contains(v, "…") {
		t.Errorf("a %d-char error should fit in %d rows:\n%s", len(msg), composeErrLines, v)
	}
	if got := height(); got != before {
		t.Errorf("view has %d lines with the error, %d without", got, before)
	}
}

var fromChoices = []string{"Me <me@x.com>", "Sales <sales@x.com>", "info@x.com"}

func fromCompose(focus ComposeField) Compose {
	c := newTestCompose(ComposeValues{To: "a@b.c"}, focus)
	c.SetFrom(fromChoices, 0)
	return c
}

func TestComposeFrom_HiddenWithFewerThanTwoChoices(t *testing.T) {
	base := newTestCompose(ComposeValues{To: "a@b.c"}, ComposeTo).View()
	for _, choices := range [][]string{nil, {"Me <me@x.com>"}} {
		c := newTestCompose(ComposeValues{To: "a@b.c"}, ComposeTo)
		c.SetFrom(choices, 0)
		if c.View() != base {
			t.Errorf("view with %d choices differs from the plain composer", len(choices))
		}
		c, _ = press(c, "shift+tab")
		if c.focus != ComposeBody {
			t.Errorf("shift+tab from To = %v, want Body (no From to land on)", c.focus)
		}
	}
}

func TestComposeFrom_ShownWithTwoChoices(t *testing.T) {
	c := fromCompose(ComposeTo)
	v := ansi.Strip(c.View())
	from, to := strings.Index(v, "From "), strings.Index(v, "To ")
	if from < 0 || to < 0 || from > to || !strings.Contains(v, "Me <me@x.com>") || strings.Contains(v, "Sales <") {
		t.Errorf("From row missing or misplaced:\n%s", v)
	}
	if got := len(strings.Split(c.View(), "\n")); got != 24 {
		t.Errorf("view is %d rows, want 24", got)
	}
}

func TestComposeFrom_TabOrder(t *testing.T) {
	c := fromCompose(ComposeTo)
	c, _ = press(c, "shift+tab")
	if c.focus != ComposeFrom {
		t.Fatalf("shift+tab from To = %v, want From", c.focus)
	}
	c, _ = press(c, "shift+tab")
	if c.focus != ComposeBody {
		t.Errorf("shift+tab from From = %v, want Body", c.focus)
	}
	c, _ = press(c, "tab")
	if c.focus != ComposeFrom {
		t.Errorf("tab from Body = %v, want From", c.focus)
	}
	c, _ = press(c, "tab")
	if c.focus != ComposeTo {
		t.Errorf("tab from From = %v, want To", c.focus)
	}
	c, _ = press(c, "shift+tab", "enter", "enter")
	if c.focus != ComposeFrom || c.fromOpen {
		t.Errorf("enter, enter in From = focus %v open %v, want From closed", c.focus, c.fromOpen)
	}
}

func TestComposeFrom_CyclesAndWraps(t *testing.T) {
	c := fromCompose(ComposeFrom)
	for i, k := range []string{"right", "right", "right", "left", "space"} {
		c, _ = press(c, k)
		want := []int{1, 2, 0, 2, 0}[i]
		if c.FromIndex() != want {
			t.Fatalf("after %q index = %d, want %d", k, c.FromIndex(), want)
		}
	}
	c, _ = press(c, "right")
	if v := ansi.Strip(c.View()); !strings.Contains(v, "Sales <sales@x.com>") {
		t.Errorf("row should show the selection:\n%s", v)
	}
}

func TestComposeFrom_IgnoresTypingAndPaste(t *testing.T) {
	c := fromCompose(ComposeFrom)
	before := c.Values()
	c = composeType(c, "xyz")
	c, _, _ = c.Update(tea.PasteMsg{Content: "pasted"})
	if c.Values() != before || c.Dirty() {
		t.Errorf("values %+v, dirty %v", c.Values(), c.Dirty())
	}
}

func TestComposeFrom_ChangeMakesItDirty(t *testing.T) {
	c := fromCompose(ComposeFrom)
	if c.Dirty() {
		t.Fatal("dirty before any change")
	}
	c, _ = press(c, "right")
	if !c.Dirty() {
		t.Error("not dirty after changing From")
	}
	if _, r := press(c, "esc"); r != ComposeNone {
		t.Errorf("esc = %v, want the close prompt", r)
	}
	c, _ = press(c, "left")
	if c.Dirty() {
		t.Error("back at the initial choice should be clean")
	}
}

func hintKeys(c Compose) string {
	var out []string
	for _, h := range c.Hints() {
		out = append(out, h.Help().Key+" "+h.Help().Desc)
	}
	return strings.Join(out, "|")
}

func TestComposeFrom_HintsPerState(t *testing.T) {
	c := fromCompose(ComposeFrom)
	if got := hintKeys(c); !strings.HasPrefix(got, "enter choose|←/→ switch|ctrl+s send") {
		t.Errorf("focused: %q", got)
	}
	c, _ = press(c, "enter")
	if got := hintKeys(c); got != "↑/↓ move|enter pick|esc close" {
		t.Errorf("open: %q", got)
	}
	c, _ = press(c, "esc", "tab")
	if got := hintKeys(c); strings.Contains(got, "←/→") || strings.Contains(got, "choose") {
		t.Errorf("To focused: %q", got)
	}
}

func TestComposeFrom_FocusedRowIsHighlightedWithChevron(t *testing.T) {
	c := fromCompose(ComposeFrom)
	if v := c.View(); !strings.Contains(v, c.th.Cursor.Render(fromChoices[0]+" ▾")) {
		t.Errorf("focused row not highlighted:\n%s", v)
	}
	c, _ = press(c, "tab")
	v := c.View()
	if strings.Contains(v, c.th.Cursor.Render(fromChoices[0])) {
		t.Error("unfocused row should not be highlighted")
	}
	if !strings.Contains(ansi.Strip(v), "Me <me@x.com> ▾") {
		t.Errorf("chevron missing when unfocused:\n%s", ansi.Strip(v))
	}
}

func TestComposeFrom_DropdownListsEveryChoiceOverTheRows(t *testing.T) {
	c := fromCompose(ComposeFrom)
	closed := len(strings.Split(c.View(), "\n"))
	c, _ = press(c, "enter")
	v := c.View()
	plain := ansi.Strip(v)
	for _, ch := range fromChoices {
		if !strings.Contains(plain, ch) {
			t.Errorf("missing %q:\n%s", ch, plain)
		}
	}
	if len(strings.Split(v, "\n")) != closed {
		t.Errorf("the list must not resize the popup:\n%s", plain)
	}
	if !strings.Contains(v, c.th.Cursor.Render(fromChoices[0]+"      ")) {
		t.Error("highlight should start on the current selection")
	}
	c, _ = press(c, "esc")
	if p := ansi.Strip(c.View()); !strings.Contains(p, "Cc ") || strings.Contains(p, "Sales <") {
		t.Errorf("rows should return when closed:\n%s", p)
	}
}

func TestComposeFrom_DropdownMovesClampsAndPicks(t *testing.T) {
	c := fromCompose(ComposeFrom)
	c, _ = press(c, "enter", "up", "k")
	if c.fromHi != 0 {
		t.Errorf("up should clamp at 0, got %d", c.fromHi)
	}
	c, _ = press(c, "down", "j", "j", "down")
	if c.fromHi != 2 {
		t.Errorf("down should clamp at last, got %d", c.fromHi)
	}
	c, _ = press(c, "up")
	if c.FromIndex() != 0 {
		t.Error("moving must not change the selection")
	}
	c, _ = press(c, "enter")
	if c.FromIndex() != 1 || c.fromOpen || c.focus != ComposeFrom || !c.Dirty() {
		t.Errorf("pick: index %d open %v focus %v dirty %v", c.FromIndex(), c.fromOpen, c.focus, c.Dirty())
	}
}

func TestComposeFrom_DropdownScrollsToKeepHighlightVisible(t *testing.T) {
	c := newTestCompose(ComposeValues{}, ComposeFrom)
	c.SetSize(80, 12) // few body rows
	many := []string{}
	for i := 0; i < 12; i++ {
		many = append(many, "addr"+string(rune('a'+i))+"@x.com")
	}
	c.SetFrom(many, 0)
	c, _ = press(c, "enter")
	for range 11 {
		c, _ = press(c, "down")
	}
	if v := ansi.Strip(c.View()); !strings.Contains(v, many[11]) {
		t.Errorf("highlighted last choice not visible:\n%s", v)
	}
}

func TestComposeFrom_DropdownOpensOnTheCurrentSelection(t *testing.T) {
	c := fromCompose(ComposeFrom)
	c, _ = press(c, "right", "enter")
	if c.fromHi != 1 || !strings.Contains(c.View(), c.th.Cursor.Render(fromChoices[1])) {
		t.Errorf("highlight = %d, want 1", c.fromHi)
	}
}

func TestComposeFrom_EscClosesTheDropdownThenBehavesNormally(t *testing.T) {
	c := fromCompose(ComposeFrom)
	c, _ = press(c, "right", "enter", "down", "esc")
	if c.fromOpen || c.prompting || c.FromIndex() != 1 {
		t.Fatalf("open %v prompting %v index %d; want closed, no prompt, unchanged", c.fromOpen, c.prompting, c.FromIndex())
	}
	c, _ = press(c, "esc")
	if !c.prompting {
		t.Error("second esc should open the close prompt (the form is dirty)")
	}
}

func TestComposeFrom_OpeningAndClosingKeepsItClean(t *testing.T) {
	c := fromCompose(ComposeFrom)
	c, _ = press(c, "enter", "esc")
	if c.Dirty() {
		t.Error("open+close must not dirty")
	}
	if _, r := press(c, "esc"); r != ComposeClose {
		t.Errorf("esc = %v, want close", r)
	}
}

func TestComposeFrom_DropdownIgnoresTypingTabAndPaste(t *testing.T) {
	c := fromCompose(ComposeFrom)
	c, _ = press(c, "enter")
	before := c.Values()
	c = composeType(c, "xq")
	c, _ = press(c, "tab", "ctrl+s")
	c, _, _ = c.Update(tea.PasteMsg{Content: "p"})
	if !c.fromOpen || c.focus != ComposeFrom || c.Values() != before || c.fromHi != 0 {
		t.Errorf("open %v focus %v hi %d values %+v", c.fromOpen, c.focus, c.fromHi, c.Values())
	}
}

func TestComposeFrom_DropdownIsABoxRightUnderFrom(t *testing.T) {
	c := fromCompose(ComposeFrom)
	c, _ = press(c, "enter")
	rows := strings.Split(ansi.Strip(c.View()), "\n")
	// rows[0] is the frame top, rows[1] From, rows[2] the box top.
	if !strings.Contains(rows[1], "From") {
		t.Fatalf("row 1 = %q", rows[1])
	}
	if !strings.Contains(rows[2], "╭") || !strings.Contains(rows[2], "╮") {
		t.Errorf("box top missing: %q", rows[2])
	}
	if !strings.Contains(rows[3], "│ Me <me@x.com> ") {
		t.Errorf("first choice should have a space of padding inside the border: %q", rows[3])
	}
	if !strings.Contains(rows[6], "╰") {
		t.Errorf("box bottom missing: %q", rows[6])
	}
	// The box is under the address, one column per label cell plus the frame.
	if strings.Index(rows[2], "╭") != strings.Index(rows[1], "Me <") {
		t.Errorf("box not aligned under the address:\n%s\n%s", rows[1], rows[2])
	}
}

func TestComposeFrom_DropdownAtTinySizesDoesNotPanic(t *testing.T) {
	for h := 6; h <= 14; h++ {
		c := newTestCompose(ComposeValues{}, ComposeFrom)
		c.SetSize(30, h)
		c.SetFrom(fromChoices, 0)
		c, _ = press(c, "enter", "down", "down")
		_ = c.View()
	}
}

func TestComposeFrom_DropdownDimsTheRestAndKeepsItVisible(t *testing.T) {
	c := newTestCompose(ComposeValues{To: "a@b.c", Cc: "aaaaaaaaaaaaaaaaaaaaaaaaTAIL", Subject: "Lunch"}, ComposeFrom)
	c.SetFrom(fromChoices, 0)
	c.SetError("boom")
	open := func(c Compose) Compose { c, _ = press(c, "enter"); return c }

	normal := c.View()
	if !strings.Contains(normal, c.th.Accent.Render("From     ")) {
		t.Fatal("focused From label should be accent when closed")
	}
	c = open(c)
	v := c.View()
	plain := ansi.Strip(v)
	for _, want := range []string{"From", "To  ", "Cc  ", "Bcc ", "Subject", "TAIL"} {
		if !strings.Contains(plain, want) {
			t.Errorf("%q should stay visible behind the list:\n%s", want, plain)
		}
	}
	if strings.Contains(v, c.th.Accent.Render("From     ")) || strings.Contains(v, c.th.Cursor.Render("Me <me@x.com> ▾")) {
		t.Error("the From row should be dimmed while the list is open")
	}
	if sgr, _, _ := strings.Cut(c.th.Muted.Render("x"), "x"); !strings.Contains(v, sgr+"aTAIL") {
		t.Error("text right of the box should be drawn muted")
	}
	if !strings.Contains(v, c.th.Cursor.Render("Me <me@x.com>      ")) {
		t.Error("the box itself must not be dimmed")
	}
	if !strings.Contains(v, c.th.Muted.Render("boom")) || strings.Contains(v, c.th.Error.Render("boom")) {
		t.Error("the footer should be dimmed too")
	}
	c, _ = press(c, "esc")
	if c.View() != normal {
		t.Error("closing should restore the normal view")
	}
}
