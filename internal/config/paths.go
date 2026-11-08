package config

import (
	"fmt"
	"os"
	"path/filepath"
)

const appName = "dk-email"

// Dir returns $XDG_CONFIG_HOME/dk-email, or ~/.config/dk-email on every OS
// (including macOS, where os.UserConfigDir would point at Library/Application Support).
func Dir() (string, error) {
	return xdgDir("XDG_CONFIG_HOME", ".config")
}

// CacheDir returns $XDG_CACHE_HOME/dk-email, or ~/.cache/dk-email.
func CacheDir() (string, error) {
	return xdgDir("XDG_CACHE_HOME", ".cache")
}

// DefaultPath returns the config.toml path inside Dir.
func DefaultPath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.toml"), nil
}

func xdgDir(env, fallback string) (string, error) {
	// The XDG spec says relative paths must be ignored.
	if base := os.Getenv(env); base != "" && filepath.IsAbs(base) {
		return filepath.Join(base, appName), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate home directory: %w", err)
	}
	return filepath.Join(home, fallback, appName), nil
}
