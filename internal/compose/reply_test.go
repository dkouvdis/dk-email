package compose

import (
	"reflect"
	"testing"
	"time"

	"github.com/dkouvdis/dk-email/internal/mail"
)

var (
	me    = mail.Address{Name: "Me", Email: "me@example.com"}
	ann   = mail.Address{Name: "Alice", Email: "alice@example.com"}
	ben   = mail.Address{Name: "Bob", Email: "bob@example.com"}
	carol = mail.Address{Name: "Carol", Email: "carol@example.com"}
	dave  = mail.Address{Email: "dave@example.com"}
)

func original() mail.Message {
	return mail.Message{
		Summary: mail.Summary{
			UID:     42,
			From:    ann,
			To:      []mail.Address{me},
			Subject: "Lunch on Friday",
			Date:    time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC),
		},
		Body: "Shall we try the new place?\n",
	}
}

func TestReply_Golden(t *testing.T) {
	msg := original()
	msg.MessageID = "m1@example.com"
	msg.Body = "Shall we try the new place?\n\nLet me know.\n"
	want := "\n" + // the line for the user's text
		"\n" +
		"On Tue, 10 Nov 2026 at 12:00, Alice <alice@example.com> wrote:\n" +
		"> Shall we try the new place?\n" +
		">\n" +
		"> Let me know.\n"
	base := Reply(msg, "INBOX", me)
	if base.Body != want {
		t.Errorf("Reply body =\n%q\nwant\n%q", base.Body, want)
	}
	// The untouched fields parse back to the same draft.
	d, err := FieldsOf(base).Draft(base)
	if err != nil {
		t.Fatal(err)
	}
	if d.Subject != "Re: Lunch on Friday" || len(d.To) != 1 || d.Body != base.Body {
		t.Errorf("Draft(fields) = %#v", d)
	}
}

func TestReply_Fields(t *testing.T) {
	msg := original()
	msg.MessageID = "m1@example.com"
	msg.References = []string{"m0@example.com"}
	d := Reply(msg, "Archive", me)
	if d.From != me {
		t.Errorf("From = %v", d.From)
	}
	if d.InReplyTo != "m1@example.com" {
		t.Errorf("InReplyTo = %q", d.InReplyTo)
	}
	if want := []string{"m0@example.com", "m1@example.com"}; !reflect.DeepEqual(d.References, want) {
		t.Errorf("References = %v, want %v", d.References, want)
	}
	if want := (&mail.MessageRef{Folder: "Archive", UID: 42}); !reflect.DeepEqual(d.ReplyTo, want) {
		t.Errorf("ReplyTo = %v, want %v", d.ReplyTo, want)
	}
	if d.ForwardOf != nil || len(d.Cc) != 0 || len(d.Bcc) != 0 {
		t.Errorf("unexpected fields: %#v", d)
	}
}

func TestReply_Recipients(t *testing.T) {
	tests := []struct {
		name      string
		from      mail.Address
		to, cc    []mail.Address
		replyTo   []mail.Address
		wantTo    []mail.Address
		wantAllTo []mail.Address
		wantAllCc []mail.Address
	}{
		{
			name: "from",
			from: ann, to: []mail.Address{me},
			wantTo: []mail.Address{ann}, wantAllTo: []mail.Address{ann},
		},
		{
			name: "reply-to wins over from",
			from: ann, to: []mail.Address{me}, replyTo: []mail.Address{ben},
			wantTo: []mail.Address{ben}, wantAllTo: []mail.Address{ben},
		},
		{
			name: "several reply-to addresses",
			from: ann, to: []mail.Address{me}, replyTo: []mail.Address{ben, carol},
			wantTo: []mail.Address{ben, carol}, wantAllTo: []mail.Address{ben, carol},
		},
		{
			name: "own message goes to its to, ignoring reply-to",
			from: me, to: []mail.Address{ann, ben}, cc: []mail.Address{carol}, replyTo: []mail.Address{dave},
			wantTo: []mail.Address{ann, ben}, wantAllTo: []mail.Address{ann, ben}, wantAllCc: []mail.Address{carol},
		},
		{
			name: "own message matched case-insensitively",
			from: mail.Address{Email: "ME@Example.com"}, to: []mail.Address{ann},
			wantTo: []mail.Address{ann}, wantAllTo: []mail.Address{ann},
		},
		{
			name: "own message, reply-all drops me from to and cc",
			from: me, to: []mail.Address{ann, {Email: "Me@EXAMPLE.com"}}, cc: []mail.Address{ben, me},
			wantTo:    []mail.Address{ann, {Email: "Me@EXAMPLE.com"}},
			wantAllTo: []mail.Address{ann}, wantAllCc: []mail.Address{ben},
		},
		{
			name: "reply-all adds to and cc",
			from: ann, to: []mail.Address{me, ben}, cc: []mail.Address{carol},
			wantTo: []mail.Address{ann}, wantAllTo: []mail.Address{ann, ben}, wantAllCc: []mail.Address{carol},
		},
		{
			name: "reply-all removes me case-insensitively",
			from: ann, to: []mail.Address{{Email: "ME@example.COM"}, ben}, cc: []mail.Address{{Name: "Me again", Email: "Me@Example.com"}, carol},
			wantTo: []mail.Address{ann}, wantAllTo: []mail.Address{ann, ben}, wantAllCc: []mail.Address{carol},
		},
		{
			name: "reply-all dedupes across to and cc, first wins",
			from: ann, to: []mail.Address{me, ann, ben, {Name: "Bobby", Email: "BOB@example.com"}}, cc: []mail.Address{ann, ben, carol, carol},
			wantTo: []mail.Address{ann}, wantAllTo: []mail.Address{ann, ben}, wantAllCc: []mail.Address{carol},
		},
		{
			name: "reply-all dedupes reply-to against to",
			from: ann, to: []mail.Address{me, ben}, replyTo: []mail.Address{ben},
			wantTo: []mail.Address{ben}, wantAllTo: []mail.Address{ben}, wantAllCc: nil,
		},
		{
			name: "cc is promoted when to ends up empty",
			from: me, to: []mail.Address{me}, cc: []mail.Address{ben, carol},
			wantTo: []mail.Address{me}, wantAllTo: []mail.Address{ben}, wantAllCc: []mail.Address{carol},
		},
		{
			name: "cc promoted leaves cc empty",
			from: me, cc: []mail.Address{ben},
			wantTo: nil, wantAllTo: []mail.Address{ben}, wantAllCc: nil,
		},
		{
			name: "nothing left",
			from: me, to: []mail.Address{me},
			wantTo: []mail.Address{me}, wantAllTo: nil, wantAllCc: nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg := original()
			msg.From, msg.To, msg.Cc, msg.ReplyTo = tc.from, tc.to, tc.cc, tc.replyTo
			r := Reply(msg, "INBOX", me)
			if !reflect.DeepEqual(r.To, tc.wantTo) || len(r.Cc) != 0 {
				t.Errorf("Reply To = %v Cc = %v, want To %v and no Cc", r.To, r.Cc, tc.wantTo)
			}
			a := ReplyAll(msg, "INBOX", me)
			if !reflect.DeepEqual(a.To, tc.wantAllTo) || !reflect.DeepEqual(a.Cc, tc.wantAllCc) {
				t.Errorf("ReplyAll To = %v Cc = %v, want To %v Cc %v", a.To, a.Cc, tc.wantAllTo, tc.wantAllCc)
			}
			if a.Subject != r.Subject || a.Body != r.Body || a.InReplyTo != r.InReplyTo || !reflect.DeepEqual(a.ReplyTo, r.ReplyTo) {
				t.Errorf("ReplyAll differs from Reply beyond recipients:\n%#v\n%#v", a, r)
			}
		})
	}
}

func TestReply_DoesNotAliasMessage(t *testing.T) {
	msg := original()
	msg.To = []mail.Address{me, ben, carol}
	msg.Cc = []mail.Address{dave}
	msg.ReplyTo = []mail.Address{ann}
	msg.References = []string{"a@x"}
	msg.MessageID = "b@x"
	_ = ReplyAll(msg, "INBOX", me)
	d := Reply(msg, "INBOX", me)
	d.To[0] = mail.Address{Email: "changed@example.com"}
	d.References[0] = "changed"
	if msg.ReplyTo[0] != ann || msg.References[0] != "a@x" || msg.To[0] != me || msg.To[1] != ben {
		t.Errorf("message was modified: %#v", msg)
	}
}

func TestReply_Subject(t *testing.T) {
	tests := []struct{ in, want string }{
		{"Lunch", "Re: Lunch"},
		{"Re: Lunch", "Re: Lunch"},
		{"RE: Lunch", "RE: Lunch"},
		{"re: Lunch", "re: Lunch"},
		{"Re:Lunch", "Re:Lunch"},
		{"  Re: Lunch", "  Re: Lunch"},
		{"", "Re: "},
		{"Re", "Re: Re"},
		{"Fwd: Re", "Re: Fwd: Re"},
		{"Réunion", "Re: Réunion"},
		{"Regarding lunch", "Re: Regarding lunch"},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			msg := original()
			msg.Subject = tc.in
			if got := Reply(msg, "INBOX", me).Subject; got != tc.want {
				t.Errorf("Subject = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestReply_Threading(t *testing.T) {
	tests := []struct {
		name       string
		id         string
		refs       []string
		wantRefs   []string
		wantInRepl string
	}{
		{"chain", "c@x", []string{"a@x", "b@x"}, []string{"a@x", "b@x", "c@x"}, "c@x"},
		{"no references", "a@x", nil, []string{"a@x"}, "a@x"},
		{"message-id already last", "b@x", []string{"a@x", "b@x"}, []string{"a@x", "b@x"}, "b@x"},
		{"duplicates and empties", "c@x", []string{"a@x", "", "b@x", "a@x"}, []string{"a@x", "b@x", "c@x"}, "c@x"},
		{"missing message-id", "", []string{"a@x", "b@x"}, []string{"a@x", "b@x"}, ""},
		{"nothing at all", "", nil, nil, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg := original()
			msg.MessageID, msg.References = tc.id, tc.refs
			d := Reply(msg, "INBOX", me)
			if d.InReplyTo != tc.wantInRepl {
				t.Errorf("InReplyTo = %q, want %q", d.InReplyTo, tc.wantInRepl)
			}
			if !reflect.DeepEqual(d.References, tc.wantRefs) {
				t.Errorf("References = %#v, want %#v", d.References, tc.wantRefs)
			}
		})
	}
}

func TestReply_Body(t *testing.T) {
	const head = "\n\nOn Tue, 10 Nov 2026 at 12:00, Alice <alice@example.com> wrote:\n"
	tests := []struct {
		name string
		mut  func(*mail.Message)
		want string
	}{
		{"single line", func(m *mail.Message) { m.Body = "Hi" }, head + "> Hi\n"},
		{"empty lines become >", func(m *mail.Message) { m.Body = "a\n\nb\n\n\nc" }, head + "> a\n>\n> b\n>\n>\n> c\n"},
		{"crlf and cr", func(m *mail.Message) { m.Body = "a\r\n\r\nb\rc\r\n" }, head + "> a\n>\n> b\n> c\n"},
		{"trailing newlines trimmed", func(m *mail.Message) { m.Body = "a\n\n\n" }, head + "> a\n"},
		{"leading empty lines kept", func(m *mail.Message) { m.Body = "\n\na" }, head + ">\n>\n> a\n"},
		{"indentation and quotes kept", func(m *mail.Message) { m.Body = "  x\n> y" }, head + ">   x\n> > y\n"},
		{"empty body", func(m *mail.Message) { m.Body = "" }, head},
		{"only newlines", func(m *mail.Message) { m.Body = "\r\n\r\n" }, head},
		{"attribution without name", func(m *mail.Message) { m.From = mail.Address{Email: "alice@example.com"} },
			"\n\nOn Tue, 10 Nov 2026 at 12:00, alice@example.com wrote:\n> Shall we try the new place?\n"},
		{"attribution quotes tricky names", func(m *mail.Message) { m.From = mail.Address{Name: "Smith, Bob", Email: "bob@example.com"} },
			"\n\nOn Tue, 10 Nov 2026 at 12:00, \"Smith, Bob\" <bob@example.com> wrote:\n> Shall we try the new place?\n"},
		{"zero date", func(m *mail.Message) { m.Date = time.Time{} },
			"\n\nAlice <alice@example.com> wrote:\n> Shall we try the new place?\n"},
		{"date in its own zone", func(m *mail.Message) {
			m.Date = time.Date(2026, 11, 10, 23, 5, 0, 0, time.FixedZone("", 9*3600))
		}, "\n\nOn Tue, 10 Nov 2026 at 23:05, Alice <alice@example.com> wrote:\n> Shall we try the new place?\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg := original()
			tc.mut(&msg)
			if got := Reply(msg, "INBOX", me).Body; got != tc.want {
				t.Errorf("Body =\n%q\nwant\n%q", got, tc.want)
			}
		})
	}
}

func TestReply_BodyIsOnlyTheMessageBody(t *testing.T) {
	msg := original()
	msg.Subject, msg.Body = "SUBJECT", "body"
	msg.Attachments = []mail.Attachment{{Name: "secret.pdf"}}
	got := Reply(msg, "INBOX", me).Body
	if want := "\n\nOn Tue, 10 Nov 2026 at 12:00, Alice <alice@example.com> wrote:\n> body\n"; got != want {
		t.Errorf("Body = %q, want %q", got, want)
	}
}
