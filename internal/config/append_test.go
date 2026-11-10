package config

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func copyFixture(t *testing.T, name string) (path string, original []byte) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "append", name))
	if err != nil {
		t.Fatal(err)
	}
	path = filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path, data
}

func work() Account {
	return Account{Name: "Work", Email: "me@example.com", IMAPHost: "imap.example.com", TLS: TLSStartTLS}
}

func TestAppendAccount_KeepsFileAndAddsBlock(t *testing.T) {
	tests := []struct {
		fixture string
		eol     string
	}{
		{"commented.toml", "\n"},
		{"no-trailing-newline.toml", "\n"},
		{"crlf.toml", "\r\n"},
		{"empty.toml", "\n"},
	}
	for _, tt := range tests {
		t.Run(tt.fixture, func(t *testing.T) {
			path, original := copyFixture(t, tt.fixture)

			if err := AppendAccount(path, work()); err != nil {
				t.Fatalf("AppendAccount() error = %v", err)
			}

			got, _ := os.ReadFile(path)
			if !bytes.HasPrefix(got, original) {
				t.Fatalf("original content not kept byte for byte:\n%q", got)
			}
			added := string(got[len(original):])
			want := strings.ReplaceAll("[[account]]\nname = \"Work\"\nemail = \"me@example.com\"\nimap_host = \"imap.example.com\"\ntls = \"starttls\"\n", "\n", tt.eol)
			if !strings.HasSuffix(added, want) {
				t.Errorf("added %q, want it to end with %q", added, want)
			}
			if tt.eol == "\n" && strings.Contains(added, "\r") || tt.eol == "\r\n" && strings.Count(added, "\n") != strings.Count(added, "\r\n") {
				t.Errorf("added block mixes line endings: %q", added)
			}
			if len(original) > 0 && !strings.HasPrefix(strings.TrimLeft(added, "\r\n"), "[[account]]") {
				t.Errorf("block not separated cleanly: %q", added)
			}

			cfg, err := Load(path)
			if err != nil {
				t.Fatalf("Load() after append error = %v", err)
			}
			last := cfg.Accounts[len(cfg.Accounts)-1]
			if last.Name != "Work" || last.IMAPPort != 143 || last.Username != "me@example.com" {
				t.Errorf("appended account loads as %+v", last)
			}
		})
	}
}

func TestAppendAccount_MissingFileIsCreatedPrivately(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")

	if err := AppendAccount(path, work()); err != nil {
		t.Fatalf("AppendAccount() error = %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", info.Mode().Perm())
	}
	if got, _ := os.ReadFile(path); !strings.HasPrefix(string(got), "[[account]]\n") {
		t.Errorf("new file starts %q, want the block with no leading blank line", got)
	}
}

func TestAppendAccount_EscapesValues(t *testing.T) {
	path, _ := copyFixture(t, "empty.toml")
	acct := work()
	acct.Name = `Work "main"`
	acct.PasswordCmd = `op read "op://Work Vault/IMAP\login/password"`
	acct.ArchiveFolder = "Archive\tOld"

	if err := AppendAccount(path, acct); err != nil {
		t.Fatalf("AppendAccount() error = %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	got := cfg.Accounts[0]
	if got.Name != acct.Name || got.PasswordCmd != acct.PasswordCmd || got.ArchiveFolder != acct.ArchiveFolder {
		t.Errorf("round trip = %q / %q / %q", got.Name, got.PasswordCmd, got.ArchiveFolder)
	}
}

func TestAppendAccount_WritesOptionalFieldsOnlyWhenSet(t *testing.T) {
	path, _ := copyFixture(t, "empty.toml")
	acct := Account{
		Name: "Proton", Email: "me@pm.me", Username: "bridge-user", IMAPHost: "127.0.0.1", IMAPPort: 1143,
		TLS: TLSStartTLS, PasswordCmd: "op read op://P/Bridge/password", ArchiveFolder: "Archive",
	}

	if err := AppendAccount(path, acct); err != nil {
		t.Fatalf("AppendAccount() error = %v", err)
	}

	got, _ := os.ReadFile(path)
	for _, want := range []string{`username = "bridge-user"`, "imap_port = 1143", `password_cmd = "op read op://P/Bridge/password"`, `archive_folder = "Archive"`} {
		if !strings.Contains(string(got), want+"\n") {
			t.Errorf("file missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(string(got), "tls_cert") {
		t.Errorf("unset tls_cert written:\n%s", got)
	}

	path2, _ := copyFixture(t, "empty.toml")
	same := work()
	same.Username = same.Email
	if err := AppendAccount(path2, same); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path2); strings.Contains(string(got), "username") {
		t.Errorf("username equal to email (the default) was written:\n%s", got)
	}
}

func TestAppendAccount_InvalidResultLeavesFileUntouched(t *testing.T) {
	tests := []struct {
		name string
		acct Account
		want string
	}{
		{"duplicate name", Account{Name: "personal", Email: "x@example.com", IMAPHost: "h"}, "duplicate account name"},
		{"missing host", Account{Name: "New", Email: "x@example.com"}, "imap_host is required"},
		{"bad tls", Account{Name: "New", Email: "x@example.com", IMAPHost: "h", TLS: "none"}, "tls must be"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, original := copyFixture(t, "commented.toml")
			before, _ := os.Stat(path)

			err := AppendAccount(path, tt.acct)

			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v, want %q", err, tt.want)
			}
			got, _ := os.ReadFile(path)
			if !bytes.Equal(got, original) {
				t.Errorf("file changed after a rejected append:\n%s", got)
			}
			after, _ := os.Stat(path)
			if !after.ModTime().Equal(before.ModTime()) {
				t.Error("file was rewritten after a rejected append")
			}
			if entries, _ := os.ReadDir(filepath.Dir(path)); len(entries) != 1 {
				t.Errorf("left temp files behind: %v", entries)
			}
		})
	}
}

func TestAppendAccount_UnparseableExistingFileIsRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	original := []byte("[ui\nbroken")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := AppendAccount(path, work()); err == nil {
		t.Fatal("AppendAccount() error = nil for a broken file")
	}
	if got, _ := os.ReadFile(path); !bytes.Equal(got, original) {
		t.Error("broken file was modified")
	}
}

func TestAppendAccount_KeepsModeAndFollowsSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "dotfiles", "dk-email.toml")
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("# dotfiles\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "config.toml")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	if err := AppendAccount(link, work()); err != nil {
		t.Fatalf("AppendAccount() error = %v", err)
	}

	if fi, _ := os.Lstat(link); fi.Mode()&os.ModeSymlink == 0 {
		t.Error("the symlink was replaced by a regular file")
	}
	got, _ := os.ReadFile(target)
	if !strings.HasPrefix(string(got), "# dotfiles\n") || !strings.Contains(string(got), `name = "Work"`) {
		t.Errorf("target = %q", got)
	}
	if fi, _ := os.Stat(target); fi.Mode().Perm() != 0o640 {
		t.Errorf("mode = %v, want the existing 0640 kept", fi.Mode().Perm())
	}
}

func TestAppendAccount_MissingDirectoryFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nope", "config.toml")

	if err := AppendAccount(path, work()); err == nil {
		t.Error("AppendAccount() error = nil for a missing directory")
	}
}

func TestQuote(t *testing.T) {
	tests := map[string]string{
		"plain":     `"plain"`,
		`a"b\c`:     `"a\"b\\c"`,
		"tab\tnl\n": `"tab\tnl\n"`,
		"bell\x07":  `"bell\u0007"`,
		"del\x7f":   `"del\u007F"`,
		"café ☕":    `"café ☕"`,
	}
	for in, want := range tests {
		if got := quote(in); got != want {
			t.Errorf("quote(%q) = %s, want %s", in, got, want)
		}
	}
}
