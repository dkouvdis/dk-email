package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenDebugLog_DisabledWritesNothing(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", base)

	log, closeLog, err := openDebugLog(false)
	if err != nil {
		t.Fatalf("openDebugLog() error = %v", err)
	}
	log.Debug("hello")
	if err := closeLog(); err != nil {
		t.Fatalf("close error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(base, "dk-email")); !os.IsNotExist(err) {
		t.Errorf("cache dir created while disabled (stat err = %v)", err)
	}
}

func TestOpenDebugLog_EnabledAppendsPrivately(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", base)
	path := filepath.Join(base, "dk-email", "debug.log")

	for _, msg := range []string{"first", "second"} {
		log, closeLog, err := openDebugLog(true)
		if err != nil {
			t.Fatalf("openDebugLog() error = %v", err)
		}
		log.Debug(msg, "width", 120)
		if err := closeLog(); err != nil {
			t.Fatalf("close error = %v", err)
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !strings.Contains(string(data), "msg=first width=120") || !strings.Contains(string(data), "msg=second") {
		t.Errorf("log = %q, want both debug lines appended", data)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Errorf("log mode = %v, want 0600", info.Mode().Perm())
	}
	if info, _ := os.Stat(filepath.Dir(path)); info.Mode().Perm() != 0o700 {
		t.Errorf("cache dir mode = %v, want 0700", info.Mode().Perm())
	}
}

func TestOpenDebugLog_UnwritableCacheFails(t *testing.T) {
	base := t.TempDir()
	if err := os.WriteFile(filepath.Join(base, "dk-email"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CACHE_HOME", base)

	if _, _, err := openDebugLog(true); err == nil || !strings.Contains(err.Error(), "debug log") {
		t.Errorf("openDebugLog() error = %v, want a debug log error", err)
	}
}
