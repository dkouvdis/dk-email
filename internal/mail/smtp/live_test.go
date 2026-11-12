//go:build live

package smtp

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/dkouvdis/dk-email/internal/config"
	"github.com/dkouvdis/dk-email/internal/secret"
)

// TestLiveSendToSelf sends a one-line message from a real account to itself,
// and only to itself.
//
//	DK_EMAIL_LIVE=1 DK_EMAIL_LIVE_ACCOUNT=Personal go test -tags live -run TestLiveSendToSelf ./internal/mail/smtp/ -v
func TestLiveSendToSelf(t *testing.T) {
	if os.Getenv("DK_EMAIL_LIVE") != "1" {
		t.Skip("set DK_EMAIL_LIVE=1 and DK_EMAIL_LIVE_ACCOUNT to send a real message to yourself")
	}
	name := os.Getenv("DK_EMAIL_LIVE_ACCOUNT")
	if name == "" {
		t.Skip("set DK_EMAIL_LIVE_ACCOUNT to an account name from config.toml to run")
	}

	path, err := config.DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath() error = %v", err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load(%q) error = %v", path, err)
	}
	var acct *config.Account
	for i := range cfg.Accounts {
		if cfg.Accounts[i].Name == name {
			acct = &cfg.Accounts[i]
		}
	}
	if acct == nil {
		t.Fatalf("no account named %q in %s", name, path)
	}
	if acct.SMTPHost == "" {
		t.Skipf("account %q has no smtp_host", name)
	}
	t.Logf("account %q -> %s:%d (smtp_tls %s) as %s", acct.Name, acct.SMTPHost, acct.SMTPPort, acct.SMTPTLS, acct.Username)

	// 1Password may prompt, so allow time for approval.
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	password, err := secret.New().Resolve(ctx, *acct)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	now := time.Now().Format(time.RFC1123Z)
	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: dk-email SMTP live test\r\nDate: %s\r\n"+
		"MIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n"+
		"Sent by dk-email's live SMTP test at %s.\r\n", acct.Email, acct.Email, now, now)

	if err := New(*acct, password).Send(ctx, acct.Email, []string{acct.Email}, []byte(msg)); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	t.Logf("sent to %s; check the inbox", acct.Email)
}
