package compose

import (
	"reflect"
	"strings"
	"testing"

	"github.com/dkouvdis/dk-email/internal/mail"
)

func TestFormat(t *testing.T) {
	d := mail.Draft{
		To:      []mail.Address{{Name: "Alice", Email: "alice@example.com"}, {Email: "bob@example.org"}},
		Subject: "Hello",
		Body:    "Hi!\n",
	}
	want := "To: Alice <alice@example.com>, bob@example.org\nCc: \nBcc: \nSubject: Hello\n\nHi!\n"
	if got := Format(d); got != want {
		t.Errorf("Format = %q, want %q", got, want)
	}
}

func TestFormatParse_RoundTrip(t *testing.T) {
	tests := []struct {
		name string
		d    mail.Draft
	}{
		{"empty cc and bcc", mail.Draft{To: []mail.Address{{Email: "a@example.com"}}, Subject: "S", Body: "b\n"}},
		{"comma and quote in name", mail.Draft{
			To: []mail.Address{{Name: `Smith, "Bob" J.`, Email: "bob@example.com"}, {Name: `back\slash`, Email: "x@example.com"}},
			Cc: []mail.Address{{Name: "Doe, Jane", Email: "jane@example.com"}}, Subject: "Q"}},
		{"non-ascii", mail.Draft{
			To:  []mail.Address{{Name: "José Álvarez", Email: "jose@example.es"}, {Name: "李雷", Email: "li@example.cn"}},
			Bcc: []mail.Address{{Name: "Zoë, M.", Email: "zoe@example.com"}}, Subject: "Café ☕", Body: "Ünïcode\n"}},
		{"body with blank lines and header-like text", mail.Draft{
			To: []mail.Address{{Email: "a@example.com"}}, Body: "\n\nTo: nobody\n\n  indented\n\ntrailing\n\n"}},
		{"no trailing newline", mail.Draft{To: []mail.Address{{Email: "a@example.com"}}, Body: "x"}},
		{"empty body", mail.Draft{To: []mail.Address{{Email: "a@example.com"}}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Parse(Format(tc.d), mail.Draft{})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.d) {
				t.Errorf("round trip:\n got %#v\nwant %#v", got, tc.d)
			}
		})
	}
}

func TestParse_KeepsBaseFields(t *testing.T) {
	base := mail.Draft{
		From:       mail.Address{Name: "Me", Email: "me@example.com"},
		To:         []mail.Address{{Email: "old@example.com"}},
		Subject:    "old",
		Body:       "old body",
		InReplyTo:  "m1@example.com",
		References: []string{"m0@example.com", "m1@example.com"},
		ReplyTo:    &mail.MessageRef{Folder: "INBOX", UID: 7},
		ForwardOf:  &mail.MessageRef{Folder: "INBOX", UID: 8},
	}
	got, err := Parse("to: new@example.com\r\nSUBJECT:  new subject \r\nbcc: b@example.com\r\n\r\nnew body\r\nline2", base)
	if err != nil {
		t.Fatal(err)
	}
	want := base
	want.To = []mail.Address{{Email: "new@example.com"}}
	want.Bcc = []mail.Address{{Email: "b@example.com"}}
	want.Subject = "new subject"
	want.Body = "new body\nline2"
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v\nwant %#v", got, want)
	}
	if len(base.Cc) != 0 || base.Subject != "old" {
		t.Error("Parse modified base")
	}
}

func TestParse_Edges(t *testing.T) {
	// Headers only, no blank line: empty body.
	d, err := Parse("To: a@example.com\nSubject: hi", mail.Draft{Body: "was"})
	if err != nil || d.Body != "" || d.Subject != "hi" {
		t.Errorf("headers only: %+v, %v", d, err)
	}
	// A missing Subject header is an empty subject.
	d, err = Parse("To: a@example.com\n\nbody", mail.Draft{Subject: "was"})
	if err != nil || d.Subject != "" || d.Body != "body" {
		t.Errorf("no subject: %+v, %v", d, err)
	}
	// Wrapped recipient list.
	d, err = Parse("To: a@example.com,\n\tb@example.com\nSubject: x\n\n", mail.Draft{})
	if err != nil || len(d.To) != 2 {
		t.Errorf("wrapped: %+v, %v", d, err)
	}
}

func TestParse_Errors(t *testing.T) {
	tests := []struct {
		name, text, want string
	}{
		{"unknown header", "Too: a@example.com\n\nx", `unknown header "Too" on line 1 (only To, Cc, Bcc and Subject are allowed)`},
		{"unknown header later", "To: a@example.com\nFrom: x@example.com\n\nx", `unknown header "From" on line 2`},
		{"no colon", "To: a@example.com\nSubject hello\n\nx", `line 2: missing ':' in header line "Subject hello"`},
		{"indent first", "  To: a@example.com\n\nx", "line 1: unexpected indented line"},
		{"duplicate", "To: a@example.com\nSubject: a\nTo: b@example.com\n\nx", `duplicate header "To" on line 3 (first on line 1)`},
		{"bad to", "To: bob@\n\nx", `To: invalid address list "bob@": `},
		{"bad cc", "To: a@example.com\nCc: not an address\n\nx", `Cc: invalid address list "not an address": `},
		{"bad bcc", "To: a@example.com\nBcc: <\n\nx", `Bcc: invalid address list "<": `},
		{"no recipients", "To:\nCc:\nBcc:\nSubject: hi\n\nbody", "no recipients: add an address to To, Cc or Bcc"},
		{"empty file", "", "no recipients: add an address to To, Cc or Bcc"},
		{"leading blank line", "\nTo: a@example.com\n", "no recipients"},
	}
	base := mail.Draft{From: mail.Address{Email: "me@example.com"}, Body: "base"}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Parse(tc.text, base)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want containing %q", err, tc.want)
			}
			if !reflect.DeepEqual(got, base) {
				t.Errorf("draft changed on error: %+v", got)
			}
		})
	}
}

func TestFormat_SingleLineValues(t *testing.T) {
	d := mail.Draft{
		To:      []mail.Address{{Name: "A\nBcc: x@y.z", Email: "a@example.com"}},
		Subject: "S\r\nBcc: x@y.z",
	}
	got, err := Parse(Format(d), mail.Draft{})
	if err != nil || len(got.Bcc) != 0 {
		t.Errorf("newline in a value injected a header: %+v, %v", got, err)
	}
}

func TestFormatAddress_NameOnlyOrEmailOnly(t *testing.T) {
	if got := formatAddress(mail.Address{Name: "Only Name"}); got != "Only Name" {
		t.Errorf("got %q", got)
	}
	if got := formatAddress(mail.Address{Email: "e@example.com"}); got != "e@example.com" {
		t.Errorf("got %q", got)
	}
}
