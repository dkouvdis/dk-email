package imap

import (
	"errors"
	"slices"
	"testing"
	"time"

	goimap "github.com/emersion/go-imap/v2"

	"github.com/dkouvdis/dk-email/internal/mail"
)

// Capability sets for the MOVE paths. IMAP4rev2 implies MOVE and UIDPLUS,
// so these advertise IMAP4rev1 only.
var (
	capsMove      = goimap.CapSet{goimap.CapIMAP4rev1: {}, goimap.CapMove: {}, goimap.CapUIDPlus: {}}
	capsUIDPlus   = goimap.CapSet{goimap.CapIMAP4rev1: {}, goimap.CapUIDPlus: {}}
	capsNoUIDPlus = goimap.CapSet{goimap.CapIMAP4rev1: {}}
)

func inboxUIDs(t *testing.T, c *Client) []mail.UID {
	t.Helper()
	sums, err := c.Summaries(timeoutCtx(t, 10*time.Second), "INBOX", 0, 100)
	if err != nil {
		t.Fatalf("Summaries: %v", err)
	}
	var out []mail.UID
	for _, s := range sums {
		out = append(out, s.UID)
	}
	slices.Sort(out)
	return out
}

func flagsOf(t *testing.T, c *Client, folder string, uid mail.UID) (seen, flagged bool) {
	t.Helper()
	sums, err := c.Summaries(timeoutCtx(t, 10*time.Second), folder, 0, 100)
	if err != nil {
		t.Fatalf("Summaries: %v", err)
	}
	for _, s := range sums {
		if s.UID == uid {
			return s.Seen, s.Flagged
		}
	}
	t.Fatalf("UID %d not in %q", uid, folder)
	return false, false
}

func count(t *testing.T, srv *testServer, folder string) int {
	t.Helper()
	st, err := srv.user.Status(folder, &goimap.StatusOptions{NumMessages: true})
	if err != nil {
		t.Fatalf("Status(%q): %v", folder, err)
	}
	return int(*st.NumMessages)
}

func TestSetFlag_SetsAndClearsBothFlags(t *testing.T) {
	srv := startServer(t, modeImplicit, capsListStatus)
	c := srv.newClient(t, modeImplicit)
	ctx := timeoutCtx(t, 10*time.Second)
	uids := inboxUIDs(t, c) // UID 1 is seen, 2 and 3 are unseen
	unseen := uids[1:]

	steps := []struct {
		flag        mail.Flag
		on          bool
		wantSeen    bool
		wantFlagged bool
	}{
		{mail.FlagSeen, true, true, false},
		{mail.FlagFlagged, true, true, true},
		{mail.FlagSeen, false, false, true},
		{mail.FlagFlagged, false, false, false},
	}
	for _, s := range steps {
		if err := c.SetFlag(ctx, "INBOX", unseen, s.flag, s.on); err != nil {
			t.Fatalf("SetFlag(%v, %v) error = %v", s.flag, s.on, err)
		}
		for _, uid := range unseen {
			if seen, flagged := flagsOf(t, c, "INBOX", uid); seen != s.wantSeen || flagged != s.wantFlagged {
				t.Errorf("after SetFlag(%v, %v): UID %d seen=%v flagged=%v", s.flag, s.on, uid, seen, flagged)
			}
		}
	}
	if seen, _ := flagsOf(t, c, "INBOX", uids[0]); !seen {
		t.Error("SetFlag touched a UID it wasn't given")
	}
}

func TestSetFlag_EdgeCases(t *testing.T) {
	srv := startServer(t, modeImplicit, capsListStatus)
	c := srv.newClient(t, modeImplicit)
	ctx := timeoutCtx(t, 10*time.Second)

	if err := c.SetFlag(ctx, "INBOX", nil, mail.FlagSeen, true); err != nil {
		t.Errorf("no UIDs: error = %v", err)
	}
	if srv.ln.accepted() != 0 {
		t.Error("SetFlag with no UIDs connected")
	}
	if err := c.SetFlag(ctx, "INBOX", []mail.UID{1}, mail.Flag(99), true); err == nil {
		t.Error("unknown flag: error = nil")
	}
	if err := c.SetFlag(ctx, "Nope", []mail.UID{1}, mail.FlagSeen, true); !errors.Is(err, mail.ErrFolderNotFound) {
		t.Errorf("unknown folder: error = %v, want ErrFolderNotFound", err)
	}
}

func TestSetFlag_UIDValidityChange(t *testing.T) {
	srv := startServer(t, modeImplicit, capsListStatus)
	srv.seedBulk(t, "Bulk", 2)
	c := srv.newClient(t, modeImplicit)
	ctx := timeoutCtx(t, 10*time.Second)
	if _, err := c.Summaries(ctx, "Bulk", 0, 10); err != nil {
		t.Fatal(err)
	}
	if err := srv.user.Delete("Bulk"); err != nil {
		t.Fatal(err)
	}
	srv.seedBulk(t, "Bulk", 2)

	if err := c.SetFlag(ctx, "Bulk", []mail.UID{1}, mail.FlagSeen, true); !errors.Is(err, mail.ErrFolderChanged) {
		t.Errorf("error = %v, want ErrFolderChanged", err)
	}
	// The error records the new UIDVALIDITY, so reset the folder again.
	if err := srv.user.Delete("Bulk"); err != nil {
		t.Fatal(err)
	}
	srv.seedBulk(t, "Bulk", 2)
	if err := c.Move(ctx, "Bulk", []mail.UID{1}, "Archive"); !errors.Is(err, mail.ErrFolderChanged) {
		t.Errorf("Move error = %v, want ErrFolderChanged", err)
	}
}

func TestMove(t *testing.T) {
	tests := []struct {
		name string
		caps goimap.CapSet
		// expunged: whether moved messages leave the source for good. Without
		// UIDPLUS they stay flagged \Deleted, hidden from Summaries.
		expunged bool
	}{
		{"IMAP4rev2", capsListStatus, true},
		{"MOVE", capsMove, true},
		{"COPY fallback with UIDPLUS", capsUIDPlus, true},
		{"COPY fallback without UIDPLUS", capsNoUIDPlus, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := startServer(t, modeImplicit, tt.caps)
			// Someone else's message already marked \Deleted must survive.
			srv.appendRaw(t, "INBOX", "Subject: keep me\n\nx\n", base(9), goimap.FlagDeleted)
			c := srv.newClient(t, modeImplicit)
			ctx := timeoutCtx(t, 10*time.Second)
			uids := inboxUIDs(t, c)
			if len(uids) != 3 {
				t.Fatalf("INBOX lists %v, want 3 (the \\Deleted one hidden)", uids)
			}

			if err := c.Move(ctx, "INBOX", uids[1:], "Archive"); err != nil {
				t.Fatalf("Move() error = %v", err)
			}

			if got := inboxUIDs(t, c); !slices.Equal(got, uids[:1]) {
				t.Errorf("INBOX lists %v after the move, want %v", got, uids[:1])
			}
			if n := count(t, srv, "Archive"); n != 2+2 {
				t.Errorf("Archive has %d messages, want 4", n)
			}
			wantInbox := 1 + 1 // the unmoved message and the other \Deleted one
			if !tt.expunged {
				wantInbox += 2
			}
			if n := count(t, srv, "INBOX"); n != wantInbox {
				t.Errorf("INBOX holds %d messages, want %d (only the moved UIDs may be expunged)", n, wantInbox)
			}

			// Repeating the move (as a retry would) must not copy again.
			if err := c.Move(ctx, "INBOX", uids[1:], "Archive"); err != nil {
				t.Fatalf("repeated Move() error = %v", err)
			}
			if n := count(t, srv, "Archive"); n != 4 {
				t.Errorf("Archive has %d messages after a repeat, want 4 (no duplicates)", n)
			}
		})
	}
}

func TestMove_EdgeCases(t *testing.T) {
	srv := startServer(t, modeImplicit, capsListStatus)
	c := srv.newClient(t, modeImplicit)
	ctx := timeoutCtx(t, 10*time.Second)

	if err := c.Move(ctx, "INBOX", nil, "Archive"); err != nil || srv.ln.accepted() != 0 {
		t.Errorf("no UIDs: error = %v, connections = %d", err, srv.ln.accepted())
	}
	if err := c.Move(ctx, "INBOX", []mail.UID{1}, "INBOX"); err != nil || count(t, srv, "INBOX") != 3 {
		t.Errorf("moving to the same folder: error = %v", err)
	}
	if err := c.Move(ctx, "INBOX", []mail.UID{1}, "Nope"); !errors.Is(err, mail.ErrFolderNotFound) {
		t.Errorf("unknown destination: error = %v, want ErrFolderNotFound", err)
	}
	if count(t, srv, "INBOX") != 3 {
		t.Error("a failed move changed the source")
	}
}
