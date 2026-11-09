package ui

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/dkouvdis/dk-email/internal/mail"
	"github.com/dkouvdis/dk-email/internal/mail/mailtest"
)

func calls(f *mailtest.Fake, method mailtest.Method) []mailtest.Call {
	var out []mailtest.Call
	for _, c := range f.Calls() {
		if c.Method == method {
			out = append(out, c)
		}
	}
	return out
}

// press sends a key and settles every command it starts.
func pressSettled(t *testing.T, m Model, k string) Model {
	t.Helper()
	next, cmd := m.Update(press(k))
	return settle(t, next.(Model), cmd)
}

func TestReader_EnterOpensThreadAndMarksRead(t *testing.T) {
	f := newFixture()
	m := started(t, f, 160, 30)

	m = pressSettled(t, m, "enter") // the Plan thread: Alice (read), Bob (unread)

	if m.focus != PaneReader {
		t.Errorf("focus = %v, want reader", m.focus)
	}
	v := plain(m)
	for _, want := range []string{"From: Bob", "Body of Re: Plan from Bob.", "Alice"} {
		if !strings.Contains(v, want) {
			t.Errorf("reader missing %q:\n%s", want, v)
		}
	}
	got := calls(f.personal, mailtest.MethodMessage)
	if len(got) != 1 || got[0].Folder != "INBOX" {
		t.Errorf("Message calls = %+v, want one for Bob's message", got)
	}
	flags := calls(f.personal, mailtest.MethodSetFlag)
	if len(flags) != 1 || flags[0].Flag != mail.FlagSeen || !flags[0].On || !slices.Equal(flags[0].UIDs, []mail.UID{got[0].UIDs[0]}) {
		t.Errorf("SetFlag calls = %+v, want \\Seen on the opened message", flags)
	}
	if fol, _ := m.side.Folder(0, "INBOX"); fol.Unread != 1 {
		t.Errorf("sidebar unread = %d, want 1 after reading Bob's message", fol.Unread)
	}
	if strings.Contains(strings.Split(plain(m), "\n")[2], "●") {
		t.Errorf("Plan row still unread after opening:\n%s", plain(m))
	}
}

func TestReader_ReadMessageIsNotFlaggedAgain(t *testing.T) {
	f := newFixture()
	m := started(t, f, 160, 30)
	m = pressSettled(t, m, "space")
	m = pressSettled(t, m, "j") // Alice's message, already read

	m = pressSettled(t, m, "enter")

	if !strings.Contains(plain(m), "Body of Plan from Alice.") {
		t.Errorf("Alice's message not shown:\n%s", plain(m))
	}
	if n := len(calls(f.personal, mailtest.MethodSetFlag)); n != 0 {
		t.Errorf("SetFlag called %d times for an already-read message", n)
	}
}

func TestReader_KeysScrollExpandAndGoBack(t *testing.T) {
	f := newFixture()
	m := started(t, f, 160, 30)
	m = pressSettled(t, m, "enter")

	m = pressSettled(t, m, "space")
	if !strings.Contains(plain(m), "Body of Plan from Alice.") {
		t.Errorf("space in the reader didn't expand and load every message:\n%s", plain(m))
	}
	if n := len(calls(f.personal, mailtest.MethodMessage)); n != 2 {
		t.Errorf("Message calls = %d, want 2", n)
	}

	m = pressSettled(t, m, "esc")
	if m.focus != PaneList {
		t.Errorf("esc left focus on %v, want list", m.focus)
	}
}

func TestReader_SlowFetchShowsLoadingAndKeysStillWork(t *testing.T) {
	f := newFixture()
	m := started(t, f, 160, 30)
	f.personal.SetDelay(300 * time.Millisecond)

	next, cmd := m.Update(press("enter"))
	m = next.(Model)
	if !strings.Contains(plain(m), "Loading…") {
		t.Errorf("no loading state while the body is fetched:\n%s", plain(m))
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()

	start := time.Now()
	m = send(t, m, press("esc"), press("j"))
	if m.focus != PaneList || time.Since(start) > 100*time.Millisecond {
		t.Errorf("keys blocked while a message loaded (focus %v)", m.focus)
	}
	result := <-done
	m = settle(t, m, func() tea.Msg { return result })
	if !strings.Contains(plain(m), "Body of Re: Plan from Bob.") {
		t.Errorf("body not shown once loaded:\n%s", plain(m))
	}
}

func TestReader_FetchErrorShowsInReaderAndStatus(t *testing.T) {
	f := newFixture()
	f.personal.Fail(mailtest.MethodMessage, errors.New("connection reset"))
	m := started(t, f, 160, 30)

	m = pressSettled(t, m, "enter")

	if !strings.Contains(plain(m), "Couldn't load this message: connection reset") {
		t.Errorf("reader doesn't show the error:\n%s", plain(m))
	}
	if !m.statusErr || !strings.Contains(m.status, "open message: connection reset") {
		t.Errorf("status = %q", m.status)
	}
	if n := len(calls(f.personal, mailtest.MethodSetFlag)); n != 0 {
		t.Error("a message that failed to load was marked read")
	}
}

func TestReader_UnsupportedSetFlagIsQuiet(t *testing.T) {
	f := newFixture()
	f.personal.Fail(mailtest.MethodSetFlag, fmt.Errorf("imap: SetFlag: %w", errors.ErrUnsupported))
	m := started(t, f, 160, 30)

	m = pressSettled(t, m, "enter")

	if m.statusErr {
		t.Errorf("status shows %q; unsupported SetFlag should only be logged", m.status)
	}
	if fol, _ := m.side.Folder(0, "INBOX"); fol.Unread != 2 {
		t.Errorf("sidebar unread = %d, want unchanged 2", fol.Unread)
	}
}

func TestReader_SetFlagErrorShows(t *testing.T) {
	f := newFixture()
	f.personal.Fail(mailtest.MethodSetFlag, errors.New("STORE refused"))
	m := started(t, f, 160, 30)

	m = pressSettled(t, m, "enter")

	if !m.statusErr || !strings.Contains(m.status, "mark seen: STORE refused") {
		t.Errorf("status = %q", m.status)
	}
}

func TestReader_StaleMessageAndOtherFolderAreIgnored(t *testing.T) {
	f := newFixture()
	m := started(t, f, 160, 30)
	next, _ := m.Update(press("enter")) // don't run the fetch yet
	m = next.(Model)

	opened, cmd := m.openFolder(0, "Receipts")
	m = settle(t, opened, cmd)
	next, _ = m.Update(messageMsg{account: 0, folder: "INBOX", uid: 3, msg: mail.Message{Body: "late"}})
	m = next.(Model)

	if m.read.IsOpen() || strings.Contains(plain(m), "late") {
		t.Errorf("opening another folder should close the reader and drop late bodies:\n%s", plain(m))
	}
}

func TestReader_FolderChangedReloads(t *testing.T) {
	f := newFixture()
	m := started(t, f, 160, 30)
	next, _ := m.Update(press("enter"))
	m = next.(Model)
	th, _, _ := m.list.Selected()
	next, cmd := m.Update(messageMsg{account: 0, folder: "INBOX", uid: th.Latest().UID, err: fmt.Errorf("x: %w", mail.ErrFolderChanged)})
	m = settle(t, next.(Model), cmd)

	if m.read.IsOpen() || !strings.Contains(m.status, "changed on the server; reloaded.") || m.list.Count() != 3 {
		t.Errorf("status %q, reader open %v, %d messages; want a reload", m.status, m.read.IsOpen(), m.list.Count())
	}
}
