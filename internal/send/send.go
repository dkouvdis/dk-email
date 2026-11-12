// Package send implements mail.Sender: it builds a draft into RFC 5322 bytes,
// delivers them over SMTP, saves a copy to Sent and marks a replied-to
// message answered. A failure before or during delivery means nothing was
// sent; a failure afterwards is reported as mail.ErrAfterDelivery.
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
}

var _ mail.Sender = (*Sender)(nil)

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

// Send delivers d. The steps run in order: fetch forwarded attachments, build
// the message once, send it over SMTP, save it to Sent (when the account
// keeps copies), and mark the replied-to message answered. Errors from the
// first three steps mean nothing was sent. Errors from the last two are
// joined into one error matching mail.ErrAfterDelivery, and the answered flag
// is still attempted when saving to Sent failed.
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

	var atts []compose.Attachment
	if d.ForwardOf != nil {
		done := s.step("fetch-original")
		raw, err := s.st.Raw(ctx, d.ForwardOf.Folder, d.ForwardOf.UID)
		done(len(raw), 0)
		if err != nil {
			return fmt.Errorf("fetching the original to forward: %w", err)
		}
		if atts, err = s.extract(raw); err != nil {
			return fmt.Errorf("copying attachments from the original: %w", err)
		}
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
	if len(after.parts) > 0 {
		return &after
	}
	return nil
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
