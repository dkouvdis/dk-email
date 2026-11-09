package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/dkouvdis/dk-email/internal/config"
)

// openDebugLog returns a logger that appends to <cache dir>/debug.log when
// enabled, or one that discards everything. Callers log metadata only: never
// passwords, password_cmd output or message content.
func openDebugLog(enabled bool) (*slog.Logger, func() error, error) {
	if !enabled {
		return slog.New(slog.DiscardHandler), func() error { return nil }, nil
	}
	dir, err := config.CacheDir()
	if err != nil {
		return nil, nil, fmt.Errorf("debug log: %w", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, nil, fmt.Errorf("debug log: %w", err)
	}
	f, err := os.OpenFile(filepath.Join(dir, "debug.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, nil, fmt.Errorf("debug log: %w", err)
	}
	return slog.New(slog.NewTextHandler(f, &slog.HandlerOptions{Level: slog.LevelDebug})), f.Close, nil
}
