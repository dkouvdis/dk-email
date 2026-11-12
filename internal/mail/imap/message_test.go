package imap

import (
	"errors"
	"strings"
	"testing"
	"time"

	goimap "github.com/emersion/go-imap/v2"

	"github.com/dkouvdis/dk-email/internal/mail"
)

func TestMessage_FetchesAndParsesWithoutSettingSeen(t *testing.T) {
	srv := startServer(t, modeImplicit, capsListStatus)
	srv.createMailbox(t, "Mail")
	srv.appendRaw(t, "Mail", `Message-ID: <m1@example.com>
From: Jane Doe <jane@example.com>
To: bob@example.com
Cc: carol@example.com
Subject: Hello
Content-Type: multipart/mixed; boundary="b"

--b
Content-Type: text/plain; charset=utf-8

Body text.
--b
Content-Type: application/pdf; name="a.pdf"
Content-Transfer-Encoding: base64

AAAA
--b--
`, base(3), goimap.FlagFlagged)
	c := srv.newClient(t, modeImplicit)
	ctx := timeoutCtx(t, 10*time.Second)
	sums, err := c.Summaries(ctx, "Mail", 0, 10)
	if err != nil {
		t.Fatal(err)
	}

	got, err := c.Message(ctx, "Mail", sums[0].UID)

	if err != nil {
		t.Fatalf("Message() error = %v", err)
	}
	if got.UID != sums[0].UID || got.Body != "Body text." || got.Subject != "Hello" || got.MessageID != "m1@example.com" {
		t.Errorf("Message() = %+v", got)
	}
	if got.Seen || !got.Flagged {
		t.Errorf("flags seen=%v flagged=%v, want unseen and flagged", got.Seen, got.Flagged)
	}
	if !got.Date.Equal(base(3)) {
		t.Errorf("Date = %v, want the internal date %v (no Date header)", got.Date, base(3))
	}
	if len(got.Cc) != 1 || len(got.Attachments) != 1 || got.Attachments[0].Name != "a.pdf" {
		t.Errorf("Cc %v, attachments %+v", got.Cc, got.Attachments)
	}

	status, err := srv.user.Status("Mail", &goimap.StatusOptions{NumUnseen: true})
	if err != nil {
		t.Fatal(err)
	}
	if *status.NumUnseen != 1 {
		t.Errorf("server unseen = %d after Message, want 1 (no implicit \\Seen)", *status.NumUnseen)
	}
}

func TestMessage_Errors(t *testing.T) {
	srv := startServer(t, modeImplicit, capsListStatus)
	c := srv.newClient(t, modeImplicit)
	ctx := timeoutCtx(t, 10*time.Second)

	if _, err := c.Message(ctx, "INBOX", 9999); !errors.Is(err, mail.ErrMessageNotFound) {
		t.Errorf("missing UID error = %v, want mail.ErrMessageNotFound", err)
	}
	if _, err := c.Message(ctx, "Nope", 1); !errors.Is(err, mail.ErrFolderNotFound) {
		t.Errorf("missing folder error = %v, want mail.ErrFolderNotFound", err)
	}
	if n := srv.ln.accepted(); n != 1 {
		t.Errorf("accepted %d connections, want 1 (errors must not reconnect)", n)
	}
}

func TestMessage_UIDValidityChange(t *testing.T) {
	srv := startServer(t, modeImplicit, capsListStatus)
	srv.seedBulk(t, "Bulk", 2)
	c := srv.newClient(t, modeImplicit)
	ctx := timeoutCtx(t, 10*time.Second)
	if _, err := c.Summaries(ctx, "Bulk", 0, 10); err != nil {
		t.Fatal(err)
	}
	if err := srv.user.Delete("Bulk"); err != nil {
		t.Fatal(err)
	}
	srv.seedBulk(t, "Bulk", 2)

	_, err := c.Message(ctx, "Bulk", 1)

	if !errors.Is(err, mail.ErrFolderChanged) || !strings.Contains(err.Error(), "Bulk") {
		t.Errorf("error = %v, want mail.ErrFolderChanged naming the folder", err)
	}
}

func TestRaw_ReturnsExactBytesWithoutSettingSeen(t *testing.T) {
	srv := startServer(t, modeImplicit, capsListStatus)
	srv.createMailbox(t, "Mail")
	raw := "Message-ID: <r1@example.com>\r\nSubject: Raw\r\nContent-Type: text/plain\r\n\r\nh\xc3\xa9llo\r\n\r\n.\r\nlast line without newline"
	if _, err := srv.user.Append("Mail", newLiteral(raw), &goimap.AppendOptions{Time: base(1)}); err != nil {
		t.Fatal(err)
	}
	c := srv.newClient(t, modeImplicit)
	ctx := timeoutCtx(t, 10*time.Second)

	got, err := c.Raw(ctx, "Mail", 1)

	if err != nil {
		t.Fatalf("Raw() error = %v", err)
	}
	if string(got) != raw {
		t.Errorf("Raw() = %q, want %q", got, raw)
	}
	status, err := srv.user.Status("Mail", &goimap.StatusOptions{NumUnseen: true})
	if err != nil {
		t.Fatal(err)
	}
	if *status.NumUnseen != 1 {
		t.Errorf("server unseen = %d after Raw, want 1 (no implicit \\Seen)", *status.NumUnseen)
	}
}

func TestRaw_Errors(t *testing.T) {
	srv := startServer(t, modeImplicit, capsListStatus)
	srv.seedBulk(t, "Bulk", 2)
	c := srv.newClient(t, modeImplicit)
	ctx := timeoutCtx(t, 10*time.Second)

	if _, err := c.Raw(ctx, "INBOX", 9999); !errors.Is(err, mail.ErrMessageNotFound) {
		t.Errorf("missing UID error = %v, want mail.ErrMessageNotFound", err)
	}
	if _, err := c.Raw(ctx, "Nope", 1); !errors.Is(err, mail.ErrFolderNotFound) {
		t.Errorf("missing folder error = %v, want mail.ErrFolderNotFound", err)
	}
	if n := srv.ln.accepted(); n != 1 {
		t.Errorf("accepted %d connections, want 1 (errors must not reconnect)", n)
	}

	if _, err := c.Summaries(ctx, "Bulk", 0, 10); err != nil {
		t.Fatal(err)
	}
	if err := srv.user.Delete("Bulk"); err != nil {
		t.Fatal(err)
	}
	srv.seedBulk(t, "Bulk", 2)
	if _, err := c.Raw(ctx, "Bulk", 1); !errors.Is(err, mail.ErrFolderChanged) {
		t.Errorf("after UIDVALIDITY change error = %v, want mail.ErrFolderChanged", err)
	}
}
