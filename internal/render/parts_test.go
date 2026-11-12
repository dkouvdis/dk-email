package render

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"mime/quotedprintable"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/dkouvdis/dk-email/internal/mail"
)

func readRaw(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// matchParse checks that parts are exactly the attachments Parse lists.
func matchParse(t *testing.T, raw []byte, parts []Part) {
	t.Helper()
	msg, err := Parse(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	var got []mail.Attachment
	for _, p := range parts {
		ct, _, _ := strings.Cut(p.ContentType, ";")
		got = append(got, mail.Attachment{Name: p.Filename, ContentType: ct, Size: int64(len(p.Data))})
	}
	if !slices.Equal(got, msg.Attachments) {
		t.Errorf("Parts = %+v\nParse  = %+v", got, msg.Attachments)
	}
}

func TestParts_NestedInlineAndRFC2231(t *testing.T) {
	raw := readRaw(t, "attachments-nested.eml")

	parts, err := Parts(raw)
	if err != nil {
		t.Fatal(err)
	}

	want := []Part{
		{Filename: "(unnamed)", ContentType: "image/png", Data: []byte("\x89PNG\r\n\x1a\n")},
		{Filename: "spacer.gif", ContentType: "image/gif", Data: []byte("GIF89a\x01\x00\x01\x00\x00\x00\x00,")},
		{Filename: "résumé €.pdf", ContentType: "application/pdf", Data: []byte("%PDF-1.4\n")},
		{Filename: "notes.txt", ContentType: "text/plain; charset=utf-8", Data: []byte("café = 1")},
	}
	if len(parts) != len(want) {
		t.Fatalf("got %d parts, want %d: %+v", len(parts), len(want), parts)
	}
	for i, w := range want {
		g := parts[i]
		if g.Filename != w.Filename || g.ContentType != w.ContentType || !bytes.Equal(g.Data, w.Data) {
			t.Errorf("part %d = %q %q %q, want %q %q %q", i, g.Filename, g.ContentType, g.Data, w.Filename, w.ContentType, w.Data)
		}
	}
	matchParse(t, raw, parts)
}

func TestParts_ByteIdenticalAcrossTransferEncodings(t *testing.T) {
	binary := make([]byte, 0, 2000)
	for i := range 2000 {
		binary = append(binary, byte(i*7))
	}
	text := []byte("Zoë's notes = 100%  \r\nsecond line, trailing space \r\n\r\nend\r\n")

	var qp bytes.Buffer
	w := quotedprintable.NewWriter(&qp)
	_, _ = w.Write(text)
	_ = w.Close()

	b64 := base64.StdEncoding.EncodeToString(binary)
	var wrapped strings.Builder
	for len(b64) > 76 {
		wrapped.WriteString(b64[:76] + "\r\n")
		b64 = b64[76:]
	}
	wrapped.WriteString(b64)

	raw := fmt.Sprintf("From: a@example.com\r\nSubject: s\r\nContent-Type: multipart/mixed; boundary=b\r\n\r\n"+
		"--b\r\nContent-Type: text/plain\r\n\r\nbody\r\n"+
		"--b\r\nContent-Type: application/octet-stream; name=\"blob.bin\"\r\nContent-Transfer-Encoding: base64\r\n\r\n%s\r\n"+
		"--b\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Disposition: attachment; filename=\"n.txt\"\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n%s\r\n"+
		"--b--\r\n", wrapped.String(), qp.String())

	parts, err := Parts([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}

	if len(parts) != 2 {
		t.Fatalf("got %d parts, want 2", len(parts))
	}
	if !bytes.Equal(parts[0].Data, binary) || parts[0].Filename != "blob.bin" || parts[0].ContentType != "application/octet-stream" {
		t.Errorf("base64 part = %q %q, %d bytes (want %d, identical)", parts[0].Filename, parts[0].ContentType, len(parts[0].Data), len(binary))
	}
	if !bytes.Equal(parts[1].Data, text) || parts[1].Filename != "n.txt" {
		t.Errorf("quoted-printable part = %q %q, want %q", parts[1].Filename, parts[1].Data, text)
	}
	matchParse(t, []byte(raw), parts)
}

func TestParts_MatchesParseOnExistingFixtures(t *testing.T) {
	for _, name := range []string{"mixed-attachment.eml", "plain.eml", "alternative.eml", "html-only.eml", "malformed.eml", "unknown-charset.eml"} {
		t.Run(name, func(t *testing.T) {
			raw := readRaw(t, name)
			parts, err := Parts(raw)
			if err != nil {
				t.Fatal(err)
			}
			matchParse(t, raw, parts)
		})
	}
}

func TestParts_TextAttachmentIsConvertedToUTF8AndLabelledSo(t *testing.T) {
	raw := "From: a@example.com\r\nContent-Type: multipart/mixed; boundary=b\r\n\r\n" +
		"--b\r\nContent-Type: text/plain; charset=iso-8859-1\r\nContent-Disposition: attachment; filename=a.txt\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\ncaf=E9\r\n--b--\r\n"

	parts, err := Parts([]byte(raw))
	if err != nil || len(parts) != 1 {
		t.Fatalf("Parts = %+v, %v", parts, err)
	}
	if p := parts[0]; p.ContentType != "text/plain; charset=utf-8" || string(p.Data) != "café" {
		t.Errorf("part = %q %q", p.ContentType, p.Data)
	}
}

func TestParts_UnknownCharsetKeepsRawBytesAndLabel(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{"multipart", "From: a@example.com\r\nContent-Type: multipart/mixed; boundary=b\r\n\r\n" +
			"--b\r\nContent-Type: text/plain; charset=x-unknown\r\nContent-Disposition: attachment; filename=a.txt\r\n\r\ncaf\xe9\r\n--b--\r\n"},
		{"single-part root", "From: a@example.com\r\nContent-Type: text/plain; charset=x-unknown; name=a.txt\r\n\r\ncaf\xe9"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parts, err := Parts([]byte(tt.raw))
			if err != nil || len(parts) != 1 {
				t.Fatalf("Parts = %+v, %v", parts, err)
			}
			if p := parts[0]; p.ContentType != "text/plain; charset=x-unknown" || !bytes.HasPrefix(p.Data, []byte("caf\xe9")) || len(p.Data) > len("caf\xe9\r\n") {
				t.Errorf("part = %q %q", p.ContentType, p.Data)
			}
		})
	}
}

func TestParts_SingleAttachmentRootIsConverted(t *testing.T) {
	raw := "From: a@example.com\r\nContent-Type: text/plain; charset=iso-8859-1; name=a.txt\r\n\r\ncaf\xe9"
	parts, err := Parts([]byte(raw))
	if err != nil || len(parts) != 1 {
		t.Fatalf("Parts = %+v, %v", parts, err)
	}
	if p := parts[0]; p.ContentType != "text/plain; charset=utf-8" || string(p.Data) != "café" {
		t.Errorf("part = %q %q", p.ContentType, p.Data)
	}
}

func TestParts_NoAttachmentsIsEmpty(t *testing.T) {
	parts, err := Parts(readRaw(t, "plain.eml"))
	if err != nil || len(parts) != 0 {
		t.Errorf("Parts = %+v, %v; want none", parts, err)
	}
}

func TestParts_UnreadableHeaderFails(t *testing.T) {
	if _, err := Parts([]byte("not a header\x00\n")); err == nil {
		t.Error("want an error")
	}
}
