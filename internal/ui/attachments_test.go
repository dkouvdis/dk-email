package ui

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/dkouvdis/dk-email/internal/mail/mailtest"
)

type attFile struct{ name, data string }

// rawMail is a multipart message with one base64 attachment per file; with
// none it is plain text. refs is the References header, or "".
func rawMail(from, id, refs string, at time.Time, files ...attFile) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s <%s@example.com>\r\nTo: me@example.com\r\nSubject: Files\r\nDate: %s\r\nMessage-ID: <%s>\r\n",
		from, strings.ToLower(from), at.Format(time.RFC1123Z), id)
	if refs != "" {
		fmt.Fprintf(&b, "In-Reply-To: <%s>\r\nReferences: <%s>\r\n", refs, refs)
	}
	b.WriteString("MIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=XX\r\n\r\n")
	b.WriteString("--XX\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nSee attached.\r\n")
	for _, f := range files {
		fmt.Fprintf(&b, "--XX\r\nContent-Type: application/octet-stream; name=%q\r\nContent-Disposition: attachment; filename=%q\r\nContent-Transfer-Encoding: base64\r\n\r\n%s\r\n",
			f.name, f.name, base64.StdEncoding.EncodeToString([]byte(f.data)))
	}
	b.WriteString("--XX--\r\n")
	return []byte(b.String())
}

// opener records the paths it is asked to open.
type opener struct {
	paths []string
	err   error
}

func (o *opener) open(path string) error {
	o.paths = append(o.paths, path)
	return o.err
}

// attachFixture opens a thread of the given raw messages (newest on top of
// the list) in the reader with all of them expanded, saving into a temp dir.
func attachFixture(t *testing.T, canOpen bool, raws ...[]byte) (Model, *mailtest.Fake, *opener, string) {
	t.Helper()
	f := newFixture()
	for _, raw := range raws {
		f.personal.AddRaw("INBOX", raw)
	}
	m := started(t, f, 160, 30)
	dir := t.TempDir()
	op := &opener{}
	m.files = fileOpener{dir: func() (string, error) { return dir, nil }, open: op.open, canOpen: func() bool { return canOpen }}
	m = pressSettled(t, m, "enter")
	if !m.readerPopup {
		t.Fatal("reader didn't open")
	}
	if len(raws) > 1 {
		m = pressSettled(t, m, "space")
	}
	return m, f.personal, op, dir
}

func rawFetches(fake *mailtest.Fake) (n int) {
	for _, c := range fake.Calls() {
		if c.Method == mailtest.MethodRaw {
			n++
		}
	}
	return n
}

func TestAttachment_OOpensTheOnlyAttachment(t *testing.T) {
	m, fake, op, dir := attachFixture(t, true, rawMail("Zed", "z1@example.com", "", fixedNow.Add(-time.Hour), attFile{"report.pdf", "%PDF-bytes"}))

	m = pressSettled(t, m, "o")

	path := filepath.Join(dir, "report.pdf")
	if got, err := os.ReadFile(path); err != nil || string(got) != "%PDF-bytes" {
		t.Errorf("saved %q, %v; want the exact bytes", got, err)
	}
	if !slices.Equal(op.paths, []string{path}) {
		t.Errorf("opened %v, want just %s", op.paths, path)
	}
	if m.statusErr || m.status != "Saved and opened "+path+"." {
		t.Errorf("status %q (error %v)", m.status, m.statusErr)
	}
	if rawFetches(fake) != 1 || m.showPicker {
		t.Errorf("raw fetches %d, picker %v; want one fetch and no picker", rawFetches(fake), m.showPicker)
	}
}

func TestAttachment_RunnableIsSavedButNotOpened(t *testing.T) {
	m, _, op, dir := attachFixture(t, true, rawMail("Zed", "z1@example.com", "", fixedNow.Add(-time.Hour), attFile{"run.sh", "echo hi"}))

	m = pressSettled(t, m, "o")

	if _, err := os.Stat(filepath.Join(dir, "run.sh")); err != nil {
		t.Errorf("not saved: %v", err)
	}
	if len(op.paths) != 0 || !strings.Contains(m.status, "can run code") {
		t.Errorf("opened %v, status %q; want no open and a note that it can run code", op.paths, m.status)
	}
}

func TestAttachment_NoAppToOpenItSavesOnly(t *testing.T) {
	m, _, op, dir := attachFixture(t, false, rawMail("Zed", "z1@example.com", "", fixedNow.Add(-time.Hour), attFile{"a.pdf", "x"}))

	m = pressSettled(t, m, "o")

	if _, err := os.Stat(filepath.Join(dir, "a.pdf")); err != nil {
		t.Errorf("not saved: %v", err)
	}
	if len(op.paths) != 0 || !strings.Contains(m.status, "no app to open it here") {
		t.Errorf("opened %v, status %q", op.paths, m.status)
	}
}

func TestAttachment_OpenerFailureIsAnError(t *testing.T) {
	m, _, op, dir := attachFixture(t, true, rawMail("Zed", "z1@example.com", "", fixedNow.Add(-time.Hour), attFile{"a.pdf", "x"}))
	op.err = errors.New("no handler")

	m = pressSettled(t, m, "o")

	if _, err := os.Stat(filepath.Join(dir, "a.pdf")); err != nil {
		t.Errorf("not saved: %v", err)
	}
	if !m.statusErr || !strings.Contains(m.status, "couldn't open it: no handler") {
		t.Errorf("status %q (error %v)", m.status, m.statusErr)
	}
}

func TestAttachment_FetchFailureIsAnErrorAndWritesNothing(t *testing.T) {
	m, fake, op, dir := attachFixture(t, true, rawMail("Zed", "z1@example.com", "", fixedNow.Add(-time.Hour), attFile{"a.pdf", "x"}))
	fake.Fail(mailtest.MethodRaw, errors.New("boom"))

	m = pressSettled(t, m, "o")

	entries, _ := os.ReadDir(dir)
	if !m.statusErr || !strings.Contains(m.status, "boom") || len(entries) != 0 || len(op.paths) != 0 {
		t.Errorf("status %q (error %v), %d files, opened %v", m.status, m.statusErr, len(entries), op.paths)
	}
}

func TestAttachment_NoneSaysSo(t *testing.T) {
	m, fake, _, _ := attachFixture(t, true, rawMail("Zed", "z1@example.com", "", fixedNow.Add(-time.Hour)))

	m, cmd := keyCmd(t, m, "o")

	if cmd != nil || m.status != "No attachments." || rawFetches(fake) != 0 {
		t.Errorf("status %q, cmd %v, raw fetches %d", m.status, cmd != nil, rawFetches(fake))
	}
}

func TestAttachment_OIsIgnoredWithTheReaderClosed(t *testing.T) {
	f := newFixture()
	f.personal.AddRaw("INBOX", rawMail("Zed", "z1@example.com", "", fixedNow.Add(-time.Hour), attFile{"a.pdf", "x"}))
	m := started(t, f, 160, 30)

	m, cmd := keyCmd(t, m, "o")

	if cmd != nil || m.showPicker || m.status != "" || rawFetches(f.personal) != 0 {
		t.Errorf("status %q, cmd %v, picker %v, raw fetches %d", m.status, cmd != nil, m.showPicker, rawFetches(f.personal))
	}
}

// twoMessages is a thread of Zed's message with one attachment and Yan's
// reply with two.
func twoMessages() [][]byte {
	return [][]byte{
		rawMail("Zed", "z1@example.com", "", fixedNow.Add(-90*time.Minute), attFile{"one.txt", "first"}),
		rawMail("Yan", "y1@example.com", "z1@example.com", fixedNow.Add(-time.Hour), attFile{"two.txt", "second"}, attFile{"three.txt", "third"}),
	}
}

func TestAttachment_PickerListsEveryAttachment(t *testing.T) {
	m, fake, op, dir := attachFixture(t, true, twoMessages()...)

	m = send(t, m, press("o"))

	if !m.showPicker || !m.pickingAttachment || rawFetches(fake) != 0 {
		t.Fatalf("picker %v, attachment picker %v, raw fetches %d", m.showPicker, m.pickingAttachment, rawFetches(fake))
	}
	view := plain(m)
	for _, want := range []string{"Open attachment", "one.txt  5 B", "two.txt  6 B", "three.txt  5 B"} {
		if !strings.Contains(view, want) {
			t.Errorf("picker missing %q:\n%s", want, view)
		}
	}

	for _, bad := range []string{"/ ", "· "} {
		if strings.Contains(view, bad) {
			t.Errorf("picker has %q (a filter line or a heading):\n%s", bad, view)
		}
	}

	m = send(t, m, press("j"), press("j")) // one.txt, two.txt, three.txt
	m, cmd := keyCmd(t, m, "enter")
	if cmd == nil {
		t.Fatalf("enter started nothing (status %q)", m.status)
	}
	m = settle(t, m, cmd)

	path := filepath.Join(dir, "three.txt")
	if got, err := os.ReadFile(path); err != nil || string(got) != "third" {
		t.Errorf("saved %q, %v; want the third attachment's bytes", got, err)
	}
	if !slices.Equal(op.paths, []string{path}) || m.showPicker || m.pickingAttachment || !m.readerPopup {
		t.Errorf("opened %v, picker %v, reader %v", op.paths, m.showPicker, m.readerPopup)
	}
}

func TestAttachment_PickerEscClosesWithoutSideEffects(t *testing.T) {
	m, fake, op, _ := attachFixture(t, true, twoMessages()...)
	m = send(t, m, press("o"))

	m = send(t, m, press("esc"))

	if m.showPicker || m.pickingAttachment || !m.readerPopup || rawFetches(fake) != 0 || len(op.paths) != 0 {
		t.Errorf("picker %v/%v, reader %v, raw fetches %d, opened %v", m.showPicker, m.pickingAttachment, m.readerPopup, rawFetches(fake), op.paths)
	}
}

func TestAttachment_ReaderHintsIncludeO(t *testing.T) {
	m, _, _, _ := attachFixture(t, true, rawMail("Zed", "z1@example.com", "", fixedNow.Add(-time.Hour), attFile{"a.pdf", "x"}))

	m = send(t, m, size(240, 30))
	if h := ansi.Strip(m.renderHints()); !strings.Contains(h, "Open attachment: o") {
		t.Errorf("reader hints = %q", h)
	}
}
