package render

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"

	"github.com/dkouvdis/dk-email/internal/mail"
)

func parseFile(t *testing.T, name string) mail.Message {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	m, err := Parse(f)
	if err != nil {
		t.Fatalf("Parse(%s) error = %v", name, err)
	}
	return m
}

func TestParse_PlainWithHeaders(t *testing.T) {
	m := parseFile(t, "plain.eml")

	if m.Body != "Hi Bob,\n\nSee you at the café." || m.BodyFromHTML {
		t.Errorf("Body = %q (fromHTML %v)", m.Body, m.BodyFromHTML)
	}
	if m.Subject != "Café ☕ plans" {
		t.Errorf("Subject = %q", m.Subject)
	}
	if m.From != (mail.Address{Name: "Jane Doe", Email: "jane@example.com"}) {
		t.Errorf("From = %+v", m.From)
	}
	if !slices.Equal(m.To, []mail.Address{{Name: "Bob", Email: "bob@example.com"}, {Email: "carol@example.com"}}) {
		t.Errorf("To = %+v", m.To)
	}
	if !slices.Equal(m.Cc, []mail.Address{{Name: "André", Email: "andre@example.com"}}) {
		t.Errorf("Cc = %+v", m.Cc)
	}
	if !m.Date.Equal(time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("Date = %v", m.Date)
	}
	if m.MessageID != "b@example.com" || m.InReplyTo != "a@example.com" ||
		!slices.Equal(m.References, []string{"root@example.com", "a@example.com"}) {
		t.Errorf("ids = %q / %q / %q", m.MessageID, m.InReplyTo, m.References)
	}
	if len(m.Attachments) != 0 {
		t.Errorf("Attachments = %+v, want none", m.Attachments)
	}
}

func TestParse_HTMLOnlyIsConvertedToText(t *testing.T) {
	m := parseFile(t, "html-only.eml")

	if !m.BodyFromHTML {
		t.Error("BodyFromHTML = false")
	}
	for _, want := range []string{"Hello & welcome!", "the post <https://example.com/post>", "One", "Two"} {
		if !strings.Contains(m.Body, want) {
			t.Errorf("Body missing %q:\n%s", want, m.Body)
		}
	}
	for _, bad := range []string{"<p>", "color: red", "alert(1)", "\r"} {
		if strings.Contains(m.Body, bad) {
			t.Errorf("Body contains %q:\n%s", bad, m.Body)
		}
	}
}

func TestParse_AlternativePrefersPlain(t *testing.T) {
	m := parseFile(t, "alternative.eml")

	if m.Body != "Plain version wins." || m.BodyFromHTML {
		t.Errorf("Body = %q (fromHTML %v)", m.Body, m.BodyFromHTML)
	}
}

func TestParse_MixedListsAttachments(t *testing.T) {
	m := parseFile(t, "mixed-attachment.eml")

	if m.Body != "Report attached." {
		t.Errorf("Body = %q", m.Body)
	}
	want := []mail.Attachment{
		{Name: "report.pdf", ContentType: "application/pdf", Size: 52},
		{Name: "logo.png", ContentType: "image/png", Size: 8},
	}
	if !slices.Equal(m.Attachments, want) {
		t.Errorf("Attachments = %+v, want %+v", m.Attachments, want)
	}
}

func TestParse_CharsetsAndTransferEncodings(t *testing.T) {
	tests := []struct {
		file, want string
	}{
		{"latin1-qp.eml", "Café crème brûlée, a long line that is soft wrapped."},
		{"base64.eml", "Hello from base64 👋"},
		{"unknown-charset.eml", "Still readable."},
	}
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			if got := parseFile(t, tt.file).Body; got != tt.want {
				t.Errorf("Body = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParse_MalformedKeepsWhatItCanRead(t *testing.T) {
	m := parseFile(t, "malformed.eml")

	if !strings.Contains(m.Body, "Text before the end of the world") || m.Subject != "Broken" {
		t.Errorf("Body = %q, Subject = %q", m.Body, m.Subject)
	}
}

func TestParse_UnreadableHeaderFails(t *testing.T) {
	if _, err := Parse(strings.NewReader("this is not\x00 a header\n")); err == nil {
		t.Error("Parse() error = nil, want an error for garbage headers")
	}
}

func TestParse_EmptyBody(t *testing.T) {
	m, err := Parse(strings.NewReader("Subject: Nothing\r\n\r\n"))
	if err != nil || m.Body != "" || m.Subject != "Nothing" {
		t.Errorf("Parse() = %q / %q, %v", m.Subject, m.Body, err)
	}
}

func TestParse_StripsTerminalEscapes(t *testing.T) {
	m := parseFile(t, "escapes.eml")

	for name, s := range map[string]string{"Subject": m.Subject, "Body": m.Body} {
		if strings.ContainsFunc(s, func(r rune) bool {
			return r != '\n' && (unicode.IsControl(r) || isBidiControl(r))
		}) {
			t.Errorf("%s still has control characters: %q", name, s)
		}
	}
	if m.Subject != "Evil subject" {
		t.Errorf("Subject = %q, want the escapes removed", m.Subject)
	}
	for _, want := range []string{"Beforeclear", "clip", "gnp.exe", "nulbell", "tab    here"} {
		if !strings.Contains(m.Body, want) {
			t.Errorf("Body %q missing %q", m.Body, want)
		}
	}
	if strings.Contains(m.Body, "pwned") || strings.Contains(m.Body, "ZXZpbA") {
		t.Errorf("escape sequence payload leaked: %q", m.Body)
	}
}

func TestSanitize(t *testing.T) {
	tests := []struct{ in, want string }{
		{"plain", "plain"},
		{"a\r\nb\rc", "a\nb\nc"},
		{"\x1b[1mbold\x1b[0m", "bold"},
		{"tab\tx", "tab    x"},
		{"\u202eevil\u2069", "evil"},
		{"keep\u200dzwj 👋🏽", "keep\u200dzwj 👋🏽"},
	}
	for _, tt := range tests {
		if got := Sanitize(tt.in); got != tt.want {
			t.Errorf("Sanitize(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
	// A C1 CSI may be dropped alone or with its parameters; either is safe.
	for _, in := range []string{"\x9b31mcsi8bit", "\u009b31mcsi8bit"} {
		got := Sanitize(in)
		if !strings.HasSuffix(got, "csi8bit") || strings.ContainsFunc(got, unicode.IsControl) || !utf8.ValidString(got) {
			t.Errorf("Sanitize(%q) = %q, want control-free UTF-8 ending in csi8bit", in, got)
		}
	}
	if got := SanitizeLine("two\nlines\x1b[2J"); got != "two lines" {
		t.Errorf("SanitizeLine = %q", got)
	}
}

func TestWrap(t *testing.T) {
	text := "The quick brown fox jumps over the lazy dog.\n\n> quoted line that is fairly long\nhttps://example.com/a/very/long/url/that/cannot/break/on/spaces"

	got := Wrap(text, 20)

	for _, l := range strings.Split(got, "\n") {
		if w := ansi.StringWidth(l); w > 20 {
			t.Errorf("line %q is %d wide, want ≤ 20", l, w)
		}
	}
	if !strings.HasPrefix(got, "The quick brown fox\njumps over the lazy\ndog.\n\n") {
		t.Errorf("Wrap kept words and blank lines badly:\n%s", got)
	}
	if strings.ReplaceAll(got, "\n", "") == "" || !strings.Contains(strings.ReplaceAll(got, "\n", ""), "url/that/cannot") {
		t.Errorf("long word lost:\n%s", got)
	}
	if Wrap("abc", 0) != "abc" {
		t.Error("width 0 should leave text alone")
	}
}
