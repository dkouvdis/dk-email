package threadlist

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/dkouvdis/dk-email/internal/mail"
)

var now = time.Date(2026, 11, 9, 15, 30, 0, 0, time.UTC)

func clock() time.Time { return now }

func press(k string) tea.KeyPressMsg {
	switch k {
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	}
	return tea.KeyPressMsg{Code: []rune(k)[0], Text: k}
}

func sum(uid mail.UID, from, subject string, at time.Time, id string, refs ...string) mail.Summary {
	s := mail.Summary{UID: uid, From: mail.Address{Name: from}, Subject: subject, Date: at, MessageID: id, Seen: true}
	if len(refs) > 0 {
		s.InReplyTo = refs[len(refs)-1]
		s.References = refs
	}
	return s
}

// inbox has three threads, newest first: Plan (3 messages), Lunch, Old news.
func inbox() []mail.Summary {
	plan1 := sum(10, "Alice", "Plan", now.Add(-5*time.Hour), "p1")
	plan2 := sum(12, "Bob", "Re: Plan", now.Add(-3*time.Hour), "p2", "p1")
	plan3 := sum(14, "Alice", "Re: Plan", now.Add(-1*time.Hour), "p3", "p1", "p2")
	plan3.Seen = false
	plan2.Flagged = true
	return []mail.Summary{
		plan3, plan2, plan1,
		sum(11, "Carol", "Lunch", now.Add(-4*time.Hour), "l1"),
		sum(5, "Dave", "Old news", time.Date(2025, 3, 2, 9, 0, 0, 0, time.UTC), "o1"),
	}
}

func lines(m Model, w int) []string {
	return strings.Split(ansi.Strip(m.View(w, 40)), "\n")
}

// col is the display column where s starts in row.
func col(row, s string) int {
	i := strings.Index(row, s)
	if i < 0 {
		return -1
	}
	return ansi.StringWidth(row[:i])
}

func cursorUID(t *testing.T, m Model) mail.UID {
	t.Helper()
	uid, ok := m.CursorUID()
	if !ok {
		t.Fatal("no cursor")
	}
	return uid
}

func send(m Model, keys ...string) (Model, tea.Cmd) {
	var cmd tea.Cmd
	for _, k := range keys {
		m, cmd = m.Update(press(k))
	}
	return m, cmd
}

func TestView_ThreadRowFormat(t *testing.T) {
	m := New(clock).SetSummaries(inbox(), false)

	got := lines(m, 70)

	if len(got) != 3 {
		t.Fatalf("got %d rows, want 3 threads:\n%s", len(got), strings.Join(got, "\n"))
	}
	plan := got[0]
	for _, want := range []string{"●", "★", "Alice", "Re: Plan (3)"} {
		if !strings.Contains(plan, want) {
			t.Errorf("plan row %q missing %q", plan, want)
		}
	}
	if !strings.HasSuffix(plan, "14:30") {
		t.Errorf("plan row %q should end with today's time 14:30", plan)
	}
	if lunch := got[1]; strings.Contains(lunch, "●") || strings.Contains(lunch, "★") || strings.Contains(lunch, "(") {
		t.Errorf("read, unstarred single-message row %q has markers or a count", lunch)
	}
	if !strings.HasSuffix(got[2], "2025-03-02") {
		t.Errorf("last year's row %q should end with the full date", got[2])
	}
	if col(got[0], "Alice") != col(got[1], "Carol") || col(got[0], "Re: Plan") != col(got[1], "Lunch") {
		t.Errorf("columns don't line up:\n%s\n%s", got[0], got[1])
	}
	for _, l := range got {
		if w := ansi.StringWidth(l); w != 70 {
			t.Errorf("row %q is %d wide, want 70", l, w)
		}
	}
}

func TestView_TruncatesToWidth(t *testing.T) {
	long := sum(1, strings.Repeat("Sender", 10), strings.Repeat("Subject ", 20), now.Add(-time.Minute), "x")
	m := New(clock).SetSummaries([]mail.Summary{long}, false)

	for _, w := range []int{24, 30, 45} {
		row := lines(m, w)[0]
		if got := ansi.StringWidth(row); got != w {
			t.Errorf("width %d: row %q is %d wide", w, row, got)
		}
		if !strings.HasSuffix(row, "15:29") || !strings.Contains(row, "…") {
			t.Errorf("width %d: row %q should truncate with … and keep the time", w, row)
		}
	}
}

func TestFormatDate(t *testing.T) {
	tests := []struct {
		at   time.Time
		want string
	}{
		{now.Add(-time.Minute), "15:29"},
		{time.Date(2026, 11, 9, 0, 1, 0, 0, time.UTC), "00:01"},
		{time.Date(2026, 11, 8, 23, 59, 0, 0, time.UTC), "Nov 8"},
		{time.Date(2026, 1, 15, 8, 0, 0, 0, time.UTC), "Jan 15"},
		{time.Date(2025, 12, 31, 8, 0, 0, 0, time.UTC), "2025-12-31"},
		{now.Add(2 * time.Hour), "17:30"}, // clock skew: still today
		{time.Time{}, ""},
	}
	for _, tt := range tests {
		if got := formatDate(tt.at, now); got != tt.want {
			t.Errorf("formatDate(%v) = %q, want %q", tt.at, got, tt.want)
		}
	}
}

func TestFormatDate_UsesNowsLocation(t *testing.T) {
	tz := time.FixedZone("UTC+10", 10*3600)
	at := time.Date(2026, 11, 9, 20, 0, 0, 0, time.UTC) // 06:00 on Nov 10 in UTC+10

	if got := formatDate(at, time.Date(2026, 11, 10, 9, 0, 0, 0, tz)); got != "06:00" {
		t.Errorf("formatDate = %q, want 06:00 in the local zone", got)
	}
}

func TestView_SubjectFallback(t *testing.T) {
	m := New(clock).SetSummaries([]mail.Summary{sum(1, "", "", now, "x")}, false)

	row := lines(m, 70)[0]

	if !strings.Contains(row, "(no subject)") || !strings.Contains(row, "(unknown sen") {
		t.Errorf("row %q, want placeholders for empty sender and subject", row)
	}
}

func TestUpdate_NavigationClamps(t *testing.T) {
	m := New(clock).SetSummaries(inbox(), false)

	steps := []struct {
		key  string
		want mail.UID
	}{
		{"j", 11}, {"down", 5}, {"j", 5}, {"k", 11}, {"up", 14}, {"k", 14}, {"G", 5}, {"g", 14},
	}
	for _, s := range steps {
		m, _ = send(m, s.key)
		if got := cursorUID(t, m); got != s.want {
			t.Fatalf("after %s: cursor on UID %d, want %d", s.key, got, s.want)
		}
	}
}

func TestUpdate_ExpandAndCollapse(t *testing.T) {
	m := New(clock).SetSummaries(inbox(), false)

	m, _ = send(m, "space")
	got := lines(m, 70)
	if len(got) != 6 {
		t.Fatalf("expanded view has %d rows, want 3 threads + 3 messages:\n%s", len(got), strings.Join(got, "\n"))
	}
	for i, wantSender := range []string{"Alice", "Bob", "Alice"} {
		row := got[1+i]
		if col(row, wantSender) <= col(got[0], "Alice") {
			t.Errorf("message row %d = %q, want %s indented past the thread's sender column", i, row, wantSender)
		}
	}
	if !strings.Contains(got[2], "★") || strings.Contains(got[2], "●") {
		t.Errorf("Bob's row %q should be starred and read", got[2])
	}
	if !strings.Contains(got[3], "●") {
		t.Errorf("the newest message row %q should be unread", got[3])
	}
	if cursorUID(t, m) != 14 {
		t.Errorf("expanding moved the cursor to UID %d", cursorUID(t, m))
	}

	// j walks into the messages, oldest first.
	m, _ = send(m, "j")
	if got := cursorUID(t, m); got != 10 {
		t.Errorf("first message row UID = %d, want 10 (oldest)", got)
	}
	m, _ = send(m, "j", "space")
	if n := len(lines(m, 70)); n != 3 {
		t.Errorf("space on a message row left %d rows, want the thread collapsed to 3", n)
	}
	if got := cursorUID(t, m); got != 14 {
		t.Errorf("collapsing put the cursor on UID %d, want the thread (14)", got)
	}
	if !m.OnThreadRow() {
		t.Error("cursor should be on the thread row after collapsing")
	}
}

func TestSelected(t *testing.T) {
	m := New(clock).SetSummaries(inbox(), false)
	if _, _, ok := New(clock).Selected(); ok {
		t.Error("Selected() ok on an empty list")
	}

	th, uid, _ := m.Selected()
	if th.Len() != 3 || uid != 0 {
		t.Errorf("thread row: Selected() = %d messages, uid %d; want 3, 0", th.Len(), uid)
	}
	m, _ = send(m, "space", "j", "j")
	th, uid, _ = m.Selected()
	if th.Len() != 3 || uid != 12 {
		t.Errorf("message row: Selected() = %d messages, uid %d; want 3, 12", th.Len(), uid)
	}
}

func TestPaging_AsksForOlderAtTheEnd(t *testing.T) {
	m := New(clock).SetSummaries(inbox(), true)

	m, cmd := send(m, "j")
	if cmd != nil {
		t.Fatal("moving before the last row asked for more")
	}
	m, cmd = send(m, "G")
	if cmd == nil {
		t.Fatal("reaching the last row didn't ask for more")
	}
	if msg, ok := cmd().(LoadMoreMsg); !ok || msg.Before != 5 {
		t.Fatalf("cmd sent %#v, want LoadMoreMsg{Before: 5}", msg)
	}
	if !m.LoadingMore() || !strings.Contains(strings.Join(lines(m, 60), "\n"), "Loading more…") {
		t.Error("list doesn't show it's loading more")
	}
	if _, cmd = send(m, "k", "j"); cmd != nil {
		t.Error("asked for more again while a page was loading")
	}
}

func TestPaging_MergesAcrossPageBoundaryWithoutMovingCursor(t *testing.T) {
	first := inbox()
	first[3].References, first[3].InReplyTo = []string{"l0"}, "l0" // Lunch replies to l0, not loaded yet
	page := []mail.Summary{
		sum(4, "Carol", "Lunch?", now.Add(-48*time.Hour), "l0"),
		sum(3, "Erin", "Ancient", time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), "a1"),
	}
	m := New(clock).SetSummaries(first, true)
	m, _ = send(m, "G")

	m = m.AddPage(page, false)

	if m.LoadingMore() {
		t.Error("still loading after the page arrived")
	}
	got := lines(m, 70)
	if len(got) != 4 {
		t.Fatalf("got %d threads after paging, want 4:\n%s", len(got), strings.Join(got, "\n"))
	}
	if !strings.Contains(got[1], "Lunch (2)") {
		t.Errorf("Lunch row %q: the older root should merge into the thread", got[1])
	}
	if !strings.Contains(got[3], "Ancient") {
		t.Errorf("last row %q, want the older thread appended", got[3])
	}
	if got := cursorUID(t, m); got != 5 {
		t.Errorf("cursor moved to UID %d after paging, want it to stay on 5", got)
	}
	if _, cmd := send(m, "G"); cmd != nil {
		t.Error("asked for more after the last page")
	}
}

func TestPaging_ExpandedThreadStaysExpandedAfterMerge(t *testing.T) {
	first := inbox()
	first[3].References, first[3].InReplyTo = []string{"l0"}, "l0"
	m := New(clock).SetSummaries(first, true)
	m, _ = send(m, "j", "space") // expand Lunch

	m = m.AddPage([]mail.Summary{sum(4, "Carol", "Lunch?", now.Add(-48*time.Hour), "l0")}, false)

	if n := len(lines(m, 70)); n != 5 {
		t.Errorf("got %d rows, want 3 threads + Lunch's 2 messages", n)
	}
}

func TestPaging_FailureAllowsRetry(t *testing.T) {
	m := New(clock).SetSummaries(inbox(), true)
	m, _ = send(m, "G")

	m = m.PageFailed()

	if m.LoadingMore() {
		t.Fatal("still loading after failure")
	}
	if _, cmd := send(m, "k", "j"); cmd == nil {
		t.Error("returning to the last row didn't retry")
	}
}

func TestPaging_DuplicatesAreDropped(t *testing.T) {
	m := New(clock).SetSummaries(inbox(), true)

	m = m.AddPage(inbox()[:2], true)

	if n := len(lines(m, 70)); n != 3 {
		t.Errorf("got %d rows after a duplicate page, want 3", n)
	}
}

func TestSetSummaries_ResetsCursorAndExpansion(t *testing.T) {
	m := New(clock).SetSummaries(inbox(), false)
	m, _ = send(m, "space", "G")

	m = m.SetSummaries(inbox(), false)

	if cursorUID(t, m) != 14 || len(lines(m, 70)) != 3 {
		t.Error("SetSummaries should start at the top with every thread collapsed")
	}
}

func TestView_EmptyAndScrolling(t *testing.T) {
	if v := ansi.Strip(New(clock).SetSummaries(nil, false).View(40, 5)); !strings.Contains(v, "No messages.") {
		t.Errorf("empty view = %q", v)
	}

	var many []mail.Summary
	for i := range 50 {
		many = append(many, sum(mail.UID(i+1), "S", fmt.Sprintf("subject %02d", i), now.Add(-time.Duration(i)*time.Minute), fmt.Sprint(i)))
	}
	m := New(clock).SetSummaries(many, false).SetHeight(10)
	m, _ = send(m, "G")

	v := ansi.Strip(m.View(50, 10))
	if n := len(strings.Split(v, "\n")); n != 10 {
		t.Errorf("view has %d lines, want 10", n)
	}
	if !strings.Contains(v, "subject 49") {
		t.Errorf("cursor row scrolled out of view:\n%s", v)
	}
	m, _ = send(m, "g")
	if v := ansi.Strip(m.View(50, 10)); !strings.Contains(v, "subject 00") {
		t.Errorf("g didn't scroll back to the top:\n%s", v)
	}
}

func TestSetSeen_UpdatesRowsInPlace(t *testing.T) {
	m := New(clock).SetSummaries(inbox(), false)
	m, _ = send(m, "space", "j")

	m = m.SetSeen([]mail.UID{14}, true)

	if strings.Contains(lines(m, 70)[0], "●") {
		t.Errorf("Plan row still unread: %q", lines(m, 70)[0])
	}
	if got := cursorUID(t, m); got != 10 || m.OnThreadRow() {
		t.Errorf("cursor moved to %d (thread row %v)", got, m.OnThreadRow())
	}
}

func TestSetFlagged(t *testing.T) {
	m := New(clock).SetSummaries(inbox(), false)

	m = m.SetFlagged([]mail.UID{12}, false).SetFlagged([]mail.UID{11}, true)

	got := lines(m, 70)
	if strings.Contains(got[0], "★") || !strings.Contains(got[1], "★") {
		t.Errorf("stars not updated:\n%s", strings.Join(got, "\n"))
	}
}

func TestLookup(t *testing.T) {
	m := New(clock).SetSummaries(inbox(), false)

	got := m.Lookup([]mail.UID{14, 99, 5})

	if len(got) != 2 || got[0].UID != 14 || got[1].UID != 5 {
		t.Errorf("Lookup = %+v, want UIDs 14 and 5", got)
	}
}

func TestRemove_MovesCursorToNextThreadAndRestoreUndoes(t *testing.T) {
	m := New(clock).SetSummaries(inbox(), true) // Plan (14,12,10), Lunch (11), Old news (5)

	m = m.Remove([]mail.UID{14, 12, 10})

	if n := len(lines(m, 70)); n != 2 {
		t.Fatalf("got %d rows after removing Plan, want 2", n)
	}
	if got := cursorUID(t, m); got != 11 {
		t.Errorf("cursor on %d after removing the cursor's thread, want the next thread (11)", got)
	}

	m, _ = send(m, "G")
	m = m.PageFailed() // the end-of-list fetch is irrelevant here
	m = m.Remove([]mail.UID{5})
	if got := cursorUID(t, m); got != 11 {
		t.Errorf("cursor on %d after removing the last thread, want the previous one (11)", got)
	}

	m = m.Restore(inbox()[:3])
	if n := len(lines(m, 70)); n != 2 || !strings.Contains(lines(m, 70)[0], "Re: Plan (3)") {
		t.Errorf("Restore didn't bring Plan back:\n%s", strings.Join(lines(m, 70), "\n"))
	}
	if got := cursorUID(t, m); got != 11 {
		t.Errorf("Restore moved the cursor to %d", got)
	}
}

func TestRemove_MessageRowKeepsThread(t *testing.T) {
	m := New(clock).SetSummaries(inbox(), false)
	m, _ = send(m, "space", "j", "j") // Bob's message (12)

	m = m.Remove([]mail.UID{12})

	got := lines(m, 70)
	if !strings.Contains(got[0], "Re: Plan (2)") {
		t.Errorf("thread row = %q, want 2 messages left", got[0])
	}
	if uid := cursorUID(t, m); uid != 14 && uid != 10 {
		t.Errorf("cursor on %d, want it to stay in the Plan thread", uid)
	}
}

func TestRemove_EverythingLeavesEmptyList(t *testing.T) {
	m := New(clock).SetSummaries(inbox()[3:4], false)

	m = m.Remove([]mail.UID{11})

	if _, ok := m.CursorUID(); ok || !strings.Contains(lines(m, 40)[0], "No messages.") {
		t.Error("removing every message should leave an empty list")
	}
}
