// Package mailtest provides an in-memory mail.Mailbox for tests.
package mailtest

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/dkouvdis/dk-email/internal/mail"
)

type Method string

const (
	MethodFolders   Method = "Folders"
	MethodSummaries Method = "Summaries"
	MethodMessage   Method = "Message"
	MethodSetFlag   Method = "SetFlag"
	MethodMove      Method = "Move"
)

// Call records one Mailbox call for assertions.
type Call struct {
	Method Method
	Folder string
	UIDs   []mail.UID
	Flag   mail.Flag
	On     bool
	To     string
}

type folder struct {
	info     mail.Folder
	messages []mail.Message // ascending UID
	nextUID  mail.UID
}

// Fake is an in-memory mail.Mailbox. It is safe for concurrent use.
type Fake struct {
	mu      sync.Mutex
	order   []string
	folders map[string]*folder
	delay   time.Duration
	errs    map[Method]error
	calls   []Call
	closed  bool
}

var _ mail.Mailbox = (*Fake)(nil)

func New() *Fake {
	return &Fake{folders: map[string]*folder{}, errs: map[Method]error{}}
}

// AddFolder adds an empty folder. Folders are listed in the order added.
func (f *Fake) AddFolder(name string, role mail.Role) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.folders[name]; ok {
		return
	}
	f.order = append(f.order, name)
	f.folders[name] = &folder{info: mail.Folder{Name: name, Delimiter: "/", Role: role}, nextUID: 1}
}

// AddMessage appends m to the folder, assigning and returning the next UID.
// It panics if the folder doesn't exist, since that's a test setup bug.
func (f *Fake) AddMessage(name string, m mail.Message) mail.UID {
	f.mu.Lock()
	defer f.mu.Unlock()
	fl, ok := f.folders[name]
	if !ok {
		panic(fmt.Sprintf("mailtest: AddMessage to unknown folder %q", name))
	}
	return fl.append(m)
}

// Messages returns a copy of the folder's current messages in UID order.
func (f *Fake) Messages(name string) []mail.Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	fl, ok := f.folders[name]
	if !ok {
		return nil
	}
	out := make([]mail.Message, len(fl.messages))
	for i, m := range fl.messages {
		out[i] = cloneMessage(m)
	}
	return out
}

// SetDelay makes every call wait d (or until its context is done) first.
func (f *Fake) SetDelay(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.delay = d
}

// Fail makes calls to method return err without changing state. A nil err clears it.
func (f *Fake) Fail(method Method, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err == nil {
		delete(f.errs, method)
		return
	}
	f.errs[method] = err
}

// Calls returns every call made so far, in order.
func (f *Fake) Calls() []Call {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Call, len(f.calls))
	for i, c := range f.calls {
		c.UIDs = slices.Clone(c.UIDs)
		out[i] = c
	}
	return out
}

func (f *Fake) Folders(ctx context.Context) ([]mail.Folder, error) {
	if err := f.begin(ctx, Call{Method: MethodFolders}); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]mail.Folder, 0, len(f.order))
	for _, name := range f.order {
		fl := f.folders[name]
		info := fl.info
		info.Total = len(fl.messages)
		info.Unread = 0
		for _, m := range fl.messages {
			if !m.Seen {
				info.Unread++
			}
		}
		out = append(out, info)
	}
	return out, nil
}

func (f *Fake) Summaries(ctx context.Context, name string, before mail.UID, limit int) ([]mail.Summary, error) {
	if err := f.begin(ctx, Call{Method: MethodSummaries, Folder: name}); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	fl, err := f.folder(name)
	if err != nil {
		return nil, err
	}
	var out []mail.Summary
	for i := len(fl.messages) - 1; i >= 0 && len(out) < limit; i-- {
		m := fl.messages[i]
		if before != 0 && m.UID >= before {
			continue
		}
		out = append(out, cloneMessage(m).Summary)
	}
	return out, nil
}

func (f *Fake) Message(ctx context.Context, name string, uid mail.UID) (mail.Message, error) {
	if err := f.begin(ctx, Call{Method: MethodMessage, Folder: name, UIDs: []mail.UID{uid}}); err != nil {
		return mail.Message{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	fl, err := f.folder(name)
	if err != nil {
		return mail.Message{}, err
	}
	for _, m := range fl.messages {
		if m.UID == uid {
			return cloneMessage(m), nil
		}
	}
	return mail.Message{}, fmt.Errorf("%s UID %d: %w", name, uid, mail.ErrMessageNotFound)
}

func (f *Fake) SetFlag(ctx context.Context, name string, uids []mail.UID, flag mail.Flag, on bool) error {
	if err := f.begin(ctx, Call{Method: MethodSetFlag, Folder: name, UIDs: slices.Clone(uids), Flag: flag, On: on}); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	fl, err := f.folder(name)
	if err != nil {
		return err
	}
	if flag != mail.FlagSeen && flag != mail.FlagFlagged {
		return fmt.Errorf("unsupported flag %s", flag)
	}
	for i := range fl.messages {
		m := &fl.messages[i]
		if !slices.Contains(uids, m.UID) {
			continue
		}
		if flag == mail.FlagSeen {
			m.Seen = on
		} else {
			m.Flagged = on
		}
	}
	return nil
}

// Move behaves like IMAP UID MOVE: moved messages get new UIDs in the
// destination, and UIDs that no longer exist are silently ignored.
func (f *Fake) Move(ctx context.Context, from string, uids []mail.UID, to string) error {
	if err := f.begin(ctx, Call{Method: MethodMove, Folder: from, UIDs: slices.Clone(uids), To: to}); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	src, err := f.folder(from)
	if err != nil {
		return err
	}
	dst, err := f.folder(to)
	if err != nil {
		return err
	}
	if src == dst {
		return fmt.Errorf("move %s: source and destination are the same folder", from)
	}
	kept := src.messages[:0]
	for _, m := range src.messages {
		if slices.Contains(uids, m.UID) {
			dst.append(m)
			continue
		}
		kept = append(kept, m)
	}
	src.messages = kept
	return nil
}

func (f *Fake) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

// begin records the call, waits for the configured delay without holding
// the lock, and returns any injected or closed error.
func (f *Fake) begin(ctx context.Context, c Call) error {
	f.mu.Lock()
	f.calls = append(f.calls, c)
	delay := f.delay
	f.mu.Unlock()

	if delay > 0 {
		t := time.NewTimer(delay)
		defer t.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return mail.ErrClosed
	}
	return f.errs[c.Method]
}

func (f *Fake) folder(name string) (*folder, error) {
	fl, ok := f.folders[name]
	if !ok {
		return nil, fmt.Errorf("%q: %w", name, mail.ErrFolderNotFound)
	}
	return fl, nil
}

func (fl *folder) append(m mail.Message) mail.UID {
	m = cloneMessage(m)
	m.UID = fl.nextUID
	fl.nextUID++
	fl.messages = append(fl.messages, m)
	return m.UID
}

func cloneMessage(m mail.Message) mail.Message {
	m.To = slices.Clone(m.To)
	m.References = slices.Clone(m.References)
	m.Cc = slices.Clone(m.Cc)
	m.Attachments = slices.Clone(m.Attachments)
	return m
}
