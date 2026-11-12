package compose

import (
	"slices"
	"strings"

	"github.com/dkouvdis/dk-email/internal/mail"
)

// Reply drafts a reply to msg, which lives in folder, from me (the account
// identity; me.Email is what identifies my own messages).
//
// To is msg's Reply-To if it has one, else its From. When msg is from me, To
// is msg's own To instead, so a reply to a message I sent goes to its
// recipients. In-Reply-To and References thread the reply and ReplyTo marks
// msg as the message to flag \Answered after sending.
//
// Body starts with an empty line for the user's text, a blank line, the
// attribution "On <date>, <from> wrote:" and msg.Body quoted with "> " on
// every line ("> " becomes ">" on empty lines), ending with a newline. With
// the headers, the composer shows:
//
//	To: Alice <alice@example.com>
//	Cc:
//	Bcc:
//	Subject: Re: Lunch
//
//	<- the cursor goes here
//
//	On Tue, 10 Nov 2026 at 12:00, Alice <alice@example.com> wrote:
//	> Shall we try the new place?
func Reply(msg mail.Message, folder string, me mail.Address) mail.Draft {
	return mail.Draft{
		From:       me,
		To:         replyRecipients(msg, me),
		Subject:    replySubject(msg.Subject),
		Body:       "\n\n" + attribution(msg) + "\n" + quote(msg.Body),
		InReplyTo:  msg.MessageID,
		References: references(msg),
		ReplyTo:    &mail.MessageRef{Folder: folder, UID: msg.UID},
	}
}

// ReplyAll is Reply plus msg's original To and Cc, minus me. Addresses are
// compared case-insensitively and listed once, in To before Cc. If that
// leaves To empty, the first Cc moves up to To.
func ReplyAll(msg mail.Message, folder string, me mail.Address) mail.Draft {
	d := Reply(msg, folder, me)
	to := d.To
	if !fromMe(msg, me) {
		to = append(slices.Clone(to), msg.To...)
	}
	seen := map[string]bool{strings.ToLower(me.Email): true}
	d.To = unseen(to, seen)
	d.Cc = unseen(msg.Cc, seen)
	if len(d.To) == 0 && len(d.Cc) > 0 {
		d.To, d.Cc = d.Cc[:1], d.Cc[1:]
		if len(d.Cc) == 0 {
			d.Cc = nil
		}
	}
	return d
}

func fromMe(msg mail.Message, me mail.Address) bool {
	return strings.EqualFold(msg.From.Email, me.Email)
}

func replyRecipients(msg mail.Message, me mail.Address) []mail.Address {
	switch {
	case fromMe(msg, me):
		return slices.Clone(msg.To)
	case len(msg.ReplyTo) > 0:
		return slices.Clone(msg.ReplyTo)
	}
	return []mail.Address{msg.From}
}

// unseen returns the addresses of l whose email isn't in seen yet, and adds
// them to seen.
func unseen(l []mail.Address, seen map[string]bool) []mail.Address {
	var out []mail.Address
	for _, a := range l {
		k := strings.ToLower(a.Email)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, a)
	}
	return out
}

func replySubject(s string) string {
	if t := strings.TrimLeft(s, " "); len(t) >= 3 && strings.EqualFold(t[:3], "re:") {
		return s
	}
	return "Re: " + s
}

// references is msg's References plus its Message-ID, without empties or
// repeats, in order.
func references(msg mail.Message) []string {
	var out []string
	for _, id := range append(slices.Clone(msg.References), msg.MessageID) {
		if id != "" && !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}

func attribution(msg mail.Message) string {
	from := formatAddress(msg.From)
	if msg.Date.IsZero() {
		return from + " wrote:"
	}
	return "On " + msg.Date.Format("Mon, 2 Jan 2006 at 15:04") + ", " + from + " wrote:"
}

// quote prefixes every line of body with "> ", or ">" for an empty line, and
// ends each with a newline. Line endings are normalised and trailing
// newlines dropped first.
func quote(body string) string {
	body = strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(body)
	body = strings.TrimRight(body, "\n")
	if body == "" {
		return ""
	}
	var b strings.Builder
	for _, line := range strings.Split(body, "\n") {
		if line == "" {
			b.WriteString(">\n")
			continue
		}
		b.WriteString("> " + line + "\n")
	}
	return b.String()
}
