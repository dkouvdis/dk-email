package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/dkouvdis/dk-email/internal/mail"
	"github.com/dkouvdis/dk-email/internal/mail/mailtest"
)

// draftsUIDs are the messages of draftsFixture's Drafts folder: a two-message
// thread, "Plan B", whose newer message is latest, and a lone draft.
type draftsUIDs struct{ first, latest, solo mail.UID }

// draftsFixture is Personal with a Drafts folder open on the list, backed by
// sender s. Every message in it has a stored draft registered with s when it
// is a DraftSender.
func draftsFixture(t *testing.T, s mail.Sender) (Model, *fixture, draftsUIDs) {
	t.Helper()
	f := newFixture()
	f.personal.AddFolder("Drafts", mail.RoleDrafts)
	add := func(subject string, ago time.Duration, id string, refs ...string) mail.UID {
		return f.personal.AddMessage("Drafts", msg("Me Myself", subject, fixedNow.Add(-ago), true, id, refs...))
	}
	u := draftsUIDs{
		solo:   add("Solo", 72*time.Hour, "d0"),
		first:  add("Plan B", 3*time.Hour, "d1"),
		latest: add("Re: Plan B", time.Hour, "d2", "d1"),
	}
	if ds, ok := s.(*mailtest.DraftSender); ok {
		alice := mail.Address{Name: "Alice", Email: "alice@example.com"}
		ref := func(uid mail.UID) mail.MessageRef { return mail.MessageRef{Folder: "Drafts", UID: uid} }
		ds.AddDraft(ref(u.solo), mail.Draft{Subject: "Solo", Body: "Just notes."})
		ds.AddDraft(ref(u.first), mail.Draft{To: []mail.Address{alice}, Subject: "Plan B", Body: "First text."})
		ds.AddDraft(ref(u.latest), mail.Draft{
			From: mail.Address{Name: "Me Myself", Email: "me@example.com"},
			To:   []mail.Address{alice}, Bcc: []mail.Address{{Email: "boss@example.com"}},
			Subject: "Re: Plan B", Body: "Latest text.", InReplyTo: "<d1@x>", References: []string{"<d1@x>"},
			ReplyTo: &mail.MessageRef{Folder: "INBOX", UID: 1},
		})
	}
	f.personalSender = s
	m := started(t, f, 160, 30)
	m.accts[0].Email, m.accts[0].FromName = "me@example.com", "Me Myself"
	next, open := m.openFolder(0, "Drafts")
	m = settle(t, next, open)
	m.focus = PaneList
	return m, f, u
}

// openedDraft presses enter and delivers the fetch result.
func openedDraft(t *testing.T, m Model) Model {
	t.Helper()
	m, cmd := keyCmd(t, m, "enter")
	if cmd == nil || m.draft == nil || !m.draft.loading || m.status != "Opening draft…" || m.readerPopup {
		t.Fatalf("cmd %v, draft %v, status %q, reader %v; want a pending fetch", cmd != nil, m.draft, m.status, m.readerPopup)
	}
	return send(t, m, cmd())
}

func TestDrafts_EnterOpensTheNewestMessageInTheComposer(t *testing.T) {
	m, _, u := draftsFixture(t, &mailtest.DraftSender{})
	list := m.list

	m = openedDraft(t, m)

	if !m.composing() || m.readerPopup || m.status != "" {
		t.Fatalf("composing %v, reader %v, status %q", m.composing(), m.readerPopup, m.status)
	}
	want := mail.MessageRef{Folder: "Drafts", UID: u.latest}
	d := m.draft.base
	if d.DraftOf == nil || *d.DraftOf != want || d.ReplyTo != nil || d.InReplyTo != "<d1@x>" || len(d.References) != 1 {
		t.Errorf("base = %+v", d)
	}
	v := m.draft.pop.Values()
	if v.To != "Alice <alice@example.com>" || v.Bcc != "boss@example.com" || v.Subject != "Re: Plan B" || v.Body != "Latest text." {
		t.Errorf("values = %+v", v)
	}
	view := plain(m)
	if !strings.Contains(view, "Draft · Re: Plan B · me@example.com") {
		t.Errorf("view missing the title:\n%s", view)
	}
	m = typed(t, m, "X")
	if got := m.draft.pop.Values(); got.To != v.To || got.Body != "XLatest text." {
		t.Errorf("typing should land at the top of the body, got %+v", got)
	}

	m, _ = keyCmd(t, m, "esc")
	m, _ = keyCmd(t, m, "d")
	if m.list.Count() != list.Count() || m.focus != PaneList || m.readerPopup {
		t.Error("the list changed while the draft was open")
	}
}

func TestDrafts_ClickOpensADraftWithoutRecipientsInTo(t *testing.T) {
	m, _, u := draftsFixture(t, &mailtest.DraftSender{})
	m = send(t, m, click(60, 3)) // selects the Solo draft
	if th, _, _ := m.list.Selected(); th.Latest().UID != u.solo || m.draft != nil {
		t.Fatalf("setup: selected %d, draft %v", th.Latest().UID, m.draft)
	}

	next, cmd := m.Update(click(60, 3)) // a second click opens
	m = next.(Model)
	if cmd == nil || m.draft == nil || !m.draft.loading || m.readerPopup {
		t.Fatalf("cmd %v, draft %v, reader %v; want a pending fetch from the click", cmd != nil, m.draft, m.readerPopup)
	}
	m = send(t, m, cmd())

	if v := m.draft.pop.Values(); v.Subject != "Solo" || v.To != "" || v.Body != "Just notes." {
		t.Fatalf("values = %+v", v)
	}
	m = typed(t, m, "bob@example.com")
	if got := m.draft.pop.Values().To; got != "bob@example.com" {
		t.Errorf("To = %q; want typing to land in To", got)
	}
	if m.draft.base.From != (mail.Address{Name: "Me Myself", Email: "me@example.com"}) {
		t.Errorf("From = %+v, want the account identity filled in", m.draft.base.From)
	}
}

func TestDrafts_SelectedMessageRowInAnExpandedThreadIsOpened(t *testing.T) {
	m, _, u := draftsFixture(t, &mailtest.DraftSender{})
	m = send(t, m, press("space"), press("j"))
	_, uid, _ := m.list.Selected()
	if uid == 0 {
		t.Fatal("setup: cursor not on a message row")
	}
	if uid == u.latest {
		m = send(t, m, press("j"))
		_, uid, _ = m.list.Selected()
	}
	if uid != u.first {
		t.Fatalf("setup: selected %d, want the older message %d", uid, u.first)
	}

	m = openedDraft(t, m)

	if d := m.draft.base; d.DraftOf == nil || d.DraftOf.UID != u.first || m.draft.pop.Values().Subject != "Plan B" {
		t.Errorf("base = %+v", d)
	}
}

func TestDrafts_AccountWithoutDrafterOpensTheReader(t *testing.T) {
	m, _, _ := draftsFixture(t, &mailtest.Sender{})

	m = pressSettled(t, m, "enter")

	if !m.readerPopup || m.draft != nil {
		t.Errorf("reader %v, draft %v; want the reader", m.readerPopup, m.draft)
	}
}

func TestDrafts_OtherFoldersStillOpenTheReader(t *testing.T) {
	m, _ := composeFixture(t)

	m = pressSettled(t, m, "enter")

	if !m.readerPopup || m.draft != nil {
		t.Errorf("reader %v, draft %v; want the reader in the inbox", m.readerPopup, m.draft)
	}
}

func TestDrafts_OpenFailureShowsTheErrorAndNoComposer(t *testing.T) {
	m, _, u := draftsFixture(t, &mailtest.DraftSender{})
	m = send(t, m, click(60, 3)) // the Solo draft
	// Forget the stored draft so OpenDraft fails.
	m.senders[0] = &mailtest.DraftSender{}

	m, cmd := keyCmd(t, m, "enter")
	if cmd == nil || m.draft == nil {
		t.Fatalf("enter started nothing (status %q)", m.status)
	}
	m = send(t, m, cmd())

	if m.draft != nil || m.composing() || m.readerPopup || !m.statusErr || !strings.HasPrefix(m.status, "open draft: ") {
		t.Errorf("draft %v, status %q (err %v)", m.draft, m.status, m.statusErr)
	}
	if th, _, _ := m.list.Selected(); th.Latest().UID != u.solo {
		t.Error("the list selection changed")
	}
}

func TestDrafts_UntouchedDraftClosesWithoutSaving(t *testing.T) {
	m, f, _ := draftsFixture(t, &mailtest.DraftSender{})
	s := f.personalSender.(*mailtest.DraftSender)
	m = openedDraft(t, m)

	m, cmd := keyCmd(t, m, "esc")

	if m.composing() || m.draft != nil || cmd != nil || m.status != "" || len(s.Saved()) != 0 {
		t.Errorf("composing %v, status %q, saved %d; want a silent close", m.composing(), m.status, len(s.Saved()))
	}
}

func TestDrafts_SavingAgainReplacesTheOpenedDraft(t *testing.T) {
	m, f, u := draftsFixture(t, &mailtest.DraftSender{})
	s := f.personalSender.(*mailtest.DraftSender)
	m = openedDraft(t, m)
	m = typed(t, m, "More. ")
	m, _ = keyCmd(t, m, "esc")

	m, cmd := keyCmd(t, m, "s")
	m = send(t, m, cmd())

	saved := s.Saved()
	if len(saved) != 1 {
		t.Fatalf("saved %d drafts, want 1", len(saved))
	}
	d := saved[0]
	if d.DraftOf == nil || *d.DraftOf != (mail.MessageRef{Folder: "Drafts", UID: u.latest}) ||
		d.Body != "More. Latest text." || len(d.Bcc) != 1 || d.InReplyTo != "<d1@x>" {
		t.Errorf("saved = %+v", d)
	}
	if m.composing() || m.status != "Draft saved" {
		t.Errorf("composing %v, status %q", m.composing(), m.status)
	}
}

func TestDrafts_SendingKeepsDraftOfAndForwardOf(t *testing.T) {
	m, f, u := draftsFixture(t, &mailtest.DraftSender{})
	s := f.personalSender.(*mailtest.DraftSender)
	orig := mail.MessageRef{Folder: "INBOX", UID: 2}
	ref := mail.MessageRef{Folder: "Drafts", UID: u.solo}
	s.AddDraft(ref, mail.Draft{To: []mail.Address{{Email: "carol@example.com"}}, Subject: "Fwd: Lunch?", Body: "FYI", ForwardOf: &orig})
	m = send(t, m, click(60, 3)) // the Solo draft
	m = openedDraft(t, m)

	m = sendKey(t, m)

	sent := s.Sent()
	if len(sent) != 1 {
		t.Fatalf("sent %d, want 1", len(sent))
	}
	d := sent[0]
	if d.DraftOf == nil || *d.DraftOf != ref || d.ForwardOf == nil || *d.ForwardOf != orig {
		t.Errorf("sent = %+v; want DraftOf %v and ForwardOf %v kept", d, ref, orig)
	}
	if m.composing() || !strings.HasPrefix(m.status, "Sent to carol@example.com") {
		t.Errorf("composing %v, status %q", m.composing(), m.status)
	}
}

func TestDrafts_OpeningIsRefusedWhileBusy(t *testing.T) {
	for _, tc := range []struct {
		name   string
		set    func(*Model)
		status string
	}{
		{"sending", func(m *Model) { m.sending = true }, "Sending…"},
		{"saving", func(m *Model) { m.saving = true }, "Saving…"},
		{"a draft is open", func(m *Model) { m.draft = &draftState{loading: true} }, "Finish or discard the draft first"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, _, _ := draftsFixture(t, &mailtest.DraftSender{})
			tc.set(&m)
			draft := m.draft

			m, cmd := keyCmd(t, m, "enter")

			if cmd != nil || m.draft != draft || m.readerPopup || m.status != tc.status {
				t.Errorf("cmd %v, reader %v, status %q; want a refusal saying %q", cmd != nil, m.readerPopup, m.status, tc.status)
			}
		})
	}
}
