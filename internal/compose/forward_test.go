package compose

import (
	"strings"
	"testing"
	"time"

	"github.com/dkouvdis/dk-email/internal/mail"
)

func TestForward_Golden(t *testing.T) {
	msg := original()
	msg.To = []mail.Address{me, ben}
	msg.Cc = []mail.Address{carol}
	msg.Body = "Shall we try the new place?\n\nLet me know.\n"
	want := "\n" + // the line for the user's text
		"\n" +
		"---------- Forwarded message ----------\n" +
		"From: Alice <alice@example.com>\n" +
		"Date: Tue, 10 Nov 2026 at 12:00\n" +
		"Subject: Lunch on Friday\n" +
		"To: Me <me@example.com>, Bob <bob@example.com>\n" +
		"Cc: Carol <carol@example.com>\n" +
		"\n" +
		"Shall we try the new place?\n" +
		"\n" +
		"Let me know.\n"
	base := Forward(msg, "INBOX", mine)
	if base.Body != want {
		t.Errorf("Forward body =\n%q\nwant\n%q", base.Body, want)
	}
	// Untouched, the draft can't be sent: it has no recipient.
	f := FieldsOf(base)
	if _, err := f.Draft(base); err == nil {
		t.Error("Draft(untouched fields) succeeded, want a no-recipients error")
	}
	f.To = "z@example.com"
	d, err := f.Draft(base)
	if err != nil {
		t.Fatal(err)
	}
	if d.Subject != "Fwd: Lunch on Friday" || d.Body != base.Body || d.From != me {
		t.Errorf("Draft(filled fields) = %#v", d)
	}
}

func TestForward_Fields(t *testing.T) {
	msg := original()
	msg.MessageID, msg.References, msg.InReplyTo = "m1@example.com", []string{"m0@example.com"}, "m0@example.com"
	msg.ReplyTo = []mail.Address{ben}
	msg.Cc = []mail.Address{carol}
	d := Forward(msg, "Archive", mine)
	if d.From != me {
		t.Errorf("From = %v", d.From)
	}
	if len(d.To)+len(d.Cc)+len(d.Bcc) != 0 {
		t.Errorf("recipients = %v %v %v, want none", d.To, d.Cc, d.Bcc)
	}
	if d.InReplyTo != "" || d.References != nil || d.ReplyTo != nil {
		t.Errorf("threading fields set: %#v", d)
	}
}

func TestForward_Subject(t *testing.T) {
	tests := []struct{ in, want string }{
		{"Lunch", "Fwd: Lunch"},
		{"Fwd: Lunch", "Fwd: Lunch"},
		{"FWD: Lunch", "FWD: Lunch"},
		{"Fw: Lunch", "Fw: Lunch"},
		{"FW: Lunch", "FW: Lunch"},
		{"  fwd: Lunch", "  fwd: Lunch"},
		{"Fwd:Lunch", "Fwd:Lunch"},
		{"", "Fwd: "},
		{"Fwd", "Fwd: Fwd"},
		{"Forward plans", "Fwd: Forward plans"},
		{"Re: Lunch", "Fwd: Re: Lunch"},
		{"Fwd Fwd: x", "Fwd: Fwd Fwd: x"},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			msg := original()
			msg.Subject = tc.in
			if got := Forward(msg, "INBOX", mine).Subject; got != tc.want {
				t.Errorf("Subject = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestForward_Body(t *testing.T) {
	const (
		head = "\n\n---------- Forwarded message ----------\n"
		from = "From: Alice <alice@example.com>\n"
		date = "Date: Tue, 10 Nov 2026 at 12:00\n"
		subj = "Subject: Lunch on Friday\n"
		to   = "To: Me <me@example.com>\n"
	)
	tests := []struct {
		name string
		mut  func(*mail.Message)
		want string
	}{
		{"no cc line without cc", func(m *mail.Message) {}, head + from + date + subj + to + "\nShall we try the new place?\n"},
		{"cc line", func(m *mail.Message) { m.Cc = []mail.Address{carol, dave} },
			head + from + date + subj + to + "Cc: Carol <carol@example.com>, dave@example.com\n\nShall we try the new place?\n"},
		{"zero date omits the line", func(m *mail.Message) { m.Date = time.Time{} }, head + from + subj + to + "\nShall we try the new place?\n"},
		{"date in its own zone", func(m *mail.Message) {
			m.Date = time.Date(2026, 11, 10, 23, 5, 0, 0, time.FixedZone("", 9*3600))
		}, head + from + "Date: Tue, 10 Nov 2026 at 23:05\n" + subj + to + "\nShall we try the new place?\n"},
		{"crlf and cr", func(m *mail.Message) { m.Body = "a\r\n\r\nb\rc\r\n" }, head + from + date + subj + to + "\na\n\nb\nc\n"},
		{"trailing newlines trimmed to one", func(m *mail.Message) { m.Body = "a\n\n\n" }, head + from + date + subj + to + "\na\n"},
		{"leading blank lines and indentation kept", func(m *mail.Message) { m.Body = "\n  x\n> y" }, head + from + date + subj + to + "\n\n  x\n> y\n"},
		{"not quoted", func(m *mail.Message) { m.Body = "a\nb" }, head + from + date + subj + to + "\na\nb\n"},
		{"empty body", func(m *mail.Message) { m.Body = "" }, head + from + date + subj + to + "\n"},
		{"only newlines", func(m *mail.Message) { m.Body = "\r\n\r\n" }, head + from + date + subj + to + "\n"},
		{"several to addresses and a tricky name", func(m *mail.Message) {
			m.From = mail.Address{Name: "Smith, Bob", Email: "bob@example.com"}
			m.To = []mail.Address{me, ben}
		}, head + "From: \"Smith, Bob\" <bob@example.com>\n" + date + subj + "To: Me <me@example.com>, Bob <bob@example.com>\n\nShall we try the new place?\n"},
		{"no to", func(m *mail.Message) { m.To = nil }, head + from + date + subj + "To: \n\nShall we try the new place?\n"},
		{"subject stays on one line", func(m *mail.Message) { m.Subject = "a\r\nBcc: x@example.com" },
			head + from + date + "Subject: a Bcc: x@example.com\n" + to + "\nShall we try the new place?\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg := original()
			tc.mut(&msg)
			if got := Forward(msg, "INBOX", mine).Body; got != tc.want {
				t.Errorf("Body =\n%q\nwant\n%q", got, tc.want)
			}
		})
	}
}

func TestForward_BodyDoesNotListAttachments(t *testing.T) {
	msg := original()
	msg.Attachments = []mail.Attachment{{Name: "secret.pdf", ContentType: "application/pdf", Size: 10}}
	if got := Forward(msg, "INBOX", mine).Body; strings.Contains(got, "secret.pdf") {
		t.Errorf("Body mentions the attachment:\n%s", got)
	}
}
