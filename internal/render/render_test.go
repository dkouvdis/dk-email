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
	"github.com/charmbracelet/x/exp/golden"

	"github.com/dkouvdis/dk-email/internal/mail"
	"github.com/dkouvdis/dk-email/internal/ui/theme"
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

func TestParse_HTMLOnlyIsConvertedToMarkdown(t *testing.T) {
	m := parseFile(t, "html-only.eml")

	if !m.BodyFromHTML {
		t.Error("BodyFromHTML = false")
	}
	for _, want := range []string{"Hello & welcome!", "Read [the post](https://example.com/post) today.", "- One\n- Two"} {
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

// Real-world emails (anonymised) whose text/plain parts are junk or that
// are HTML-only. Update with go test ./internal/render -run Golden -update
func TestParse_Golden(t *testing.T) {
	for _, name := range []string{"account-password-reset", "shop-newsletter", "shop-shipping", "news-digest", "community-discussion", "marketplace-newsletter"} {
		t.Run(name, func(t *testing.T) {
			m := parseFile(t, name+".eml")
			if !m.BodyFromHTML {
				t.Error("BodyFromHTML = false, want the HTML part")
			}
			golden.RequireEqual(t, m.Body)
		})
	}
}

func TestPlainIsJunk(t *testing.T) {
	tests := []struct {
		name, plain string
		want        bool
	}{
		{"real text", "Hi Bob,\n\nSee you at 5 <bob@example.com>.\nA & B", false},
		{"one stray tag", "Use <br> for breaks.", false},
		{"empty", " \n \n", true},
		{"leaked tags", "Hello<br>\n<br> world <br>", true},
		{"leaked entities", "&zwnj; &zwnj; &zwnj; hi", true},
		{"zero-width padding", "Briefing " + strings.Repeat("\u200c ", 12) + "\nHi", true},
		{"one zero-width char", "Hi\u200cthere", false},
		{"template code", "%%=redirectTo(concat(\nHi", true},
		{"generated links", "View comment ( https://example.com/a )\n\nUnsubscribe ( https://example.com/u )", true},
		{"hubspot links", "View listing (https://example.com/a )\n\nManage preferences (https://example.com/p )", true},
		{"one spaced link", "See the docs ( https://example.com/a ) for more.", false},
		{"mostly blank", strings.Repeat("\n", 30) + "Hi", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := plainIsJunk(tt.plain); got != tt.want {
				t.Errorf("plainIsJunk(%q) = %v, want %v", tt.plain, got, tt.want)
			}
		})
	}
}

func TestHTMLToMarkdown_StripsEmailCruft(t *testing.T) {
	src := `<html><head><title>T</title></head><body>
<div style="display:none; max-height:0">Preheader&zwnj;&nbsp;&zwnj;</div>
<a style="mso-hide: all !important" href="https://example.com/c">View comment</a>
<img src="https://t.example/open.gif" width="1" height="1" alt="pixel">
<!--[if mso]><p>Conditional</p><![endif]-->
<table role="presentation"><tr><td><a href="https://example.com/"><img src="logo.png" alt=""></a></td></tr>
<tr><td><h1>Big news</h1><p>Body <a href="javascript:alert(1)">bad</a> <a href="https://example.com/x">good</a></p></td></tr></table>
<table><tr><th>Item</th><th>Price</th></tr><tr><td>Coffee</td><td>£9</td></tr></table>
</body></html>`
	got := htmlToMarkdown(src)
	for _, bad := range []string{"Preheader", "pixel", "Conditional", "javascript", "logo", "](https://example.com/)", "\u200c", "T\n"} {
		if strings.Contains(got, bad) {
			t.Errorf("output contains %q:\n%s", bad, got)
		}
	}
	for _, want := range []string{"[View comment](https://example.com/c)", "# Big news", "Body bad [good](https://example.com/x)", "| Item   | Price |", "| Coffee | £9    |"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
}

func TestMarkdown_StylesAndFitsWidth(t *testing.T) {
	md := "# Order shipped\n\nShip To:\nTest Customer\nFlat 1\n\n[Track](https://example.com/" + strings.Repeat("x", 60) + ")"
	out, err := Markdown(md, 30, theme.Default().Markdown)
	if err != nil {
		t.Fatal(err)
	}
	plain := ansi.Strip(out)
	if !strings.Contains(plain, "Order shipped") || strings.Contains(plain, "#") {
		t.Errorf("heading not styled:\n%s", plain)
	}
	if !strings.Contains(plain, "Ship To:\nTest Customer\nFlat 1") {
		t.Errorf("line breaks not kept:\n%s", plain)
	}
	for l := range strings.SplitSeq(plain, "\n") {
		if ansi.StringWidth(l) > 30 {
			t.Errorf("line wider than 30: %q", l)
		}
	}
	if out == plain {
		t.Error("no styling applied")
	}
}

func TestMarkdown_LinksShowOnlyTextAsHyperlinks(t *testing.T) {
	cases := []struct {
		name, md, want string
		width          int
		urls           []string
	}{
		{"link", "See [the offer](https://example.com/a).", "See the offer.", 80, []string{"https://example.com/a"}},
		{"bare URL", "Visit https://example.com/b now", "Visit https://example.com/b now", 80, []string{"https://example.com/b"}},
		{"wrapped link text", "aaaa bbbb cccc [link text](https://example.com/c) dd", "aaaa bbbb cccc link\ntext dd", 22, []string{"https://example.com/c"}},
		{"two links", "[one](https://example.com/1) or [two](https://example.com/2)", "one or two", 80, []string{"https://example.com/1", "https://example.com/2"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := Markdown(c.md, c.width, theme.Default().Markdown)
			if err != nil {
				t.Fatal(err)
			}
			if plain := ansi.Strip(out); plain != c.want {
				t.Errorf("text = %q, want %q", plain, c.want)
			}
			for _, u := range c.urls {
				if !strings.Contains(out, ";"+u+"\a") {
					t.Errorf("no hyperlink to %s in %q", u, out)
				}
			}
			if !strings.Contains(out, "4m") {
				t.Errorf("links not underlined: %q", out)
			}
		})
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

func TestParse_ReplyTo(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   []mail.Address
	}{
		{"absent", "", nil},
		{"one", "Reply-To: Support <help@example.com>\r\n", []mail.Address{{Name: "Support", Email: "help@example.com"}}},
		{"two", "Reply-To: a@example.com, \"B, Bee\" <b@example.com>\r\n", []mail.Address{{Email: "a@example.com"}, {Name: "B, Bee", Email: "b@example.com"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := Parse(strings.NewReader("From: x@example.com\r\n" + tt.header + "Subject: Hi\r\n\r\nbody\r\n"))
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(m.ReplyTo, tt.want) || (tt.want == nil) != (m.ReplyTo == nil) {
				t.Errorf("ReplyTo = %#v, want %#v", m.ReplyTo, tt.want)
			}
		})
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
