package compose

import (
	"reflect"
	"strings"
	"testing"

	"github.com/dkouvdis/dk-email/internal/mail"
)

func TestFields_RoundTrip(t *testing.T) {
	tests := []struct {
		name string
		d    mail.Draft
	}{
		{"plain emails", mail.Draft{To: []mail.Address{{Email: "a@example.com"}, {Email: "b@example.org"}}, Subject: "S", Body: "b\n"}},
		{"comma and quote in name", mail.Draft{
			To: []mail.Address{{Name: `Smith, "Bob" J.`, Email: "bob@example.com"}, {Name: `back\slash`, Email: "x@example.com"}},
			Cc: []mail.Address{{Name: "Doe, Jane", Email: "jane@example.com"}}, Subject: "Q"}},
		{"non-ascii", mail.Draft{
			To:  []mail.Address{{Name: "José Álvarez", Email: "jose@example.es"}, {Name: "李雷", Email: "li@example.cn"}},
			Bcc: []mail.Address{{Name: "Zoë, M.", Email: "zoe@example.com"}}, Subject: "Café ☕", Body: "Ünïcode\n"}},
		{"body kept verbatim", mail.Draft{
			To: []mail.Address{{Email: "a@example.com"}}, Body: "\n\nTo: nobody\n\n  indented\n\ntrailing\n\n"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := FieldsOf(tc.d).Draft(mail.Draft{})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.d) {
				t.Errorf("round trip:\n got %#v\nwant %#v", got, tc.d)
			}
		})
	}
}

func TestFields_Draft_KeepsBaseFields(t *testing.T) {
	base := mail.Draft{
		From:       mail.Address{Name: "Me", Email: "me@example.com"},
		To:         []mail.Address{{Email: "old@example.com"}},
		Cc:         []mail.Address{{Email: "oldcc@example.com"}},
		Subject:    "old",
		Body:       "old body",
		InReplyTo:  "m1@example.com",
		References: []string{"m0@example.com", "m1@example.com"},
		ReplyTo:    &mail.MessageRef{Folder: "INBOX", UID: 7},
	}
	got, err := Fields{To: "new@example.com", Subject: "new", Body: "new body"}.Draft(base)
	if err != nil {
		t.Fatal(err)
	}
	want := base
	want.To = []mail.Address{{Email: "new@example.com"}}
	want.Cc = nil
	want.Subject = "new"
	want.Body = "new body"
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v\nwant %#v", got, want)
	}
}

func TestFields_Draft_Errors(t *testing.T) {
	base := mail.Draft{
		To:      []mail.Address{{Email: "old@example.com"}},
		Subject: "old",
	}
	tests := []struct {
		name string
		f    Fields
		want string
	}{
		{"bad To", Fields{To: "not an address"}, "To: invalid address list"},
		{"bad Cc", Fields{To: "a@example.com", Cc: "a@example.com, <"}, "Cc: invalid address list"},
		{"bad Bcc", Fields{To: "a@example.com", Bcc: "@@"}, "Bcc: invalid address list"},
		{"no recipients", Fields{Subject: "s", Body: "b"}, "no recipients: add an address to To, Cc or Bcc"},
		{"whitespace recipients", Fields{To: " \t", Cc: "  "}, "no recipients"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.f.Draft(base)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want containing %q", err, tc.want)
			}
			if strings.Contains(err.Error(), "\n") {
				t.Errorf("error is not single-line: %q", err)
			}
			if !reflect.DeepEqual(got, base) {
				t.Errorf("base changed on error: %#v", got)
			}
		})
	}
}

func TestFields_Draft_BccOnlyIsValid(t *testing.T) {
	got, err := Fields{Bcc: "secret@example.com"}.Draft(mail.Draft{})
	if err != nil {
		t.Fatal(err)
	}
	if got.To != nil || got.Cc != nil || !reflect.DeepEqual(got.Bcc, []mail.Address{{Email: "secret@example.com"}}) {
		t.Errorf("got To=%v Cc=%v Bcc=%v", got.To, got.Cc, got.Bcc)
	}
}

func TestFields_Draft_EmptyAddressFieldsAreNil(t *testing.T) {
	got, err := Fields{To: " a@example.com ", Cc: "", Bcc: " \t "}.Draft(mail.Draft{
		Cc: []mail.Address{{Email: "stale@example.com"}}, Bcc: []mail.Address{{Email: "stale@example.com"}}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Cc != nil || got.Bcc != nil {
		t.Errorf("Cc = %#v, Bcc = %#v, want nil", got.Cc, got.Bcc)
	}
	if !reflect.DeepEqual(got.To, []mail.Address{{Email: "a@example.com"}}) {
		t.Errorf("To = %#v", got.To)
	}
}

func TestFields_Draft_SubjectAndBody(t *testing.T) {
	tests := []struct {
		name          string
		f             Fields
		subject, body string
	}{
		{"newlines flattened", Fields{Subject: "a\nBcc: evil@example.com\r\nb\rc"}, "a Bcc: evil@example.com b c", ""},
		{"subject trimmed", Fields{Subject: "  hi \n"}, "hi", ""},
		{"crlf body", Fields{Body: "one\r\ntwo\r\n\r\n"}, "", "one\ntwo\n\n"},
		{"body not trimmed", Fields{Body: "\n  indented\n\n"}, "", "\n  indented\n\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.f.To = "a@example.com"
			got, err := tc.f.Draft(mail.Draft{})
			if err != nil {
				t.Fatal(err)
			}
			if got.Subject != tc.subject || got.Body != tc.body {
				t.Errorf("Subject = %q, Body = %q; want %q, %q", got.Subject, got.Body, tc.subject, tc.body)
			}
		})
	}
}

func TestFieldsOf_SubjectSingleLine(t *testing.T) {
	f := FieldsOf(mail.Draft{Subject: "a\nb"})
	if f.Subject != "a b" {
		t.Errorf("Subject = %q", f.Subject)
	}
}

func TestFields_DraftForSave(t *testing.T) {
	base := mail.Draft{
		From: mail.Address{Email: "me@example.com"},
		To:   []mail.Address{{Email: "old@example.com"}},
	}
	got, err := Fields{Subject: "a\nb", Body: "x\r\ny"}.DraftForSave(base)
	if err != nil {
		t.Fatal(err)
	}
	if got.To != nil || got.Cc != nil || got.Bcc != nil {
		t.Errorf("recipients not nil: %#v", got)
	}
	if got.From != base.From || got.Subject != "a b" || got.Body != "x\ny" {
		t.Errorf("got %#v", got)
	}

	got, err = Fields{To: "not an address"}.DraftForSave(base)
	if err == nil || !strings.Contains(err.Error(), "To: invalid address list") {
		t.Fatalf("err = %v, want To address error", err)
	}
	if !reflect.DeepEqual(got, base) {
		t.Errorf("base changed on error: %#v", got)
	}
}
