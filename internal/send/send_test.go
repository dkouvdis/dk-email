package send

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-message"

	"github.com/dkouvdis/dk-email/internal/compose"
	"github.com/dkouvdis/dk-email/internal/config"
	"github.com/dkouvdis/dk-email/internal/mail"
	"github.com/dkouvdis/dk-email/internal/mail/imap"
	"github.com/dkouvdis/dk-email/internal/mail/smtp"
	"github.com/dkouvdis/dk-email/internal/render"
)

var (
	_ Transport = (*smtp.Transport)(nil)
	_ Store     = (*imap.Client)(nil)
)

type fakeTransport struct {
	calls *[]string
	err   error
	from  string
	rcpts []string
	msg   []byte
}

func (f *fakeTransport) Send(_ context.Context, from string, rcpts []string, msg []byte) error {
	*f.calls = append(*f.calls, "smtp")
	f.from, f.rcpts, f.msg = from, rcpts, msg
	return f.err
}

type fakeStore struct {
	calls *[]string

	rawErr, sentErr, appendErr, flagErr error
	noSent                              bool
	raw                                 []byte

	rawFolder    string
	rawUID       mail.UID
	appendFolder string
	appended     []byte
	appendFlags  []mail.Flag
	flagFolder   string
	flagUIDs     []mail.UID
	flag         mail.Flag
	flagOn       bool
}

func (f *fakeStore) Raw(_ context.Context, folder string, uid mail.UID) ([]byte, error) {
	*f.calls = append(*f.calls, "raw")
	f.rawFolder, f.rawUID = folder, uid
	return f.raw, f.rawErr
}

func (f *fakeStore) SentFolder(context.Context) (string, bool, error) {
	*f.calls = append(*f.calls, "sentfolder")
	return "Sent Items", !f.noSent, f.sentErr
}

func (f *fakeStore) Append(_ context.Context, folder string, raw []byte, flags ...mail.Flag) error {
	*f.calls = append(*f.calls, "append")
	f.appendFolder, f.appended, f.appendFlags = folder, raw, flags
	return f.appendErr
}

func (f *fakeStore) SetFlag(_ context.Context, folder string, uids []mail.UID, fl mail.Flag, on bool) error {
	*f.calls = append(*f.calls, "setflag")
	f.flagFolder, f.flagUIDs, f.flag, f.flagOn = folder, uids, fl, on
	return f.flagErr
}

type rig struct {
	s     *Sender
	t     *fakeTransport
	st    *fakeStore
	calls *[]string
	logs  *bytes.Buffer
}

func newRig(acct config.Account) *rig {
	calls := new([]string)
	r := &rig{
		t:     &fakeTransport{calls: calls},
		st:    &fakeStore{calls: calls},
		calls: calls,
		logs:  &bytes.Buffer{},
	}
	log := slog.New(slog.NewTextHandler(r.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	r.s = New(acct, r.t, r.st, log)
	r.s.now = func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) }
	r.s.newID = func(mail.Address) string { return "fixedid@example.com" }
	return r
}

func acct() config.Account {
	return config.Account{Name: "Work", Email: "me@example.com", IMAPHost: "imap.example.com", FromName: "Me Myself"}
}

func draft() mail.Draft {
	return mail.Draft{
		From:    mail.Address{Email: "me@example.com"},
		To:      []mail.Address{{Email: "Alice@example.com"}},
		Cc:      []mail.Address{{Email: "alice@EXAMPLE.com"}, {Email: "bob@example.com"}},
		Bcc:     []mail.Address{{Email: "carol@example.com"}, {Email: "BOB@example.com"}},
		Subject: "Quarterly secrets",
		Body:    "the launch is on Friday",
	}
}

func order(r *rig) string { return strings.Join(*r.calls, ",") }

func TestSend_NewMessage(t *testing.T) {
	r := newRig(acct())
	if err := r.s.Send(context.Background(), draft()); err != nil {
		t.Fatal(err)
	}
	if got, want := order(r), "smtp,sentfolder,append"; got != want {
		t.Fatalf("calls = %s, want %s", got, want)
	}
	if r.t.from != "me@example.com" {
		t.Errorf("envelope from = %q", r.t.from)
	}
	want := []string{"Alice@example.com", "bob@example.com", "carol@example.com"}
	if !reflect.DeepEqual(r.t.rcpts, want) {
		t.Errorf("rcpts = %v, want %v", r.t.rcpts, want)
	}
	if !bytes.Equal(r.st.appended, r.t.msg) || len(r.t.msg) == 0 {
		t.Error("appended bytes differ from sent bytes")
	}
	if r.st.appendFolder != "Sent Items" || !reflect.DeepEqual(r.st.appendFlags, []mail.Flag{mail.FlagSeen}) {
		t.Errorf("append folder=%q flags=%v", r.st.appendFolder, r.st.appendFlags)
	}
	if bytes.Contains(bytes.ToLower(r.t.msg), []byte("bcc:")) {
		t.Error("message contains a Bcc header")
	}
	if !bytes.Contains(r.t.msg, []byte("Message-Id: <fixedid@example.com>")) {
		t.Errorf("message id not injected:\n%s", r.t.msg)
	}
}

func TestSend_FromFallsBackToAccount(t *testing.T) {
	r := newRig(acct())
	d := draft()
	d.From = mail.Address{}
	if err := r.s.Send(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(r.t.msg, []byte("From: \"Me Myself\" <me@example.com>")) {
		t.Errorf("From header not built from account:\n%s", r.t.msg)
	}
}

func TestSend_SaveSentDisabled(t *testing.T) {
	no := false
	gmail := acct()
	gmail.IMAPHost = "imap.gmail.com"
	explicit := acct()
	explicit.SaveSent = &no
	for name, a := range map[string]config.Account{"explicit": explicit, "gmail default": gmail} {
		t.Run(name, func(t *testing.T) {
			r := newRig(a)
			if err := r.s.Send(context.Background(), draft()); err != nil {
				t.Fatal(err)
			}
			if got := order(r); got != "smtp" {
				t.Errorf("calls = %s, want smtp only", got)
			}
		})
	}
}

func TestSend_Reply(t *testing.T) {
	r := newRig(acct())
	d := draft()
	d.ReplyTo = &mail.MessageRef{Folder: "INBOX", UID: 42}
	if err := r.s.Send(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	if got, want := order(r), "smtp,sentfolder,append,setflag"; got != want {
		t.Fatalf("calls = %s, want %s", got, want)
	}
	st := r.st
	if st.flagFolder != "INBOX" || !reflect.DeepEqual(st.flagUIDs, []mail.UID{42}) || st.flag != mail.FlagAnswered || !st.flagOn {
		t.Errorf("SetFlag(%q, %v, %v, %v)", st.flagFolder, st.flagUIDs, st.flag, st.flagOn)
	}
}

func TestSend_Forward(t *testing.T) {
	r := newRig(acct())
	r.st.raw = []byte("original raw")
	var got []byte
	r.s.extract = func(raw []byte) ([]compose.Attachment, error) {
		got = raw
		return []compose.Attachment{{Filename: "report.pdf", ContentType: "application/pdf", Data: []byte("%PDF-data")}}, nil
	}
	d := draft()
	d.ForwardOf = &mail.MessageRef{Folder: "Archive", UID: 7}
	if err := r.s.Send(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	if o, want := order(r), "raw,smtp,sentfolder,append"; o != want {
		t.Fatalf("calls = %s, want %s", o, want)
	}
	if r.st.rawFolder != "Archive" || r.st.rawUID != 7 || string(got) != "original raw" {
		t.Errorf("Raw(%q, %d), extractor got %q", r.st.rawFolder, r.st.rawUID, got)
	}
	if !bytes.Contains(r.t.msg, []byte("report.pdf")) {
		t.Error("sent bytes don't contain the attachment")
	}
	if !bytes.Equal(r.st.appended, r.t.msg) {
		t.Error("appended bytes differ from sent bytes")
	}
}

// original builds the raw message being forwarded, attachments included.
func original(t *testing.T, atts ...compose.Attachment) []byte {
	t.Helper()
	d := mail.Draft{
		From:    mail.Address{Email: "alice@example.com"},
		To:      []mail.Address{{Email: "me@example.com"}},
		Subject: "Original",
		Body:    "see attached",
	}
	raw, err := compose.Build(d, atts, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), "orig@example.com")
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func forwardDraft() mail.Draft {
	d := draft()
	d.ForwardOf = &mail.MessageRef{Folder: "Archive", UID: 7}
	return d
}

func TestSend_ForwardCopiesEveryAttachmentIntact(t *testing.T) {
	binary := make([]byte, 5000)
	for i := range binary {
		binary[i] = byte(i * 31)
	}
	atts := []compose.Attachment{
		{Filename: "report.pdf", ContentType: "application/pdf", Data: binary},
		{Filename: "résumé €.txt", ContentType: "text/plain; charset=utf-8", Data: []byte("café\r\nline two\r\n")},
		{Filename: "logo.png", ContentType: "image/png", Data: []byte("\x89PNG\r\n\x1a\n")},
	}
	r := newRig(acct())
	r.st.raw = original(t, atts...)

	if err := r.s.Send(context.Background(), forwardDraft()); err != nil {
		t.Fatal(err)
	}

	if got, want := order(r), "raw,smtp,sentfolder,append"; got != want {
		t.Fatalf("calls = %s, want %s (raw fetched exactly once, before anything is sent)", got, want)
	}
	if r.st.rawFolder != "Archive" || r.st.rawUID != 7 {
		t.Errorf("Raw(%q, %d)", r.st.rawFolder, r.st.rawUID)
	}
	e, err := message.Read(bytes.NewReader(r.t.msg))
	if err != nil {
		t.Fatal(err)
	}
	if mt, _, _ := e.Header.ContentType(); mt != "multipart/mixed" {
		t.Fatalf("Content-Type = %q, want multipart/mixed", mt)
	}
	parts, err := render.Parts(r.t.msg)
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != len(atts) {
		t.Fatalf("sent %d attachments, want %d", len(parts), len(atts))
	}
	for i, a := range atts {
		if p := parts[i]; p.Filename != a.Filename || p.ContentType != a.ContentType || !bytes.Equal(p.Data, a.Data) {
			t.Errorf("attachment %d = %q %q (%d bytes), want %q %q (%d bytes, identical)", i, p.Filename, p.ContentType, len(p.Data), a.Filename, a.ContentType, len(a.Data))
		}
	}
	sent, err := parseSent(t, r.t.msg)
	if err != nil || sent.Body != "the launch is on Friday" {
		t.Errorf("body = %q, %v", sent.Body, err)
	}
	if !bytes.Equal(r.st.appended, r.t.msg) {
		t.Error("appended bytes differ from sent bytes")
	}
}

// parseSent reads the message the way the reader would.
func parseSent(t *testing.T, raw []byte) (mail.Message, error) {
	t.Helper()
	return render.Parse(bytes.NewReader(raw))
}

func TestSend_ForwardWithoutAttachmentsIsPlainText(t *testing.T) {
	r := newRig(acct())
	r.st.raw = original(t)

	if err := r.s.Send(context.Background(), forwardDraft()); err != nil {
		t.Fatal(err)
	}

	e, err := message.Read(bytes.NewReader(r.t.msg))
	if err != nil {
		t.Fatal(err)
	}
	if mt, _, _ := e.Header.ContentType(); mt != "text/plain" {
		t.Errorf("Content-Type = %q, want text/plain", mt)
	}
	if got := order(r); got != "raw,smtp,sentfolder,append" {
		t.Errorf("calls = %s", got)
	}
}

func TestSend_ForwardOfUnreadableOriginalSendsNothing(t *testing.T) {
	r := newRig(acct())
	r.st.raw = []byte("not a header\x00\n")

	err := r.s.Send(context.Background(), forwardDraft())

	if err == nil || errors.Is(err, mail.ErrAfterDelivery) {
		t.Fatalf("err = %v, want a pre-delivery error", err)
	}
	if got := order(r); got != "raw" || r.t.msg != nil || r.st.appended != nil {
		t.Errorf("calls = %s, sent %q, appended %q; want only the fetch", got, r.t.msg, r.st.appended)
	}
}

func TestSend_NothingSentErrors(t *testing.T) {
	boom := errors.New("boom")
	fwd := func(d *mail.Draft) { d.ForwardOf = &mail.MessageRef{Folder: "INBOX", UID: 1} }
	tests := []struct {
		name  string
		setup func(r *rig, d *mail.Draft)
		calls string
	}{
		{"smtp", func(r *rig, d *mail.Draft) { r.t.err = boom }, "smtp"},
		{"raw", func(r *rig, d *mail.Draft) { r.st.rawErr = boom; fwd(d) }, "raw"},
		{"extract", func(r *rig, d *mail.Draft) {
			fwd(d)
			r.s.extract = func([]byte) ([]compose.Attachment, error) { return nil, boom }
		}, "raw"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(acct())
			d := draft()
			d.ReplyTo = &mail.MessageRef{Folder: "INBOX", UID: 1}
			tc.setup(r, &d)
			err := r.s.Send(context.Background(), d)
			if !errors.Is(err, boom) {
				t.Fatalf("err = %v, want it to wrap boom", err)
			}
			if errors.Is(err, mail.ErrAfterDelivery) {
				t.Error("error wraps ErrAfterDelivery")
			}
			if got := order(r); got != tc.calls {
				t.Errorf("calls = %s, want %s", got, tc.calls)
			}
		})
	}
}

func TestSend_NoRecipients(t *testing.T) {
	r := newRig(acct())
	d := draft()
	d.To, d.Cc, d.Bcc = nil, []mail.Address{{Email: ""}}, nil
	if err := r.s.Send(context.Background(), d); err == nil {
		t.Fatal("expected error")
	}
	if len(*r.calls) != 0 {
		t.Errorf("calls = %v", *r.calls)
	}
}

func TestSend_NotSetUp(t *testing.T) {
	if err := New(acct(), nil, &fakeStore{calls: new([]string)}, nil).Send(context.Background(), draft()); err == nil {
		t.Error("nil transport: expected error")
	}
	if err := New(acct(), &fakeTransport{calls: new([]string)}, nil, nil).Send(context.Background(), draft()); err == nil {
		t.Error("nil store: expected error")
	}
}

func TestSend_CancelledContextSendsNothing(t *testing.T) {
	r := newRig(acct())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := r.s.Send(ctx, draft())
	if !errors.Is(err, context.Canceled) || errors.Is(err, mail.ErrAfterDelivery) {
		t.Fatalf("err = %v", err)
	}
	if len(*r.calls) != 0 {
		t.Errorf("calls = %v", *r.calls)
	}
}

func TestSend_AfterDeliveryFailures(t *testing.T) {
	boom := errors.New("boom")
	tests := []struct {
		name    string
		setup   func(st *fakeStore)
		calls   string
		message string
		cause   error
	}{
		{"sent folder error", func(st *fakeStore) { st.sentErr = boom },
			"smtp,sentfolder,setflag", "sent, but couldn't save a copy to Sent: boom", boom},
		{"no sent folder", func(st *fakeStore) { st.noSent = true },
			"smtp,sentfolder,setflag", "sent, but no Sent folder; copy not saved", nil},
		{"append error", func(st *fakeStore) { st.appendErr = boom },
			"smtp,sentfolder,append,setflag", "sent, but couldn't save a copy to Sent: boom", boom},
		{"flag error", func(st *fakeStore) { st.flagErr = boom },
			"smtp,sentfolder,append,setflag", "sent, but couldn't mark the original answered: boom", boom},
		{"both fail", func(st *fakeStore) { st.appendErr, st.flagErr = boom, errors.New("nope") },
			"smtp,sentfolder,append,setflag",
			"sent, but couldn't save a copy to Sent: boom; and couldn't mark the original answered: nope", boom},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(acct())
			tc.setup(r.st)
			d := draft()
			d.ReplyTo = &mail.MessageRef{Folder: "INBOX", UID: 9}
			err := r.s.Send(context.Background(), d)
			if !errors.Is(err, mail.ErrAfterDelivery) {
				t.Fatalf("err = %v, want ErrAfterDelivery", err)
			}
			if tc.cause != nil && !errors.Is(err, tc.cause) {
				t.Errorf("cause not reachable: %v", err)
			}
			if err.Error() != tc.message {
				t.Errorf("message = %q, want %q", err.Error(), tc.message)
			}
			if got := order(r); got != tc.calls {
				t.Errorf("calls = %s, want %s", got, tc.calls)
			}
		})
	}
}

func TestSend_DebugLogHasNoPersonalData(t *testing.T) {
	r := newRig(acct())
	r.st.raw = []byte("original")
	r.s.extract = func([]byte) ([]compose.Attachment, error) { return nil, nil }
	d := draft()
	d.ForwardOf = &mail.MessageRef{Folder: "INBOX", UID: 1}
	d.ReplyTo = &mail.MessageRef{Folder: "INBOX", UID: 1}
	if err := r.s.Send(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	logs := r.logs.String()
	for _, step := range []string{"fetch-original", "build", "smtp", "save-sent", "mark-answered"} {
		if !strings.Contains(logs, "step="+step) {
			t.Errorf("no log line for step %s:\n%s", step, logs)
		}
	}
	if !strings.Contains(logs, "account=Work") || !strings.Contains(logs, "recipients=3") {
		t.Errorf("missing account or recipient count:\n%s", logs)
	}
	for _, secret := range []string{"example.com", "Alice", "Quarterly", "secrets", "launch", "Friday"} {
		if strings.Contains(logs, secret) {
			t.Errorf("log contains %q:\n%s", secret, logs)
		}
	}
}
