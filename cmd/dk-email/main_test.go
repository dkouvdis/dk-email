package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRun_VersionFlag(t *testing.T) {
	for _, flag := range []string{"--version", "-version"} {
		t.Run(flag, func(t *testing.T) {
			var stdout, stderr bytes.Buffer

			err := run([]string{flag}, &stdout, &stderr)

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

	err := run([]string{"--config", path}, &stdout, &stderr)

	if err != nil {
		t.Fatalf("run() error = %v", err)
	}
}

func TestRun_CustomConfigPathDoesNotWriteGitignore(t *testing.T) {
	var stdout, stderr bytes.Buffer
	dir := t.TempDir()

	if err := run([]string{"--config", filepath.Join(dir, "config.toml")}, &stdout, &stderr); err != nil {
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

	if err := run(nil, &stdout, &stderr); err != nil {
		t.Fatalf("run() error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(base, "dk-email", ".gitignore")); err != nil {
		t.Errorf("config dir .gitignore not created: %v", err)
	}
}

func TestRun_InvalidConfigFails(t *testing.T) {
	var stdout, stderr bytes.Buffer
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[ui]\nlist_width = 5\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := run([]string{"--config", path}, &stdout, &stderr)

	if err == nil || !strings.Contains(err.Error(), "ui.list_width") {
		t.Fatalf("run() error = %v, want a list_width validation error", err)
	}
}

func TestRun_UnknownFlagFails(t *testing.T) {
	var stdout, stderr bytes.Buffer

	err := run([]string{"--nope"}, &stdout, &stderr)

	if err == nil {
		t.Fatal("run() error = nil, want error for unknown flag")
	}
}
