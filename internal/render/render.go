// Package render turns a raw RFC 5322 message into displayable text: it
// prefers text/plain, converts HTML-only mail to text, decodes charsets and
// transfer encodings, lists attachments, and strips terminal escapes.
package render

import (
	"fmt"
	"io"
	"mime"
	"regexp"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/emersion/go-message"
	_ "github.com/emersion/go-message/charset" // registers charsets beyond UTF-8 and ASCII
	gomail "github.com/emersion/go-message/mail"

	"github.com/dkouvdis/dk-email/internal/mail"
)

// Parse reads a whole message. Only unreadable top-level headers are an
// error; problems in the body (unknown charsets, broken multipart) keep
// whatever text could be read.
func Parse(r io.Reader) (mail.Message, error) {
	e, rootUnknownCharset, err := readEntity(r)
	if err != nil {
		return mail.Message{}, err
	}

	h := gomail.Header{Header: e.Header}
	msg := mail.Message{Summary: headerSummary(h), Cc: addresses(h, "Cc"), ReplyTo: addresses(h, "Reply-To")}

	var plain, html string
	var havePlain, haveHTML bool
	walkLeaves(e, rootUnknownCharset, func(l leaf) {
		if l.attachment {
			n, _ := io.Copy(io.Discard, l.body)
			msg.Attachments = append(msg.Attachments, mail.Attachment{
				Name:        l.name,
				ContentType: l.mediaType,
				Size:        n,
			})
			return
		}
		// A truncated part still yields the bytes read before the error.
		b, _ := io.ReadAll(l.body)
		switch {
		case l.mediaType == "text/plain" && !havePlain:
			plain, havePlain = string(b), true
		case l.mediaType == "text/html" && !haveHTML:
			html, haveHTML = string(b), true
		}
	})

	switch {
	case havePlain && !(haveHTML && plainIsJunk(plain)):
		msg.Body = plain
	case haveHTML:
		msg.Body = htmlToMarkdown(html)
		msg.BodyFromHTML = true
	}
	msg.Body = strings.TrimRight(Sanitize(msg.Body), " \n")
	return msg, nil
}

// readEntity reads the message structure. Only unreadable top-level headers
// are an error; an unknown charset or transfer encoding still yields an
// entity. unknownCharset reports that the entity's own charset could not be
// converted, which Walk does not report for the root entity.
func readEntity(r io.Reader) (e *message.Entity, unknownCharset bool, err error) {
	e, err = message.Read(r)
	if e == nil {
		return nil, false, fmt.Errorf("parse message: %w", err)
	}
	if err != nil && !message.IsUnknownCharset(err) && !message.IsUnknownEncoding(err) {
		return nil, false, fmt.Errorf("parse message: %w", err)
	}
	return e, message.IsUnknownCharset(err), nil
}

// leaf is a non-multipart part of a message.
type leaf struct {
	mediaType string // lower-case; text/plain when the part has no Content-Type
	// attachment is the one rule for what counts as an attachment, shared by
	// Parse (metadata) and Parts (bytes): an attachment disposition, a
	// filename (Content-Disposition filename, else Content-Type name), or any
	// type other than text/plain and text/html.
	attachment bool
	name       string // display name of an attachment, "(unnamed)" if it has none
	// charset is the part's declared charset parameter, "" if none.
	// charsetKept means body still has those original bytes because the
	// charset is unknown; otherwise body is UTF-8 for text parts.
	charset     string
	charsetKept bool
	body        io.Reader
}

// walkLeaves calls fn for every non-multipart part of e, in message order.
// body is only valid during the call. rootUnknownCharset is readEntity's flag
// for e itself, since Walk only reports charset errors of nested parts.
func walkLeaves(e *message.Entity, rootUnknownCharset bool, fn func(leaf)) {
	_ = e.Walk(func(path []int, part *message.Entity, err error) error {
		if part == nil {
			return nil
		}
		unknownCharset := message.IsUnknownCharset(err)
		if len(path) == 0 {
			unknownCharset = rootUnknownCharset
		}
		mediaType, params, _ := part.Header.ContentType()
		if mediaType == "" {
			mediaType = "text/plain"
		}
		if strings.HasPrefix(mediaType, "multipart/") {
			return nil
		}
		disp, dparams, _ := part.Header.ContentDisposition()
		name := dparams["filename"]
		if name == "" {
			name = params["name"]
		}
		isText := mediaType == "text/plain" || mediaType == "text/html"
		l := leaf{mediaType: mediaType, body: part.Body, charset: params["charset"], charsetKept: unknownCharset}
		if disp == "attachment" || name != "" || !isText {
			l.attachment, l.name = true, attachmentName(name)
		}
		fn(l)
		return nil
	})
}

func headerSummary(h gomail.Header) mail.Summary {
	s := mail.Summary{}
	subject, err := h.Subject()
	if err != nil {
		subject = h.Get("Subject")
	}
	s.Subject = SanitizeLine(subject)
	if from := addresses(h, "From"); len(from) > 0 {
		s.From = from[0]
	}
	s.To = addresses(h, "To")
	s.Date, _ = h.Date()
	s.MessageID, _ = h.MessageID()
	if ids, err := h.MsgIDList("In-Reply-To"); err == nil {
		s.InReplyTo = strings.Join(ids, " ")
	}
	s.References, _ = h.MsgIDList("References")
	return s
}

func addresses(h gomail.Header, key string) []mail.Address {
	list, err := h.AddressList(key)
	if err != nil {
		return nil
	}
	out := make([]mail.Address, 0, len(list))
	for _, a := range list {
		out = append(out, mail.Address{Name: SanitizeLine(a.Name), Email: SanitizeLine(a.Address)})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func attachmentName(name string) string {
	if decoded, err := new(mime.WordDecoder).DecodeHeader(name); err == nil {
		name = decoded
	}
	if name = SanitizeLine(name); name == "" {
		return "(unnamed)"
	}
	return name
}

var (
	plainTags     = regexp.MustCompile(`(?i)</?(?:br|p|div|span|td|tr|table|a|img|font)\b[^>]*>`)
	plainEntities = regexp.MustCompile(`&(?:[a-zA-Z]{2,8}|#[0-9]{2,6});`)
	// Preheader padding: a run of zero-width characters, spaces between.
	plainPadding = regexp.MustCompile(`(?:[\x{200b}-\x{200d}\x{2060}\x{feff}\x{034f}][ \x{a0}]*){10}`)
	// Generated links, "label ( https://… )" (html2text) or "label
	// (https://… )" (HubSpot): a plain part generated from the HTML, whose
	// links read better from the HTML itself.
	plainLinks = regexp.MustCompile(`\( ?https?://\S+ \)`)
	// A stub telling the reader their client can't show HTML ("your email
	// software can't display HTML emails"), standing in for the real content.
	plainNoHTML = regexp.MustCompile(`(?i)\b(?:can['’]?t|cannot|can not|unable to|(?:does|do)(?:n['’]?t| not))\s+(?:display|show|view|read|render|support)\s+(?:html|rich[- ]text)`)
)

// plainIsJunk spots text/plain parts machine-generated from the HTML without
// rendering it: leftover tags and entities, unexpanded template code,
// preheader padding, an only-HTML stub, or mostly blank lines. A real
// plain-text email trips none of these.
func plainIsJunk(s string) bool {
	if strings.TrimSpace(s) == "" {
		return true
	}
	if strings.Contains(s, "%%=") || strings.Contains(s, "%%[") || plainPadding.MatchString(s) ||
		plainNoHTML.MatchString(s) {
		return true
	}
	if len(plainTags.FindAllStringIndex(s, 3)) == 3 || len(plainEntities.FindAllStringIndex(s, 3)) == 3 ||
		len(plainLinks.FindAllStringIndex(s, 2)) == 2 {
		return true
	}
	lines := strings.Split(s, "\n")
	blank := 0
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			blank++
		}
	}
	return blank >= 20 && blank*2 > len(lines)
}

// Wrap breaks text into lines of at most width cells, at spaces where it
// can and inside long words such as URLs where it must. Width ≤ 0 leaves
// the text unchanged.
func Wrap(text string, width int) string {
	if width <= 0 {
		return text
	}
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		wrapped := strings.Split(ansi.Wrap(l, width, ""), "\n")
		for j := range wrapped {
			wrapped[j] = strings.TrimRight(wrapped[j], " ")
		}
		lines[i] = strings.Join(wrapped, "\n")
	}
	return strings.Join(lines, "\n")
}
