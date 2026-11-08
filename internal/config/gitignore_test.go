package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const wantGitignore = "*\n!.gitignore\n!config.toml\n!keys.toml\n!themes/\n!themes/**\n"

func TestEnsureDir_CreatesPrivateDirWithGitignore(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "dk-email")

	if err := EnsureDir(dir); err != nil {
		t.Fatalf("EnsureDir() error = %v", err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Errorf("dir mode = %o, want 700", perm)
	}
	got, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	if string(got) != wantGitignore {
		t.Errorf(".gitignore = %q, want %q", got, wantGitignore)
	}
}

func TestEnsureDir_NeverOverwritesExistingGitignore(t *testing.T) {
	dir := t.TempDir()
	custom := "# my own rules\n*\n"
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := EnsureDir(dir); err != nil {
		t.Fatalf("EnsureDir() error = %v", err)
	}

	got, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if string(got) != custom {
		t.Errorf(".gitignore = %q, want it left as %q", got, custom)
	}
}

func TestEnsureDir_IsIdempotent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "dk-email")

	for i := range 2 {
		if err := EnsureDir(dir); err != nil {
			t.Fatalf("EnsureDir() call %d error = %v", i+1, err)
		}
	}
}

func TestEnsureDir_FailsWhenPathIsAFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := EnsureDir(file); err == nil {
		t.Fatal("EnsureDir() error = nil, want error")
	}
}

// Proves the whitelist keeps config files committable and everything else out of git.
func TestGitignore_WhitelistsOnlyConfigFiles(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	git := func(args ...string) (string, error) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}
	if out, err := git("init", "-q"); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	if err := EnsureDir(dir); err != nil {
		t.Fatalf("EnsureDir() error = %v", err)
	}

	for _, path := range []string{"cache.db", "debug.log", "mail/inbox.eml", "state.json"} {
		if _, err := git("check-ignore", "-q", path); err != nil {
			t.Errorf("%s should be ignored", path)
		}
	}
	for _, path := range []string{".gitignore", "config.toml", "keys.toml", "themes/dark.toml"} {
		if _, err := git("check-ignore", "-q", path); err == nil {
			t.Errorf("%s should not be ignored", path)
		}
	}
}
