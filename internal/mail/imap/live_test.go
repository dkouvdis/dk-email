//go:build live

package imap

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dkouvdis/dk-email/internal/config"
	"github.com/dkouvdis/dk-email/internal/mail"
	"github.com/dkouvdis/dk-email/internal/search"
	"github.com/dkouvdis/dk-email/internal/secret"
)

// liveClient connects to the account named by DK_EMAIL_LIVE_ACCOUNT in the
// default config.toml, or skips the test when it isn't set.
func liveClient(t *testing.T) (context.Context, config.Account, *Client) {
	t.Helper()
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
		names := make([]string, 0, len(cfg.Accounts))
		for _, a := range cfg.Accounts {
			names = append(names, fmt.Sprintf("%q", a.Name))
		}
		t.Fatalf("no account named %q in %s; available accounts: %s", name, path, strings.Join(names, ", "))
	}
	t.Logf("config %s: account %q -> %s:%d (tls %s) as %s", path, acct.Name, acct.IMAPHost, acct.IMAPPort, acct.TLS, acct.Username)

	// 1Password may prompt, so allow time for approval.
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	password, err := secret.New().Resolve(ctx, *acct)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	c, err := Dial(ctx, *acct, password)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	t.Cleanup(func() { c.Close() })
	return ctx, *acct, c
}

// TestLiveFolders lists the folders of a real account. It is strictly
// read-only: it only logs in and issues LIST/STATUS.
//
//	DK_EMAIL_LIVE_ACCOUNT=Personal go test -tags live -run TestLiveFolders ./internal/mail/imap/ -v
func TestLiveFolders(t *testing.T) {
	ctx, acct, c := liveClient(t)

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

// TestLiveSummaries shows the 200 newest INBOX messages. It is read-only:
// EXAMINE, SEARCH and FETCH with BODY.PEEK, and it checks no mail was marked read.
//
//	DK_EMAIL_LIVE_ACCOUNT=Personal go test -tags live -run TestLiveSummaries ./internal/mail/imap/ -v
func TestLiveSummaries(t *testing.T) {
	ctx, _, c := liveClient(t)
	unreadBefore := inboxUnread(ctx, t, c)

	start := time.Now()
	sums, err := c.Summaries(ctx, "INBOX", 0, 200)
	if err != nil {
		t.Fatalf("Summaries() error = %v", err)
	}
	t.Logf("fetched %d summaries in %v", len(sums), time.Since(start).Round(time.Millisecond))

	for _, s := range sums {
		mark := " "
		if !s.Seen {
			mark = "●"
		}
		if s.Flagged {
			mark += "*"
		} else {
			mark += " "
		}
		t.Logf("%s %6d  %s  %-24.24s  %s  (refs %d)", mark, s.UID, s.Date.Local().Format("2006-01-02 15:04"), s.From.DisplayName(), s.Subject, len(s.References))
	}
	for i := 1; i < len(sums); i++ {
		if sums[i].UID >= sums[i-1].UID {
			t.Fatalf("UIDs not newest first at %d: %d after %d", i, sums[i].UID, sums[i-1].UID)
		}
	}
	if after := inboxUnread(ctx, t, c); after < unreadBefore {
		t.Errorf("INBOX unread dropped from %d to %d; Summaries must not mark mail read", unreadBefore, after)
	}
}

// TestLiveSearch checks server-side search on a real account's INBOX. It is
// strictly read-only: EXAMINE, SEARCH and FETCH with BODY.PEEK. The totals of
// is:unread and -is:unread are compared with the folder's own counts, and no
// mail may be marked read. Set DK_EMAIL_LIVE_SEARCH to also log the results of
// one query, to compare with the provider's web search.
//
//	DK_EMAIL_LIVE_ACCOUNT=Personal go test -tags live -run TestLiveSearch ./internal/mail/imap/ -v
//	DK_EMAIL_LIVE_ACCOUNT=Personal DK_EMAIL_LIVE_SEARCH='from:bob since:2026-01-01' go test -tags live -run TestLiveSearch ./internal/mail/imap/ -v
func TestLiveSearch(t *testing.T) {
	ctx, _, c := liveClient(t)
	unreadBefore, totalBefore := inboxCounts(ctx, t, c)
	t.Logf("INBOX per Folders: %d unread, %d total", unreadBefore, totalBefore)

	run := func(query string) ([]mail.Summary, int) {
		t.Helper()
		start := time.Now()
		sums, total, err := c.Search(ctx, "INBOX", search.Parse(query), 0, 200)
		if err != nil {
			t.Fatalf("Search(%q) error = %v", query, err)
		}
		t.Logf("%q: %d matches, %d returned in %v", query, total, len(sums), time.Since(start).Round(time.Millisecond))
		if len(sums) > 200 {
			t.Errorf("Search(%q) returned %d messages, want at most 200", query, len(sums))
		}
		for i := 1; i < len(sums); i++ {
			if sums[i].UID >= sums[i-1].UID {
				t.Fatalf("Search(%q): UIDs not newest first at %d: %d after %d", query, i, sums[i].UID, sums[i-1].UID)
			}
		}
		return sums, total
	}

	_, unread := run("is:unread")
	_, read := run("-is:unread")
	if unread != unreadBefore {
		t.Errorf("is:unread total = %d, but Folders reports %d unread in INBOX", unread, unreadBefore)
	}
	if unread+read != totalBefore {
		t.Errorf("is:unread (%d) + -is:unread (%d) = %d, but Folders reports %d messages in INBOX (the server's total may include \\Deleted messages)", unread, read, unread+read, totalBefore)
	}

	if q := os.Getenv("DK_EMAIL_LIVE_SEARCH"); q != "" {
		sums, total := run(q)
		t.Logf("manual query %q: %d matches", q, total)
		for _, s := range sums {
			mark := " "
			if !s.Seen {
				mark = "●"
			}
			if s.Flagged {
				mark += "*"
			} else {
				mark += " "
			}
			t.Logf("%s %6d  %s  %-24.24s  %s  (refs %d)", mark, s.UID, s.Date.Local().Format("2006-01-02 15:04"), s.From.DisplayName(), s.Subject, len(s.References))
		}
	}

	if after := inboxUnread(ctx, t, c); after < unreadBefore {
		t.Errorf("INBOX unread dropped from %d to %d; Search must not mark mail read", unreadBefore, after)
	}
}

func inboxUnread(ctx context.Context, t *testing.T, c *Client) int {
	t.Helper()
	unread, _ := inboxCounts(ctx, t, c)
	return unread
}

func inboxCounts(ctx context.Context, t *testing.T, c *Client) (unread, total int) {
	t.Helper()
	folders, err := c.Folders(ctx)
	if err != nil {
		t.Fatalf("Folders() error = %v", err)
	}
	for _, f := range folders {
		if f.Role == mail.RoleInbox {
			return f.Unread, f.Total
		}
	}
	t.Fatal("no INBOX")
	return 0, 0
}
