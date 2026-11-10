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
		{200, KindWide}, {119, KindNarrow}, {79, KindList}, {80, KindNarrow}, {120, KindWide},
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
	if m.panes.Kind != KindWide {
		t.Fatalf("after second v: kind %v, want wide", m.panes.Kind)
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
		{"tab", PaneReader},
		{"tab", PaneSidebar}, // wraps
		{"shift+tab", PaneReader},
		{"l", PaneReader}, // no wrap
		{"h", PaneList},
		{"left", PaneSidebar},
		{"h", PaneSidebar}, // no wrap
		{"right", PaneList},
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

	m = send(t, m, size(100, 30))

	if m.focus != PaneList {
		t.Errorf("after narrowing, focus = %v, want list (sidebar is hidden)", m.focus)
	}
	m = send(t, m, press("tab"), size(70, 30))
	if m.focus != PaneList {
		t.Errorf("after list mode, focus = %v, want list", m.focus)
	}
	if m = send(t, m, press("tab")); m.focus != PaneList {
		t.Errorf("tab in list mode moved focus to %v", m.focus)
	}
}

func TestModel_HintsFollowFocus(t *testing.T) {
	m := send(t, New(testConfig(), nil, nil), size(200, 30))
	hints := func() string { return ansi.Strip(m.renderHints()) }

	if !strings.Contains(hints(), "archive") || strings.Contains(hints(), "accounts") {
		t.Errorf("wide list hints = %q, want triage keys and no account picker", hints())
	}
	m = send(t, m, press("h"))
	if strings.Contains(hints(), "archive") {
		t.Errorf("sidebar hints = %q, want no triage keys", hints())
	}
	m = send(t, m, size(100, 30))
	if !strings.Contains(hints(), "accounts") {
		t.Errorf("narrow hints = %q, want the a picker key", hints())
	}
	m = send(t, m, size(60, 30))
	if h := hints(); !strings.HasSuffix(strings.TrimRight(h, " "), "? help • q quit") || !strings.Contains(h, "…") {
		t.Errorf("60-col hints = %q, want truncated pane keys ending in ? help • q quit", h)
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
			if w >= 40 && !strings.HasSuffix(strings.TrimRight(ansi.Strip(bar), " "), "? help • q quit") {
				t.Fatalf("width %d focus %v: hint bar %q doesn't end with ? help • q quit", w, m.focus, ansi.Strip(bar))
			}
			m = send(t, m, press("tab"))
		}
	}
}

func TestModel_HelpPopup(t *testing.T) {
	m := send(t, New(testConfig(), nil, nil), size(120, 30), press("?"))
	if !m.showHelp || !strings.Contains(plain(m), "esc close") {
		t.Fatal("? didn't open the help popup")
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
		if m.showHelp || strings.Contains(plain(m), "esc close") {
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
	if !strings.Contains(ansi.Strip(withHelp.renderHints()), "q close") {
		t.Errorf("hint bar = %q, want q close while a popup is open", ansi.Strip(withHelp.renderHints()))
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

	send(t, New(testConfig(), nil, log), size(200, 30), size(201, 30), size(100, 30), press("tab"), press("?"), press("esc"))

	out := buf.String()
	for _, want := range []string{"msg=start accounts=2", "kind=wide", "kind=narrow", "pane=reader", "popup opened", "popup closed"} {
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
		{"wide", 140, 24, false, nil},
		{"wide-sidebar-focused", 140, 24, false, []string{"h", "j"}},
		{"wide-account-error", 140, 24, true, nil},
		{"wide-thread-expanded", 140, 24, false, []string{"space", "j"}},
		{"wide-reading", 140, 24, false, []string{"enter"}},
		{"narrow", 100, 24, false, nil},
		{"narrow-picker", 100, 24, false, []string{"a", "j"}},
		{"wide-move-picker", 140, 24, false, []string{"m"}},
		{"list", 70, 20, false, nil},
		{"list-reader-popup", 70, 24, false, []string{"enter"}},
		{"help", 120, 30, false, []string{"?"}},
		{"help-scrolled", 120, 30, false, []string{"?", "G"}},
		{"help-fits", 120, 50, false, []string{"?"}},
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
