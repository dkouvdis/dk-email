package ui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/dkouvdis/dk-email/internal/config"
	"github.com/dkouvdis/dk-email/internal/mail"
	"github.com/dkouvdis/dk-email/internal/mail/mailtest"
	"github.com/dkouvdis/dk-email/internal/ui/popup"
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
	return replyFixtureWith(t, nil)
}

// replyFixtureWith is replyFixture where the latest message has atts.
func replyFixtureWith(t *testing.T, atts []mail.Attachment) (Model, *mailtest.Sender, *mailtest.Fake, replyUIDs) {
	t.Helper()
	s := &mailtest.Sender{}
	f := newFixture()
	f.personalSender = s
	add := func(from mail.Address, to, cc []mail.Address, subject string, ago time.Duration, seen bool, id string, refs ...string) mail.UID {
		m := msg("", subject, fixedNow.Add(-ago), seen, id, refs...)
		m.From, m.To, m.Cc = from, to, cc
		m.Body = "Body of " + id + ".\n\nSecond paragraph."
		if id == "o3" {
			m.Attachments = atts
		}
		return f.personal.AddMessage("INBOX", m)
	}
	var u replyUIDs
	u.first = add(replyAlice, []mail.Address{replyMe, replyBob}, []mail.Address{replyCarol}, "Offsite", 3*time.Hour, false, "o1")
	u.second = add(replyBob, []mail.Address{replyMe}, nil, "Re: Offsite", 2*time.Hour, true, "o2", "o1")
	u.latest = add(replyDave, []mail.Address{replyMe, replyBob}, []mail.Address{replyAlice, {Email: "ME@example.com"}, replyCarol}, "Re: Offsite", time.Hour, true, "o3", "o1", "o2")

	m := started(t, f, 160, 30)
	m.accts[0].Email, m.accts[0].FromName = replyMe.Email, replyMe.Name
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
	if !m.composing() {
		t.Fatalf("%s didn't open the composer (draft %v, status %q)", k, m.draft, m.status)
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
	if cmd == nil || m.draft == nil || !m.draft.loading || m.status != "Loading message…" || m.composing() {
		t.Fatalf("cmd %v, draft %v, status %q; want a pending fetch", cmd != nil, m.draft, m.status)
	}
	if v := plain(m); strings.Contains(v, "Reply · ") {
		t.Errorf("composer opened before the fetch finished:\n%s", v)
	}
	m = send(t, m, cmd())

	if !m.composing() {
		t.Fatalf("draft %v; want the composer open", m.draft)
	}
	if got := messageFetches(fake); len(got) != 1 || got[0] != u.latest {
		t.Errorf("fetched %v, want just the latest message %d", got, u.latest)
	}
	latest := fake.Messages("INBOX")[u.latest-1]
	want := popup.ComposeValues{
		To:      "Dave <dave@example.com>",
		Subject: "Re: Offsite",
		Body:    "\n\nOn " + latest.Date.Format("Mon, 2 Jan 2006 at 15:04") + ", Dave <dave@example.com> wrote:\n> Body of o3.\n>\n> Second paragraph.\n",
	}
	if got := m.draft.pop.Values(); got != want {
		t.Errorf("values:\n%+v\nwant\n%+v", got, want)
	}
	if view := plain(m); !strings.Contains(view, "Reply · Re: Offsite · me@example.com") {
		t.Errorf("composer title missing:\n%s", view)
	}
	// A reply starts in the body, above the quote.
	typedAtTop := typed(t, m, "Hi")
	if got := typedAtTop.draft.pop.Values().Body; !strings.HasPrefix(got, "Hi\n\nOn ") {
		t.Errorf("typing landed in %q; want the top of the body", got)
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
	v := m.draft.pop.Values()
	if v.To != "Dave <dave@example.com>, Bob <bob@example.com>" || v.Cc != "Alice <alice@example.com>, Carol <carol@example.com>" {
		t.Errorf("To %q, Cc %q", v.To, v.Cc)
	}
	if view := plain(m); !strings.Contains(view, "Reply all · Re: Offsite · me@example.com") {
		t.Errorf("composer title missing:\n%s", view)
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
	if !strings.Contains(m.draft.pop.Values().Body, "> Body of o1.") {
		t.Errorf("body doesn't quote the selected message:\n%s", m.draft.pop.Values().Body)
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

	if cmd != nil || !m.composing() {
		t.Fatalf("draft %v; want the composer open at once from the reader's copy", m.draft)
	}
	if got := len(messageFetches(fake)); got != before {
		t.Errorf("fetched %d more times, want the loaded body reused", got-before)
	}
	d := m.draft.base
	if d.ReplyTo.UID != u.first || addrs(d.To) != "alice@example.com" || !strings.Contains(m.draft.pop.Values().Body, "> Body of o1.") {
		t.Errorf("draft = %+v\n%s", d, m.draft.pop.Values().Body)
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
		if d.ReplyTo.UID != u.latest || d.InReplyTo != "o3" || !strings.Contains(m2.draft.pop.Values().Body, "> Body of o3.") {
			t.Errorf("%s draft = %+v\n%s", k, d, m2.draft.pop.Values().Body)
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
		if cmd != nil || m2.draft != nil || m2.status != want {
			t.Errorf("%s: cmd %v, draft %v, status %q", k, cmd != nil, m2.draft, m2.status)
		}
	}
}

func TestReply_BlockedWhileAnotherDraftIsOpenOrSending(t *testing.T) {
	m, _, fake, _ := replyFixture(t)
	m, _ = keyCmd(t, m, "r") // fetch pending
	if m.draft == nil {
		t.Fatal("setup: no draft")
	}

	m, cmd := keyCmd(t, m, "R")

	if cmd != nil || m.status != "Finish or discard the draft first" || len(messageFetches(fake)) != 0 {
		t.Errorf("R during a draft: cmd %v, status %q", cmd != nil, m.status)
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

	for k, want := range map[string]string{"r": "Finish or discard the draft first", "R": "Finish or discard the draft first", "c": "Finish or discard the draft first"} {
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

	if m.draft != nil || !m.statusErr || m.status != "open message: connection reset" {
		t.Errorf("draft %v, status %q (err %v)", m.draft, m.status, m.statusErr)
	}
	fake.Fail(mailtest.MethodMessage, nil)
	if _, cmd := keyCmd(t, m, "r"); cmd == nil {
		t.Error("r should work again after the error")
	}
}

func TestReply_StaleFetchResultIsIgnored(t *testing.T) {
	m, _, _, _ := replyFixture(t)

	m = send(t, m, replyFetchedMsg{account: 0, folder: "INBOX", msg: mail.Message{Summary: mail.Summary{UID: 1}}})

	if m.draft != nil {
		t.Errorf("draft %v; a result nobody waits for must not open the composer", m.draft)
	}
}

func TestReply_UntouchedComposerClosesAtOnce(t *testing.T) {
	m, s, _, _ := replyFixture(t)
	m = replied(t, m, "r")

	m = send(t, m, press("esc"))

	if m.composing() || m.status != "" || len(s.Sent()) != 0 {
		t.Errorf("composing %v, status %q", m.composing(), m.status)
	}
}

func TestReply_SendCarriesThreadingAndAnsweredTarget(t *testing.T) {
	m, s, _, u := replyFixture(t)
	m = replied(t, m, "r")
	m = typed(t, m, "Sounds good.")

	m = sendKey(t, m)

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
	if h := hints(); !strings.Contains(h, "Reply: r | Forward: f | New message: c") {
		t.Errorf("list hints = %q", h)
	}
	m = pressSettled(t, m, "enter")
	if h := hints(); !strings.Contains(h, "Reply: r | Forward: f | Open attachment: o | New message: c") {
		t.Errorf("reader hints = %q", h)
	}
}

// aliasFixture is an inbox with one message from Alice, addressed To the
// account's alias and Cc the account address, on an account that has the alias.
func aliasFixture(t *testing.T) Model {
	t.Helper()
	f := newFixture()
	f.personalSender = &mailtest.Sender{}
	m := msg("", "Invoice", fixedNow.Add(-time.Hour), true, "i1")
	m.From = replyAlice
	m.To = []mail.Address{{Email: "Sales@example.com"}, replyMe}
	m.Body = "Body of i1."
	f.personal.AddMessage("INBOX", m)
	model := started(t, f, 160, 30)
	model.accts[0].Email, model.accts[0].FromName = replyMe.Email, replyMe.Name
	model.accts[0].Aliases = []config.Alias{{Email: "sales@example.com", Name: "Sales Team"}}
	return model
}

func TestReply_FromTheAliasTheMessageWasSentTo(t *testing.T) {
	m := replied(t, aliasFixture(t), "r")

	if want := (mail.Address{Name: "Sales Team", Email: "sales@example.com"}); m.draft.base.From != want {
		t.Errorf("From = %v, want %v", m.draft.base.From, want)
	}
	if view := plain(m); !strings.Contains(view, "Reply · Re: Invoice · sales@example.com") || strings.Contains(view, "me@example.com") {
		t.Errorf("composer title should show the alias, not the account:\n%s", view)
	}
}

func TestReply_ReplyAllListsNeitherTheAccountNorItsAlias(t *testing.T) {
	m := replied(t, aliasFixture(t), "R")

	d := m.draft.base
	if got := addrs(d.To); got != "alice@example.com" || len(d.Cc) != 0 {
		t.Errorf("To = %s, Cc = %s; want just Alice", got, addrs(d.Cc))
	}
	if v := m.draft.pop.Values(); v.To != "Alice <alice@example.com>" || v.Cc != "" {
		t.Errorf("To %q, Cc %q", v.To, v.Cc)
	}
}
