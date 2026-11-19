// Package notify decides when new mail is announced and builds the
// announcement. It holds no I/O.
package notify

import "github.com/dkouvdis/dk-email/internal/mail"

type baseline struct {
	next     mail.UID
	validity uint32
}

// Tracker remembers each account's INBOX UIDNEXT and counts new mail from it.
// Accounts are keyed by an id that is never reused (ui.Model.ids). The zero
// value is not usable; call NewTracker.
type Tracker struct {
	base  map[int]baseline
	moved map[int]int
}

func NewTracker() *Tracker {
	return &Tracker{base: map[int]baseline{}, moved: map[int]int{}}
}

// Since reports the INBOX UIDNEXT an account's next count must start from,
// or 0 when it has no baseline yet.
func (t *Tracker) Since(id int) mail.UID {
	return t.base[id].next
}

// Observe records an account's folders from the server and returns how many
// messages arrived in its INBOX since the last observation, less the ones
// dk-email moved there (see Moved). counted is how many unread INBOX messages
// have a UID of at least from (see mail.Mailbox.UnreadFrom), or -1 when they weren't
// counted. The first observation only sets the baseline.
//
// A UIDNEXT rise only triggers a count: servers may leave gaps in UIDs, so
// the rise overstates the arrivals. Without a count starting at the baseline
// nothing changes, and the next round counts the whole gap.
func (t *Tracker) Observe(id int, folders []mail.Folder, from mail.UID, counted int) int {
	var inbox *mail.Folder
	for i := range folders {
		if folders[i].Role == mail.RoleInbox {
			inbox = &folders[i]
			break
		}
	}
	if inbox == nil || inbox.UIDNext == 0 || inbox.UIDValidity == 0 {
		t.Forget(id)
		return 0
	}
	cur := baseline{next: inbox.UIDNext, validity: inbox.UIDValidity}
	old, ok := t.base[id]
	if !ok || old.validity != cur.validity || cur.next < old.next {
		t.base[id] = cur
		delete(t.moved, id)
		return 0
	}
	if cur.next == old.next {
		return 0
	}
	if counted < 0 || from != old.next {
		return 0
	}
	n := max(counted-t.moved[id], 0)
	t.base[id] = cur
	delete(t.moved, id)
	return n
}

// Moved records n unread messages dk-email moved into the account's INBOX, so
// the UIDNEXT rise they cause isn't counted as new mail. A negative n undoes a
// failed move; the total never goes below 0.
func (t *Tracker) Moved(id, n int) {
	t.moved[id] = max(t.moved[id]+n, 0)
}

// Forget drops an account.
func (t *Tracker) Forget(id int) {
	delete(t.base, id)
	delete(t.moved, id)
}
