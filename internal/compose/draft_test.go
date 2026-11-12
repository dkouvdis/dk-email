package compose

import (
	"reflect"
	"strings"
	"testing"

	dkmail "github.com/dkouvdis/dk-email/internal/mail"
)

func TestBuildDraft_WritesBccAndBuildDoesNot(t *testing.T) {
	d := dkmail.Draft{
		From: alice, To: []dkmail.Address{bob}, Bcc: []dkmail.Address{{Name: "Secret", Email: "secret@example.com"}},
		Subject: "Quiet copy", Body: "hi\n",
	}
	saved, err := BuildDraft(d, nil, fixedNow, fixedID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(saved), "\r\nBcc: \"Secret\" <secret@example.com>\r\n") {
		t.Errorf("BuildDraft output has no Bcc header:\n%s", saved)
	}
	sent, err := Build(d, nil, fixedNow, fixedID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(sent), "Bcc") || strings.Contains(string(sent), "secret@example.com") {
		t.Errorf("Build wrote the Bcc recipient:\n%s", sent)
	}

	d.Bcc = nil
	a, _ := BuildDraft(d, nil, fixedNow, fixedID)
	b, _ := Build(d, nil, fixedNow, fixedID)
	if string(a) != string(b) || strings.Contains(string(a), "Bcc") {
		t.Errorf("without Bcc recipients BuildDraft must equal Build:\n%s", a)
	}
}

func TestFromRaw_RoundTrip(t *testing.T) {
	cases := buildCases()
	cases = append(cases, buildCase{name: "everything", d: dkmail.Draft{
		From:       dkmail.Address{Name: "Zoë Müller", Email: "zoe@example.com"},
		To:         []dkmail.Address{{Name: "José Álvarez, Jr.", Email: "jose@example.es"}, bob},
		Cc:         []dkmail.Address{{Email: "carol@example.net"}},
		Bcc:        []dkmail.Address{{Name: "Zoë Secret", Email: "secret@example.com"}},
		Subject:    "Re: Café ☕ — réunion",
		Body:       "Bonjour — ça va ?\n\n> quoted\n",
		InReplyTo:  "parent@example.com",
		References: []string{"root@example.com", "parent@example.com"},
	}, atts: []Attachment{{Filename: "résumé.pdf", ContentType: "application/pdf", Data: []byte("%PDF")}}})
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := BuildDraft(tc.d, tc.atts, fixedNow, fixedID)
			if err != nil {
				t.Fatal(err)
			}
			got, err := FromRaw(raw)
			if err != nil {
				t.Fatal(err)
			}
			want := tc.d
			want.InReplyTo = strings.Trim(want.InReplyTo, "<>")
			want.Subject = strings.TrimSpace(singleLine(want.Subject))
			if !reflect.DeepEqual(got, want) {
				t.Errorf("FromRaw =\n%+v\nwant\n%+v", got, want)
			}
		})
	}
}

func TestFromRaw_OtherClients(t *testing.T) {
	const head = "From: me@example.com\r\nTo: you@example.com\r\nSubject: =?UTF-8?Q?Caf=C3=A9?=\r\n"
	tests := []struct {
		name string
		raw  string
		body string
	}{
		{"plain latin1", head + "Content-Type: text/plain; charset=iso-8859-1\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\ncaf=E9\r\nline two\r\n", "café\nline two\n"},
		{"alternative prefers plain", head + "Content-Type: multipart/alternative; boundary=b\r\n\r\n--b\r\nContent-Type: text/plain\r\n\r\nplain text\r\n--b\r\nContent-Type: text/html\r\n\r\n<p>html</p>\r\n--b--\r\n", "plain text"},
		{"html only is converted", head + "Content-Type: text/html; charset=utf-8\r\n\r\n<p>Hello <b>world</b></p>\r\n", "Hello **world**"},
		{"text attachment is not the body", head + "Content-Type: multipart/mixed; boundary=b\r\n\r\n--b\r\nContent-Type: text/plain; name=n.txt\r\nContent-Disposition: attachment; filename=n.txt\r\n\r\nattached\r\n--b\r\nContent-Type: text/plain\r\n\r\nreal body\r\n--b--\r\n", "real body"},
		{"no body", head + "\r\n", ""},
		{"image only", head + "Content-Type: image/png\r\n\r\nxx\r\n", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d, err := FromRaw([]byte(tc.raw))
			if err != nil {
				t.Fatal(err)
			}
			if d.Body != tc.body || d.Subject != "Café" || d.From.Email != "me@example.com" || len(d.To) != 1 {
				t.Errorf("FromRaw = %+v, want body %q", d, tc.body)
			}
		})
	}
}

func TestFromRaw_Malformed(t *testing.T) {
	for name, raw := range map[string]string{
		"empty":       "",
		"not a mail":  "this is not an email\r\n",
		"bad address": "From: me@example.com\r\nTo: <<<\r\n\r\nbody\r\n",
	} {
		t.Run(name, func(t *testing.T) {
			if d, err := FromRaw([]byte(raw)); err == nil {
				t.Errorf("FromRaw = %+v, want an error", d)
			}
		})
	}
}
