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

// Observe records an account's folders from the server and returns how many
// messages its INBOX gained since the last observation, less the ones dk-email
// moved there (see Moved). The first observation only sets the baseline.
func (t *Tracker) Observe(id int, folders []mail.Folder) int {
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
	moved := t.moved[id]
	t.base[id] = cur
	delete(t.moved, id)
	if !ok || old.validity != cur.validity || cur.next < old.next {
		return 0
	}
	return max(int(cur.next-old.next)-moved, 0)
}

// Moved records n messages dk-email moved into the account's INBOX, so the
// UIDNEXT rise they cause isn't counted as new mail. A negative n undoes a
// failed move; the total never goes below 0.
func (t *Tracker) Moved(id, n int) {
	t.moved[id] = max(t.moved[id]+n, 0)
}

// Forget drops an account.
func (t *Tracker) Forget(id int) {
	delete(t.base, id)
	delete(t.moved, id)
}
