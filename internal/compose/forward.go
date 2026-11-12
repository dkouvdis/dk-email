package compose

import (
	"strings"

	"github.com/dkouvdis/dk-email/internal/mail"
)

// Forward drafts a forward of msg, which lives in folder, from me. The
// recipients are empty for the user to fill in, and there is no threading:
// no In-Reply-To, References or ReplyTo. ForwardOf points at msg so the
// sender copies its attachments.
//
// Body starts with an empty line for the user's text, a blank line, then the
// forwarded-message block with msg.Body unquoted, ending with a newline. The
// Date line is left out when msg has no date and the Cc line when it has no
// Cc. With the headers, the composer shows:
//
//	To:
//	Cc:
//	Bcc:
//	Subject: Fwd: Lunch
//
//	<- the cursor goes here
//
//	---------- Forwarded message ----------
//	From: Alice <alice@example.com>
//	Date: Tue, 10 Nov 2026 at 12:00
//	Subject: Lunch
//	To: Me <me@example.com>
//
//	Shall we try the new place?
func Forward(msg mail.Message, folder string, me mail.Address) mail.Draft {
	return mail.Draft{
		From:      me,
		Subject:   forwardSubject(msg.Subject),
		Body:      "\n\n" + forwardBlock(msg),
		ForwardOf: &mail.MessageRef{Folder: folder, UID: msg.UID},
	}
}

func forwardSubject(s string) string {
	t := strings.ToLower(strings.TrimLeft(s, " "))
	if strings.HasPrefix(t, "fwd:") || strings.HasPrefix(t, "fw:") {
		return s
	}
	return "Fwd: " + s
}

func forwardBlock(msg mail.Message) string {
	var b strings.Builder
	b.WriteString("---------- Forwarded message ----------\n")
	b.WriteString("From: " + formatAddress(msg.From) + "\n")
	if !msg.Date.IsZero() {
		b.WriteString("Date: " + msg.Date.Format("Mon, 2 Jan 2006 at 15:04") + "\n")
	}
	b.WriteString("Subject: " + singleLine(msg.Subject) + "\n")
	b.WriteString("To: " + formatAddresses(msg.To) + "\n")
	if len(msg.Cc) > 0 {
		b.WriteString("Cc: " + formatAddresses(msg.Cc) + "\n")
	}
	b.WriteString("\n")
	body := strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(msg.Body)
	if body = strings.TrimRight(body, "\n"); body != "" {
		b.WriteString(body + "\n")
	}
	return b.String()
}
