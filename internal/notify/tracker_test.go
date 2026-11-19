package notify

import (
	"testing"

	"github.com/dkouvdis/dk-email/internal/mail"
)

func inbox(next mail.UID, validity uint32) []mail.Folder {
	return []mail.Folder{
		{Name: "Sent", Role: mail.RoleSent, UIDNext: 99, UIDValidity: 7},
		{Name: "INBOX", Role: mail.RoleInbox, UIDNext: next, UIDValidity: validity},
	}
}

func TestTracker_Observe(t *testing.T) {
	type step struct {
		moved   int // Moved(1, moved) before observing, when non-zero
		folders []mail.Folder
		counted int // messages at or above Since, or -1 when not counted
		want    int
		since   mail.UID // Since(1) expected before observing, when non-zero
	}
	tests := []struct {
		name  string
		steps []step
	}{
		{"baseline returns 0", []step{{folders: inbox(10, 1), counted: -1, want: 0}}},
		{"delivered mail is counted", []step{
			{folders: inbox(10, 1), counted: -1},
			{folders: inbox(13, 1), counted: 3, want: 3, since: 10},
			{folders: inbox(13, 1), counted: 0, want: 0, since: 13},
		}},
		{"moved mail is subtracted", []step{
			{folders: inbox(10, 1), counted: -1},
			{moved: 2, folders: inbox(15, 1), counted: 5, want: 3},
		}},
		{"moved is clamped and reset", []step{
			{folders: inbox(10, 1), counted: -1},
			{moved: 5, folders: inbox(13, 1), counted: 3, want: 0},
			{folders: inbox(14, 1), counted: 1, want: 1},
		}},
		{"undone move is not subtracted", []step{
			{folders: inbox(10, 1), counted: -1},
			{moved: 2},
			{moved: -2, folders: inbox(11, 1), counted: 1, want: 1},
		}},
		{"a UID gap is not new mail", []step{
			{folders: inbox(10, 1), counted: -1},
			{moved: 1, folders: inbox(15, 1), counted: 1, want: 0},
		}},
		{"uncounted round keeps the baseline and moved", []step{
			{folders: inbox(10, 1), counted: -1},
			{moved: 1, folders: inbox(12, 1), counted: -1, want: 0, since: 10},
			{folders: inbox(14, 1), counted: 4, want: 3, since: 10},
		}},
		{"unchanged UIDNEXT keeps moved", []step{
			{folders: inbox(10, 1), counted: -1},
			{moved: 1, folders: inbox(10, 1), counted: -1, want: 0},
			{folders: inbox(12, 1), counted: 2, want: 1},
		}},
		{"UIDVALIDITY change rebaselines", []step{
			{folders: inbox(10, 1), counted: -1},
			{folders: inbox(50, 2), counted: -1, want: 0},
			{folders: inbox(52, 2), counted: 2, want: 2, since: 50},
		}},
		{"lower UIDNEXT rebaselines", []step{
			{folders: inbox(10, 1), counted: -1},
			{folders: inbox(5, 1), counted: -1, want: 0},
			{folders: inbox(6, 1), counted: 1, want: 1},
		}},
		{"missing INBOX returns 0 and drops the baseline", []step{
			{folders: inbox(10, 1), counted: -1},
			{folders: []mail.Folder{{Name: "Sent", Role: mail.RoleSent, UIDNext: 3, UIDValidity: 1}}, counted: -1, want: 0},
			{folders: inbox(20, 1), counted: -1, want: 0, since: 0},
			{folders: inbox(21, 1), counted: 1, want: 1},
		}},
		{"INBOX without UIDNEXT drops the baseline", []step{
			{folders: inbox(10, 1), counted: -1},
			{folders: inbox(0, 1), counted: -1, want: 0},
			{folders: inbox(20, 1), counted: -1, want: 0},
		}},
		{"INBOX without UIDVALIDITY drops the baseline", []step{
			{folders: inbox(10, 1), counted: -1},
			{folders: inbox(11, 0), counted: -1, want: 0},
			{folders: inbox(12, 1), counted: -1, want: 0},
		}},
		{"moves before the first observation still count", []step{
			{moved: 1},
			{folders: inbox(10, 1), counted: -1},
			{moved: 1, folders: inbox(12, 1), counted: 2, want: 1},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := NewTracker()
			for i, s := range tt.steps {
				if s.moved != 0 {
					tr.Moved(1, s.moved)
				}
				if s.folders == nil {
					continue
				}
				from := tr.Since(1)
				if s.since != 0 && from != s.since {
					t.Errorf("step %d: Since = %d, want %d", i, from, s.since)
				}
				got := tr.Observe(1, s.folders, from, s.counted)
				if got != s.want {
					t.Errorf("step %d: Observe = %d, want %d", i, got, s.want)
				}
			}
		})
	}
}

func TestTracker_FromMismatchKeepsBaseline(t *testing.T) {
	tr := NewTracker()
	tr.Observe(1, inbox(10, 1), 0, -1)

	if got := tr.Observe(1, inbox(12, 1), 11, 1); got != 0 {
		t.Errorf("mismatched from = %d, want 0", got)
	}
	if got := tr.Since(1); got != 10 {
		t.Errorf("Since = %d, want the old baseline 10", got)
	}
	if got := tr.Observe(1, inbox(12, 1), 10, 2); got != 2 {
		t.Errorf("next round = %d, want 2", got)
	}
}

func TestTracker_AccountsAreIndependent(t *testing.T) {
	tr := NewTracker()
	tr.Observe(1, inbox(10, 1), 0, -1)
	tr.Observe(2, inbox(100, 1), 0, -1)

	if got := tr.Observe(1, inbox(12, 1), 10, 2); got != 2 {
		t.Errorf("account 1 = %d, want 2", got)
	}
	if got := tr.Observe(2, inbox(100, 1), 100, 0); got != 0 {
		t.Errorf("account 2 = %d, want 0", got)
	}
}

func TestTracker_ForgetStartsOver(t *testing.T) {
	tr := NewTracker()
	tr.Observe(1, inbox(10, 1), 0, -1)

	tr.Forget(1)

	if got := tr.Observe(1, inbox(15, 1), 0, -1); got != 0 {
		t.Errorf("after Forget = %d, want 0 (a new baseline)", got)
	}
}
