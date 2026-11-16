package compose

import (
	"slices"
	"strings"

	"github.com/dkouvdis/dk-email/internal/mail"
)

// Reply drafts a reply to msg, which lives in folder. mine are the addresses
// the account sends as; mine[0] is the account address, the rest its aliases.
// They identify my own messages and choose From (see pickFrom).
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
func Reply(msg mail.Message, folder string, mine []mail.Address) mail.Draft {
	return mail.Draft{
		From:       pickFrom(msg, mine),
		To:         replyRecipients(msg, mine),
		Subject:    replySubject(msg.Subject),
		Body:       "\n\n" + attribution(msg) + "\n" + quote(msg.Body),
		InReplyTo:  msg.MessageID,
		References: references(msg),
		ReplyTo:    &mail.MessageRef{Folder: folder, UID: msg.UID},
	}
}

// ReplyAll is Reply plus msg's original To and Cc, minus all of mine. Addresses are
// compared case-insensitively and listed once, in To before Cc. If that
// leaves To empty, the first Cc moves up to To.
func ReplyAll(msg mail.Message, folder string, mine []mail.Address) mail.Draft {
	d := Reply(msg, folder, mine)
	to := d.To
	if !fromMe(msg, mine) {
		to = append(slices.Clone(to), msg.To...)
	}
	seen := map[string]bool{}
	for _, a := range mine {
		seen[strings.ToLower(a.Email)] = true
	}
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

// fromMe reports whether msg was sent from any of mine.
func fromMe(msg mail.Message, mine []mail.Address) bool {
	_, ok := find(msg.From, mine)
	return ok
}

// find returns the entry of mine with a's email, ignoring case.
func find(a mail.Address, mine []mail.Address) (mail.Address, bool) {
	for _, m := range mine {
		if strings.EqualFold(a.Email, m.Email) {
			return m, true
		}
	}
	return mail.Address{}, false
}

// pickFrom is the address to reply or forward from. For my own message it is
// the address it was sent from; else the first of msg's To, then Cc, that is
// one of mine; else mine[0]. It returns the entry from mine, so its
// configured name is used. With no mine it returns the zero address.
func pickFrom(msg mail.Message, mine []mail.Address) mail.Address {
	if len(mine) == 0 {
		return mail.Address{}
	}
	if m, ok := find(msg.From, mine); ok {
		return m
	}
	for _, l := range [][]mail.Address{msg.To, msg.Cc} {
		for _, a := range l {
			if m, ok := find(a, mine); ok {
				return m
			}
		}
	}
	return mine[0]
}

func replyRecipients(msg mail.Message, mine []mail.Address) []mail.Address {
	switch {
	case fromMe(msg, mine):
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
