package ui

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/teatest/v2"

	"github.com/dkouvdis/dk-email/internal/config"
	"github.com/dkouvdis/dk-email/internal/mail/mailtest"
	"github.com/dkouvdis/dk-email/internal/ui/popup"
)

func testConfig() config.Config {
	cfg := config.Default()
	cfg.Accounts = []config.Account{{Name: "Personal"}, {Name: "Work"}}
	return cfg
}

func press(k string) tea.KeyPressMsg {
	switch k {
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "shift+tab":
		return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	case "ctrl+c":
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	case "ctrl+s":
		return tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}
	case "ctrl+r":
		return tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	}
	r := []rune(k)[0]
	return tea.KeyPressMsg{Code: r, Text: k}
}

// send feeds msgs through Update and returns the resulting model.
func send(t *testing.T, m Model, msgs ...tea.Msg) Model {
	t.Helper()
	for _, msg := range msgs {
		next, _ := m.Update(msg)
		m = next.(Model)
	}
	return m
}

func size(w, h int) tea.WindowSizeMsg { return tea.WindowSizeMsg{Width: w, Height: h} }

func plain(m Model) string { return ansi.Strip(m.View().Content) }

func TestModel_ViewFillsTerminalExactly(t *testing.T) {
	for _, s := range [][2]int{{60, 20}, {79, 24}, {80, 24}, {119, 30}, {120, 30}, {200, 50}} {
		m := send(t, New(testConfig(), nil, nil), size(s[0], s[1]))
		for _, help := range []bool{false, true} {
			if help {
				m = send(t, m, press("?"))
			}
			view := m.View().Content
			if w, h := lipgloss.Width(view), lipgloss.Height(view); w != s[0] || h != s[1] {
				t.Errorf("%dx%d (help %v): view is %dx%d", s[0], s[1], help, w, h)
			}
		}
	}
}

func TestModel_ResizeSwitchesLayoutLive(t *testing.T) {
	m := New(testConfig(), nil, nil)

	steps := []struct {
		w    int
		want Kind
	}{
		{200, KindNarrow}, {79, KindList}, {80, KindNarrow}, {120, KindNarrow},
	}
	for _, s := range steps {
		m = send(t, m, size(s.w, 30))
		if m.panes.Kind != s.want {
			t.Errorf("width %d: Kind = %v, want %v", s.w, m.panes.Kind, s.want)
		}
	}
}

func TestModel_ToggleView(t *testing.T) {
	m := send(t, New(testConfig(), nil, nil), size(130, 30), press("v"))
	if m.panes.Kind != KindList || m.mode != ModeList {
		t.Fatalf("after v at 130 cols: kind %v mode %v, want list", m.panes.Kind, m.mode)
	}
	m = send(t, m, press("v"))
	if m.panes.Kind != KindNarrow {
		t.Fatalf("after second v: kind %v, want narrow", m.panes.Kind)
	}

	m = send(t, m, size(70, 30), press("v"))
	if m.mode != ModeSplit || m.panes.Kind != KindList {
		t.Errorf("v below 80 cols changed mode to %v (kind %v); it must have no effect", m.mode, m.panes.Kind)
	}
	if !strings.Contains(plain(m), "List mode is forced below 80 columns") {
		t.Error("status line doesn't explain why v was ignored")
	}
	if !strings.Contains(plain(m), "list (forced)") {
		t.Error("status line doesn't show the forced list layout")
	}
}

func TestModel_DefaultLayoutList(t *testing.T) {
	cfg := testConfig()
	cfg.UI.DefaultLayout = config.LayoutList

	m := send(t, New(cfg, nil, nil), size(200, 30))

	if m.panes.Kind != KindList {
		t.Errorf("Kind = %v, want list from default_layout", m.panes.Kind)
	}
}

func TestModel_FocusCycling(t *testing.T) {
	m := send(t, New(testConfig(), nil, nil), size(150, 30))
	if m.focus != PaneList {
		t.Fatalf("initial focus = %v, want list", m.focus)
	}

	tests := []struct {
		key  string
		want Pane
	}{
		{"tab", PaneSidebar}, // wraps
		{"tab", PaneList},
		{"shift+tab", PaneSidebar},
		{"h", PaneSidebar}, // no wrap
		{"l", PaneList},
		{"right", PaneList}, // no wrap
		{"left", PaneSidebar},
	}
	for i, tt := range tests {
		m = send(t, m, press(tt.key))
		if m.focus != tt.want {
			t.Fatalf("step %d (%s): focus = %v, want %v", i, tt.key, m.focus, tt.want)
		}
	}
}

func TestModel_FocusMovesOffHiddenPane(t *testing.T) {
	m := send(t, New(testConfig(), nil, nil), size(150, 30), press("h"))
	if m.focus != PaneSidebar {
		t.Fatalf("focus = %v, want sidebar", m.focus)
	}

	m = send(t, m, size(70, 30))

	if m.focus != PaneList {
		t.Errorf("after list mode, focus = %v, want list (sidebar is hidden)", m.focus)
	}
	if m = send(t, m, press("tab")); m.focus != PaneList {
		t.Errorf("tab in list mode moved focus to %v", m.focus)
	}
}

func TestModel_HintsFollowFocus(t *testing.T) {
	m := send(t, New(testConfig(), nil, nil), size(200, 30))
	hints := func() string { return ansi.Strip(m.renderHints()) }

	if !strings.Contains(hints(), "Archive: e") || strings.Contains(hints(), "Accounts") {
		t.Errorf("narrow list hints = %q, want triage keys and no account picker", hints())
	}
	if !strings.Contains(hints(), "Reply: r | Forward: f | New message: c") {
		t.Errorf("narrow list hints = %q, want Reply before New message", hints())
	}
	m = send(t, m, press("h"))
	if strings.Contains(hints(), "Archive") {
		t.Errorf("sidebar hints = %q, want no triage keys", hints())
	}
	m = send(t, m, size(90, 30), press("v"))
	if !strings.Contains(hints(), "Accounts: a") {
		t.Errorf("list hints = %q, want the a picker key", hints())
	}
	m = send(t, m, size(60, 30))
	if h := hints(); !strings.HasSuffix(strings.TrimRight(h, " "), "Help: ? | Quit: q") || !strings.Contains(h, "…") {
		t.Errorf("60-col hints = %q, want truncated pane keys ending in Help: ? | Quit: q", h)
	}
}

func TestModel_HintBarReadsDescriptionThenKey(t *testing.T) {
	m := send(t, New(testConfig(), nil, nil), size(200, 30))

	bar := m.renderHints()
	h := ansi.Strip(bar)

	if !strings.HasPrefix(h, "Up: k/↑ | Down: j/↓ | Open: enter | Expand: space") || strings.Contains(h, "•") {
		t.Errorf("hint bar = %q, want lazygit style: Description: key | …", h)
	}
	if !strings.Contains(bar, "\x1b[36m") {
		t.Errorf("hint bar isn't drawn in the hint colour (ANSI cyan): %q", bar)
	}
}

func TestModel_HintBarAlwaysFitsAndEndsWithHelpAndQuit(t *testing.T) {
	for w := 20; w <= 220; w++ {
		m := send(t, New(testConfig(), nil, nil), size(w, 30))
		for range m.visiblePanes() {
			bar := m.renderHints()
			if got := lipgloss.Width(bar); got != w {
				t.Fatalf("width %d focus %v: hint bar is %d wide", w, m.focus, got)
			}
			if w >= 40 && !strings.HasSuffix(strings.TrimRight(ansi.Strip(bar), " "), "Help: ? | Quit: q") {
				t.Fatalf("width %d focus %v: hint bar %q doesn't end with Help: ? | Quit: q", w, m.focus, ansi.Strip(bar))
			}
			m = send(t, m, press("tab"))
		}
	}
}

func TestModel_HelpPopup(t *testing.T) {
	m := send(t, New(testConfig(), nil, nil), size(120, 30), press("?"))
	if !m.showHelp || !strings.Contains(plain(m), "press / to filter by key") {
		t.Fatal("? didn't open the help popup")
	}
	if h := strings.TrimSpace(ansi.Strip(m.renderHints())); !strings.HasSuffix(h, "Filter: / | Close: esc/q/?") || strings.Contains(h, "Quit") {
		t.Errorf("hint bar = %q, want the help popup's own keys", h)
	}

	m = send(t, m, press("tab"), press("v"))
	if m.focus != PaneList || m.mode != ModeSplit {
		t.Error("keys leaked through the help popup")
	}

	m = send(t, m, press("G"))
	if v := plain(m); !strings.Contains(v, "General") || strings.Contains(v, "Navigation") {
		t.Errorf("G didn't scroll the help popup to the bottom:\n%s", v)
	}
	m = send(t, m, press("esc"), press("?"))
	if v := plain(m); !strings.Contains(v, "Navigation") {
		t.Errorf("reopened help popup isn't scrolled back to the top:\n%s", v)
	}

	for _, k := range []string{"esc", "?"} {
		m = send(t, m, press("?"), press(k))
		if m.showHelp || strings.Contains(plain(m), "press / to filter by key") {
			t.Errorf("%s didn't close the help popup", k)
		}
	}
}

func TestModel_Quit(t *testing.T) {
	quits := func(cmd tea.Cmd) bool {
		if cmd == nil {
			return false
		}
		_, ok := cmd().(tea.QuitMsg)
		return ok
	}
	m := send(t, New(testConfig(), nil, nil), size(120, 30))

	for _, k := range []string{"q", "ctrl+c"} {
		if _, cmd := m.Update(press(k)); !quits(cmd) {
			t.Errorf("%s with no popup didn't quit", k)
		}
	}

	withHelp := send(t, m, press("?"))
	next, cmd := withHelp.Update(press("q"))
	if quits(cmd) || next.(Model).showHelp {
		t.Error("q with the help popup open should close it, not quit")
	}
	reading := pressSettled(t, started(t, newFixture(), 70, 24), "enter")
	if !reading.readerPopup || !strings.HasSuffix(strings.TrimRight(ansi.Strip(reading.renderHints()), " "), "Help: ? | Close: q") {
		t.Errorf("reader hint bar = %q, want it to end with Help: ? | Close: q", ansi.Strip(reading.renderHints()))
	}
	if _, cmd := withHelp.Update(press("ctrl+c")); !quits(cmd) {
		t.Error("ctrl+c with a popup open didn't quit")
	}
}

func TestModel_TinyTerminal(t *testing.T) {
	m := send(t, New(testConfig(), nil, nil), size(15, 4))
	if !strings.Contains(plain(m), "Terminal too small") {
		t.Errorf("view = %q, want a too-small notice", plain(m))
	}
	if New(testConfig(), nil, nil).View().Content != "" {
		t.Error("view before the first resize should be empty")
	}
}

func TestModel_NoAccountsStatus(t *testing.T) {
	m := send(t, New(config.Default(), nil, nil), size(120, 30))
	if !strings.Contains(plain(m), "No accounts configured yet.") {
		t.Error("status line doesn't say there are no accounts")
	}
}

func TestModel_DebugLogRecordsMetadata(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	send(t, New(testConfig(), nil, log), size(200, 30), size(201, 30), press("tab"), size(70, 30), press("?"), press("esc"))

	out := buf.String()
	for _, want := range []string{"msg=start accounts=2", "kind=narrow", "kind=list", "pane=sidebar", "popup opened", "popup closed"} {
		if !strings.Contains(out, want) {
			t.Errorf("debug log missing %q:\n%s", want, out)
		}
	}
	if n := strings.Count(out, "msg=layout"); n != 2 {
		t.Errorf("logged %d layout lines, want 2 (only when the kind changes)", n)
	}
}

// The golden tests connect fake accounts and apply keys synchronously (so
// spinner and load timing can't change the output), then run the model in
// a real teatest program at fixed sizes and compare the final screen without ANSI
// styling. Update them with go test ./internal/ui/ -run Golden -update
func TestGolden_FirstRunConnect(t *testing.T) {
	m := firstRun(t, newOnboarding(), nil)
	m = typeKeys(t, m, "Gmail")
	m = send(t, m, press("tab"))
	m = typeKeys(t, m, "me@gmail.com")

	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(140, 34))
	tm.Send(press("ctrl+c"))

	final := tm.FinalModel(t, teatest.WithFinalTimeout(5*time.Second)).(Model)
	teatest.RequireEqualOutput(t, []byte(plain(final)))
}

func TestGolden(t *testing.T) {
	tests := []struct {
		name     string
		w, h     int
		workFail bool
		keys     []string
	}{
		{"narrow", 140, 24, false, nil},
		{"narrow-sidebar-focused", 140, 24, false, []string{"h", "j"}},
		{"narrow-account-error", 140, 24, true, nil},
		{"narrow-thread-expanded", 140, 24, false, []string{"space", "j"}},
		{"narrow-reading", 140, 24, false, []string{"enter"}},
		{"narrow-move-picker", 140, 24, false, []string{"m"}},
		{"list", 70, 20, false, nil},
		{"list-picker", 70, 24, false, []string{"a", "j"}},
		{"list-reader-popup", 70, 24, false, []string{"enter"}},
		{"help", 120, 30, false, []string{"?"}},
		{"help-scrolled", 120, 30, false, []string{"?", "G"}},
		{"help-fits", 120, 54, false, []string{"?"}},
		{"help-filtered", 120, 30, false, []string{"?", "/", "a", "r", "c", "h"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture()
			if tt.workFail {
				f.errs["Work"] = errors.New(`login to account "Work" (mail.example.com:993) failed: invalid credentials`)
			}
			m := started(t, f, tt.w, tt.h)
			// Keys may start fetches, so they're applied and settled here
			// rather than sent through teatest, where timing would vary.
			for _, k := range tt.keys {
				m = pressSettled(t, m, k)
			}
			m.connect = nil // already connected; Init must not reconnect

			tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(tt.w, tt.h))
			tm.Send(press("ctrl+c")) // q would only close an open popup

			final := tm.FinalModel(t, teatest.WithFinalTimeout(5*time.Second)).(Model)
			teatest.RequireEqualOutput(t, []byte(plain(final)))
		})
	}
}

func TestModel_HelpFilter(t *testing.T) {
	m := send(t, New(testConfig(), nil, nil), size(120, 30), press("?"), press("/"))

	for _, k := range []string{"a", "r", "c", "h"} {
		m = send(t, m, press(k))
	}
	pop := ansi.Strip(m.helpPop.View())
	if !strings.Contains(pop, "archive") || strings.Contains(pop, "star") || strings.Contains(pop, "Navigation") {
		t.Errorf("typing arch should leave only e archive under Threads:\n%s", pop)
	}

	next, cmd := m.Update(press("q"))
	m = next.(Model)
	if cmd != nil || !m.showHelp || !strings.Contains(ansi.Strip(m.helpPop.View()), "No keys match") {
		t.Error("q while filtering should be typed into the filter, not close or quit")
	}

	m = send(t, m, press("esc"))
	if !m.showHelp {
		t.Fatal("the first esc should only clear the filter")
	}
	m = send(t, m, press("esc"))
	if m.showHelp {
		t.Fatal("the second esc should close the popup")
	}

	m = send(t, m, press("?"))
	if !strings.Contains(ansi.Strip(m.helpPop.View()), "press / to filter") {
		t.Error("reopening the popup should start with an empty filter")
	}
	if _, cmd := m.Update(press("ctrl+c")); cmd == nil {
		t.Error("ctrl+c should still quit with help open")
	}
}

// sgrPrefix is the escape a style writes before its text.
func sgrPrefix(s lipgloss.Style) string {
	pre, _, _ := strings.Cut(s.Render("x"), "x")
	return pre
}

// popupRows is the screen rows a popup covers.
func popupRows(m Model) (top, bottom int) {
	var view string
	switch {
	case m.showConnect:
		view = m.connectPop.View()
	case m.showHelp:
		view = m.helpPop.View()
	case m.showPicker:
		view = m.picker.View()
	default:
		p := m.panes.Popup
		return p.Y, p.Y + p.H
	}
	_, y := popup.Center(m.width, m.height-footerRows, view)
	return y, y + lipgloss.Height(view)
}

// With a popup open, the panes behind it are drawn in the muted style only:
// no selected row and no focused border colour. Without one nothing changes.
func TestModel_PopupDimsTheBackdrop(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		width int // the picker is only used in list mode
		// ownSelection popups draw a selected row or button of their own.
		ownSelection bool
	}{
		{"none", "", 160, false},
		{"reader", "enter", 160, false},
		{"help", "?", 160, false},
		{"picker", "a", 70, true},
		{"composer", "c", 160, false},
		{"connect", "A", 160, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture()
			f.personalSender = &mailtest.DraftSender{} // the composer needs a sender
			m := started(t, f, tt.width, 30)
			m.accts[0].Email = "me@example.com"
			m = m.WithOnboarding(newOnboarding().onboarding())
			if tt.key != "" {
				m = pressSettled(t, m, tt.key)
			}
			selected := sgrPrefix(m.theme.Cursor)
			accent := sgrPrefix(lipgloss.NewStyle().Foreground(m.theme.AccentColor))
			screen := m.render()
			lines := strings.Split(screen, "\n")

			if tt.key == "" {
				if m.popupOpen() || m.showConnect {
					t.Fatal("a popup is open")
				}
				if !strings.Contains(screen, selected) || !strings.Contains(lines[0], accent) {
					t.Error("without a popup the selected row and the focused border should be drawn as before")
				}
				return
			}
			if !m.popupOpen() && !m.showConnect {
				t.Fatal("the popup didn't open")
			}

			top, bottom := popupRows(m)
			for i, l := range lines[:m.height-footerRows] {
				if i >= top && i < bottom {
					continue
				}
				if strings.Contains(l, selected) || strings.Contains(l, accent) {
					t.Errorf("row %d, outside the popup, still has selection or focus colour: %q", i, l)
				}
			}
			if !tt.ownSelection && strings.Contains(screen, selected) {
				t.Error("a selected row is still highlighted behind the popup")
			}
		})
	}
}

func TestModel_DimmedKeepsTextAndWidth(t *testing.T) {
	m := started(t, newFixture(), 160, 30)
	block := lipgloss.JoinHorizontal(lipgloss.Top, m.renderPane(PaneSidebar, m.panes.Sidebar, m.side.View), m.renderPane(PaneList, m.panes.List, m.listView))

	got := m.theme.Dim(block)

	if ansi.Strip(got) != ansi.Strip(block) {
		t.Error("dimming changed the text")
	}
	if strings.Contains(got, sgrPrefix(m.theme.Cursor)) {
		t.Error("dimmed block still has the selection colour")
	}
	for i, l := range strings.Split(got, "\n") {
		if lipgloss.Width(l) != m.width {
			t.Errorf("line %d is %d wide, want %d", i, lipgloss.Width(l), m.width)
		}
	}
}

// The top-most popup's own keys fill the hint bar, with no Help/Quit tail,
// and the shared fitting still cuts them with … at narrow widths.
func TestModel_HintBarShowsThePopupKeys(t *testing.T) {
	f := newFixture()
	f.personalSender = &mailtest.DraftSender{}
	base := started(t, f, 160, 30)
	base.accts[0].Email = "me@example.com"
	base = base.WithOnboarding(newOnboarding().onboarding())

	// Each composer needs its own draft, which the models would share.
	composing := pressSettled(t, base, "c")
	prompting := send(t, pressSettled(t, base, "c"), press("x"), press("esc"))
	busy := pressSettled(t, base, "c")
	busy.draft.pop.SetBusy("Sending…")
	filtering := pressSettled(t, pressSettled(t, base, "?"), "/")

	tests := []struct {
		name string
		m    Model
		want string
	}{
		{"composer", composing, "Send: ctrl+s | Next field: tab | Previous field: shift+tab | Close: esc"},
		{"composer prompt", prompting, "Save: s | Discard: d | Keep editing: e | Save & close: esc"},
		{"composer busy", busy, "Quit now: ctrl+c"},
		{"help", pressSettled(t, base, "?"), "Up: k/↑ | Down: j/↓ | Page: pgup/pgdn | Top/bottom: g/G | Filter: / | Close: esc/q/?"},
		{"help filter", filtering, "Keep: enter | Clear: esc"},
		{"picker", send(t, base, size(70, 30), press("a")), "Filter: / | Move: j/k | Open: enter | Close: esc/q"},
		{"connect", pressSettled(t, base, "A"), "Next: tab | Choose: ←→ | Test & save: enter | Cancel: esc"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := strings.TrimRight(ansi.Strip(tt.m.renderHints()), " "); got != tt.want {
				t.Errorf("hint bar = %q, want %q", got, tt.want)
			}
			for w := minWidth; w <= 120; w++ {
				m := send(t, tt.m, size(w, 30))
				bar := m.renderHints()
				if got := lipgloss.Width(bar); got != w {
					t.Fatalf("width %d: hint bar is %d wide", w, got)
				}
				text := strings.TrimRight(ansi.Strip(bar), " ")
				if strings.Contains(text, "Help: ?") || strings.Contains(text, "Quit: q") {
					t.Fatalf("width %d: %q has the pane tail", w, text)
				}
				full := w >= ansi.StringWidth(tt.want)
				if full != (text == tt.want) || !full && !strings.HasSuffix(text, "…") {
					t.Fatalf("width %d: hint bar %q, want %q or a cut ending in …", w, text, tt.want)
				}
			}
		})
	}
}
