package compose

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/emersion/go-message"
	gomail "github.com/emersion/go-message/mail"

	"github.com/dkouvdis/dk-email/internal/mail"
	"github.com/dkouvdis/dk-email/internal/render"
)

// FromRaw parses a saved draft back into a mail.Draft: From, To, Cc, Bcc,
// Subject, In-Reply-To, References and Body. It reads what BuildDraft writes
// and tolerates drafts made by other clients. Body is the first text/plain
// part that isn't an attachment, decoded, with LF line endings; with none, an
// HTML part is converted to text the way the reader does, and with no body at
// all Body is "". Attachments are not read (send.OpenDraft reads them); ReplyTo and DraftOf are left for the caller.
func FromRaw(raw []byte) (mail.Draft, error) {
	e, err := message.Read(bytes.NewReader(raw))
	if e == nil || (err != nil && !message.IsUnknownCharset(err) && !message.IsUnknownEncoding(err)) {
		return mail.Draft{}, fmt.Errorf("parsing the draft: %w", err)
	}
	if e.Header.Len() == 0 {
		return mail.Draft{}, fmt.Errorf("parsing the draft: no headers")
	}
	h := gomail.Header{Header: e.Header}

	var d mail.Draft
	var from, to, cc, bcc []mail.Address
	for _, f := range []struct {
		key string
		dst *[]mail.Address
	}{{"From", &from}, {"To", &to}, {"Cc", &cc}, {"Bcc", &bcc}} {
		if *f.dst, err = addressList(h, f.key); err != nil {
			return mail.Draft{}, err
		}
	}
	if len(from) > 0 {
		d.From = from[0]
	}
	d.To, d.Cc, d.Bcc = to, cc, bcc

	subject, err := h.Subject()
	if err != nil {
		subject = h.Get("Subject")
	}
	d.Subject = render.SanitizeLine(subject)
	if ids, err := h.MsgIDList("In-Reply-To"); err == nil {
		d.InReplyTo = strings.Join(ids, " ")
	}
	d.References, _ = h.MsgIDList("References")

	plain, ok := plainBody(e)
	if !ok {
		if m, err := render.Parse(bytes.NewReader(raw)); err == nil && m.BodyFromHTML {
			plain = m.Body
		}
	}
	d.Body = strings.ReplaceAll(strings.ToValidUTF8(plain, ""), "\r\n", "\n")
	return d, nil
}

func addressList(h gomail.Header, key string) ([]mail.Address, error) {
	list, err := h.AddressList(key)
	if err != nil {
		return nil, fmt.Errorf("parsing the draft's %s header: %w", key, err)
	}
	var out []mail.Address
	for _, a := range list {
		out = append(out, mail.Address{Name: render.SanitizeLine(a.Name), Email: render.SanitizeLine(a.Address)})
	}
	return out, nil
}

// plainBody returns the first text/plain part that isn't an attachment, by
// the same rule the reader uses to list attachments.
func plainBody(e *message.Entity) (string, bool) {
	var body string
	var found bool
	_ = e.Walk(func(_ []int, part *message.Entity, _ error) error {
		if part == nil || found {
			return nil
		}
		mediaType, params, _ := part.Header.ContentType()
		if mediaType == "" {
			mediaType = "text/plain"
		}
		if mediaType != "text/plain" {
			return nil
		}
		disp, dparams, _ := part.Header.ContentDisposition()
		if disp == "attachment" || dparams["filename"] != "" || params["name"] != "" {
			return nil
		}
		// A truncated part still yields the bytes read before the error.
		b, _ := io.ReadAll(part.Body)
		body, found = string(b), true
		return nil
	})
	return body, found
}
