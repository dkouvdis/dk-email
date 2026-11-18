// Package send implements mail.Sender: it builds a draft into RFC 5322 bytes,
// delivers them over SMTP, saves a copy to Sent, marks a replied-to message
// answered and removes the saved draft it came from. A failure before or
// during delivery means nothing was sent; a failure afterwards is reported as
// mail.ErrAfterDelivery. It also implements mail.Drafter: saving a draft to
// the Drafts folder and reopening it.
package send

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/dkouvdis/dk-email/internal/compose"
	"github.com/dkouvdis/dk-email/internal/config"
	"github.com/dkouvdis/dk-email/internal/mail"
	"github.com/dkouvdis/dk-email/internal/render"
)

// Transport delivers raw message bytes; *smtp.Transport satisfies it.
type Transport interface {
	Send(ctx context.Context, from string, rcpts []string, msg []byte) error
}

// Store is the IMAP access sending needs; *imap.Client satisfies it.
type Store interface {
	Raw(ctx context.Context, folder string, uid mail.UID) ([]byte, error)
	Append(ctx context.Context, folder string, raw []byte, flags ...mail.Flag) error
	SentFolder(ctx context.Context) (name string, ok bool, err error)
	SetFlag(ctx context.Context, folder string, uids []mail.UID, f mail.Flag, on bool) error
	DraftsFolder(ctx context.Context) (name string, ok bool, err error)
	Delete(ctx context.Context, folder string, uids []mail.UID) error
}

var (
	_ mail.Sender  = (*Sender)(nil)
	_ mail.Drafter = (*Sender)(nil)
)

// Sender implements mail.Sender for one account.
type Sender struct {
	acct    config.Account
	t       Transport
	st      Store
	log     *slog.Logger
	now     func() time.Time
	newID   func(mail.Address) string
	extract func(raw []byte) ([]compose.Attachment, error)
}

// New returns a Sender for acct. A nil log discards output. Send reports an
// error if t or st is nil.
func New(acct config.Account, t Transport, st Store, log *slog.Logger) *Sender {
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Sender{
		acct:    acct,
		t:       t,
		st:      st,
		log:     log,
		now:     time.Now,
		newID:   compose.MessageID,
		extract: extractAttachments,
	}
}

// extractAttachments copies the attachments of a raw message, with the same
// rules the reader uses to list them.
func extractAttachments(raw []byte) ([]compose.Attachment, error) {
	parts, err := render.Parts(raw)
	if err != nil {
		return nil, err
	}
	atts := make([]compose.Attachment, len(parts))
	for i, p := range parts {
		atts[i] = compose.Attachment{Filename: p.Filename, ContentType: p.ContentType, Data: p.Data}
	}
	return atts, nil
}

// afterDeliveryError reports that the message was delivered but follow-up
// steps failed. Its text reads as one sentence for the status line, e.g.
// "sent, but couldn't save a copy to Sent: <cause>"; it matches
// mail.ErrAfterDelivery and each underlying cause with errors.Is.
type afterDeliveryError struct {
	parts  []string
	causes []error
}

func (e *afterDeliveryError) Error() string {
	return "sent, but " + strings.Join(e.parts, "; and ")
}

func (e *afterDeliveryError) Unwrap() []error {
	return append([]error{mail.ErrAfterDelivery}, e.causes...)
}

func (e *afterDeliveryError) add(part string, cause error) {
	if cause != nil {
		part += ": " + cause.Error()
	}
	e.parts = append(e.parts, part)
	if cause != nil {
		e.causes = append(e.causes, cause)
	}
}

// draftSavedError reports that the new draft was saved but the copy it
// replaces couldn't be removed. It matches mail.ErrDraftSaved and the cause
// with errors.Is.
type draftSavedError struct{ cause error }

func (e *draftSavedError) Error() string {
	return "draft saved, but couldn't remove the old copy: " + e.cause.Error()
}

func (e *draftSavedError) Unwrap() []error { return []error{mail.ErrDraftSaved, e.cause} }

// Send delivers d. The steps run in order: fetch forwarded attachments, build
// the message once, send it over SMTP, save it to Sent (when the account
// keeps copies), mark the replied-to message answered, and remove the saved
// draft d.DraftOf. Errors from the first three steps mean nothing was sent.
// Errors from the rest are joined into one error matching
// mail.ErrAfterDelivery, and each later step is still attempted when an
// earlier one failed. The draft is removed last, once the message is out.
//
// An empty d.From.Email falls back to the account email and from_name.
func (s *Sender) Send(ctx context.Context, d mail.Draft) error {
	if s.t == nil || s.st == nil {
		return errors.New("send: sending isn't set up for this account")
	}
	rcpts := recipients(d)
	if len(rcpts) == 0 {
		return errors.New("send: no recipients")
	}
	if d.From.Email == "" {
		d.From = mail.Address{Name: s.acct.FromName, Email: s.acct.Email}
	}

	atts, err := s.forwardedAttachments(ctx, d)
	if err != nil {
		return err
	}

	done := s.step("build")
	raw, err := compose.Build(d, atts, s.now(), s.newID(d.From))
	done(len(raw), len(rcpts))
	if err != nil {
		return fmt.Errorf("building the message: %w", err)
	}

	if err := ctx.Err(); err != nil {
		return err
	}
	done = s.step("smtp")
	err = s.t.Send(ctx, s.acct.Email, rcpts, raw)
	done(len(raw), len(rcpts))
	if err != nil {
		return fmt.Errorf("sending: %w", err)
	}

	var after afterDeliveryError
	if s.acct.SaveSentCopy() {
		if err := s.saveSent(ctx, raw); err != nil {
			after.add(err.part, err.cause)
		}
	}
	if d.ReplyTo != nil {
		done := s.step("mark-answered")
		err := s.st.SetFlag(ctx, d.ReplyTo.Folder, []mail.UID{d.ReplyTo.UID}, mail.FlagAnswered, true)
		done(0, 0)
		if err != nil {
			after.add("couldn't mark the original answered", err)
		}
	}
	if d.DraftOf != nil {
		if err := s.deleteDraft(ctx, *d.DraftOf); err != nil {
			after.add("couldn't remove the draft", err)
		}
	}
	if len(after.parts) > 0 {
		return &after
	}
	return nil
}

// forwardedAttachments fetches the message d.ForwardOf points at and copies
// its attachments; it returns nil when d forwards nothing.
func (s *Sender) forwardedAttachments(ctx context.Context, d mail.Draft) ([]compose.Attachment, error) {
	if d.ForwardOf == nil {
		return nil, nil
	}
	done := s.step("fetch-original")
	raw, err := s.st.Raw(ctx, d.ForwardOf.Folder, d.ForwardOf.UID)
	done(len(raw), 0)
	if err != nil {
		return nil, fmt.Errorf("fetching the original to forward: %w", err)
	}
	atts, err := s.extract(raw)
	if err != nil {
		return nil, fmt.Errorf("copying attachments from the original: %w", err)
	}
	return atts, nil
}

func (s *Sender) deleteDraft(ctx context.Context, ref mail.MessageRef) error {
	done := s.step("delete-draft")
	defer func() { done(0, 0) }()
	return s.st.Delete(ctx, ref.Folder, []mail.UID{ref.UID})
}

// SaveDraft stores d in the Drafts folder, flagged \Draft and \Seen, with
// its Bcc recipients kept. It needs no recipients. The steps run in order:
// fetch forwarded attachments, build the message, find the Drafts folder,
// append, and remove the copy d.DraftOf replaces. An error from the first
// four steps means nothing was saved; a failure to remove the old copy is
// returned wrapping mail.ErrDraftSaved and its cause.
//
// An empty d.From.Email falls back to the account email and from_name.
func (s *Sender) SaveDraft(ctx context.Context, d mail.Draft) error {
	if s.st == nil {
		return errors.New("save draft: saving isn't set up for this account")
	}
	if d.From.Email == "" {
		d.From = mail.Address{Name: s.acct.FromName, Email: s.acct.Email}
	}
	atts, err := s.forwardedAttachments(ctx, d)
	if err != nil {
		return err
	}

	done := s.step("build-draft")
	raw, err := compose.BuildDraft(d, atts, s.now(), s.newID(d.From))
	done(len(raw), 0)
	if err != nil {
		return fmt.Errorf("building the draft: %w", err)
	}

	done = s.step("save-draft")
	name, ok, err := s.st.DraftsFolder(ctx)
	if err == nil && !ok {
		err = errors.New("no Drafts folder")
	}
	if err != nil {
		done(len(raw), 0)
		return fmt.Errorf("saving the draft: %w", err)
	}
	err = s.st.Append(ctx, name, raw, mail.FlagDraft, mail.FlagSeen)
	done(len(raw), 0)
	if err != nil {
		return fmt.Errorf("saving the draft: %w", err)
	}

	if d.DraftOf != nil {
		if err := s.deleteDraft(ctx, *d.DraftOf); err != nil {
			return &draftSavedError{err}
		}
	}
	return nil
}

// OpenDraft reads the saved draft at ref for editing. The result's DraftOf is
// ref, so saving or sending it replaces the stored copy. Its attachments are
// read into d.Attachments, so save and send write them from memory; ForwardOf
// and ReplyTo stay unset.
func (s *Sender) OpenDraft(ctx context.Context, ref mail.MessageRef) (mail.Draft, error) {
	if s.st == nil {
		return mail.Draft{}, errors.New("open draft: opening drafts isn't set up for this account")
	}
	done := s.step("open-draft")
	raw, err := s.st.Raw(ctx, ref.Folder, ref.UID)
	done(len(raw), 0)
	if err != nil {
		return mail.Draft{}, fmt.Errorf("opening the draft: %w", err)
	}
	d, err := compose.FromRaw(raw)
	if err != nil {
		return mail.Draft{}, fmt.Errorf("opening the draft: %w", err)
	}
	atts, err := s.extract(raw)
	if err != nil {
		return mail.Draft{}, fmt.Errorf("opening the draft: %w", err)
	}
	d.DraftOf = &ref
	for _, a := range atts {
		d.Attachments = append(d.Attachments, mail.File{Name: a.Filename, ContentType: a.ContentType, Data: a.Data})
	}
	return d, nil
}

type stepError struct {
	part  string
	cause error
}

func (s *Sender) saveSent(ctx context.Context, raw []byte) *stepError {
	done := s.step("save-sent")
	defer func() { done(len(raw), 0) }()
	name, ok, err := s.st.SentFolder(ctx)
	if err != nil {
		return &stepError{"couldn't save a copy to Sent", err}
	}
	if !ok {
		return &stepError{part: "no Sent folder; copy not saved"}
	}
	if err := s.st.Append(ctx, name, raw, mail.FlagSeen); err != nil {
		return &stepError{"couldn't save a copy to Sent", err}
	}
	return nil
}

// step starts timing a step and returns a function that logs it at debug
// level. Only the account name, step, duration, size and recipient count are
// recorded; never addresses, subjects or bodies.
func (s *Sender) step(name string) func(size, rcpts int) {
	start := time.Now()
	return func(size, rcpts int) {
		s.log.Debug("send step",
			"account", s.acct.Name,
			"step", name,
			"duration", time.Since(start),
			"bytes", size,
			"recipients", rcpts)
	}
}

// recipients returns the To, Cc and Bcc addresses, deduplicated
// case-insensitively, in first-seen order and original casing.
func recipients(d mail.Draft) []string {
	var out []string
	seen := map[string]bool{}
	for _, list := range [][]mail.Address{d.To, d.Cc, d.Bcc} {
		for _, a := range list {
			k := strings.ToLower(a.Email)
			if a.Email == "" || seen[k] {
				continue
			}
			seen[k] = true
			out = append(out, a.Email)
		}
	}
	return out
}
