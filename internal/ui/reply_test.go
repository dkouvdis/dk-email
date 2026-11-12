package ui

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/dkouvdis/dk-email/internal/mail"
	"github.com/dkouvdis/dk-email/internal/mail/mailtest"
)

var (
	replyMe    = mail.Address{Name: "Me Myself", Email: "me@example.com"}
	replyAlice = mail.Address{Name: "Alice", Email: "alice@example.com"}
	replyBob   = mail.Address{Name: "Bob", Email: "bob@example.com"}
	replyCarol = mail.Address{Name: "Carol", Email: "carol@example.com"}
	replyDave  = mail.Address{Name: "Dave", Email: "dave@example.com"}
)

// replyUIDs are the UIDs of the "Offsite" thread, oldest first.
type replyUIDs struct{ first, second, latest mail.UID }

// replyFixture is composeFixture plus a three-message "Offsite" thread, the
// newest thread in the inbox, whose first message is the only unread one.
func replyFixture(t *testing.T) (Model, *mailtest.Sender, *mailtest.Fake, replyUIDs) {
	t.Helper()
	t.Setenv("TMPDIR", t.TempDir())
	s := &mailtest.Sender{}
	f := newFixture()
	f.personalSender = s
	add := func(from mail.Address, to, cc []mail.Address, subject string, ago time.Duration, seen bool, id string, refs ...string) mail.UID {
		m := msg("", subject, fixedNow.Add(-ago), seen, id, refs...)
		m.From, m.To, m.Cc = from, to, cc
		m.Body = "Body of " + id + ".\n\nSecond paragraph."
		return f.personal.AddMessage("INBOX", m)
	}
	var u replyUIDs
	u.first = add(replyAlice, []mail.Address{replyMe, replyBob}, []mail.Address{replyCarol}, "Offsite", 3*time.Hour, false, "o1")
	u.second = add(replyBob, []mail.Address{replyMe}, nil, "Re: Offsite", 2*time.Hour, true, "o2", "o1")
	u.latest = add(replyDave, []mail.Address{replyMe, replyBob}, []mail.Address{replyAlice, {Email: "ME@example.com"}, replyCarol}, "Re: Offsite", time.Hour, true, "o3", "o1", "o2")

	m := started(t, f, 160, 30)
	m.accts[0].Email, m.accts[0].FromName = replyMe.Email, replyMe.Name
	m.editorCmd = func(path string) *exec.Cmd { return exec.Command("true", path) }
	return m, s, f.personal, u
}

// replied presses k and, if that started a fetch, delivers its result.
func replied(t *testing.T, m Model, k string) Model {
	t.Helper()
	m, cmd := keyCmd(t, m, k)
	if cmd == nil {
		t.Fatalf("%s started nothing (status %q)", k, m.status)
	}
	if m.draft != nil && m.draft.loading {
		m = send(t, m, cmd())
	}
	if m.draft == nil || m.draft.loading || m.editorPath == "" {
		t.Fatalf("%s didn't open the editor (draft %v, status %q)", k, m.draft, m.status)
	}
	return m
}

func messageFetches(fake *mailtest.Fake) (uids []mail.UID) {
	for _, c := range fake.Calls() {
		if c.Method == mailtest.MethodMessage {
			uids = append(uids, c.UIDs...)
		}
	}
	return uids
}

func addrs(l []mail.Address) string {
	parts := make([]string, len(l))
	for i, a := range l {
		parts[i] = a.Email
	}
	return strings.Join(parts, ",")
}

func TestReply_CollapsedThreadRepliesToTheLatestMessage(t *testing.T) {
	m, _, fake, u := replyFixture(t)

	m, cmd := keyCmd(t, m, "r")
	if cmd == nil || m.draft == nil || !m.draft.loading || m.status != "Loading message…" || m.editorPath != "" {
		t.Fatalf("cmd %v, draft %v, status %q, editor %q; want a pending fetch", cmd != nil, m.draft, m.status, m.editorPath)
	}
	m = send(t, m, cmd())

	if m.draft == nil || m.draft.loading || m.editorPath == "" {
		t.Fatalf("draft %v, editor %q; want the editor open", m.draft, m.editorPath)
	}
	if got := messageFetches(fake); len(got) != 1 || got[0] != u.latest {
		t.Errorf("fetched %v, want just the latest message %d", got, u.latest)
	}
	latest := fake.Messages("INBOX")[u.latest-1]
	want := "To: Dave <dave@example.com>\n" +
		"Cc: \n" +
		"Bcc: \n" +
		"Subject: Re: Offsite\n" +
		"\n\n\n" +
		"On " + latest.Date.Format("Mon, 2 Jan 2006 at 15:04") + ", Dave <dave@example.com> wrote:\n" +
		"> Body of o3.\n>\n> Second paragraph.\n"
	if m.draft.orig != want {
		t.Errorf("template:\n%q\nwant\n%q", m.draft.orig, want)
	}
	d := m.draft.base
	if d.From != replyMe || d.InReplyTo != "o3" || strings.Join(d.References, " ") != "o1 o2 o3" ||
		d.ReplyTo == nil || *d.ReplyTo != (mail.MessageRef{Folder: "INBOX", UID: u.latest}) {
		t.Errorf("base draft = %+v", d)
	}
	if m.status == "Loading message…" {
		t.Error("status still says loading")
	}
}

func TestReply_ReplyAllIncludesToAndCcMinusMe(t *testing.T) {
	m, _, _, _ := replyFixture(t)

	m = replied(t, m, "R")

	d := m.draft.base
	if got := addrs(d.To); got != "dave@example.com,bob@example.com" {
		t.Errorf("To = %s", got)
	}
	if got := addrs(d.Cc); got != "alice@example.com,carol@example.com" {
		t.Errorf("Cc = %s (me must be gone, case-insensitively)", got)
	}
	for _, want := range []string{"To: Dave <dave@example.com>, Bob <bob@example.com>\n", "Cc: Alice <alice@example.com>, Carol <carol@example.com>\n"} {
		if !strings.Contains(m.draft.orig, want) {
			t.Errorf("template missing %q:\n%s", want, m.draft.orig)
		}
	}
}

func TestReply_ExpandedRowRepliesToTheSelectedMessage(t *testing.T) {
	m, _, fake, u := replyFixture(t)
	m = send(t, m, press("space"), press("j")) // thread row, then its oldest message

	m = replied(t, m, "r")

	if got := messageFetches(fake); len(got) != 1 || got[0] != u.first {
		t.Errorf("fetched %v, want the selected message %d", got, u.first)
	}
	d := m.draft.base
	if got := addrs(d.To); got != "alice@example.com" || d.InReplyTo != "o1" || d.ReplyTo.UID != u.first || d.Subject != "Re: Offsite" {
		t.Errorf("draft = %+v", d)
	}
	if !strings.Contains(m.draft.orig, "> Body of o1.") {
		t.Errorf("template doesn't quote the selected message:\n%s", m.draft.orig)
	}
}

func TestReply_ExpandedThreadRowStillMeansTheLatest(t *testing.T) {
	m, _, _, u := replyFixture(t)
	m = send(t, m, press("space")) // expanded, cursor still on the thread row

	m = replied(t, m, "r")

	if m.draft.base.ReplyTo.UID != u.latest {
		t.Errorf("replied to %d, want the latest %d", m.draft.base.ReplyTo.UID, u.latest)
	}
}

func TestReply_ReaderOpenedOnASelectedMessageRepliesToItWithoutRefetching(t *testing.T) {
	m, _, fake, u := replyFixture(t)
	m = send(t, m, press("space"), press("j")) // select the oldest message on its row
	m = pressSettled(t, m, "enter")
	if !m.readerPopup {
		t.Fatal("reader didn't open")
	}
	before := len(messageFetches(fake))

	m, cmd := keyCmd(t, m, "r")

	if cmd == nil || m.draft == nil || m.draft.loading || m.editorPath == "" {
		t.Fatalf("draft %v, editor %q; want the editor open at once from the reader's copy", m.draft, m.editorPath)
	}
	if got := len(messageFetches(fake)); got != before {
		t.Errorf("fetched %d more times, want the loaded body reused", got-before)
	}
	d := m.draft.base
	if d.ReplyTo.UID != u.first || addrs(d.To) != "alice@example.com" || !strings.Contains(m.draft.orig, "> Body of o1.") {
		t.Errorf("draft = %+v\n%s", d, m.draft.orig)
	}
}

func TestReply_ReaderOnAnUnreadThreadRepliesToTheLatestNotTheOldestUnread(t *testing.T) {
	m, _, fake, u := replyFixture(t)
	m = pressSettled(t, m, "enter") // expands the oldest unread message, o1, and loads it
	if !m.readerPopup || m.read.Current() != u.first {
		t.Fatalf("setup: reader open %v, current %d, want o1 (%d)", m.readerPopup, m.read.Current(), u.first)
	}
	before := len(messageFetches(fake))

	for _, k := range []string{"r", "R"} {
		m2 := replied(t, m, k)

		if got := messageFetches(fake); len(got) <= before || got[len(got)-1] != u.latest {
			t.Errorf("%s fetched %v, want the latest message %d fetched last", k, got, u.latest)
		}
		d := m2.draft.base
		if d.ReplyTo.UID != u.latest || d.InReplyTo != "o3" || !strings.Contains(m2.draft.orig, "> Body of o3.") {
			t.Errorf("%s draft = %+v\n%s", k, d, m2.draft.orig)
		}
	}
}

func TestReply_ReaderBodyOfAnotherThreadIsNotReused(t *testing.T) {
	m, _, fake, _ := replyFixture(t)
	m = pressSettled(t, m, "enter")
	m = send(t, m, press("esc"), press("j")) // back to the list, on the Plan thread
	before := len(messageFetches(fake))

	m = replied(t, m, "r")

	if got := len(messageFetches(fake)); got != before+1 {
		t.Errorf("fetches went %d → %d, want one new fetch", before, got)
	}
	if m.draft.base.InReplyTo != "p2" {
		t.Errorf("InReplyTo = %q, want the Plan thread's latest, p2", m.draft.base.InReplyTo)
	}
}

func TestReply_SidebarFocusDoesNothing(t *testing.T) {
	m, _, fake, _ := replyFixture(t)
	m = send(t, m, press("h"))
	if m.focus != PaneSidebar {
		t.Fatalf("focus = %v, want the sidebar", m.focus)
	}

	for _, k := range []string{"r", "R"} {
		m2, cmd := keyCmd(t, m, k)
		if cmd != nil || m2.draft != nil || len(messageFetches(fake)) != 0 {
			t.Errorf("%s in the sidebar: cmd %v, draft %v", k, cmd != nil, m2.draft)
		}
	}
}

func TestReply_NotConfiguredAccountSaysSo(t *testing.T) {
	m, _, _, _ := replyFixture(t)
	next, open := m.openFolder(1, "INBOX")
	m = settle(t, next, open)
	if m.open.account != 1 {
		t.Fatalf("open account = %d, want Work", m.open.account)
	}

	for _, k := range []string{"r", "R"} {
		m2, cmd := keyCmd(t, m, k)
		want := "Sending isn't set up for Work: add smtp_host to its config"
		if cmd != nil || m2.draft != nil || m2.editorPath != "" || m2.status != want {
			t.Errorf("%s: cmd %v, draft %v, status %q", k, cmd != nil, m2.draft, m2.status)
		}
	}
}

func TestReply_BlockedWhileAnotherDraftIsOpenOrSending(t *testing.T) {
	m, _, fake, _ := replyFixture(t)
	m, _ = keyCmd(t, m, "c") // editor "running"
	if m.draft == nil {
		t.Fatal("setup: no draft")
	}

	m, cmd := keyCmd(t, m, "r")

	if cmd != nil || m.status != "Finish or discard the draft first" || len(messageFetches(fake)) != 0 {
		t.Errorf("r during a draft: cmd %v, status %q", cmd != nil, m.status)
	}
	m.draft, m.sending = nil, true
	m, cmd = keyCmd(t, m, "R")
	if cmd != nil || m.status != "Sending…" {
		t.Errorf("R while sending: cmd %v, status %q", cmd != nil, m.status)
	}
	_ = m.Close()
}

func TestReply_PendingFetchHoldsTheDraftSlot(t *testing.T) {
	m, _, fake, _ := replyFixture(t)
	m, _ = keyCmd(t, m, "r")

	for k, want := range map[string]string{"r": "Finish or discard the draft first", "R": "Finish or discard the draft first", "c": "Finish or discard the draft first", "q": "Finish or discard the draft first"} {
		m2, cmd := keyCmd(t, m, k)
		if cmd != nil || m2.status != want {
			t.Errorf("%s while loading: cmd %v, status %q, want %q", k, cmd != nil, m2.status, want)
		}
	}
	if got := len(messageFetches(fake)); got != 0 {
		t.Errorf("fetched %d times before the command ran", got)
	}
}

func TestReply_FetchErrorClearsTheDraftAndShowsTheError(t *testing.T) {
	m, _, fake, _ := replyFixture(t)
	fake.Fail(mailtest.MethodMessage, errors.New("connection reset"))

	m, cmd := keyCmd(t, m, "r")
	m = send(t, m, cmd())

	if m.draft != nil || m.editorPath != "" || !m.statusErr || m.status != "open message: connection reset" {
		t.Errorf("draft %v, editor %q, status %q (err %v)", m.draft, m.editorPath, m.status, m.statusErr)
	}
	fake.Fail(mailtest.MethodMessage, nil)
	if _, cmd := keyCmd(t, m, "r"); cmd == nil {
		t.Error("r should work again after the error")
	}
}

func TestReply_StaleFetchResultIsIgnored(t *testing.T) {
	m, _, _, _ := replyFixture(t)

	m = send(t, m, replyFetchedMsg{account: 0, folder: "INBOX", msg: mail.Message{Summary: mail.Summary{UID: 1}}})

	if m.draft != nil || m.editorPath != "" {
		t.Errorf("draft %v, editor %q; a result nobody waits for must not open the editor", m.draft, m.editorPath)
	}
}

func TestReply_UntouchedTemplateIsDiscarded(t *testing.T) {
	m, s, _, _ := replyFixture(t)
	m = replied(t, m, "r")

	m = send(t, m, editorDoneMsg{text: m.draft.orig, orig: m.draft.orig})

	if m.draft != nil || m.showConfirm || m.status != "Draft discarded" || len(s.Sent()) != 0 {
		t.Errorf("draft %v, popup %v, status %q", m.draft, m.showConfirm, m.status)
	}
}

func TestReply_ConfirmPopupShowsSubjectAndRecipients(t *testing.T) {
	m, _, _, _ := replyFixture(t)
	m = replied(t, m, "R")
	text := strings.Replace(m.draft.orig, "Subject: Re: Offsite\n\n", "Subject: Re: Offsite\n\nSounds good.\n", 1)

	m = send(t, m, editorDoneMsg{text: text, orig: m.draft.orig})

	view := plain(m)
	for _, want := range []string{"Send message?", "Me Myself <me@example.com>", "Dave <dave@example.com>", "Bob <bob@example.com>", "Alice <alice@example.com>", "Re: Offsite", "Sounds good."} {
		if !strings.Contains(view, want) {
			t.Errorf("popup missing %q:\n%s", want, view)
		}
	}
}

func TestReply_SendCarriesThreadingAndAnsweredTarget(t *testing.T) {
	m, s, _, u := replyFixture(t)
	m = replied(t, m, "r")
	text := strings.Replace(m.draft.orig, "Subject: Re: Offsite\n\n", "Subject: Re: Offsite\n\nSounds good.\n", 1)
	m = send(t, m, editorDoneMsg{text: text, orig: m.draft.orig})

	m = yes(t, m)

	got := s.Sent()
	if len(got) != 1 {
		t.Fatalf("sent %d drafts, want 1", len(got))
	}
	d := got[0]
	if d.From != replyMe || addrs(d.To) != "dave@example.com" || d.Subject != "Re: Offsite" ||
		d.InReplyTo != "o3" || strings.Join(d.References, " ") != "o1 o2 o3" ||
		d.ReplyTo == nil || *d.ReplyTo != (mail.MessageRef{Folder: "INBOX", UID: u.latest}) {
		t.Errorf("draft = %+v", d)
	}
	if !strings.HasPrefix(d.Body, "Sounds good.\n") || !strings.Contains(d.Body, "> Body of o3.\n>\n> Second paragraph.\n") {
		t.Errorf("body = %q", d.Body)
	}
	if m.draft != nil || m.status != "Sent to dave@example.com" {
		t.Errorf("draft %v, status %q", m.draft, m.status)
	}
}

func TestReply_HintsAdvertiseReply(t *testing.T) {
	m, _, _, _ := replyFixture(t)
	m = send(t, m, size(240, 30))
	hints := func() string { return ansi.Strip(m.renderHints()) }
	if h := hints(); !strings.Contains(h, "Reply: r | New message: c") {
		t.Errorf("list hints = %q", h)
	}
	m = pressSettled(t, m, "enter")
	if h := hints(); !strings.Contains(h, "Reply: r | New message: c") {
		t.Errorf("reader hints = %q", h)
	}
}
