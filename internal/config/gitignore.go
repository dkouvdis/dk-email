package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Ignores everything in the config dir except files meant to be kept in dotfiles,
// so state, logs or a redirected cache can never be committed by accident.
const gitignoreContent = "*\n!.gitignore\n!config.toml\n!keys.toml\n!themes/\n!themes/**\n"

// EnsureDir creates dir with mode 0700 and writes the whitelist .gitignore
// if one doesn't exist yet. An existing .gitignore is never modified.
func EnsureDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	f, err := os.OpenFile(filepath.Join(dir, ".gitignore"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, fs.ErrExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("create config .gitignore: %w", err)
	}
	if _, err := f.WriteString(gitignoreContent); err != nil {
		f.Close()
		return fmt.Errorf("write config .gitignore: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("write config .gitignore: %w", err)
	}
	return nil
}
