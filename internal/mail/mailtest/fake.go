// Package mailtest provides an in-memory mail.Mailbox and a recording
// mail.Sender for tests.
package mailtest

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/dkouvdis/dk-email/internal/mail"
	"github.com/dkouvdis/dk-email/internal/render"
)

type Method string

const (
	MethodFolders   Method = "Folders"
	MethodSummaries Method = "Summaries"
	MethodSearch    Method = "Search"
	MethodCountFrom Method = "CountFrom"
	MethodMessage   Method = "Message"
	MethodRaw       Method = "Raw"
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
	Query  string // Search's raw query
}

type folder struct {
	info     mail.Folder
	messages []mail.Message      // ascending UID
	raw      map[mail.UID][]byte // exact bytes of messages added with AddRaw
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

// AddRaw parses raw as an RFC 5322 message and appends it like AddMessage,
// keeping a copy of the bytes for Raw. It panics if the folder doesn't exist
// or raw doesn't parse, since that's a test setup bug.
func (f *Fake) AddRaw(name string, raw []byte) mail.UID {
	m, err := render.Parse(bytes.NewReader(raw))
	if err != nil {
		panic(fmt.Sprintf("mailtest: AddRaw parse: %v", err))
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	fl, ok := f.folders[name]
	if !ok {
		panic(fmt.Sprintf("mailtest: AddRaw to unknown folder %q", name))
	}
	uid := fl.append(m)
	if fl.raw == nil {
		fl.raw = map[mail.UID][]byte{}
	}
	fl.raw[uid] = slices.Clone(raw)
	return uid
}

// SkipUIDs raises the folder's next UID by n without adding messages, like a
// server that leaves gaps in its UIDs. It is a test aid and panics if the
// folder doesn't exist.
func (f *Fake) SkipUIDs(name string, n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fl, ok := f.folders[name]
	if !ok {
		panic(fmt.Sprintf("mailtest: SkipUIDs on unknown folder %q", name))
	}
	fl.nextUID += mail.UID(n)
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
		info.UIDNext, info.UIDValidity = fl.nextUID, 1
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

// Search matches case-insensitive substrings and flags in memory. Dates
// compare the header Date by UTC calendar day, not the server's received
// date, so don't write IMAP date-boundary tests against the Fake.
func (f *Fake) Search(ctx context.Context, name string, q mail.SearchQuery, before mail.UID, limit int) ([]mail.Summary, int, error) {
	if err := f.begin(ctx, Call{Method: MethodSearch, Folder: name, Query: q.Raw}); err != nil {
		return nil, 0, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	fl, err := f.folder(name)
	if err != nil {
		return nil, 0, err
	}
	if limit <= 0 {
		return nil, 0, nil
	}
	var out []mail.Summary
	total := 0
	for i := len(fl.messages) - 1; i >= 0; i-- {
		m := fl.messages[i]
		if before != 0 && m.UID >= before {
			continue
		}
		if !matchesAll(m, q.Terms) {
			continue
		}
		total++
		if len(out) < limit {
			out = append(out, cloneMessage(m).Summary)
		}
	}
	return out, total, nil
}

// CountFrom counts the folder's messages with a UID of at least uid.
func (f *Fake) CountFrom(ctx context.Context, name string, uid mail.UID) (int, error) {
	if err := f.begin(ctx, Call{Method: MethodCountFrom, Folder: name, UIDs: []mail.UID{uid}}); err != nil {
		return 0, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	fl, err := f.folder(name)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, m := range fl.messages {
		if m.UID >= uid {
			n++
		}
	}
	return n, nil
}

func matchesAll(m mail.Message, terms []mail.SearchTerm) bool {
	for _, t := range terms {
		if matchesTerm(m, t) == t.Not {
			return false
		}
	}
	return true
}

func matchesTerm(m mail.Message, t mail.SearchTerm) bool {
	has := func(fields ...string) bool {
		needle := strings.ToLower(t.Value)
		for _, f := range fields {
			if strings.Contains(strings.ToLower(f), needle) {
				return true
			}
		}
		return false
	}
	addrs := func(as ...[]mail.Address) []string {
		var out []string
		for _, list := range as {
			for _, a := range list {
				out = append(out, a.String())
			}
		}
		return out
	}
	switch t.Kind {
	case mail.SearchText:
		fields := append(addrs([]mail.Address{m.From}, m.To, m.Cc), m.Subject, m.Body)
		return has(fields...)
	case mail.SearchFrom:
		return has(m.From.String())
	case mail.SearchTo:
		return has(addrs(m.To)...)
	case mail.SearchCc:
		return has(addrs(m.Cc)...)
	case mail.SearchSubject:
		return has(m.Subject)
	case mail.SearchBody:
		return has(m.Body)
	case mail.SearchUnread:
		return !m.Seen
	case mail.SearchRead:
		return m.Seen
	case mail.SearchStarred:
		return m.Flagged
	case mail.SearchSince:
		return !messageDay(m).Before(t.Date)
	case mail.SearchBefore:
		return messageDay(m).Before(t.Date)
	}
	return true
}

func messageDay(m mail.Message) time.Time {
	return m.Date.UTC().Truncate(24 * time.Hour)
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

// Raw returns a copy of the bytes stored by AddRaw.
func (f *Fake) Raw(ctx context.Context, name string, uid mail.UID) ([]byte, error) {
	if err := f.begin(ctx, Call{Method: MethodRaw, Folder: name, UIDs: []mail.UID{uid}}); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	fl, err := f.folder(name)
	if err != nil {
		return nil, err
	}
	for _, m := range fl.messages {
		if m.UID == uid {
			raw, ok := fl.raw[uid]
			if !ok {
				return nil, fmt.Errorf("%s UID %d has no raw bytes (use AddRaw): %w", name, uid, mail.ErrMessageNotFound)
			}
			return slices.Clone(raw), nil
		}
	}
	return nil, fmt.Errorf("%s UID %d: %w", name, uid, mail.ErrMessageNotFound)
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
	if flag != mail.FlagSeen && flag != mail.FlagFlagged && flag != mail.FlagAnswered {
		return fmt.Errorf("unsupported flag %s", flag)
	}
	for i := range fl.messages {
		m := &fl.messages[i]
		if !slices.Contains(uids, m.UID) {
			continue
		}
		// Messages don't model \Answered; the call is only recorded.
		switch flag {
		case mail.FlagSeen:
			m.Seen = on
		case mail.FlagFlagged:
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
			raw, hasRaw := src.raw[m.UID]
			delete(src.raw, m.UID)
			newUID := dst.append(m)
			if hasRaw {
				if dst.raw == nil {
					dst.raw = map[mail.UID][]byte{}
				}
				dst.raw[newUID] = raw
			}
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

// Sender is a fake mail.Sender that records drafts. It is safe for concurrent use.
type Sender struct {
	mu    sync.Mutex
	sent  []mail.Draft
	calls int
	errs  []error
}

var _ mail.Sender = (*Sender)(nil)

// FailWith scripts errors by call number: call n returns errs[n] (0-based,
// counted from the first call ever made). A nil or missing entry succeeds.
// It replaces any earlier script.
func (s *Sender) FailWith(errs ...error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.errs = slices.Clone(errs)
}

// Sent returns a copy of every draft passed to Send, in order, including
// ones that were answered with an error.
func (s *Sender) Sent() []mail.Draft {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]mail.Draft, len(s.sent))
	for i, d := range s.sent {
		out[i] = cloneDraft(d)
	}
	return out
}

// Send records d, then returns ctx.Err() if ctx is done, else the scripted
// error for this call.
func (s *Sender) Send(ctx context.Context, d mail.Draft) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sent = append(s.sent, cloneDraft(d))
	n := s.calls
	s.calls++
	if err := ctx.Err(); err != nil {
		return err
	}
	if n < len(s.errs) {
		return s.errs[n]
	}
	return nil
}

// DraftSender is a Sender that is also a mail.Drafter: it records saved
// drafts and serves the ones registered with AddDraft. Use a plain Sender
// for an account that can't save drafts.
type DraftSender struct {
	Sender
	saved     []mail.Draft
	saveCalls int
	saveErrs  []error
	stored    map[mail.MessageRef]mail.Draft
}

var (
	_ mail.Sender  = (*DraftSender)(nil)
	_ mail.Drafter = (*DraftSender)(nil)
)

// FailSaveWith scripts SaveDraft errors by call number like Sender.FailWith.
func (s *DraftSender) FailSaveWith(errs ...error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.saveErrs = slices.Clone(errs)
}

// Saved returns a copy of every draft passed to SaveDraft, in order,
// including ones that were answered with an error.
func (s *DraftSender) Saved() []mail.Draft {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]mail.Draft, len(s.saved))
	for i, d := range s.saved {
		out[i] = cloneDraft(d)
	}
	return out
}

// SaveDraft records d, then returns ctx.Err() if ctx is done, else the
// scripted error for this call.
func (s *DraftSender) SaveDraft(ctx context.Context, d mail.Draft) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.saved = append(s.saved, cloneDraft(d))
	n := s.saveCalls
	s.saveCalls++
	if err := ctx.Err(); err != nil {
		return err
	}
	if n < len(s.saveErrs) {
		return s.saveErrs[n]
	}
	return nil
}

// AddDraft makes OpenDraft(ref) return d with DraftOf set to ref.
func (s *DraftSender) AddDraft(ref mail.MessageRef, d mail.Draft) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stored == nil {
		s.stored = map[mail.MessageRef]mail.Draft{}
	}
	s.stored[ref] = cloneDraft(d)
}

// OpenDraft returns the draft registered for ref, or an error if there is none.
func (s *DraftSender) OpenDraft(ctx context.Context, ref mail.MessageRef) (mail.Draft, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return mail.Draft{}, err
	}
	d, ok := s.stored[ref]
	if !ok {
		return mail.Draft{}, fmt.Errorf("mailtest: no draft %v", ref)
	}
	d = cloneDraft(d)
	d.DraftOf = &ref
	return d, nil
}

func cloneDraft(d mail.Draft) mail.Draft {
	d.To = slices.Clone(d.To)
	d.Cc = slices.Clone(d.Cc)
	d.Bcc = slices.Clone(d.Bcc)
	d.References = slices.Clone(d.References)
	if d.ReplyTo != nil {
		ref := *d.ReplyTo
		d.ReplyTo = &ref
	}
	if d.DraftOf != nil {
		ref := *d.DraftOf
		d.DraftOf = &ref
	}
	if d.Attachments != nil {
		files := make([]mail.File, len(d.Attachments))
		for i, f := range d.Attachments {
			f.Data = slices.Clone(f.Data)
			files[i] = f
		}
		d.Attachments = files
	}
	return d
}
