package mailtest

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/dkouvdis/dk-email/internal/mail"
)

var ctx = context.Background()

func msg(subject string, seen bool) mail.Message {
	return mail.Message{
		Summary: mail.Summary{
			From:    mail.Address{Name: "Jane", Email: "jane@example.com"},
			Subject: subject,
			Date:    time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC),
			Seen:    seen,
		},
		Body: "body of " + subject,
	}
}

func inbox() *Fake {
	f := New()
	f.AddFolder("INBOX", mail.RoleInbox)
	f.AddFolder("[Gmail]/Trash", mail.RoleTrash)
	return f
}

func uids(sums []mail.Summary) []mail.UID {
	out := make([]mail.UID, len(sums))
	for i, s := range sums {
		out[i] = s.UID
	}
	return out
}

func TestFolders_ListsInInsertionOrderWithCounts(t *testing.T) {
	f := inbox()
	f.AddMessage("INBOX", msg("a", false))
	f.AddMessage("INBOX", msg("b", true))
	f.AddMessage("INBOX", msg("c", false))

	got, err := f.Folders(ctx)

	if err != nil {
		t.Fatalf("Folders() error = %v", err)
	}
	want := []mail.Folder{
		{Name: "INBOX", Delimiter: "/", Role: mail.RoleInbox, Unread: 2, Total: 3},
		{Name: "[Gmail]/Trash", Delimiter: "/", Role: mail.RoleTrash, Unread: 0, Total: 0},
	}
	if len(got) != len(want) {
		t.Fatalf("Folders() = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Folders()[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestAddMessage_AssignsIncreasingUIDsPerFolder(t *testing.T) {
	f := inbox()

	a := f.AddMessage("INBOX", msg("a", false))
	b := f.AddMessage("INBOX", msg("b", false))
	c := f.AddMessage("[Gmail]/Trash", msg("c", false))

	if a != 1 || b != 2 || c != 1 {
		t.Errorf("UIDs = %d, %d, %d; want 1, 2, 1", a, b, c)
	}
}

func TestSummaries_NewestFirstWithPaging(t *testing.T) {
	f := inbox()
	for range 5 {
		f.AddMessage("INBOX", msg("m", false))
	}

	first, err := f.Summaries(ctx, "INBOX", 0, 2)
	if err != nil {
		t.Fatalf("Summaries() error = %v", err)
	}
	second, _ := f.Summaries(ctx, "INBOX", first[len(first)-1].UID, 2)
	third, _ := f.Summaries(ctx, "INBOX", second[len(second)-1].UID, 2)

	got := [][]mail.UID{uids(first), uids(second), uids(third)}
	want := [][]mail.UID{{5, 4}, {3, 2}, {1}}
	for i := range want {
		if !slices.Equal(got[i], want[i]) {
			t.Errorf("page %d = %v, want %v", i+1, got[i], want[i])
		}
	}
}

func TestSummaries_EmptyBelowOldest(t *testing.T) {
	f := inbox()
	f.AddMessage("INBOX", msg("a", false))

	got, err := f.Summaries(ctx, "INBOX", 1, 10)

	if err != nil || len(got) != 0 {
		t.Errorf("Summaries() = %v, %v; want empty", got, err)
	}
}

func TestSummaries_ReturnCopies(t *testing.T) {
	f := inbox()
	m := msg("a", false)
	m.References = []string{"<root@x>"}
	f.AddMessage("INBOX", m)

	got, _ := f.Summaries(ctx, "INBOX", 0, 10)
	got[0].Subject = "changed"
	got[0].References[0] = "changed"

	again, _ := f.Summaries(ctx, "INBOX", 0, 10)
	if again[0].Subject != "a" || again[0].References[0] != "<root@x>" {
		t.Errorf("caller mutation leaked into the fake: %+v", again[0])
	}
}

func TestMessage_ReturnsFullMessage(t *testing.T) {
	f := inbox()
	uid := f.AddMessage("INBOX", msg("hello", false))

	got, err := f.Message(ctx, "INBOX", uid)

	if err != nil {
		t.Fatalf("Message() error = %v", err)
	}
	if got.UID != uid || got.Subject != "hello" || got.Body != "body of hello" {
		t.Errorf("Message() = %+v", got)
	}
}

func TestMessage_DoesNotMarkSeen(t *testing.T) {
	f := inbox()
	uid := f.AddMessage("INBOX", msg("hello", false))

	if _, err := f.Message(ctx, "INBOX", uid); err != nil {
		t.Fatal(err)
	}

	if f.Messages("INBOX")[0].Seen {
		t.Error("Message() marked the message seen; only SetFlag should")
	}
}

func TestMessage_UnknownUID(t *testing.T) {
	f := inbox()

	_, err := f.Message(ctx, "INBOX", 42)

	if !errors.Is(err, mail.ErrMessageNotFound) {
		t.Errorf("Message() error = %v, want ErrMessageNotFound", err)
	}
}

func TestUnknownFolder(t *testing.T) {
	f := inbox()
	checks := map[string]error{
		"Summaries": func() error { _, err := f.Summaries(ctx, "Nope", 0, 10); return err }(),
		"Message":   func() error { _, err := f.Message(ctx, "Nope", 1); return err }(),
		"SetFlag":   f.SetFlag(ctx, "Nope", []mail.UID{1}, mail.FlagSeen, true),
		"Move from": f.Move(ctx, "Nope", []mail.UID{1}, "INBOX"),
		"Move to":   f.Move(ctx, "INBOX", []mail.UID{1}, "Nope"),
	}
	for name, err := range checks {
		if !errors.Is(err, mail.ErrFolderNotFound) {
			t.Errorf("%s error = %v, want ErrFolderNotFound", name, err)
		}
	}
}

func TestSetFlag_SetsAndClearsFlags(t *testing.T) {
	f := inbox()
	a := f.AddMessage("INBOX", msg("a", false))
	b := f.AddMessage("INBOX", msg("b", false))

	if err := f.SetFlag(ctx, "INBOX", []mail.UID{a, b}, mail.FlagSeen, true); err != nil {
		t.Fatal(err)
	}
	if err := f.SetFlag(ctx, "INBOX", []mail.UID{b}, mail.FlagFlagged, true); err != nil {
		t.Fatal(err)
	}
	if err := f.SetFlag(ctx, "INBOX", []mail.UID{a}, mail.FlagSeen, false); err != nil {
		t.Fatal(err)
	}

	got := f.Messages("INBOX")
	if got[0].Seen || got[0].Flagged {
		t.Errorf("a = seen:%v flagged:%v, want both false", got[0].Seen, got[0].Flagged)
	}
	if !got[1].Seen || !got[1].Flagged {
		t.Errorf("b = seen:%v flagged:%v, want both true", got[1].Seen, got[1].Flagged)
	}
}

func TestSetFlag_UnknownFlag(t *testing.T) {
	f := inbox()
	uid := f.AddMessage("INBOX", msg("a", false))

	if err := f.SetFlag(ctx, "INBOX", []mail.UID{uid}, mail.Flag(99), true); err == nil {
		t.Error("SetFlag() with unknown flag: error = nil")
	}
}

func TestMove_MovesAndAssignsNewUIDs(t *testing.T) {
	f := inbox()
	a := f.AddMessage("INBOX", msg("a", false))
	b := f.AddMessage("INBOX", msg("b", false))
	f.AddMessage("[Gmail]/Trash", msg("old", true))

	if err := f.Move(ctx, "INBOX", []mail.UID{a}, "[Gmail]/Trash"); err != nil {
		t.Fatalf("Move() error = %v", err)
	}

	inboxMsgs, trash := f.Messages("INBOX"), f.Messages("[Gmail]/Trash")
	if len(inboxMsgs) != 1 || inboxMsgs[0].UID != b {
		t.Errorf("INBOX = %+v, want only b", inboxMsgs)
	}
	if len(trash) != 2 || trash[1].Subject != "a" || trash[1].UID != 2 {
		t.Errorf("Trash = %+v, want old then a with UID 2", trash)
	}
}

// Real IMAP servers silently ignore UIDs that no longer exist, so the fake does too.
func TestMove_IgnoresMissingUIDs(t *testing.T) {
	f := inbox()

	if err := f.Move(ctx, "INBOX", []mail.UID{42}, "[Gmail]/Trash"); err != nil {
		t.Errorf("Move() error = %v, want nil", err)
	}
}

func TestMove_ToSameFolderFails(t *testing.T) {
	f := inbox()
	uid := f.AddMessage("INBOX", msg("a", false))

	if err := f.Move(ctx, "INBOX", []mail.UID{uid}, "INBOX"); err == nil {
		t.Error("Move() to the same folder: error = nil")
	}
}

func TestFail_InjectsErrorPerMethod(t *testing.T) {
	boom := errors.New("boom")
	f := inbox()
	uid := f.AddMessage("INBOX", msg("a", false))
	for _, m := range []Method{MethodFolders, MethodSummaries, MethodMessage, MethodSetFlag, MethodMove} {
		f.Fail(m, boom)
	}

	_, errFolders := f.Folders(ctx)
	_, errSummaries := f.Summaries(ctx, "INBOX", 0, 10)
	_, errMessage := f.Message(ctx, "INBOX", uid)
	errSetFlag := f.SetFlag(ctx, "INBOX", []mail.UID{uid}, mail.FlagSeen, true)
	errMove := f.Move(ctx, "INBOX", []mail.UID{uid}, "[Gmail]/Trash")

	for name, err := range map[string]error{"Folders": errFolders, "Summaries": errSummaries, "Message": errMessage, "SetFlag": errSetFlag, "Move": errMove} {
		if !errors.Is(err, boom) {
			t.Errorf("%s error = %v, want injected error", name, err)
		}
	}
	if m := f.Messages("INBOX"); len(m) != 1 || m[0].Seen {
		t.Errorf("failed calls changed state: %+v", m)
	}
}

func TestFail_NilClearsError(t *testing.T) {
	f := inbox()
	f.Fail(MethodFolders, errors.New("boom"))
	f.Fail(MethodFolders, nil)

	if _, err := f.Folders(ctx); err != nil {
		t.Errorf("Folders() error = %v, want nil after clearing", err)
	}
}

func TestDelay_RespectsContextCancellation(t *testing.T) {
	f := inbox()
	f.SetDelay(time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	start := time.Now()

	_, err := f.Folders(ctx)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Folders() error = %v, want DeadlineExceeded", err)
	}
	if time.Since(start) > time.Second {
		t.Error("Folders() ignored context cancellation during delay")
	}
}

func TestDelay_SlowsCalls(t *testing.T) {
	f := inbox()
	f.SetDelay(30 * time.Millisecond)
	start := time.Now()

	if _, err := f.Folders(ctx); err != nil {
		t.Fatal(err)
	}

	if time.Since(start) < 30*time.Millisecond {
		t.Error("Folders() returned before the configured delay")
	}
}

func TestCalls_RecordsEveryCall(t *testing.T) {
	f := inbox()
	uid := f.AddMessage("INBOX", msg("a", false))

	f.Folders(ctx)
	f.SetFlag(ctx, "INBOX", []mail.UID{uid}, mail.FlagSeen, true)
	f.Move(ctx, "INBOX", []mail.UID{uid}, "[Gmail]/Trash")

	got := f.Calls()
	want := []Call{
		{Method: MethodFolders},
		{Method: MethodSetFlag, Folder: "INBOX", UIDs: []mail.UID{uid}, Flag: mail.FlagSeen, On: true},
		{Method: MethodMove, Folder: "INBOX", UIDs: []mail.UID{uid}, To: "[Gmail]/Trash"},
	}
	if len(got) != len(want) {
		t.Fatalf("Calls() = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i].Method != want[i].Method || got[i].Folder != want[i].Folder || got[i].To != want[i].To ||
			got[i].Flag != want[i].Flag || got[i].On != want[i].On || !slices.Equal(got[i].UIDs, want[i].UIDs) {
			t.Errorf("Calls()[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestClose_LaterCallsFail(t *testing.T) {
	f := inbox()

	if err := f.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if _, err := f.Folders(ctx); !errors.Is(err, mail.ErrClosed) {
		t.Errorf("Folders() after Close error = %v, want ErrClosed", err)
	}
	if err := f.Close(); err != nil {
		t.Errorf("second Close() error = %v, want nil", err)
	}
}

func TestFake_IsSafeForConcurrentUse(t *testing.T) {
	f := inbox()
	uid := f.AddMessage("INBOX", msg("a", false))
	done := make(chan struct{})

	for range 8 {
		go func() {
			defer func() { done <- struct{}{} }()
			f.Folders(ctx)
			f.Summaries(ctx, "INBOX", 0, 10)
			f.SetFlag(ctx, "INBOX", []mail.UID{uid}, mail.FlagFlagged, true)
			f.Calls()
		}()
	}
	for range 8 {
		<-done
	}
}
