package config

import (
	"path/filepath"
	"testing"
)

func TestDir_UsesXDGConfigHome(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)

	got, err := Dir()

	if err != nil {
		t.Fatalf("Dir() error = %v", err)
	}
	if want := filepath.Join(base, "dk-email"); got != want {
		t.Errorf("Dir() = %q, want %q", got, want)
	}
}

func TestDir_FallsBackToDotConfigOnEveryOS(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", home)

	got, err := Dir()

	if err != nil {
		t.Fatalf("Dir() error = %v", err)
	}
	if want := filepath.Join(home, ".config", "dk-email"); got != want {
		t.Errorf("Dir() = %q, want %q", got, want)
	}
}

func TestDir_IgnoresRelativeXDGConfigHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", "relative/path")
	t.Setenv("HOME", home)

	got, err := Dir()

	if err != nil {
		t.Fatalf("Dir() error = %v", err)
	}
	if want := filepath.Join(home, ".config", "dk-email"); got != want {
		t.Errorf("Dir() = %q, want %q", got, want)
	}
}

func TestCacheDir_UsesXDGCacheHome(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", base)

	got, err := CacheDir()

	if err != nil {
		t.Fatalf("CacheDir() error = %v", err)
	}
	if want := filepath.Join(base, "dk-email"); got != want {
		t.Errorf("CacheDir() = %q, want %q", got, want)
	}
}

func TestCacheDir_FallsBackToDotCache(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("HOME", home)

	got, err := CacheDir()

	if err != nil {
		t.Fatalf("CacheDir() error = %v", err)
	}
	if want := filepath.Join(home, ".cache", "dk-email"); got != want {
		t.Errorf("CacheDir() = %q, want %q", got, want)
	}
}

func TestDefaultPath_IsConfigTomlInDir(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)

	got, err := DefaultPath()

	if err != nil {
		t.Fatalf("DefaultPath() error = %v", err)
	}
	if want := filepath.Join(base, "dk-email", "config.toml"); got != want {
		t.Errorf("DefaultPath() = %q, want %q", got, want)
	}
}
