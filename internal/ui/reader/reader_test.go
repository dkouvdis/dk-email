package reader

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/dkouvdis/dk-email/internal/mail"
	"github.com/dkouvdis/dk-email/internal/thread"
	"github.com/dkouvdis/dk-email/internal/ui/theme"
)

var now = time.Date(2026, 11, 9, 15, 30, 0, 0, time.UTC)

func press(k string) tea.KeyPressMsg {
	switch k {
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	case "pgdown":
		return tea.KeyPressMsg{Code: tea.KeyPgDown}
	case "pgup":
		return tea.KeyPressMsg{Code: tea.KeyPgUp}
	}
	return tea.KeyPressMsg{Code: []rune(k)[0], Text: k}
}

func summary(uid mail.UID, from string, hoursAgo int, seen bool) mail.Summary {
	return mail.Summary{
		UID: uid, From: mail.Address{Name: from, Email: strings.ToLower(from) + "@example.com"},
		To: []mail.Address{{Email: "me@example.com"}}, Subject: "Re: Plan",
		Date: now.Add(-time.Duration(hoursAgo) * time.Hour), Seen: seen,
	}
}

// conversation is oldest first: Alice (read), Bob (unread), Carol (unread).
func conversation() thread.Thread {
	return thread.Thread{Messages: []mail.Summary{
		summary(1, "Alice", 5, true),
		summary(2, "Bob", 3, false),
		summary(3, "Carol", 1, false),
	}}
}

func withBody(s mail.Summary, text string) mail.Message {
	return mail.Message{Summary: s, Body: text, Cc: []mail.Address{{Name: "Dan", Email: "dan@example.com"}}}
}

func view(m Model) string { return ansi.Strip(m.View(60, 200)) }

func open(th thread.Thread, focus mail.UID) Model {
	return New(theme.Default(), func() time.Time { return now }).SetSize(60, 200).Open(0, "INBOX", th, focus)
}

func TestOpen_ExpandsFirstUnreadAndAsksForItsBody(t *testing.T) {
	m := open(conversation(), 0)

	if got := m.Missing(); !slices.Equal(got, []mail.UID{2}) {
		t.Fatalf("Missing() = %v, want [2] (first unread)", got)
	}
	v := view(m)
	if !strings.Contains(v, "Re: Plan") || !strings.Contains(v, "Loading…") {
		t.Errorf("view lacks subject or loading state:\n%s", v)
	}
	// Collapsed messages are one line each, oldest first.
	ia, ib, ic := strings.Index(v, "Alice"), strings.Index(v, "From: Bob"), strings.Index(v, "Carol")
	if !(ia >= 0 && ia < ib && ib < ic) {
		t.Errorf("messages not stacked oldest to newest with Bob expanded:\n%s", v)
	}
	if strings.Contains(v, "From: Alice") || strings.Contains(v, "From: Carol") {
		t.Errorf("only Bob should be expanded:\n%s", v)
	}
}

func TestOpen_AllReadExpandsLatest(t *testing.T) {
	th := conversation()
	for i := range th.Messages {
		th.Messages[i].Seen = true
	}

	if got := open(th, 0).Missing(); !slices.Equal(got, []mail.UID{3}) {
		t.Errorf("Missing() = %v, want [3] (latest)", got)
	}
}

func TestOpen_FocusedMessageIsExpanded(t *testing.T) {
	if got := open(conversation(), 1).Missing(); !slices.Equal(got, []mail.UID{1}) {
		t.Errorf("Missing() = %v, want [1]", got)
	}
}

func TestSetMessage_ShowsHeadersBodyAndAttachments(t *testing.T) {
	m := open(conversation(), 0).MarkLoading([]mail.UID{2})
	if len(m.Missing()) != 0 {
		t.Fatal("a loading message is still reported missing")
	}
	msg := withBody(conversation().Messages[1], "Sounds good.\n\nSee you there, with a line long enough to need wrapping at sixty columns.")
	msg.Attachments = []mail.Attachment{{Name: "plan.pdf", ContentType: "application/pdf", Size: 2048}}

	m = m.SetMessage(2, msg, nil)

	v := view(m)
	for _, want := range []string{
		"From: Bob <bob@example.com>",
		"To:   me@example.com",
		"Cc:   Dan <dan@example.com>",
		"Date: Mon 9 Nov 2026 12:30",
		"Sounds good.",
		"plan.pdf",
		"2.0 KB",
	} {
		if !strings.Contains(v, want) {
			t.Errorf("view missing %q:\n%s", want, v)
		}
	}
	for _, l := range strings.Split(v, "\n") {
		if w := ansi.StringWidth(l); w > 60 {
			t.Errorf("line %q is %d wide, want ≤ 60", l, w)
		}
	}
}

func TestSetMessage_StylesOnlyHTMLBodiesAsMarkdown(t *testing.T) {
	m := open(conversation(), 2)
	html := withBody(conversation().Messages[1], "## Hello\n\nSee **this** and [the site](https://example.com/a).")
	html.BodyFromHTML = true

	raw := m.SetMessage(2, html, nil).View(60, 200)
	v := ansi.Strip(raw)
	for _, want := range []string{"Hello", "See this and the site."} {
		if !strings.Contains(v, want) {
			t.Errorf("HTML body missing %q:\n%s", want, v)
		}
	}
	if strings.Contains(v, "## ") || strings.Contains(v, "**") || strings.Contains(v, "https://") {
		t.Errorf("markdown syntax or link URL left in HTML body:\n%s", v)
	}
	if !strings.Contains(raw, "\x1b]8;") || !strings.Contains(raw, ";https://example.com/a\a") {
		t.Errorf("link is not a terminal hyperlink: %q", raw)
	}

	plain := withBody(conversation().Messages[1], "## not a heading, **literal**")
	if v := view(m.SetMessage(2, plain, nil)); !strings.Contains(v, "## not a heading, **literal**") {
		t.Errorf("plain body was restyled:\n%s", v)
	}
}

func TestSetSize_RewrapsMarkdownBody(t *testing.T) {
	msg := withBody(conversation().Messages[1], strings.Repeat("word ", 30))
	msg.BodyFromHTML = true
	m := open(conversation(), 2).SetMessage(2, msg, nil).SetSize(20, 200)

	v := ansi.Strip(m.View(20, 200))
	for l := range strings.SplitSeq(v, "\n") {
		if ansi.StringWidth(l) > 20 {
			t.Fatalf("line wider than 20 after resize: %q", l)
		}
	}
	if !strings.Contains(v, "word word word") {
		t.Errorf("body missing after resize:\n%s", v)
	}
}

func TestSetMessage_ErrorAndUnknownUID(t *testing.T) {
	m := open(conversation(), 0)

	m = m.SetMessage(99, mail.Message{Body: "stray"}, nil)
	m = m.SetMessage(2, mail.Message{}, errors.New("fetch timed out"))

	v := view(m)
	if strings.Contains(v, "stray") {
		t.Error("a message outside the thread was shown")
	}
	if !strings.Contains(v, "Couldn't load this message: fetch timed out") {
		t.Errorf("error not shown:\n%s", v)
	}
	if len(m.Missing()) != 0 {
		t.Error("a failed message is asked for again automatically")
	}
}

func TestSpace_TogglesExpandAll(t *testing.T) {
	m := open(conversation(), 0).MarkLoading([]mail.UID{2})

	m, _ = m.Update(press("space"))
	if got := m.Missing(); !slices.Equal(got, []mail.UID{1, 3}) {
		t.Errorf("after space Missing() = %v, want [1 3]", got)
	}
	if v := view(m); !strings.Contains(v, "From: Alice") || !strings.Contains(v, "From: Carol") {
		t.Errorf("space didn't expand every message:\n%s", v)
	}

	m, _ = m.Update(press("space"))
	if v := view(m); strings.Contains(v, "From: Alice") || !strings.Contains(v, "From: Bob") {
		t.Errorf("second space didn't go back to the default:\n%s", v)
	}
}

func TestSetSeen_UpdatesMarkers(t *testing.T) {
	m := open(conversation(), 0)
	if !strings.Contains(view(m), "● Carol") {
		t.Fatalf("unread collapsed message lacks a dot:\n%s", view(m))
	}

	m = m.SetSeen([]mail.UID{3}, true)

	if strings.Contains(view(m), "● Carol") {
		t.Errorf("Carol still marked unread:\n%s", view(m))
	}
	if !m.Unread(2) || m.Unread(3) {
		t.Error("Unread() doesn't reflect SetSeen")
	}
}

func TestScrolling(t *testing.T) {
	long := strings.Repeat("line\n", 100)
	m := New(theme.Default(), func() time.Time { return now }).SetSize(40, 10).Open(0, "INBOX", conversation(), 0)
	m = m.SetMessage(2, withBody(conversation().Messages[1], long), nil)

	top := ansi.Strip(m.View(40, 10))
	m, _ = m.Update(press("j"))
	if ansi.Strip(m.View(40, 10)) == top {
		t.Error("j didn't scroll")
	}
	m, _ = m.Update(press("k"))
	m, _ = m.Update(press("k"))
	if ansi.Strip(m.View(40, 10)) != top {
		t.Error("k didn't scroll back to the top, or scrolled past it")
	}

	m, _ = m.Update(press("G"))
	bottom := ansi.Strip(m.View(40, 10))
	if !strings.Contains(bottom, "Carol") {
		t.Errorf("G didn't reach the last message:\n%s", bottom)
	}
	m, _ = m.Update(press("j"))
	if ansi.Strip(m.View(40, 10)) != bottom {
		t.Error("scrolled past the bottom")
	}
	m, _ = m.Update(press("pgup"))
	m, _ = m.Update(press("pgdown"))
	if ansi.Strip(m.View(40, 10)) != bottom {
		t.Error("pgup then pgdown didn't return to the bottom")
	}
	m, _ = m.Update(press("g"))
	if got := ansi.Strip(m.View(40, 10)); got != top || len(strings.Split(got, "\n")) != 10 {
		t.Errorf("g didn't return to the top:\n%s", got)
	}
}

func TestEmptyAndClose(t *testing.T) {
	m := New(theme.Default(), time.Now)
	if m.IsOpen() || !strings.Contains(ansi.Strip(m.View(40, 5)), "No message selected.") {
		t.Error("new reader should be empty")
	}
	m = open(conversation(), 0)
	if !m.IsOpen() || !m.Showing(0, "INBOX", 2) || m.Showing(0, "INBOX", 99) || m.Showing(1, "INBOX", 2) {
		t.Error("Showing() is wrong")
	}
	if m.Close().IsOpen() {
		t.Error("Close() left the reader open")
	}
}

func TestFormatSize(t *testing.T) {
	for in, want := range map[int64]string{0: "0 B", 999: "999 B", 2048: "2.0 KB", 5 << 20: "5.0 MB"} {
		if got := FormatSize(in); got != want {
			t.Errorf("FormatSize(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestCurrent(t *testing.T) {
	if got := open(conversation(), 0).Current(); got != 2 {
		t.Errorf("Current() = %d, want the first unread (2)", got)
	}
	if got := open(conversation(), 1).Current(); got != 1 {
		t.Errorf("Current() = %d, want the focused message (1)", got)
	}
	if got := New(theme.Default(), time.Now).Current(); got != 0 {
		t.Errorf("Current() on an empty reader = %d, want 0", got)
	}
}

func TestReplyTarget(t *testing.T) {
	if got := open(conversation(), 0).ReplyTarget(); got != 3 {
		t.Errorf("ReplyTarget() = %d, want the latest (3), not the first unread", got)
	}
	m := open(conversation(), 1)
	if got := m.ReplyTarget(); got != 1 {
		t.Errorf("ReplyTarget() = %d, want the focused message (1)", got)
	}
	if got := m.Remove([]mail.UID{2}).ReplyTarget(); got != 1 {
		t.Errorf("ReplyTarget() = %d after removing another message, want 1", got)
	}
	if got := m.Remove([]mail.UID{1}).ReplyTarget(); got != 3 {
		t.Errorf("ReplyTarget() = %d after removing the focused message, want the latest (3)", got)
	}
	if got := open(conversation(), 99).ReplyTarget(); got != 3 {
		t.Errorf("ReplyTarget() = %d for a focus outside the thread, want the latest (3)", got)
	}
	if got := New(theme.Default(), time.Now).ReplyTarget(); got != 0 {
		t.Errorf("ReplyTarget() on an empty reader = %d, want 0", got)
	}
	// Equal dates: the highest UID wins.
	same := thread.Thread{Messages: []mail.Summary{summary(7, "A", 1, true), summary(9, "B", 1, true), summary(8, "C", 1, true)}}
	if got := open(same, 0).ReplyTarget(); got != 9 {
		t.Errorf("ReplyTarget() = %d on a date tie, want the highest UID (9)", got)
	}
}

func TestSetFlaggedAndRemove(t *testing.T) {
	m := open(conversation(), 0).SetFlagged([]mail.UID{1}, true)
	if !strings.Contains(view(m), "*") {
		t.Errorf("starred message has no star:\n%s", view(m))
	}

	m = m.Remove([]mail.UID{2})
	if strings.Contains(view(m), "Bob") || !m.IsOpen() {
		t.Errorf("Bob's message still shown, or the reader closed:\n%s", view(m))
	}
	if m.Current() != 3 {
		t.Errorf("Current() = %d after removing it, want the next message (3)", m.Current())
	}

	m = m.Remove([]mail.UID{1, 3})
	if m.IsOpen() {
		t.Error("reader still open with no messages left")
	}
}

func longThread(t *testing.T, w, h int) Model {
	t.Helper()
	m := New(theme.Default(), func() time.Time { return now }).SetSize(w, h).Open(0, "INBOX", conversation(), 0)
	return m.SetMessage(2, withBody(conversation().Messages[1], strings.Repeat("line\n", 100)), nil)
}

func TestScrollbar_ThumbAtTopAndWidthsAlign(t *testing.T) {
	m := longThread(t, 40, 5)
	rows := strings.Split(ansi.Strip(m.View(40, 5)), "\n")
	if len(rows) != 5 {
		t.Fatalf("rows = %d, want 5", len(rows))
	}
	for i, r := range rows {
		if w := ansi.StringWidth(r); w != 40 {
			t.Errorf("row %d width = %d, want 40: %q", i, w, r)
		}
	}
	if !strings.HasSuffix(rows[0], "┃") {
		t.Errorf("first row lacks the thumb: %q", rows[0])
	}
	if strings.HasSuffix(rows[4], "┃") {
		t.Errorf("thumb should not reach the last row at the top: %q", rows[4])
	}
}

func TestScrollbar_ThumbAtBottom(t *testing.T) {
	m, _ := longThread(t, 40, 5).Update(press("G"))
	rows := strings.Split(ansi.Strip(m.View(40, 5)), "\n")
	if !strings.HasSuffix(rows[len(rows)-1], "┃") {
		t.Errorf("last row lacks the thumb at the bottom:\n%s", strings.Join(rows, "\n"))
	}
	if strings.HasSuffix(rows[0], "┃") {
		t.Errorf("first row still has the thumb at the bottom: %q", rows[0])
	}
}

func TestScrollbar_AbsentWhenContentFits(t *testing.T) {
	m := open(conversation(), 0)
	if v := ansi.Strip(m.View(60, 200)); strings.Contains(v, "┃") {
		t.Errorf("short message shows a scrollbar:\n%s", v)
	}
}

func TestScrollbar_NoBarWhenNarrow(t *testing.T) {
	m := longThread(t, 19, 5)
	v := ansi.Strip(m.View(19, 5))
	if strings.Contains(v, "┃") {
		t.Errorf("narrow pane shows a scrollbar:\n%s", v)
	}
	if contentWidth(19) != 19 || contentWidth(20) != 18 {
		t.Errorf("contentWidth(19)=%d contentWidth(20)=%d", contentWidth(19), contentWidth(20))
	}
}
