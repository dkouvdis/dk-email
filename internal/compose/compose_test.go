package compose

import (
	"bytes"
	"flag"
	"io"
	"net/mail"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	gomail "github.com/emersion/go-message/mail"

	dkmail "github.com/dkouvdis/dk-email/internal/mail"
)

var update = flag.Bool("update", false, "rewrite the golden .eml files in testdata")

var (
	fixedNow = time.Date(2026, 11, 12, 14, 53, 0, 0, time.FixedZone("CET", 3600))
	fixedID  = "00112233445566778899aabbccddeeff@example.com"
	alice    = dkmail.Address{Name: "Alice Example", Email: "alice@example.com"}
	bob      = dkmail.Address{Name: "Bob", Email: "bob@example.org"}
)

type buildCase struct {
	name string
	d    dkmail.Draft
}

func buildCases() []buildCase {
	long := strings.Repeat("word ", 400) + "\n" + strings.Repeat("x", 1500) + "\nend\n"
	return []buildCase{
		{name: "ascii", d: dkmail.Draft{
			From: alice, To: []dkmail.Address{bob}, Cc: []dkmail.Address{{Email: "carol@example.net"}},
			Subject: "Lunch on Friday", Body: "Shall we try the new place?\n\nAlice\n",
		}},
		{name: "non-ascii", d: dkmail.Draft{
			From:    dkmail.Address{Name: "Zoë Müller", Email: "zoe@example.com"},
			To:      []dkmail.Address{{Name: "José Álvarez, Jr.", Email: "jose@example.es"}},
			Subject: "Café ☕ — réunion à 10h", Body: "Bonjour — ça va ?\nTrailing space \nno final newline",
		}},
		{name: "long-line", d: dkmail.Draft{
			From: alice, To: []dkmail.Address{bob},
			Subject: strings.Repeat("A very long subject ", 12), Body: long,
		}},
		{name: "bcc-omitted", d: dkmail.Draft{
			From: alice, To: []dkmail.Address{bob}, Bcc: []dkmail.Address{{Name: "Secret", Email: "secret@example.com"}},
			Subject: "Quiet copy", Body: "Nobody sees the Bcc.\n",
		}},
		{name: "attachments", d: dkmail.Draft{
			From: alice, To: []dkmail.Address{bob}, Subject: "Files", Body: "See attached.\n",
			Attachments: []dkmail.File{
				{Name: "notes.txt", ContentType: "text/plain; charset=utf-8", Data: []byte("line one\nline two\n")},
				{Name: "résumé.pdf", Data: bytes.Repeat([]byte{0, 1, 2, 250, 255, 'A'}, 40)},
			},
		}},
		{name: "reply", d: dkmail.Draft{
			From: alice, To: []dkmail.Address{bob}, Subject: "Re: Lunch on Friday",
			InReplyTo:  "<msg2@example.org>",
			References: []string{"msg1@example.org", "msg2@example.org"},
			Body:       "Sounds good.\n\n> Shall we?\n",
		}},
	}
}

func TestBuild_Golden(t *testing.T) {
	for _, tc := range buildCases() {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Build(tc.d, fixedNow, fixedID)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join("testdata", tc.name+".eml")
			if *update {
				if err := os.WriteFile(path, got, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("%v (run with -update to create it)", err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("output differs from %s (run with -update after checking):\n%s", path, got)
			}
		})
	}
}

func TestBuild_WellFormedAndReparses(t *testing.T) {
	for _, tc := range buildCases() {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := Build(tc.d, fixedNow, fixedID)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(bytes.ReplaceAll(raw, []byte("\r\n"), nil), []byte("\n")) {
				t.Error("found a bare LF; all line endings must be CRLF")
			}
			for _, line := range bytes.Split(raw, []byte("\r\n")) {
				if len(line) > 998 {
					t.Fatalf("line of %d bytes exceeds the 998 limit", len(line))
				}
			}
			if regexp.MustCompile(`(?mi)^bcc:`).Match(raw) {
				t.Error("Bcc header written")
			}

			r, err := gomail.CreateReader(bytes.NewReader(raw))
			if err != nil {
				t.Fatal(err)
			}
			h := r.Header
			if got, _ := h.Subject(); got != strings.TrimSpace(tc.d.Subject) && got != tc.d.Subject {
				t.Errorf("Subject = %q, want %q", got, tc.d.Subject)
			}
			if got, _ := h.MessageID(); got != fixedID {
				t.Errorf("Message-ID = %q, want %q", got, fixedID)
			}
			if got, _ := h.Date(); !got.Equal(fixedNow) || !strings.HasSuffix(h.Get("Date"), "+0100") {
				t.Errorf("Date = %q, want %v in its own zone", h.Get("Date"), fixedNow)
			}
			if h.Get("MIME-Version") != "1.0" {
				t.Errorf("MIME-Version = %q", h.Get("MIME-Version"))
			}
			for key, want := range map[string][]dkmail.Address{
				"From": {tc.d.From}, "To": tc.d.To, "Cc": tc.d.Cc,
			} {
				got, _ := h.AddressList(key)
				if !sameAddrs(got, want) {
					t.Errorf("%s = %v, want %v", key, got, want)
				}
			}

			var body string
			var files []dkmail.File
			for {
				p, err := r.NextPart()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				data, _ := io.ReadAll(p.Body)
				switch ph := p.Header.(type) {
				case *gomail.InlineHeader:
					body = string(data)
				case *gomail.AttachmentHeader:
					name, _ := ph.Filename()
					ct, _, _ := ph.ContentType()
					files = append(files, dkmail.File{Name: name, ContentType: ct, Data: data})
				}
			}
			if got := strings.ReplaceAll(body, "\r\n", "\n"); got != tc.d.Body {
				t.Errorf("body = %q, want %q", got, tc.d.Body)
			}
			if len(files) != len(tc.d.Attachments) {
				t.Fatalf("got %d attachments, want %d", len(files), len(tc.d.Attachments))
			}
			for i, a := range tc.d.Attachments {
				if files[i].Name != a.Name || !bytes.Equal(files[i].Data, a.Data) {
					t.Errorf("attachment %d = %q (%d bytes), want %q (%d bytes)", i, files[i].Name, len(files[i].Data), a.Name, len(a.Data))
				}
			}
		})
	}
}

func TestBuild_ReplyHeadersAndTextPartFirst(t *testing.T) {
	tc := buildCases()[5]
	raw, err := Build(tc.d, fixedNow, "<"+fixedID+">")
	if err != nil {
		t.Fatal(err)
	}
	m, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if got := m.Header.Get("In-Reply-To"); got != "<msg2@example.org>" {
		t.Errorf("In-Reply-To = %q", got)
	}
	if got := m.Header.Get("References"); got != "<msg1@example.org> <msg2@example.org>" {
		t.Errorf("References = %q", got)
	}
	if got := m.Header.Get("Message-Id"); got != "<"+fixedID+">" {
		t.Errorf("Message-ID = %q (brackets in msgID must not double)", got)
	}

	tc = buildCases()[4]
	raw, _ = Build(tc.d, fixedNow, fixedID)
	if i, j := bytes.Index(raw, []byte("text/plain")), bytes.Index(raw, []byte("Content-Disposition")); i < 0 || j < i {
		t.Error("text part must come before the attachments")
	}
}

func TestBuild_ReplyHeadersAbsentWhenUnset(t *testing.T) {
	raw, err := Build(buildCases()[0].d, fixedNow, fixedID)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range []string{"In-Reply-To:", "References:"} {
		if bytes.Contains(raw, []byte(h)) {
			t.Errorf("%s written without a value", h)
		}
	}
}

func TestBuild_SanitizesInjectionAndFilenames(t *testing.T) {
	d := dkmail.Draft{
		From: alice, To: []dkmail.Address{{Name: "Eve\r\nBcc: x@y.z", Email: "eve@example.com"}},
		Subject: "Hi\r\nBcc: evil@example.com", Body: "x",
	}
	d.Attachments = []dkmail.File{
		{Name: "../../etc/pass\nwd", ContentType: "bad content type"},
		{Name: ""},
		{Name: `C:\dir\win.txt`, ContentType: "text/plain"},
	}
	raw, err := Build(d, fixedNow, fixedID)
	if err != nil {
		t.Fatal(err)
	}
	if regexp.MustCompile(`(?mi)^bcc:`).Match(raw) {
		t.Errorf("header injection:\n%s", raw)
	}
	r, err := gomail.CreateReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	var names, types []string
	for {
		p, err := r.NextPart()
		if err != nil {
			break
		}
		if ah, ok := p.Header.(*gomail.AttachmentHeader); ok {
			n, _ := ah.Filename()
			ct, _, _ := ah.ContentType()
			names, types = append(names, n), append(types, ct)
		}
	}
	if got, want := strings.Join(names, "|"), "pass_wd|attachment|win.txt"; got != want {
		t.Errorf("filenames = %q, want %q", got, want)
	}
	if got, want := strings.Join(types, "|"), "application/octet-stream|application/octet-stream|text/plain"; got != want {
		t.Errorf("content types = %q, want %q", got, want)
	}
}

func TestBuild_Deterministic(t *testing.T) {
	tc := buildCases()[4]
	a, _ := Build(tc.d, fixedNow, fixedID)
	b, _ := Build(tc.d, fixedNow, fixedID)
	c, _ := Build(tc.d, fixedNow, "other@example.com")
	if !bytes.Equal(a, b) {
		t.Error("same input produced different output")
	}
	if bytes.Equal(a, c) {
		t.Error("a different Message-ID should give a different boundary")
	}
}

func TestBodyPart(t *testing.T) {
	h, body := BodyPart(dkmail.Draft{Body: "héllo"})
	ct, params, _ := h.ContentType()
	if ct != "text/plain" || params["charset"] != "utf-8" || h.Get("Content-Transfer-Encoding") != "quoted-printable" {
		t.Errorf("header = %q %v %q", ct, params, h.Get("Content-Transfer-Encoding"))
	}
	if string(body) != "héllo" {
		t.Errorf("body = %q", body)
	}
}

func TestMessageID(t *testing.T) {
	re := regexp.MustCompile(`^[0-9a-f]{32}@example\.com$`)
	a, b := MessageID(alice), MessageID(alice)
	if !re.MatchString(a) {
		t.Errorf("MessageID = %q, want 32 hex chars @ example.com and no angle brackets", a)
	}
	if a == b {
		t.Error("two ids are equal")
	}
	for _, email := range []string{"nodomain", "me@", "", "me@bad domain>", "me@exämple.com"} {
		if got := MessageID(dkmail.Address{Email: email}); !strings.HasSuffix(got, "@localhost") {
			t.Errorf("MessageID(%q) = %q, want @localhost", email, got)
		}
	}
	if got := MessageID(dkmail.Address{Email: "a@b@sub.example.org"}); !strings.HasSuffix(got, "@sub.example.org") {
		t.Errorf("MessageID = %q, want the domain after the last @", got)
	}
}

func TestNew(t *testing.T) {
	d := New(alice)
	if d.From != alice || len(d.To)+len(d.Cc)+len(d.Bcc) != 0 || d.Subject != "" || d.Body != "" {
		t.Errorf("New = %+v", d)
	}
}

func sameAddrs(got []*gomail.Address, want []dkmail.Address) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i].Name != want[i].Name || got[i].Address != want[i].Email {
			return false
		}
	}
	return true
}
