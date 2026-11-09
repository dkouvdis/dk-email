//go:build live

package imap

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/dkouvdis/dk-email/internal/config"
	"github.com/dkouvdis/dk-email/internal/mail"
	"github.com/dkouvdis/dk-email/internal/secret"
)

// TestLiveFolders lists the folders of a real account. It is strictly
// read-only: it only logs in and issues LIST/STATUS.
//
//	DK_EMAIL_LIVE_ACCOUNT=Personal go test -tags live -run TestLiveFolders ./internal/mail/imap/ -v
func TestLiveFolders(t *testing.T) {
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

	// 1Password may prompt, so allow time for approval.
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	password, err := secret.New().Resolve(ctx, *acct)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	c, err := Dial(ctx, *acct, password)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer c.Close()

	folders, err := c.Folders(ctx)
	if err != nil {
		t.Fatalf("Folders() error = %v", err)
	}

	var hasInbox, hasAll, hasTrash bool
	for _, f := range folders {
		t.Logf("%-30q role=%-8v unread=%d total=%d", f.Name, f.Role, f.Unread, f.Total)
		switch f.Role {
		case mail.RoleInbox:
			hasInbox = true
		case mail.RoleAll:
			hasAll = true
		case mail.RoleTrash:
			hasTrash = true
		}
	}
	if !hasInbox {
		t.Error("no folder has RoleInbox")
	}
	if acct.IMAPHost == "imap.gmail.com" {
		if !hasAll {
			t.Error("no folder has RoleAll, want [Gmail]/All Mail")
		}
		if !hasTrash {
			t.Error("no folder has RoleTrash, want [Gmail]/Trash")
		}
	}
}
