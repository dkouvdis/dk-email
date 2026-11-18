package ui

import (
	"bytes"
	"errors"
	"mime"
	"strings"
	"testing"

	"github.com/dkouvdis/dk-email/internal/mail"
	"github.com/dkouvdis/dk-email/internal/mail/mailtest"
	"github.com/dkouvdis/dk-email/internal/render"
	"github.com/dkouvdis/dk-email/internal/ui/popup"
)

var forwardAtts = []mail.File{
	{Name: "report.pdf", ContentType: "application/pdf", Data: bytes.Repeat([]byte("%PDF-1.7 \x00\xff"), 40)},
	{Name: "notes.txt", ContentType: "text/plain", Data: []byte("remember the milk\n")},
}

func rawFetchUIDs(fake *mailtest.Fake) (uids []mail.UID) {
	for _, c := range fake.Calls() {
		if c.Method == mailtest.MethodRaw {
			uids = append(uids, c.UIDs...)
		}
	}
	return uids
}

// checkCarried fails unless got holds exactly the files of want, by name,
// media type and bytes.
func checkCarried(t *testing.T, got, want []mail.File) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("carries %d attachments, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		gt, _, _ := mime.ParseMediaType(got[i].ContentType)
		wt, _, _ := mime.ParseMediaType(want[i].ContentType)
		if got[i].Name != want[i].Name || gt != wt || !bytes.Equal(got[i].Data, want[i].Data) {
			t.Errorf("attachment %d = %q %q (%d bytes), want %q %q (%d bytes)",
				i, got[i].Name, got[i].ContentType, len(got[i].Data), want[i].Name, want[i].ContentType, len(want[i].Data))
		}
	}
}

func TestForward_CollapsedThreadForwardsTheLatestMessage(t *testing.T) {
	m, _, fake, u := forwardFixture(t, forwardAtts)

	m, cmd := keyCmd(t, m, "f")
	if cmd == nil || m.draft == nil || !m.draft.loading || m.status != "Loading message…" || m.composing() {
		t.Fatalf("cmd %v, draft %v, status %q; want a pending fetch", cmd != nil, m.draft, m.status)
	}
	m = send(t, m, cmd())

	if !m.composing() {
		t.Fatalf("draft %v; want the composer open", m.draft)
	}
	if got := rawFetchUIDs(fake); len(got) != 1 || got[0] != u.latest {
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
	if d.From != replyMe || len(d.To) != 0 || d.InReplyTo != "" || len(d.References) != 0 || d.ReplyTo != nil {
		t.Errorf("base draft = %+v", d)
	}
	checkCarried(t, d.Attachments, forwardAtts)
	if m.status == "Loading message…" {
		t.Error("status still says loading")
	}
}

func TestForward_ExpandedRowForwardsTheSelectedMessage(t *testing.T) {
	m, _, fake, u := forwardFixture(t, nil)
	m = send(t, m, press("space"), press("j"))

	m = replied(t, m, "f")

	if got := rawFetchUIDs(fake); len(got) != 1 || got[0] != u.first {
		t.Errorf("fetched %v, want the selected message %d", got, u.first)
	}
	if d := m.draft.base; len(d.Attachments) != 0 || d.Subject != "Fwd: Offsite" || !strings.Contains(m.draft.pop.Values().Body, "Body of o1.") {
		t.Errorf("draft = %+v\n%s", d, m.draft.pop.Values().Body)
	}
}

func TestForward_ReaderStillFetchesRawBeforeOpening(t *testing.T) {
	m, _, fake, u := forwardFixture(t, forwardAtts)
	m = pressSettled(t, m, "enter") // the oldest unread message
	if !m.readerPopup {
		t.Fatal("reader didn't open")
	}
	m = send(t, m, press("esc"), press("space"), press("j"), press("j"), press("j")) // select the latest on its row
	m = pressSettled(t, m, "enter")
	if !m.readerPopup || m.read.Current() != u.latest {
		t.Fatalf("setup: reader open %v on %d, want the latest %d", m.readerPopup, m.read.Current(), u.latest)
	}
	before := len(rawFetchUIDs(fake))

	m, cmd := keyCmd(t, m, "f")

	if cmd == nil || m.composing() || m.draft == nil || !m.draft.loading || m.status != "Loading message…" {
		t.Fatalf("cmd %v, draft %v, status %q; want a pending fetch, not the composer", cmd != nil, m.draft, m.status)
	}
	m = send(t, m, cmd())

	if got := rawFetchUIDs(fake); len(got) != before+1 || got[len(got)-1] != u.latest {
		t.Errorf("raw fetches %v, want one more, of %d", got, u.latest)
	}
	if !m.composing() {
		t.Fatalf("draft = %+v; want the composer open after the fetch", m.draft)
	}
	checkCarried(t, m.draft.base.Attachments, forwardAtts)
}

func TestForward_ReaderOnAnUnreadThreadForwardsTheLatest(t *testing.T) {
	m, _, fake, u := forwardFixture(t, nil)
	m = pressSettled(t, m, "enter") // opens on the oldest unread message
	before := len(rawFetchUIDs(fake))

	m = replied(t, m, "f")

	if got := rawFetchUIDs(fake); len(got) <= before || got[len(got)-1] != u.latest {
		t.Errorf("fetched %v, want the latest message %d fetched last", got, u.latest)
	}
	if !strings.Contains(m.draft.pop.Values().Body, "Body of o3.") {
		t.Errorf("draft = %+v\n%s", m.draft.base, m.draft.pop.Values().Body)
	}
}

func TestForward_SidebarFocusDoesNothing(t *testing.T) {
	m, _, fake, _ := forwardFixture(t, nil)
	m = send(t, m, press("h"))

	m2, cmd := keyCmd(t, m, "f")

	if cmd != nil || m2.draft != nil || len(messageFetches(fake)) != 0 || len(rawFetchUIDs(fake)) != 0 {
		t.Errorf("f in the sidebar: cmd %v, draft %v", cmd != nil, m2.draft)
	}
}

func TestForward_NotConfiguredAccountSaysSo(t *testing.T) {
	m, _, _, _ := forwardFixture(t, nil)
	next, open := m.openFolder(1, "INBOX")
	m = settle(t, next, open)

	m2, cmd := keyCmd(t, m, "f")

	want := "Sending isn't set up for Work: add smtp_host to its config"
	if cmd != nil || m2.draft != nil || m2.status != want {
		t.Errorf("cmd %v, draft %v, status %q", cmd != nil, m2.draft, m2.status)
	}
}

func TestForward_BlockedWhileAnotherDraftIsOpenOrSending(t *testing.T) {
	m, _, fake, _ := forwardFixture(t, nil)
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
	m, s, _, _ := forwardFixture(t, forwardAtts)
	m = replied(t, m, "f")

	m = send(t, m, press("esc"))

	if m.composing() || m.status != "" || len(s.Sent()) != 0 {
		t.Errorf("composing %v, status %q", m.composing(), m.status)
	}
}

func TestForward_NeedsARecipient(t *testing.T) {
	m, s, _, _ := forwardFixture(t, forwardAtts)
	m = replied(t, m, "f")

	m, cmd := keyCmd(t, m, "ctrl+s")

	if cmd != nil || len(s.Sent()) != 0 || !strings.Contains(plain(m), "no recipients") {
		t.Errorf("cmd %v, sent %d, view:\n%s", cmd != nil, len(s.Sent()), plain(m))
	}
}

func TestForward_SendCarriesAttachmentsAndNoThreading(t *testing.T) {
	m, s, _, _ := forwardFixture(t, forwardAtts)
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
	checkCarried(t, d.Attachments, forwardAtts)
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

func TestForward_SendsExactlyTheOriginalsAttachments(t *testing.T) {
	three := []mail.File{
		{Name: "a.pdf", ContentType: "application/pdf", Data: []byte("%PDF a \x00\x01\xfe")},
		{Name: "b.png", ContentType: "image/png", Data: bytes.Repeat([]byte{0x89, 'P', 'N', 'G', 0, 1, 2}, 50)},
		{Name: "c.txt", ContentType: "text/plain", Data: []byte("plain, as Parts defines it\n")},
	}
	for n, files := range map[int][]mail.File{0: nil, 1: three[:1], 3: three} {
		m, s, fake, u := forwardFixture(t, files)
		raw, err := fake.Raw(t.Context(), "INBOX", u.latest)
		if err != nil {
			t.Fatal(err)
		}
		parts, err := render.Parts(raw)
		if err != nil || len(parts) != n {
			t.Fatalf("n=%d: Parts = %d parts, err %v", n, len(parts), err)
		}
		m = replied(t, m, "f")
		m = typed(t, m, "zed@example.com")

		m = sendKey(t, m)

		got := s.Sent()
		if len(got) != 1 {
			t.Fatalf("n=%d: sent %d drafts, want 1", n, len(got))
		}
		if len(got[0].Attachments) != n {
			t.Fatalf("n=%d: sent %d attachments", n, len(got[0].Attachments))
		}
		checkCarried(t, got[0].Attachments, files)
		for i, p := range parts {
			a := got[0].Attachments[i]
			if a.Name != p.Filename || a.ContentType != p.ContentType || !bytes.Equal(a.Data, p.Data) {
				t.Errorf("n=%d: attachment %d = %+v, want Parts' %q %q", n, i, a.Name, p.Filename, p.ContentType)
			}
		}
	}
}

func TestForward_FailedFetchNeverOpensTheComposer(t *testing.T) {
	m, _, fake, _ := forwardFixture(t, forwardAtts)
	fake.Fail(mailtest.MethodRaw, errors.New("boom"))

	m, cmd := keyCmd(t, m, "f")
	if cmd == nil || m.draft == nil || !m.draft.loading {
		t.Fatalf("cmd %v, draft %v; want a pending fetch", cmd != nil, m.draft)
	}
	m = send(t, m, cmd())

	if m.composing() || m.draft != nil || !strings.Contains(m.status, "boom") || !m.statusErr {
		t.Fatalf("composing %v, draft %v, status %q (error %v); want no composer, a freed slot and the error", m.composing(), m.draft, m.status, m.statusErr)
	}

	fake.Fail(mailtest.MethodRaw, nil)
	m = replied(t, m, "f")
	checkCarried(t, m.draft.base.Attachments, forwardAtts)
}
