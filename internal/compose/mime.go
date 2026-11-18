package compose

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"mime"
	netmail "net/mail"
	"path/filepath"
	"strings"
	"time"

	"github.com/emersion/go-message"
	gomail "github.com/emersion/go-message/mail"

	"github.com/dkouvdis/dk-email/internal/mail"
)

// Attachment is a file copied into an outgoing message.
type Attachment struct {
	Filename    string
	ContentType string // e.g. "application/pdf"; "" means application/octet-stream
	Data        []byte
}

// BodyPart is the single seam for the message body. It returns the
// Content-Type and Content-Transfer-Encoding header of the body part and the
// unencoded body bytes; Build writes them through go-message, which applies
// the transfer encoding. Today that is text/plain; charset=utf-8,
// quoted-printable, with lines left as written. A Markdown mode can later
// return a multipart/alternative header and its pre-encoded body here without
// changing Build's callers.
func BodyPart(d mail.Draft) (message.Header, []byte) {
	var h message.Header
	h.SetContentType("text/plain", map[string]string{"charset": "utf-8"})
	h.Set("Content-Transfer-Encoding", "quoted-printable")
	return h, []byte(d.Body)
}

// Build writes d as an RFC 5322 message with CRLF line endings. The Bcc
// header is never written: Bcc recipients are only passed to the transport.
// Use BuildDraft for a copy that keeps them. d.Attachments are written first,
// then atts, as attachment parts. The output is deterministic for
// the same (d, atts, now, msgID), including the multipart boundary, which is
// derived from msgID. msgID is a Message-ID without angle brackets, as
// MessageID returns it.
func Build(d mail.Draft, atts []Attachment, now time.Time, msgID string) ([]byte, error) {
	return build(d, atts, now, msgID, false)
}

// BuildDraft is Build for a message saved to Drafts, which also writes the
// Bcc header (when d has Bcc recipients) so reopening the draft restores
// them. Never send its output.
func BuildDraft(d mail.Draft, atts []Attachment, now time.Time, msgID string) ([]byte, error) {
	return build(d, atts, now, msgID, true)
}

func build(d mail.Draft, atts []Attachment, now time.Time, msgID string, withBcc bool) ([]byte, error) {
	partHeader, body := BodyPart(d)

	// go-message writes header fields in reverse order of insertion.
	var top []func(h *gomail.Header)
	add := func(f func(h *gomail.Header)) { top = append(top, f) }
	add(func(h *gomail.Header) { h.Set("MIME-Version", "1.0") })
	add(func(h *gomail.Header) { h.SetDate(now) })
	add(func(h *gomail.Header) { h.SetMessageID(trimID(msgID)) })
	add(func(h *gomail.Header) { h.SetAddressList("From", toNet([]mail.Address{d.From})) })
	add(func(h *gomail.Header) { h.SetAddressList("To", toNet(d.To)) })
	add(func(h *gomail.Header) { h.SetAddressList("Cc", toNet(d.Cc)) })
	if withBcc && len(d.Bcc) > 0 {
		add(func(h *gomail.Header) { h.SetAddressList("Bcc", toNet(d.Bcc)) })
	}
	add(func(h *gomail.Header) { h.SetText("Subject", singleLine(d.Subject)) })
	add(func(h *gomail.Header) { h.SetMsgIDList("In-Reply-To", ids(d.InReplyTo)) })
	add(func(h *gomail.Header) { h.SetMsgIDList("References", ids(strings.Join(d.References, " "))) })

	all := make([]Attachment, 0, len(d.Attachments)+len(atts))
	for _, f := range d.Attachments {
		all = append(all, Attachment{Filename: f.Name, ContentType: f.ContentType, Data: f.Data})
	}
	atts = append(all, atts...)

	var h gomail.Header
	if len(atts) == 0 {
		for _, k := range []string{"Content-Transfer-Encoding", "Content-Type"} {
			h.Set(k, partHeader.Get(k))
		}
	} else {
		h.SetContentType("multipart/mixed", map[string]string{"boundary": boundary(msgID)})
	}
	for i := len(top) - 1; i >= 0; i-- {
		top[i](&h)
	}

	var out bytes.Buffer
	w, err := message.CreateWriter(&out, h.Header)
	if err != nil {
		return nil, err
	}
	if len(atts) == 0 {
		if _, err := w.Write(body); err != nil {
			return nil, err
		}
		if err := w.Close(); err != nil {
			return nil, err
		}
		return out.Bytes(), nil
	}

	tw, err := w.CreatePart(partHeader)
	if err != nil {
		return nil, err
	}
	if _, err := tw.Write(body); err != nil {
		return nil, err
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	for _, a := range atts {
		aw, err := w.CreatePart(attachmentHeader(a))
		if err != nil {
			return nil, err
		}
		if _, err := aw.Write(a.Data); err != nil {
			return nil, err
		}
		if err := aw.Close(); err != nil {
			return nil, err
		}
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("compose: finishing message: %w", err)
	}
	return out.Bytes(), nil
}

func attachmentHeader(a Attachment) message.Header {
	ct := "application/octet-stream"
	var params map[string]string
	if mt, p, err := mime.ParseMediaType(a.ContentType); err == nil {
		ct, params = mt, p
	}
	name := strings.Map(func(r rune) rune {
		if r < ' ' || r == 0x7f {
			return '_'
		}
		return r
	}, filepath.Base(strings.ReplaceAll(a.Filename, `\`, "/")))
	if name == "." || name == "/" || name == "" {
		name = "attachment"
	}

	var h message.Header
	h.SetContentType(ct, params)
	// FormatMediaType writes non-ASCII names as RFC 2231 (filename*=utf-8''...).
	h.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	h.Set("Content-Transfer-Encoding", "base64")
	return h
}

// boundary is derived from msgID so output is reproducible. It can't occur in
// quoted-printable or base64 text, which never contain "=_".
func boundary(msgID string) string {
	sum := sha256.Sum256([]byte(msgID))
	return "=_dk-email-" + hex.EncodeToString(sum[:12])
}

func toNet(l []mail.Address) []*netmail.Address {
	out := make([]*netmail.Address, len(l))
	for i, a := range l {
		out[i] = &netmail.Address{Name: singleLine(a.Name), Address: a.Email}
	}
	return out
}

func trimID(id string) string {
	return strings.TrimSpace(strings.Trim(strings.TrimSpace(id), "<>"))
}

// ids splits a space-separated list of ids (with or without angle brackets).
func ids(s string) []string {
	var out []string
	for _, f := range strings.Fields(s) {
		if id := trimID(f); id != "" {
			out = append(out, id)
		}
	}
	return out
}
