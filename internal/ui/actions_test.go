package ui

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/dkouvdis/dk-email/internal/mail"
	"github.com/dkouvdis/dk-email/internal/mail/mailtest"
)

// Personal INBOX holds UID 1 Alice "Plan" (read), 2 Carol "Lunch?"
// (unread) and 3 Bob "Re: Plan" (unread). The list shows the Plan thread
// (1, 3) first, then Lunch.

func lastCall(t *testing.T, f *mailtest.Fake, method mailtest.Method) mailtest.Call {
	t.Helper()
	got := calls(f, method)
	if len(got) == 0 {
		t.Fatalf("no %s call", method)
	}
	return got[len(got)-1]
}

func unread(m Model, folder string) int {
	f, _ := m.side.Folder(0, folder)
	return f.Unread
}

func subjects(f *mailtest.Fake, folder string) []string {
	var out []string
	for _, msg := range f.Messages(folder) {
		out = append(out, msg.Subject)
	}
	slices.Sort(out)
	return out
}

func TestTriage_ToggleReadOnThread(t *testing.T) {
	f := newFixture()
	m := started(t, f, 160, 30)

	m = pressSettled(t, m, "u") // Plan has an unread message: mark the thread read
	if c := lastCall(t, f.personal, mailtest.MethodSetFlag); c.Flag != mail.FlagSeen || !c.On || !slices.Equal(c.UIDs, []mail.UID{3}) {
		t.Errorf("SetFlag = %+v, want \\Seen on UID 3 only (1 is already read)", c)
	}
	if unread(m, "INBOX") != 1 || strings.Contains(strings.Split(plain(m), "\n")[2], "●") {
		t.Errorf("thread still unread or count wrong (%d):\n%s", unread(m, "INBOX"), plain(m))
	}

	m = pressSettled(t, m, "u") // all read now: mark the thread unread
	if c := lastCall(t, f.personal, mailtest.MethodSetFlag); c.On || !slices.Equal(c.UIDs, []mail.UID{1, 3}) {
		t.Errorf("SetFlag = %+v, want unread on 1 and 3", c)
	}
	if unread(m, "INBOX") != 3 {
		t.Errorf("unread = %d, want 3", unread(m, "INBOX"))
	}
}

func TestTriage_StarOnThreadAndMessage(t *testing.T) {
	f := newFixture()
	m := started(t, f, 160, 30)

	m = pressSettled(t, m, "s")
	if c := lastCall(t, f.personal, mailtest.MethodSetFlag); c.Flag != mail.FlagFlagged || !c.On || !slices.Equal(c.UIDs, []mail.UID{1, 3}) {
		t.Errorf("thread star = %+v, want \\Flagged on 1 and 3", c)
	}
	if !strings.Contains(plain(m), "*") {
		t.Errorf("no star shown:\n%s", plain(m))
	}

	m = pressSettled(t, m, "space")
	m = pressSettled(t, m, "j") // Alice's message
	m = pressSettled(t, m, "s")
	if c := lastCall(t, f.personal, mailtest.MethodSetFlag); c.On || !slices.Equal(c.UIDs, []mail.UID{1}) {
		t.Errorf("message unstar = %+v, want UID 1 only", c)
	}
	m = pressSettled(t, m, "u")
	if c := lastCall(t, f.personal, mailtest.MethodSetFlag); c.Flag != mail.FlagSeen || c.On || !slices.Equal(c.UIDs, []mail.UID{1}) {
		t.Errorf("message u = %+v, want UID 1 marked unread", c)
	}
}

func TestTriage_ArchiveAndDeleteThread(t *testing.T) {
	tests := []struct {
		key, dest, status string
	}{
		{"e", "[Gmail]/All Mail", "Archived 2 messages."},
		{"d", "[Gmail]/Trash", "Deleted 2 messages."},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			f := newFixture()
			m := started(t, f, 160, 30)

			next, cmd := m.Update(press(tt.key))
			m = next.(Model)
			if strings.Contains(plain(m), "Re: Plan") {
				t.Errorf("thread not removed before the server answered:\n%s", plain(m))
			}
			m = settle(t, m, cmd)

			c := lastCall(t, f.personal, mailtest.MethodMove)
			if c.Folder != "INBOX" || c.To != tt.dest || !slices.Equal(c.UIDs, []mail.UID{1, 3}) {
				t.Errorf("Move = %+v, want UIDs 1 and 3 to %s", c, tt.dest)
			}
			if got := subjects(f.personal, tt.dest); !slices.Contains(got, "Plan") || !slices.Contains(got, "Re: Plan") {
				t.Errorf("%s holds %v: the messages must be moved, never expunged", tt.dest, got)
			}
			if uid, _ := m.list.CursorUID(); uid != 2 {
				t.Errorf("cursor on %d, want the next thread (Lunch, 2)", uid)
			}
			if m.status != tt.status || unread(m, "INBOX") != 1 {
				t.Errorf("status %q, unread %d; want %q and 1", m.status, unread(m, "INBOX"), tt.status)
			}
		})
	}
}

func TestTriage_FailuresRollBack(t *testing.T) {
	f := newFixture()
	f.personal.Fail(mailtest.MethodSetFlag, errors.New("STORE refused"))
	f.personal.Fail(mailtest.MethodMove, errors.New("MOVE refused"))
	m := started(t, f, 160, 30)
	before := plain(m)

	m = pressSettled(t, m, "s")
	if !strings.Contains(m.status, "star: STORE refused") || plain(m) == before && strings.Contains(plain(m), "*") {
		t.Errorf("star failure: status %q", m.status)
	}
	if strings.Contains(plain(m), "*") {
		t.Errorf("star not rolled back:\n%s", plain(m))
	}

	m = pressSettled(t, m, "u")
	if unread(m, "INBOX") != 2 || !strings.Contains(m.status, "mark as read: STORE refused") {
		t.Errorf("read failure: unread %d, status %q", unread(m, "INBOX"), m.status)
	}

	m = pressSettled(t, m, "e")
	if !strings.Contains(plain(m), "Re: Plan (2)") || unread(m, "INBOX") != 2 {
		t.Errorf("archive not rolled back (unread %d):\n%s", unread(m, "INBOX"), plain(m))
	}
	if !m.statusErr || m.status != "archive: MOVE refused" {
		t.Errorf("status = %q", m.status)
	}
	if got := subjects(f.personal, "INBOX"); len(got) != 3 {
		t.Errorf("INBOX on the server holds %v after a failed move", got)
	}
}

func TestTriage_DisabledOrAlreadyThere(t *testing.T) {
	f := newFixture()
	m := started(t, f, 160, 30)

	opened, cmd := m.openFolder(1, "INBOX") // Work has no Trash
	m = settle(t, opened, cmd)
	m = pressSettled(t, m, "d")
	if !m.statusErr || !strings.Contains(m.status, "can't delete in Work: no trash folder") {
		t.Errorf("status = %q, want delete disabled for Work", m.status)
	}
	if n := len(calls(f.work, mailtest.MethodMove)); n != 0 {
		t.Errorf("Work Move called %d times", n)
	}

	f.personal.AddMessage("[Gmail]/All Mail", msg("Dan", "Old", fixedNow, true, "o1"))
	f.personal.AddMessage("[Gmail]/Trash", msg("Eve", "Junk", fixedNow, true, "j1"))
	for folder, want := range map[string]string{
		"[Gmail]/All Mail": "Already in the archive folder.",
		"[Gmail]/Trash":    "Already in Trash; dk-email never deletes mail permanently.",
	} {
		opened, cmd := m.openFolder(0, folder)
		m = settle(t, opened, cmd)
		k := "e"
		if strings.HasSuffix(folder, "Trash") {
			k = "d"
		}
		m = pressSettled(t, m, k)
		if m.status != want {
			t.Errorf("%s in %s: status %q, want %q", k, folder, m.status, want)
		}
	}
	if n := len(calls(f.personal, mailtest.MethodMove)); n != 0 {
		t.Errorf("Personal Move called %d times", n)
	}
}

func TestTriage_InReaderActsOnCurrentMessage(t *testing.T) {
	f := newFixture()
	m := started(t, f, 160, 30)
	m = pressSettled(t, m, "enter") // Plan thread; Bob's message (3) is current

	m = pressSettled(t, m, "s")
	if c := lastCall(t, f.personal, mailtest.MethodSetFlag); c.Flag != mail.FlagFlagged || !slices.Equal(c.UIDs, []mail.UID{3}) {
		t.Errorf("reader s = %+v, want UID 3 only", c)
	}

	m = pressSettled(t, m, "e")
	if c := lastCall(t, f.personal, mailtest.MethodMove); !slices.Equal(c.UIDs, []mail.UID{3}) {
		t.Errorf("reader e = %+v, want UID 3 only", c)
	}
	if !m.read.IsOpen() || m.read.Current() != 1 || !m.readerPopup {
		t.Errorf("reader open %v current %d popup %v; want Alice's message still shown", m.read.IsOpen(), m.read.Current(), m.readerPopup)
	}
	if !strings.Contains(plain(m), "Plan") || strings.Contains(plain(m), "Re: Plan (2)") {
		t.Errorf("list should keep the thread with one message:\n%s", plain(m))
	}

	m = pressSettled(t, m, "d")
	if m.read.IsOpen() || m.readerPopup || m.focus != PaneList {
		t.Errorf("deleting the last message should close the reader (open %v, popup %v, focus %v)", m.read.IsOpen(), m.readerPopup, m.focus)
	}
}

func TestTriage_InPopup(t *testing.T) {
	f := newFixture()
	m := started(t, f, 70, 24)
	m = pressSettled(t, m, "j")     // Lunch: one message
	m = pressSettled(t, m, "enter") // popup

	m = pressSettled(t, m, "s")
	if c := lastCall(t, f.personal, mailtest.MethodSetFlag); c.Flag != mail.FlagFlagged || !slices.Equal(c.UIDs, []mail.UID{2}) {
		t.Errorf("popup s = %+v, want UID 2", c)
	}
	m = pressSettled(t, m, "d")
	if m.readerPopup || m.read.IsOpen() {
		t.Error("deleting the only message should close the popup")
	}
	if got := subjects(f.personal, "[Gmail]/Trash"); !slices.Equal(got, []string{"Lunch?"}) {
		t.Errorf("Trash holds %v", got)
	}
}

func TestTriage_IgnoredInSidebarAndWhileLoading(t *testing.T) {
	f := newFixture()
	m := started(t, f, 160, 30)

	m = pressSettled(t, m, "h")
	m = pressSettled(t, m, "d")
	m = pressSettled(t, m, "u")
	if len(calls(f.personal, mailtest.MethodMove))+len(calls(f.personal, mailtest.MethodSetFlag)) != 0 {
		t.Error("triage keys acted with the sidebar focused")
	}

	m = pressSettled(t, m, "l")
	next, _ := m.openFolder(0, "Receipts") // not settled: still loading
	m = next
	if next2, cmd := m.Update(press("d")); cmd != nil || len(next2.(Model).status) > 0 && next2.(Model).statusErr {
		t.Error("d acted while the folder was loading")
	}
}

func TestMove_PickerListsOtherFoldersOfThisAccountOnly(t *testing.T) {
	m := started(t, newFixture(), 160, 30)

	m = pressSettled(t, m, "m")

	if !m.showPicker {
		t.Fatal("m didn't open the picker")
	}
	v := ansi.Strip(m.picker.View())
	for _, want := range []string{"Move 2 messages to", "Sent Mail", "All Mail", "Trash", "Receipts"} {
		if !strings.Contains(v, want) {
			t.Errorf("picker missing %q:\n%s", want, v)
		}
	}
	if strings.Contains(v, "Work") || strings.Contains(v, "Archive") {
		t.Errorf("picker lists another account's folder:\n%s", v)
	}
	m = pressSettled(t, m, "g")
	if p, _ := m.picker.Selected(); p.folder == "INBOX" {
		t.Error("the current folder can be chosen as the move target")
	}
}

func TestMove_ThreadToChosenFolder(t *testing.T) {
	f := newFixture()
	m := started(t, f, 160, 30)

	// Sent Mail, All Mail, Trash, Receipts.
	for _, k := range []string{"m", "j", "j", "j"} {
		m = pressSettled(t, m, k)
	}
	next, cmd := m.Update(press("enter"))
	m = next.(Model)
	if m.showPicker || strings.Contains(plain(m), "Re: Plan") {
		t.Errorf("picker open %v, or the thread wasn't removed at once:\n%s", m.showPicker, plain(m))
	}
	m = settle(t, m, cmd)

	if c := lastCall(t, f.personal, mailtest.MethodMove); c.To != "Receipts" || !slices.Equal(c.UIDs, []mail.UID{1, 3}) {
		t.Errorf("Move = %+v, want UIDs 1 and 3 to Receipts", c)
	}
	if got := subjects(f.personal, "Receipts"); len(got) != 3 {
		t.Errorf("Receipts holds %v, want the receipt plus the Plan thread", got)
	}
	if m.status != "Moved 2 messages to Receipts." || unread(m, "INBOX") != 1 {
		t.Errorf("status %q, unread %d", m.status, unread(m, "INBOX"))
	}
}

func TestMove_MessageScopeAndEscCancels(t *testing.T) {
	f := newFixture()
	m := started(t, f, 160, 30)
	m = pressSettled(t, m, "space")
	m = pressSettled(t, m, "j") // Alice's message only

	m = pressSettled(t, m, "m")
	if !strings.Contains(plain(m), "Move 1 message to") {
		t.Errorf("picker title wrong:\n%s", plain(m))
	}
	m = pressSettled(t, m, "esc")
	if m.showPicker || m.moveTargets != nil || len(calls(f.personal, mailtest.MethodMove)) != 0 {
		t.Fatal("esc should close the picker without moving")
	}

	m = pressSettled(t, m, "m")
	m = pressSettled(t, m, "enter") // Sent Mail
	if c := lastCall(t, f.personal, mailtest.MethodMove); c.To != "[Gmail]/Sent Mail" || !slices.Equal(c.UIDs, []mail.UID{1}) {
		t.Errorf("Move = %+v, want UID 1 to Sent Mail", c)
	}
	if !strings.Contains(plain(m), "Re: Plan") || strings.Contains(plain(m), "Re: Plan (2)") {
		t.Errorf("the thread should remain with one message:\n%s", plain(m))
	}
}

func TestMove_FailureRollsBack(t *testing.T) {
	f := newFixture()
	f.personal.Fail(mailtest.MethodMove, errors.New("MOVE refused"))
	m := started(t, f, 160, 30)

	for _, k := range []string{"m", "enter"} {
		m = pressSettled(t, m, k)
	}

	if !strings.Contains(plain(m), "Re: Plan (2)") || m.status != "move: MOVE refused" {
		t.Errorf("status %q, view:\n%s", m.status, plain(m))
	}
}

func TestMove_FromReaderPopup(t *testing.T) {
	f := newFixture()
	m := started(t, f, 70, 24)
	m = pressSettled(t, m, "enter") // popup on Bob's message (3)

	m = pressSettled(t, m, "m")
	if !m.showPicker || !m.readerPopup || !strings.Contains(plain(m), "Move 1 message to") {
		t.Fatalf("m in the popup: picker %v, reader popup %v", m.showPicker, m.readerPopup)
	}
	m = pressSettled(t, m, "q") // closes the picker only
	if m.showPicker || !m.readerPopup {
		t.Fatal("q should close the move picker and keep the reader popup")
	}
	for _, k := range []string{"m", "G", "enter"} { // last folder: Receipts
		m = pressSettled(t, m, k)
	}
	if c := lastCall(t, f.personal, mailtest.MethodMove); c.To != "Receipts" || !slices.Equal(c.UIDs, []mail.UID{3}) {
		t.Errorf("Move = %+v, want UID 3 to Receipts", c)
	}
	if !m.readerPopup || m.read.Current() != 1 {
		t.Errorf("popup %v current %d; want Alice's message still shown", m.readerPopup, m.read.Current())
	}
}
