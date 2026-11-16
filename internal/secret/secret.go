// Package secret resolves account passwords from a password_cmd (e.g. the
// 1Password CLI) or the OS keychain. Passwords are never logged or included
// in errors.
package secret

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/zalando/go-keyring"

	"github.com/dkouvdis/dk-email/internal/config"
)

const (
	keychainService = "dk-email"

	// Generous because `op read` may wait for a Touch ID or approval prompt.
	DefaultTimeout = 60 * time.Second

	maxStderrRunes = 200
)

var (
	// ErrNoPassword means no password_cmd is set and the keychain has no entry.
	ErrNoPassword = errors.New("no password: set password_cmd or store one in the keychain")
	// ErrEmptyOutput means password_cmd succeeded but printed no password.
	ErrEmptyOutput = errors.New("command printed no password")
)

// Runner executes a shell command and returns its stdout and stderr.
type Runner func(ctx context.Context, command string) (stdout, stderr []byte, err error)

// CmdError reports a failed password_cmd. It carries the command's stderr but
// never its stdout, which may contain the password.
type CmdError struct {
	Account string
	Stderr  string
	Err     error
}

func (e *CmdError) Error() string {
	msg := fmt.Sprintf("password_cmd for account %q failed: %v", e.Account, e.Err)
	if e.Stderr != "" {
		msg += ": " + e.Stderr
	}
	return msg
}

func (e *CmdError) Unwrap() error { return e.Err }

type Resolver struct {
	Run     Runner
	Timeout time.Duration
}

// New returns a Resolver that runs password_cmd with `sh -c` in the user's environment.
func New() *Resolver {
	return &Resolver{Run: shellRunner, Timeout: DefaultTimeout}
}

// Resolve returns the account's password: from password_cmd when set,
// otherwise from the OS keychain.
func (r *Resolver) Resolve(ctx context.Context, acct config.Account) (string, error) {
	if strings.TrimSpace(acct.PasswordCmd) != "" {
		return r.fromCommand(ctx, acct)
	}
	return fromKeychain(acct.Name)
}

func (r *Resolver) fromCommand(ctx context.Context, acct config.Account) (string, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, r.Timeout)
	defer cancel()

	stdout, stderr, err := r.Run(cmdCtx, acct.PasswordCmd)
	if err != nil {
		switch {
		case ctx.Err() != nil:
			err = ctx.Err()
		case cmdCtx.Err() != nil:
			err = fmt.Errorf("timed out after %s (is 1Password waiting for approval?): %w", r.Timeout, context.DeadlineExceeded)
		}
		return "", &CmdError{Account: acct.Name, Stderr: sanitize(string(stderr)), Err: err}
	}

	// Only the first line is the password, matching the `pass` convention of
	// metadata on following lines.
	first, _, _ := strings.Cut(string(stdout), "\n")
	pw := strings.TrimSuffix(first, "\r")
	if pw == "" {
		return "", &CmdError{Account: acct.Name, Stderr: sanitize(string(stderr)), Err: ErrEmptyOutput}
	}
	return pw, nil
}

func fromKeychain(account string) (string, error) {
	pw, err := keyring.Get(keychainService, account)
	if errors.Is(err, keyring.ErrNotFound) || (err == nil && pw == "") {
		return "", fmt.Errorf("account %q: %w", account, ErrNoPassword)
	}
	if err != nil {
		return "", fmt.Errorf("read keychain for account %q: %w", account, err)
	}
	return pw, nil
}

// Store saves the account's password in the OS keychain.
func Store(account, password string) error {
	if strings.TrimSpace(account) == "" {
		return errors.New("store password: account name is required")
	}
	if password == "" {
		return errors.New("store password: password is empty")
	}
	if err := keyring.Set(keychainService, account, password); err != nil {
		return fmt.Errorf("save password to keychain for account %q: %w", account, err)
	}
	return nil
}

// Delete removes the account's password from the OS keychain. An entry that
// doesn't exist is not an error.
func Delete(account string) error {
	err := keyring.Delete(keychainService, account)
	if err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return fmt.Errorf("delete keychain password for account %q: %w", account, err)
	}
	return nil
}

// Copy stores the keychain password kept under oldName also under newName,
// leaving the old entry in place, for a renamed account. With no old entry
// there is nothing to copy and it succeeds.
func Copy(oldName, newName string) error {
	if oldName == newName {
		return nil
	}
	pw, err := keyring.Get(keychainService, oldName)
	if errors.Is(err, keyring.ErrNotFound) || (err == nil && pw == "") {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read keychain for account %q: %w", oldName, err)
	}
	return Store(newName, pw)
}

var ansiEscape = regexp.MustCompile(`\x1b(?:\[[0-?]*[ -/]*[@-~]|\][^\x07\x1b]*(?:\x07|\x1b\\)|.)`)

// sanitize makes stderr safe to show in the TUI: no escape sequences or
// control characters, whitespace collapsed, and length capped.
func sanitize(s string) string {
	s = ansiEscape.ReplaceAllString(s, "")
	s = strings.Map(func(r rune) rune {
		switch {
		case unicode.IsSpace(r):
			return ' '
		case unicode.IsControl(r):
			return -1
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if runes := []rune(s); len(runes) > maxStderrRunes {
		s = string(runes[:maxStderrRunes]) + "…"
	}
	return s
}
