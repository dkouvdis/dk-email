package ui

import (
	"strings"
	"testing"

	"github.com/dkouvdis/dk-email/internal/mail"
	"github.com/dkouvdis/dk-email/internal/ui/popup"
)

var forwardAtts = []mail.Attachment{
	{Name: "report.pdf", ContentType: "application/pdf", Size: 1258291},
	{Name: "notes.txt", ContentType: "text/plain", Size: 512},
}

func TestForward_CollapsedThreadForwardsTheLatestMessage(t *testing.T) {
	m, _, fake, u := replyFixtureWith(t, forwardAtts)

	m, cmd := keyCmd(t, m, "f")
	if cmd == nil || m.draft == nil || !m.draft.loading || m.status != "Loading message…" || m.composing() {
		t.Fatalf("cmd %v, draft %v, status %q; want a pending fetch", cmd != nil, m.draft, m.status)
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
		Subject: "Fwd: Re: Offsite",
		Body: "\n\n---------- Forwarded message ----------\n" +
			"From: Dave <dave@example.com>\n" +
			"Date: " + latest.Date.Format("Mon, 2 Jan 2006 at 15:04") + "\n" +
			"Subject: Re: Offsite\n" +
			"To: Me Myself <me@example.com>, Bob <bob@example.com>\n" +
			"Cc: Alice <alice@example.com>, ME@example.com, Carol <carol@example.com>\n" +
			"\n" +
			"Body of o3.\n\nSecond paragraph.\n",
	}
	if got := m.draft.pop.Values(); got != want {
		t.Errorf("values:\n%+v\nwant\n%+v", got, want)
	}
	if view := plain(m); !strings.Contains(view, "Forward · Fwd: Re: Offsite · me@example.com") {
		t.Errorf("composer title missing:\n%s", view)
	}
	// A forward starts in To.
	if got := typed(t, m, "zed@example.com").draft.pop.Values().To; got != "zed@example.com" {
		t.Errorf("typing landed in To = %q", got)
	}
	d := m.draft.base
	if d.From != replyMe || len(d.To) != 0 || d.InReplyTo != "" || len(d.References) != 0 || d.ReplyTo != nil ||
		d.ForwardOf == nil || *d.ForwardOf != (mail.MessageRef{Folder: "INBOX", UID: u.latest}) {
		t.Errorf("base draft = %+v", d)
	}
	if m.status == "Loading message…" {
		t.Error("status still says loading")
	}
}

func TestForward_ExpandedRowForwardsTheSelectedMessage(t *testing.T) {
	m, _, fake, u := replyFixture(t)
	m = send(t, m, press("space"), press("j"))

	m = replied(t, m, "f")

	if got := messageFetches(fake); len(got) != 1 || got[0] != u.first {
		t.Errorf("fetched %v, want the selected message %d", got, u.first)
	}
	if d := m.draft.base; d.ForwardOf.UID != u.first || d.Subject != "Fwd: Offsite" || !strings.Contains(m.draft.pop.Values().Body, "Body of o1.") {
		t.Errorf("draft = %+v\n%s", d, m.draft.pop.Values().Body)
	}
}

func TestForward_ReaderUsesItsLoadedMessageWithoutRefetching(t *testing.T) {
	m, _, fake, u := replyFixtureWith(t, forwardAtts)
	m = pressSettled(t, m, "enter") // the oldest unread message
	if !m.readerPopup {
		t.Fatal("reader didn't open")
	}
	m = send(t, m, press("esc"), press("space"), press("j"), press("j"), press("j")) // select the latest on its row
	m = pressSettled(t, m, "enter")
	if !m.readerPopup || m.read.Current() != u.latest {
		t.Fatalf("setup: reader open %v on %d, want the latest %d", m.readerPopup, m.read.Current(), u.latest)
	}
	before := len(messageFetches(fake))

	m, cmd := keyCmd(t, m, "f")

	if cmd != nil || !m.composing() {
		t.Fatalf("draft %v; want the composer open at once from the reader's copy", m.draft)
	}
	if got := len(messageFetches(fake)); got != before {
		t.Errorf("fetched %d more times, want the loaded body reused", got-before)
	}
	if m.draft.base.ForwardOf.UID != u.latest {
		t.Errorf("draft = %+v", m.draft.base)
	}
}

func TestForward_ReaderOnAnUnreadThreadForwardsTheLatest(t *testing.T) {
	m, _, fake, u := replyFixture(t)
	m = pressSettled(t, m, "enter") // opens on the oldest unread message
	before := len(messageFetches(fake))

	m = replied(t, m, "f")

	if got := messageFetches(fake); len(got) <= before || got[len(got)-1] != u.latest {
		t.Errorf("fetched %v, want the latest message %d fetched last", got, u.latest)
	}
	if m.draft.base.ForwardOf.UID != u.latest {
		t.Errorf("forwarding %d, want the latest %d", m.draft.base.ForwardOf.UID, u.latest)
	}
}

func TestForward_SidebarFocusDoesNothing(t *testing.T) {
	m, _, fake, _ := replyFixture(t)
	m = send(t, m, press("h"))

	m2, cmd := keyCmd(t, m, "f")

	if cmd != nil || m2.draft != nil || len(messageFetches(fake)) != 0 {
		t.Errorf("f in the sidebar: cmd %v, draft %v", cmd != nil, m2.draft)
	}
}

func TestForward_NotConfiguredAccountSaysSo(t *testing.T) {
	m, _, _, _ := replyFixture(t)
	next, open := m.openFolder(1, "INBOX")
	m = settle(t, next, open)

	m2, cmd := keyCmd(t, m, "f")

	want := "Sending isn't set up for Work: add smtp_host to its config"
	if cmd != nil || m2.draft != nil || m2.status != want {
		t.Errorf("cmd %v, draft %v, status %q", cmd != nil, m2.draft, m2.status)
	}
}

func TestForward_BlockedWhileAnotherDraftIsOpenOrSending(t *testing.T) {
	m, _, fake, _ := replyFixture(t)
	m, _ = keyCmd(t, m, "r") // fetch pending

	m, cmd := keyCmd(t, m, "f")

	if cmd != nil || m.status != "Finish or discard the draft first" || len(messageFetches(fake)) != 0 {
		t.Errorf("f during a draft: cmd %v, status %q", cmd != nil, m.status)
	}
	m.draft, m.sending = nil, true
	m, cmd = keyCmd(t, m, "f")
	if cmd != nil || m.status != "Sending…" {
		t.Errorf("f while sending: cmd %v, status %q", cmd != nil, m.status)
	}
	_ = m.Close()
}

func TestForward_UntouchedComposerClosesAtOnce(t *testing.T) {
	m, s, _, _ := replyFixtureWith(t, forwardAtts)
	m = replied(t, m, "f")

	m = send(t, m, press("esc"))

	if m.composing() || m.status != "" || len(s.Sent()) != 0 {
		t.Errorf("composing %v, status %q", m.composing(), m.status)
	}
}

func TestForward_NeedsARecipient(t *testing.T) {
	m, s, _, _ := replyFixtureWith(t, forwardAtts)
	m = replied(t, m, "f")

	m, cmd := keyCmd(t, m, "ctrl+s")

	if cmd != nil || len(s.Sent()) != 0 || !strings.Contains(plain(m), "no recipients") {
		t.Errorf("cmd %v, sent %d, view:\n%s", cmd != nil, len(s.Sent()), plain(m))
	}
}

func TestForward_SendCarriesForwardOfAndNoThreading(t *testing.T) {
	m, s, _, u := replyFixtureWith(t, forwardAtts)
	m = replied(t, m, "f")
	m = typed(t, m, "zed@example.com")

	m = sendKey(t, m)

	got := s.Sent()
	if len(got) != 1 {
		t.Fatalf("sent %d drafts, want 1", len(got))
	}
	d := got[0]
	if d.From != replyMe || addrs(d.To) != "zed@example.com" || d.Subject != "Fwd: Re: Offsite" {
		t.Errorf("draft = %+v", d)
	}
	if d.ForwardOf == nil || *d.ForwardOf != (mail.MessageRef{Folder: "INBOX", UID: u.latest}) {
		t.Errorf("ForwardOf = %v, want INBOX/%d", d.ForwardOf, u.latest)
	}
	if d.InReplyTo != "" || len(d.References) != 0 || d.ReplyTo != nil {
		t.Errorf("threading fields set: %+v", d)
	}
	if !strings.Contains(d.Body, "---------- Forwarded message ----------") || !strings.Contains(d.Body, "Body of o3.") {
		t.Errorf("body = %q", d.Body)
	}
	if m.draft != nil || m.status != "Sent to zed@example.com" {
		t.Errorf("draft %v, status %q", m.draft, m.status)
	}
}
