package imap

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	goimap "github.com/emersion/go-imap/v2"

	"github.com/dkouvdis/dk-email/internal/mail"
)

// createMailbox makes an unlisted mailbox on the test server. It can be
// selected but doesn't affect the seeded folder list.
func (s *testServer) createMailbox(t *testing.T, name string) {
	t.Helper()
	if err := s.user.Create(name, nil); err != nil {
		t.Fatalf("create %q: %v", name, err)
	}
}

func (s *testServer) appendRaw(t *testing.T, mailbox, raw string, at time.Time, flags ...goimap.Flag) {
	t.Helper()
	raw = strings.ReplaceAll(raw, "\n", "\r\n")
	if _, err := s.user.Append(mailbox, newLiteral(raw), &goimap.AppendOptions{Flags: flags, Time: at}); err != nil {
		t.Fatalf("append to %q: %v", mailbox, err)
	}
}

// seedBulk appends n messages whose subject encodes their 1-based position.
func (s *testServer) seedBulk(t *testing.T, mailbox string, n int) {
	t.Helper()
	s.createMailbox(t, mailbox)
	for i := 1; i <= n; i++ {
		raw := fmt.Sprintf("Message-ID: <bulk-%d@example.com>\nSubject: message %03d\n\nbody\n", i, i)
		s.appendRaw(t, mailbox, raw, base(i))
	}
}

func base(i int) time.Time {
	return time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(i) * time.Minute)
}

func TestSummaries_PagesNewestFirstWithoutGapsOrDuplicates(t *testing.T) {
	srv := startServer(t, modeImplicit, capsListStatus)
	srv.seedBulk(t, "Bulk", 450)
	c := srv.newClient(t, modeImplicit)
	ctx := timeoutCtx(t, 20*time.Second)

	var all []mail.Summary
	var pages []int
	before := mail.UID(0)
	for {
		page, err := c.Summaries(ctx, "Bulk", before, 200)
		if err != nil {
			t.Fatalf("Summaries(before=%d) error = %v", before, err)
		}
		if len(page) == 0 {
			break
		}
		pages = append(pages, len(page))
		all = append(all, page...)
		before = page[len(page)-1].UID
	}

	if !slices.Equal(pages, []int{200, 200, 50}) {
		t.Errorf("page sizes = %v, want [200 200 50]", pages)
	}
	if len(all) != 450 {
		t.Fatalf("got %d summaries, want 450", len(all))
	}
	for i, s := range all {
		wantSubject := fmt.Sprintf("message %03d", 450-i)
		if s.Subject != wantSubject {
			t.Fatalf("summary %d subject = %q, want %q (newest first, no gaps)", i, s.Subject, wantSubject)
		}
		if i > 0 && s.UID >= all[i-1].UID {
			t.Fatalf("UIDs not strictly descending at %d: %d after %d", i, s.UID, all[i-1].UID)
		}
	}
}

func TestSummaries_ParsesHeadersFlagsAndThreadingIDs(t *testing.T) {
	srv := startServer(t, modeImplicit, capsListStatus)
	srv.createMailbox(t, "Threads")
	srv.appendRaw(t, "Threads", `Message-ID: <a@example.com>
From: Jane Doe <jane@example.com>
To: Bob <bob@example.com>, carol@example.com
Subject: =?UTF-8?B?Q2Fmw6kg4piVIHBsYW5z?=
Date: Mon, 02 Nov 2026 10:00:00 +0000

hi
`, base(1), goimap.FlagSeen, goimap.FlagFlagged)
	// No Date header, so the server's internal date is used. References is
	// folded over two lines.
	srv.appendRaw(t, "Threads", `Message-ID: <b@example.com>
In-Reply-To: <a@example.com>
References: <root@example.com>
 <a@example.com>
From: =?ISO-8859-1?Q?Andr=E9?= <andre@example.com>
To: jane@example.com
Subject: =?ISO-8859-1?Q?Re:_Caf=E9?=

reply
`, base(2))
	c := srv.newClient(t, modeImplicit)

	got, err := c.Summaries(timeoutCtx(t, 10*time.Second), "Threads", 0, 10)

	if err != nil {
		t.Fatalf("Summaries() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d summaries, want 2", len(got))
	}
	reply, orig := got[0], got[1]

	wantOrig := mail.Summary{
		UID:       orig.UID,
		From:      mail.Address{Name: "Jane Doe", Email: "jane@example.com"},
		To:        []mail.Address{{Name: "Bob", Email: "bob@example.com"}, {Email: "carol@example.com"}},
		Subject:   "Café ☕ plans",
		Date:      time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC),
		Seen:      true,
		Flagged:   true,
		MessageID: "a@example.com",
	}
	assertSummary(t, orig, wantOrig)

	wantReply := mail.Summary{
		UID:        reply.UID,
		From:       mail.Address{Name: "André", Email: "andre@example.com"},
		To:         []mail.Address{{Email: "jane@example.com"}},
		Subject:    "Re: Café",
		Date:       base(2),
		MessageID:  "b@example.com",
		InReplyTo:  "a@example.com",
		References: []string{"root@example.com", "a@example.com"},
	}
	assertSummary(t, reply, wantReply)
}

func assertSummary(t *testing.T, got, want mail.Summary) {
	t.Helper()
	if !got.Date.Equal(want.Date) {
		t.Errorf("UID %d Date = %v, want %v", got.UID, got.Date, want.Date)
	}
	got.Date, want.Date = time.Time{}, time.Time{}
	if got.From != want.From || got.Subject != want.Subject || got.Seen != want.Seen ||
		got.Flagged != want.Flagged || got.MessageID != want.MessageID || got.InReplyTo != want.InReplyTo ||
		!slices.Equal(got.To, want.To) || !slices.Equal(got.References, want.References) {
		t.Errorf("summary =\n  %+v\nwant\n  %+v", got, want)
	}
}

func TestSummaries_DoesNotChangeFlags(t *testing.T) {
	srv := startServer(t, modeImplicit, capsListStatus)
	c := srv.newClient(t, modeImplicit)

	got, err := c.Summaries(timeoutCtx(t, 10*time.Second), "INBOX", 0, 10)

	if err != nil {
		t.Fatalf("Summaries() error = %v", err)
	}
	unread := 0
	for _, s := range got {
		if !s.Seen {
			unread++
		}
	}
	if len(got) != 3 || unread != 2 {
		t.Errorf("got %d summaries with %d unread, want 3 with 2 unread", len(got), unread)
	}
	status, err := srv.user.Status("INBOX", &goimap.StatusOptions{NumUnseen: true})
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if *status.NumUnseen != 2 {
		t.Errorf("server unseen count = %d after Summaries, want 2 (no implicit \\Seen)", *status.NumUnseen)
	}
}

func TestSummaries_EmptyResults(t *testing.T) {
	srv := startServer(t, modeImplicit, capsListStatus)
	c := srv.newClient(t, modeImplicit)
	ctx := timeoutCtx(t, 10*time.Second)

	tests := []struct {
		name   string
		folder string
		before mail.UID
		limit  int
	}{
		{"empty folder", "Trash", 0, 200},
		{"nothing below UID 1", "INBOX", 1, 200},
		{"zero limit", "INBOX", 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := c.Summaries(ctx, tt.folder, tt.before, tt.limit)

			if err != nil || len(got) != 0 {
				t.Errorf("Summaries() = %v, %v; want empty, nil", got, err)
			}
		})
	}
}

func TestSummaries_UnknownFolderKeepsConnection(t *testing.T) {
	srv := startServer(t, modeImplicit, capsListStatus)
	c := srv.newClient(t, modeImplicit)
	ctx := timeoutCtx(t, 10*time.Second)
	if _, err := c.Folders(ctx); err != nil {
		t.Fatalf("Folders() error = %v", err)
	}

	_, err := c.Summaries(ctx, "Nope", 0, 10)

	if !errors.Is(err, mail.ErrFolderNotFound) {
		t.Fatalf("Summaries() error = %v, want mail.ErrFolderNotFound", err)
	}
	if !strings.Contains(err.Error(), `"Nope"`) {
		t.Errorf("error %q doesn't name the folder", err)
	}
	if _, err := c.Summaries(ctx, "INBOX", 0, 10); err != nil {
		t.Fatalf("Summaries(INBOX) after error = %v", err)
	}
	if n := srv.ln.accepted(); n != 1 {
		t.Errorf("server accepted %d connections, want 1", n)
	}
}

func TestSummaries_UIDValidityChangeFailsPagingUntilReload(t *testing.T) {
	srv := startServer(t, modeImplicit, capsListStatus)
	srv.seedBulk(t, "Bulk", 5)
	c := srv.newClient(t, modeImplicit)
	ctx := timeoutCtx(t, 10*time.Second)

	first, err := c.Summaries(ctx, "Bulk", 0, 2)
	if err != nil {
		t.Fatalf("first page error = %v", err)
	}

	if err := srv.user.Delete("Bulk"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	srv.seedBulk(t, "Bulk", 3)

	_, err = c.Summaries(ctx, "Bulk", first[len(first)-1].UID, 2)
	if !errors.Is(err, mail.ErrFolderChanged) {
		t.Fatalf("paging after UIDVALIDITY change error = %v, want mail.ErrFolderChanged", err)
	}

	reloaded, err := c.Summaries(ctx, "Bulk", 0, 2)
	if err != nil {
		t.Fatalf("reload error = %v", err)
	}
	if reloaded[0].Subject != "message 003" {
		t.Errorf("reload newest subject = %q, want message 003", reloaded[0].Subject)
	}
	next, err := c.Summaries(ctx, "Bulk", reloaded[len(reloaded)-1].UID, 2)
	if err != nil {
		t.Fatalf("paging after reload error = %v", err)
	}
	if len(next) != 1 || next[0].Subject != "message 001" {
		t.Errorf("page after reload = %+v, want just message 001", next)
	}
	if n := srv.ln.accepted(); n != 1 {
		t.Errorf("server accepted %d connections, want 1 (ErrFolderChanged must not reconnect)", n)
	}
}

func TestParseReferences(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want []string
	}{
		{"empty", "", nil},
		{"no references header", "\r\n", nil},
		{"folded", "References: <a@x>\r\n <b@x>\r\n\r\n", []string{"a@x", "b@x"}},
		{"malformed falls back to brackets", "References: junk <a@x> \"q\" <b@x\r\n\r\n", []string{"a@x"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseReferences([]byte(tt.raw)); !slices.Equal(got, tt.want) {
				t.Errorf("parseReferences(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestSummaries_StripsTerminalEscapesFromEnvelope(t *testing.T) {
	srv := startServer(t, modeImplicit, capsListStatus)
	srv.createMailbox(t, "Evil")
	srv.appendRaw(t, "Evil", "From: Mallory <m@example.com>\nSubject: Hi\x1b]0;pwned\x07 there\n\nbody\n", base(1))
	c := srv.newClient(t, modeImplicit)

	got, err := c.Summaries(timeoutCtx(t, 10*time.Second), "Evil", 0, 10)

	if err != nil || len(got) != 1 {
		t.Fatalf("Summaries() = %v, %v", got, err)
	}
	if got[0].Subject != "Hi there" || got[0].From.Name != "Mallory" {
		t.Errorf("subject %q, from %q; want escapes stripped", got[0].Subject, got[0].From.Name)
	}
}

// memserver drops addresses whose names hold control characters, so names
// are checked here rather than end to end.
func TestNewAddress_StripsTerminalEscapes(t *testing.T) {
	got := newAddress(goimap.Address{Name: "Mal\x1b[31mlory\u202e", Mailbox: "m\x07", Host: "example.com"})

	if got != (mail.Address{Name: "Mallory", Email: "m@example.com"}) {
		t.Errorf("newAddress = %+v, want escapes stripped", got)
	}
}
