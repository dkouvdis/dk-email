package mailtest

import (
	"context"
	"errors"
	"slices"
	"sync"
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

func TestSetFlag_AnsweredIsAcceptedWithoutChangingOtherFlags(t *testing.T) {
	f := inbox()
	uid := f.AddMessage("INBOX", msg("a", false))

	if err := f.SetFlag(ctx, "INBOX", []mail.UID{uid}, mail.FlagAnswered, true); err != nil {
		t.Fatal(err)
	}

	if got := f.Messages("INBOX")[0]; got.Seen || got.Flagged {
		t.Errorf("message = seen:%v flagged:%v, want both false", got.Seen, got.Flagged)
	}
	calls := f.Calls()
	if last := calls[len(calls)-1]; last.Flag != mail.FlagAnswered || !last.On {
		t.Errorf("last call = %+v, want FlagAnswered on", last)
	}
}

func TestSender_RecordsDraftsInOrder(t *testing.T) {
	s := &Sender{}
	to := []mail.Address{{Email: "a@example.com"}}
	ref := &mail.MessageRef{Folder: "INBOX", UID: 7}

	if err := s.Send(ctx, mail.Draft{Subject: "one", To: to, ReplyTo: ref}); err != nil {
		t.Fatal(err)
	}
	if err := s.Send(ctx, mail.Draft{Subject: "two"}); err != nil {
		t.Fatal(err)
	}
	to[0].Email = "mutated@example.com"
	ref.UID = 99

	got := s.Sent()
	if len(got) != 2 || got[0].Subject != "one" || got[1].Subject != "two" {
		t.Fatalf("Sent() = %+v, want drafts one, two", got)
	}
	if got[0].To[0].Email != "a@example.com" || got[0].ReplyTo.UID != 7 {
		t.Errorf("recorded draft aliases caller data: %+v", got[0])
	}
	got[0].To[0].Email = "x"
	if s.Sent()[0].To[0].Email != "a@example.com" {
		t.Error("Sent() returned aliased data")
	}
}

func TestSender_ScriptedErrorsPerCall(t *testing.T) {
	errA := errors.New("a")
	s := &Sender{}
	s.FailWith(nil, errA, nil)

	var got []error
	for range 4 {
		got = append(got, s.Send(ctx, mail.Draft{}))
	}

	want := []error{nil, errA, nil, nil}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("call %d error = %v, want %v", i, got[i], want[i])
		}
	}
	if n := len(s.Sent()); n != 4 {
		t.Errorf("recorded %d drafts, want 4 (failed sends are recorded)", n)
	}
}

func TestSender_HonoursContextCancellation(t *testing.T) {
	s := &Sender{}
	c, cancel := context.WithCancel(ctx)
	cancel()

	if err := s.Send(c, mail.Draft{}); !errors.Is(err, context.Canceled) {
		t.Errorf("Send() error = %v, want context.Canceled", err)
	}
}

func TestSender_IsSafeForConcurrentUse(t *testing.T) {
	s := &Sender{}
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = s.Send(ctx, mail.Draft{})
			_ = s.Sent()
		}()
	}
	wg.Wait()

	if n := len(s.Sent()); n != 20 {
		t.Errorf("recorded %d drafts, want 20", n)
	}
}

func TestDraftSender_RecordsSavesAndServesStoredDrafts(t *testing.T) {
	errA := errors.New("a")
	s := &DraftSender{}
	s.FailSaveWith(nil, errA)
	ref := mail.MessageRef{Folder: "Drafts", UID: 7}
	s.AddDraft(ref, mail.Draft{Subject: "Hi"})

	if err := s.SaveDraft(ctx, mail.Draft{Subject: "one"}); err != nil {
		t.Errorf("first save = %v", err)
	}
	if err := s.SaveDraft(ctx, mail.Draft{Subject: "two", DraftOf: &ref}); err != errA {
		t.Errorf("second save = %v, want the scripted error", err)
	}
	got := s.Saved()
	if len(got) != 2 || got[1].Subject != "two" || got[1].DraftOf == nil || *got[1].DraftOf != ref || len(s.Sent()) != 0 {
		t.Errorf("saved = %+v", got)
	}

	d, err := s.OpenDraft(ctx, ref)
	if err != nil || d.Subject != "Hi" || d.DraftOf == nil || *d.DraftOf != ref {
		t.Errorf("OpenDraft = %+v, %v", d, err)
	}
	if _, err := s.OpenDraft(ctx, mail.MessageRef{Folder: "Drafts", UID: 8}); err == nil {
		t.Error("OpenDraft of an unknown draft succeeded")
	}
}

func searchFake() *Fake {
	f := inbox()
	day := func(d int) time.Time { return time.Date(2026, 11, d, 23, 30, 0, 0, time.UTC) }
	f.AddMessage("INBOX", mail.Message{
		Summary: mail.Summary{
			From: mail.Address{Name: "Alice", Email: "alice@example.com"}, To: []mail.Address{{Email: "bob@example.com"}},
			Subject: "Lunch plans", Date: day(1), Seen: true,
		},
		Cc: []mail.Address{{Email: "carol@example.com"}}, Body: "pizza at noon",
	})
	f.AddMessage("INBOX", mail.Message{
		Summary: mail.Summary{
			From: mail.Address{Email: "dave@example.com"}, To: []mail.Address{{Email: "alice@example.com"}},
			Subject: "Invoice", Date: day(5), Flagged: true,
		},
		Body: "please pay",
	})
	f.AddMessage("INBOX", mail.Message{
		Summary: mail.Summary{
			From: mail.Address{Email: "erin@example.com"}, To: []mail.Address{{Email: "bob@example.com"}},
			Subject: "Re: Lunch plans", Date: day(9),
		},
		Body: "sounds good",
	})
	return f
}

func TestSearch_MatchesEachKind(t *testing.T) {
	f := searchFake()
	d := func(day int) time.Time { return time.Date(2026, 11, day, 0, 0, 0, 0, time.UTC) }
	tests := []struct {
		name string
		term mail.SearchTerm
		want []mail.UID
	}{
		{"text in subject", mail.SearchTerm{Kind: mail.SearchText, Value: "LUNCH"}, []mail.UID{3, 1}},
		{"text in body", mail.SearchTerm{Kind: mail.SearchText, Value: "pizza"}, []mail.UID{1}},
		{"text in cc", mail.SearchTerm{Kind: mail.SearchText, Value: "carol"}, []mail.UID{1}},
		{"text in from", mail.SearchTerm{Kind: mail.SearchText, Value: "dave"}, []mail.UID{2}},
		{"text in to", mail.SearchTerm{Kind: mail.SearchText, Value: "bob@"}, []mail.UID{3, 1}},
		{"from name", mail.SearchTerm{Kind: mail.SearchFrom, Value: "alice"}, []mail.UID{1}},
		{"to", mail.SearchTerm{Kind: mail.SearchTo, Value: "alice"}, []mail.UID{2}},
		{"cc", mail.SearchTerm{Kind: mail.SearchCc, Value: "carol"}, []mail.UID{1}},
		{"subject", mail.SearchTerm{Kind: mail.SearchSubject, Value: "invoice"}, []mail.UID{2}},
		{"subject ignores body", mail.SearchTerm{Kind: mail.SearchSubject, Value: "pizza"}, nil},
		{"body", mail.SearchTerm{Kind: mail.SearchBody, Value: "noon"}, []mail.UID{1}},
		{"body ignores subject", mail.SearchTerm{Kind: mail.SearchBody, Value: "invoice"}, nil},
		{"unread", mail.SearchTerm{Kind: mail.SearchUnread}, []mail.UID{3, 2}},
		{"read", mail.SearchTerm{Kind: mail.SearchRead}, []mail.UID{1}},
		{"starred", mail.SearchTerm{Kind: mail.SearchStarred}, []mail.UID{2}},
		{"since is inclusive", mail.SearchTerm{Kind: mail.SearchSince, Date: d(5)}, []mail.UID{3, 2}},
		{"before is exclusive", mail.SearchTerm{Kind: mail.SearchBefore, Date: d(5)}, []mail.UID{1}},
		{"negated field", mail.SearchTerm{Kind: mail.SearchSubject, Value: "lunch", Not: true}, []mail.UID{2}},
		{"negated flag", mail.SearchTerm{Kind: mail.SearchUnread, Not: true}, []mail.UID{1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, total, err := f.Search(ctx, "INBOX", mail.SearchQuery{Terms: []mail.SearchTerm{tt.term}}, 0, 10)

			if err != nil {
				t.Fatalf("Search() error = %v", err)
			}
			if !slices.Equal(uids(got), tt.want) && !(len(got) == 0 && len(tt.want) == 0) {
				t.Errorf("UIDs = %v, want %v", uids(got), tt.want)
			}
			if total != len(tt.want) {
				t.Errorf("total = %d, want %d", total, len(tt.want))
			}
		})
	}
}

func TestSearch_DateIgnoresTimeOfDayAndZone(t *testing.T) {
	f := inbox()
	zone := time.FixedZone("x", 5*3600)
	// 02:00 on Nov 6 in UTC+5 is 21:00 on Nov 5 UTC.
	f.AddMessage("INBOX", mail.Message{Summary: mail.Summary{Subject: "a", Date: time.Date(2026, 11, 6, 2, 0, 0, 0, zone)}})
	q := mail.SearchQuery{Terms: []mail.SearchTerm{{Kind: mail.SearchBefore, Date: time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)}}}

	got, total, err := f.Search(ctx, "INBOX", q, 0, 10)

	if err != nil || len(got) != 1 || total != 1 {
		t.Errorf("Search() = %v, %d, %v; want the message (Nov 5 UTC)", uids(got), total, err)
	}
}

func TestSearch_TermsAreAnded(t *testing.T) {
	f := searchFake()
	q := mail.SearchQuery{Terms: []mail.SearchTerm{
		{Kind: mail.SearchSubject, Value: "lunch"},
		{Kind: mail.SearchUnread},
	}}

	got, total, err := f.Search(ctx, "INBOX", q, 0, 10)

	if err != nil || !slices.Equal(uids(got), []mail.UID{3}) || total != 1 {
		t.Errorf("Search() = %v, %d, %v; want [3], 1", uids(got), total, err)
	}
}

func TestSearch_NoTermsMatchesEverything(t *testing.T) {
	f := searchFake()

	got, total, err := f.Search(ctx, "INBOX", mail.SearchQuery{}, 0, 10)

	if err != nil || len(got) != 3 || total != 3 {
		t.Errorf("Search() = %v, %d, %v; want all 3", uids(got), total, err)
	}
}

func TestSearch_PagesNewestFirstWithTotal(t *testing.T) {
	f := inbox()
	for i := 0; i < 7; i++ {
		f.AddMessage("INBOX", msg("hit", false))
	}
	q := mail.SearchQuery{Terms: []mail.SearchTerm{{Kind: mail.SearchSubject, Value: "hit"}}}

	first, total, err := f.Search(ctx, "INBOX", q, 0, 3)
	if err != nil || !slices.Equal(uids(first), []mail.UID{7, 6, 5}) || total != 7 {
		t.Fatalf("first page = %v, %d, %v; want [7 6 5], 7", uids(first), total, err)
	}
	second, total, err := f.Search(ctx, "INBOX", q, 5, 3)
	if err != nil || !slices.Equal(uids(second), []mail.UID{4, 3, 2}) || total != 4 {
		t.Fatalf("second page = %v, %d, %v; want [4 3 2], 4", uids(second), total, err)
	}
	none, total, err := f.Search(ctx, "INBOX", q, 1, 3)
	if err != nil || len(none) != 0 || total != 0 {
		t.Errorf("below UID 1 = %v, %d, %v; want empty", uids(none), total, err)
	}
	zero, total, err := f.Search(ctx, "INBOX", q, 0, 0)
	if err != nil || zero != nil || total != 0 {
		t.Errorf("limit 0 = %v, %d, %v; want nil, 0, nil", zero, total, err)
	}
}

func TestSearch_RecordsCallAndHonorsFail(t *testing.T) {
	f := searchFake()
	q := mail.SearchQuery{Raw: "from:alice", Terms: []mail.SearchTerm{{Kind: mail.SearchFrom, Value: "alice"}}}
	if _, _, err := f.Search(ctx, "INBOX", q, 0, 10); err != nil {
		t.Fatal(err)
	}
	boom := errors.New("boom")
	f.Fail(MethodSearch, boom)

	got, total, err := f.Search(ctx, "INBOX", q, 0, 10)

	if !errors.Is(err, boom) || got != nil || total != 0 {
		t.Errorf("Search() = %v, %d, %v; want nil, 0, boom", got, total, err)
	}
	calls := f.Calls()
	want := Call{Method: MethodSearch, Folder: "INBOX", Query: "from:alice"}
	if len(calls) != 2 || calls[0].Method != want.Method || calls[0].Folder != want.Folder || calls[0].Query != want.Query {
		t.Errorf("Calls() = %+v, want %+v first", calls, want)
	}
}

func TestSearch_UnknownFolder(t *testing.T) {
	_, _, err := searchFake().Search(ctx, "Nope", mail.SearchQuery{}, 0, 10)

	if !errors.Is(err, mail.ErrFolderNotFound) {
		t.Errorf("error = %v, want ErrFolderNotFound", err)
	}
}
