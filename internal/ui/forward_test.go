package ui

import (
	"errors"
	"strings"
	"testing"

	"github.com/dkouvdis/dk-email/internal/mail"
)

var forwardAtts = []mail.Attachment{
	{Name: "report.pdf", ContentType: "application/pdf", Size: 1258291},
	{Name: "notes.txt", ContentType: "text/plain", Size: 512},
}

func filledForward(m Model, to string) string {
	return strings.Replace(m.draft.orig, "To: \n", "To: "+to+"\n", 1)
}

func TestForward_CollapsedThreadForwardsTheLatestMessage(t *testing.T) {
	m, _, fake, u := replyFixtureWith(t, forwardAtts)

	m, cmd := keyCmd(t, m, "f")
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
	want := "To: \n" +
		"Cc: \n" +
		"Bcc: \n" +
		"Subject: Fwd: Re: Offsite\n" +
		"\n\n\n" +
		"---------- Forwarded message ----------\n" +
		"From: Dave <dave@example.com>\n" +
		"Date: " + latest.Date.Format("Mon, 2 Jan 2006 at 15:04") + "\n" +
		"Subject: Re: Offsite\n" +
		"To: Me Myself <me@example.com>, Bob <bob@example.com>\n" +
		"Cc: Alice <alice@example.com>, ME@example.com, Carol <carol@example.com>\n" +
		"\n" +
		"Body of o3.\n\nSecond paragraph.\n"
	if m.draft.orig != want {
		t.Errorf("template:\n%q\nwant\n%q", m.draft.orig, want)
	}
	d := m.draft.base
	if d.From != replyMe || len(d.To) != 0 || d.InReplyTo != "" || len(d.References) != 0 || d.ReplyTo != nil ||
		d.ForwardOf == nil || *d.ForwardOf != (mail.MessageRef{Folder: "INBOX", UID: u.latest}) {
		t.Errorf("base draft = %+v", d)
	}
	if len(m.draft.attachments) != 2 || m.draft.attachments[0].Name != "report.pdf" {
		t.Errorf("attachments = %+v", m.draft.attachments)
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
	if d := m.draft.base; d.ForwardOf.UID != u.first || d.Subject != "Fwd: Offsite" || !strings.Contains(m.draft.orig, "Body of o1.") {
		t.Errorf("draft = %+v\n%s", d, m.draft.orig)
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

	if cmd == nil || m.draft == nil || m.draft.loading || m.editorPath == "" {
		t.Fatalf("draft %v, editor %q; want the editor open at once from the reader's copy", m.draft, m.editorPath)
	}
	if got := len(messageFetches(fake)); got != before {
		t.Errorf("fetched %d more times, want the loaded body reused", got-before)
	}
	if m.draft.base.ForwardOf.UID != u.latest || len(m.draft.attachments) != 2 {
		t.Errorf("draft = %+v, attachments %+v", m.draft.base, m.draft.attachments)
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
	if cmd != nil || m2.draft != nil || m2.editorPath != "" || m2.status != want {
		t.Errorf("cmd %v, draft %v, status %q", cmd != nil, m2.draft, m2.status)
	}
}

func TestForward_BlockedWhileAnotherDraftIsOpenOrSending(t *testing.T) {
	m, _, fake, _ := replyFixture(t)
	m, _ = keyCmd(t, m, "c")

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

func TestForward_UntouchedTemplateIsDiscarded(t *testing.T) {
	m, s, _, _ := replyFixtureWith(t, forwardAtts)
	m = replied(t, m, "f")

	m = send(t, m, editorDoneMsg{text: m.draft.orig, orig: m.draft.orig})

	if m.draft != nil || m.showConfirm || m.status != "Draft discarded" || len(s.Sent()) != 0 {
		t.Errorf("draft %v, popup %v, status %q", m.draft, m.showConfirm, m.status)
	}
}

func TestForward_ConfirmPopupListsAttachmentsWithSizes(t *testing.T) {
	m, _, _, _ := replyFixtureWith(t, forwardAtts)
	m = replied(t, m, "f")

	m = send(t, m, editorDoneMsg{text: filledForward(m, "Zed <zed@example.com>"), orig: m.draft.orig})

	view := plain(m)
	for _, want := range []string{"Send message?", "Zed <zed@example.com>", "Fwd: Re: Offsite", "Attachments", "report.pdf (1.2 MB)", "notes.txt (512 B)", "y send"} {
		if !strings.Contains(view, want) {
			t.Errorf("popup missing %q:\n%s", want, view)
		}
	}
}

func TestForward_ConfirmPopupWithoutAttachmentsHasNoSection(t *testing.T) {
	m, _, _, _ := replyFixture(t)
	m = replied(t, m, "f")

	m = send(t, m, editorDoneMsg{text: filledForward(m, "zed@example.com"), orig: m.draft.orig})

	if view := plain(m); !strings.Contains(view, "Send message?") || strings.Contains(view, "Attachments") {
		t.Errorf("popup:\n%s", view)
	}
}

func TestForward_AttachmentsSurviveReeditAndSendFailure(t *testing.T) {
	m, s, _, _ := replyFixtureWith(t, forwardAtts)
	m = replied(t, m, "f")
	m = send(t, m, editorDoneMsg{text: filledForward(m, "zed@example.com"), orig: m.draft.orig})
	m, _ = keyCmd(t, m, "e")
	m = send(t, m, editorDoneMsg{text: filledForward(m, "zed@example.com") + "More.\n", orig: m.draft.orig})
	if view := plain(m); !strings.Contains(view, "report.pdf (1.2 MB)") {
		t.Errorf("popup after re-edit:\n%s", view)
	}
	s.FailWith(errors.New("smtp: connection refused"))

	m = yes(t, m)

	if view := plain(m); !strings.Contains(view, "Send failed") || !strings.Contains(view, "notes.txt (512 B)") {
		t.Errorf("popup after a failed send:\n%s", view)
	}
}

func TestForward_SendCarriesForwardOfAndNoThreading(t *testing.T) {
	m, s, _, u := replyFixtureWith(t, forwardAtts)
	m = replied(t, m, "f")
	m = send(t, m, editorDoneMsg{text: filledForward(m, "zed@example.com"), orig: m.draft.orig})

	m = yes(t, m)

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
