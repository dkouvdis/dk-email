// Package secret resolves account passwords from a password_cmd (e.g. the
// 1Password CLI) or the OS keychain. It also keeps OAuth tokens and client
// secrets in the keychain. None of them are logged or included in errors.
package secret

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/zalando/go-keyring"

	"github.com/dkouvdis/dk-email/internal/config"
	"github.com/dkouvdis/dk-email/internal/oauth"
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
	// ErrNoToken means the keychain has no usable OAuth sign-in for the account.
	ErrNoToken = errors.New("no OAuth sign-in: sign in to the account again")
)

// Runner executes a shell command and returns its stdout and stderr.
type Runner func(ctx context.Context, command string) (stdout, stderr []byte, err error)

// CmdError reports a failed password_cmd or oauth_client_secret_cmd. It carries
// the command's stderr but never its stdout, which may contain the secret.
type CmdError struct {
	// Key is the config key that holds the command, e.g. "password_cmd".
	Key     string
	Account string
	Stderr  string
	Err     error
}

func (e *CmdError) Error() string {
	msg := fmt.Sprintf("%s for account %q failed: %v", e.Key, e.Account, e.Err)
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
		return r.fromCommand(ctx, acct, acct.PasswordCmd, "password_cmd")
	}
	return fromKeychain(acct.Name)
}

// ResolveClientSecret returns the account's OAuth client secret: from
// oauth_client_secret_cmd when set, otherwise from the OS keychain. A client
// secret is optional, so no entry is an empty secret, not an error.
func (r *Resolver) ResolveClientSecret(ctx context.Context, acct config.Account) (string, error) {
	if strings.TrimSpace(acct.OAuthClientSecretCmd) != "" {
		return r.fromCommand(ctx, acct, acct.OAuthClientSecretCmd, "oauth_client_secret_cmd")
	}
	cs, err := keyring.Get(keychainService, clientSecretKey(acct.Name))
	if errors.Is(err, keyring.ErrNotFound) || (err == nil && cs == "") {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read keychain OAuth client secret for account %q: %w", acct.Name, err)
	}
	return cs, nil
}

func (r *Resolver) fromCommand(ctx context.Context, acct config.Account, command, key string) (string, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, r.Timeout)
	defer cancel()

	stdout, stderr, err := r.Run(cmdCtx, command)
	if err != nil {
		switch {
		case ctx.Err() != nil:
			err = ctx.Err()
		case cmdCtx.Err() != nil:
			err = fmt.Errorf("timed out after %s (is 1Password waiting for approval?): %w", r.Timeout, context.DeadlineExceeded)
		}
		return "", &CmdError{Key: key, Account: acct.Name, Stderr: sanitize(string(stderr)), Err: err}
	}

	// Only the first line is the password, matching the `pass` convention of
	// metadata on following lines.
	first, _, _ := strings.Cut(string(stdout), "\n")
	pw := strings.TrimSuffix(first, "\r")
	if pw == "" {
		return "", &CmdError{Key: key, Account: acct.Name, Stderr: sanitize(string(stderr)), Err: ErrEmptyOutput}
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

// tokenKey and clientSecretKey keep OAuth entries apart from the password,
// which is stored under the bare account name.
func tokenKey(account string) string        { return "oauth:" + account }
func clientSecretKey(account string) string { return "oauth-client-secret:" + account }

// LoadToken returns the account's OAuth sign-in from the OS keychain. A missing
// or unreadable entry is ErrNoToken.
func LoadToken(account string) (oauth.Token, error) {
	raw, err := keyring.Get(keychainService, tokenKey(account))
	if errors.Is(err, keyring.ErrNotFound) || (err == nil && raw == "") {
		return oauth.Token{}, fmt.Errorf("account %q: %w", account, ErrNoToken)
	}
	if err != nil {
		return oauth.Token{}, fmt.Errorf("read keychain OAuth token for account %q: %w", account, err)
	}
	var tok oauth.Token
	// The JSON error can quote token bytes, so it is dropped.
	if json.Unmarshal([]byte(raw), &tok) != nil || tok.RefreshToken == "" {
		return oauth.Token{}, fmt.Errorf("account %q: stored OAuth token is unreadable: %w", account, ErrNoToken)
	}
	return tok, nil
}

// StoreToken saves the account's OAuth sign-in in the OS keychain.
func StoreToken(account string, tok oauth.Token) error {
	if strings.TrimSpace(account) == "" {
		return errors.New("store OAuth token: account name is required")
	}
	if tok.RefreshToken == "" {
		return errors.New("store OAuth token: refresh token is empty")
	}
	raw, err := json.Marshal(tok)
	if err != nil {
		return errors.New("store OAuth token: encode failed")
	}
	if err := keyring.Set(keychainService, tokenKey(account), string(raw)); err != nil {
		return fmt.Errorf("save OAuth token to keychain for account %q: %w", account, err)
	}
	return nil
}

// DeleteToken removes the account's OAuth sign-in from the OS keychain. An
// entry that doesn't exist is not an error.
func DeleteToken(account string) error {
	err := keyring.Delete(keychainService, tokenKey(account))
	if err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return fmt.Errorf("delete keychain OAuth token for account %q: %w", account, err)
	}
	return nil
}

// StoreClientSecret saves the account's OAuth client secret in the OS keychain.
func StoreClientSecret(account, secret string) error {
	if strings.TrimSpace(account) == "" {
		return errors.New("store OAuth client secret: account name is required")
	}
	if secret == "" {
		return errors.New("store OAuth client secret: secret is empty")
	}
	if err := keyring.Set(keychainService, clientSecretKey(account), secret); err != nil {
		return fmt.Errorf("save OAuth client secret to keychain for account %q: %w", account, err)
	}
	return nil
}

// DeleteClientSecret removes the account's OAuth client secret from the OS
// keychain. An entry that doesn't exist is not an error.
func DeleteClientSecret(account string) error {
	err := keyring.Delete(keychainService, clientSecretKey(account))
	if err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return fmt.Errorf("delete keychain OAuth client secret for account %q: %w", account, err)
	}
	return nil
}

// CopyOAuth stores the keychain OAuth token and client secret kept under
// oldName also under newName, leaving the old entries in place, for a renamed
// account. A missing entry is skipped.
func CopyOAuth(oldName, newName string) error {
	if oldName == newName {
		return nil
	}
	tok, err := keyring.Get(keychainService, tokenKey(oldName))
	switch {
	case errors.Is(err, keyring.ErrNotFound) || (err == nil && tok == ""):
	case err != nil:
		return fmt.Errorf("read keychain OAuth token for account %q: %w", oldName, err)
	default:
		if err := keyring.Set(keychainService, tokenKey(newName), tok); err != nil {
			return fmt.Errorf("save OAuth token to keychain for account %q: %w", newName, err)
		}
	}
	cs, err := keyring.Get(keychainService, clientSecretKey(oldName))
	switch {
	case errors.Is(err, keyring.ErrNotFound) || (err == nil && cs == ""):
	case err != nil:
		return fmt.Errorf("read keychain OAuth client secret for account %q: %w", oldName, err)
	default:
		return StoreClientSecret(newName, cs)
	}
	return nil
}

// CopyClientSecret stores the keychain OAuth client secret kept under oldName
// also under newName, leaving the old entry in place, for a renamed account
// that signed in again. A client secret already under newName wins, so it is
// not replaced. It reports whether it copied one; with no old entry, or the
// same name, there is nothing to copy and it succeeds.
func CopyClientSecret(oldName, newName string) (copied bool, err error) {
	if oldName == newName {
		return false, nil
	}
	existing, err := keyring.Get(keychainService, clientSecretKey(newName))
	switch {
	case errors.Is(err, keyring.ErrNotFound) || (err == nil && existing == ""):
	case err != nil:
		return false, fmt.Errorf("read keychain OAuth client secret for account %q: %w", newName, err)
	default:
		return false, nil
	}
	cs, err := keyring.Get(keychainService, clientSecretKey(oldName))
	if errors.Is(err, keyring.ErrNotFound) || (err == nil && cs == "") {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read keychain OAuth client secret for account %q: %w", oldName, err)
	}
	if err := StoreClientSecret(newName, cs); err != nil {
		return false, err
	}
	return true, nil
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
