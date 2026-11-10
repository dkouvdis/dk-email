package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dkouvdis/dk-email/internal/config"
)

func TestRun_VersionFlag(t *testing.T) {
	for _, flag := range []string{"--version", "-version"} {
		t.Run(flag, func(t *testing.T) {
			var stdout, stderr bytes.Buffer

			err := run([]string{flag}, &stdout, &stderr, noUI)

			if err != nil {
				t.Fatalf("run() error = %v", err)
			}
			if !strings.HasPrefix(stdout.String(), "dk-email ") {
				t.Errorf("stdout = %q, want it to start with %q", stdout.String(), "dk-email ")
			}
		})
	}
}

func TestRun_AcceptsConfigFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	path := filepath.Join(t.TempDir(), "config.toml")

	err := run([]string{"--config", path}, &stdout, &stderr, noUI)

	if err != nil {
		t.Fatalf("run() error = %v", err)
	}
}

func TestRun_CustomConfigPathDoesNotWriteGitignore(t *testing.T) {
	var stdout, stderr bytes.Buffer
	dir := t.TempDir()

	if err := run([]string{"--config", filepath.Join(dir, "config.toml")}, &stdout, &stderr, noUI); err != nil {
		t.Fatalf("run() error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, ".gitignore")); !os.IsNotExist(err) {
		t.Errorf(".gitignore written next to a custom --config path (stat err = %v)", err)
	}
}

func TestRun_FirstRunCreatesConfigDir(t *testing.T) {
	var stdout, stderr bytes.Buffer
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)

	if err := run(nil, &stdout, &stderr, noUI); err != nil {
		t.Fatalf("run() error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(base, "dk-email", ".gitignore")); err != nil {
		t.Errorf("config dir .gitignore not created: %v", err)
	}
}

func TestRun_InvalidConfigFails(t *testing.T) {
	var stdout, stderr bytes.Buffer
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[ui]\nrefresh_interval = \"0s\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := run([]string{"--config", path}, &stdout, &stderr, noUI)

	if err == nil || !strings.Contains(err.Error(), "ui.refresh_interval") {
		t.Fatalf("run() error = %v, want a refresh_interval validation error", err)
	}
}

func TestRun_UnknownFlagFails(t *testing.T) {
	var stdout, stderr bytes.Buffer

	err := run([]string{"--nope"}, &stdout, &stderr, noUI)

	if err == nil {
		t.Fatal("run() error = nil, want error for unknown flag")
	}
}

func noUI(*config.Config) error { return nil }

func TestRun_StartsUIWithLoadedConfig(t *testing.T) {
	var stdout, stderr bytes.Buffer
	path := filepath.Join(t.TempDir(), "config.toml")
	body := "[[account]]\nname = \"Personal\"\nemail = \"me@example.com\"\nimap_host = \"imap.example.com\"\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	var got *config.Config

	err := run([]string{"--config", path}, &stdout, &stderr, func(cfg *config.Config) error {
		got = cfg
		return errors.New("ui failed")
	})

	if err == nil || err.Error() != "ui failed" {
		t.Errorf("run() error = %v, want the UI's error", err)
	}
	if got == nil || len(got.Accounts) != 1 || got.Accounts[0].Name != "Personal" || got.Path != path {
		t.Errorf("UI started with %+v, want the loaded config", got)
	}
}

func TestRun_VersionDoesNotStartUI(t *testing.T) {
	var stdout, stderr bytes.Buffer

	err := run([]string{"--version"}, &stdout, &stderr, func(*config.Config) error {
		t.Error("UI started for --version")
		return nil
	})

	if err != nil {
		t.Fatalf("run() error = %v", err)
	}
}
