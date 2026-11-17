package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/dkouvdis/dk-email/internal/secret"
)

const (
	maxBodyBytes    = 1 << 20
	maxCleanRunes   = 200
	refreshMargin   = 2 * time.Minute
	defaultLifetime = time.Hour
)

// ErrInvalidGrant means the stored sign-in is no longer accepted.
var ErrInvalidGrant = errors.New("sign-in expired or was revoked: sign in again")

// Error is an OAuth error response from the provider.
type Error struct {
	Code        string
	Description string
}

// Error describes the OAuth error response.
func (e *Error) Error() string {
	if e.Description != "" {
		return "oauth: " + e.Code + ": " + e.Description
	}
	return "oauth: " + e.Code
}

// Is reports whether target is ErrInvalidGrant and the code is invalid_grant.
func (e *Error) Is(target error) bool {
	return target == ErrInvalidGrant && e.Code == "invalid_grant"
}

// Config is what the flows and TokenSource need to talk to a provider.
type Config struct {
	Provider Provider
	ClientID string
	// ClientSecret is optional; it is sent only when non-empty.
	ClientSecret string
	// LoginHint is optional; the account email, sent as login_hint.
	LoginHint string
	// HTTPClient nil means a client with a 30s timeout.
	HTTPClient *http.Client
	// Now nil means time.Now.
	Now func() time.Time

	// sleep nil means a context-aware timer; tests set it.
	sleep func(ctx context.Context, d time.Duration) error
}

func (c Config) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c Config) client() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (c Config) wait(ctx context.Context, d time.Duration) error {
	if c.sleep != nil {
		return c.sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// clean makes provider text safe to show: secrets redacted, control
// characters removed, whitespace collapsed and length capped.
func clean(s string, secrets ...string) string {
	for _, sec := range secrets {
		if sec != "" {
			s = strings.ReplaceAll(s, sec, "[redacted]")
		}
	}
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
	if runes := []rune(s); len(runes) > maxCleanRunes {
		s = string(runes[:maxCleanRunes]) + "…"
	}
	return s
}

// errorFields are the error members of an OAuth JSON response.
type errorFields struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

// post sends a form to endpoint and decodes the JSON response into out. An
// OAuth error response is returned as *Error; secrets are redacted from it.
// Response bodies never appear in other errors.
func (c Config) post(ctx context.Context, endpoint string, form url.Values, out any, secrets ...string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("oauth: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := c.client().Do(req)
	if err != nil {
		return fmt.Errorf("oauth: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return fmt.Errorf("oauth: %w", err)
	}
	var ef errorFields
	jsonErr := json.Unmarshal(body, &ef)
	if jsonErr == nil && ef.Error != "" {
		return &Error{Code: clean(ef.Error), Description: clean(ef.ErrorDescription, secrets...)}
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("oauth: token endpoint returned %s", resp.Status)
	}
	// The JSON error can quote body bytes, so it is dropped.
	if jsonErr != nil || json.Unmarshal(body, out) != nil {
		return errors.New("oauth: token endpoint returned an unreadable response")
	}
	return nil
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
}

// tokenRequest posts form to the token endpoint and returns the new token.
func (c Config) tokenRequest(ctx context.Context, form url.Values, secrets ...string) (secret.OAuthToken, error) {
	form.Set("client_id", c.ClientID)
	if c.ClientSecret != "" {
		form.Set("client_secret", c.ClientSecret)
		secrets = append(secrets, c.ClientSecret)
	}
	var tr tokenResponse
	if err := c.post(ctx, c.Provider.TokenURL, form, &tr, secrets...); err != nil {
		return secret.OAuthToken{}, err
	}
	if tr.AccessToken == "" {
		return secret.OAuthToken{}, errors.New("oauth: token response has no access_token")
	}
	life := time.Duration(tr.ExpiresIn) * time.Second
	if tr.ExpiresIn <= 0 {
		life = defaultLifetime
	}
	return secret.OAuthToken{
		AccessToken:  tr.AccessToken,
		RefreshToken: tr.RefreshToken,
		Expiry:       c.now().Add(life),
	}, nil
}

// Source yields a current access token for XOAUTH2.
type Source interface {
	Token(ctx context.Context) (string, error)
}

// CommandSource adapts a token command (password_cmd on an oauth2 account,
// e.g. oama) to a Source: it is called for every token.
type CommandSource func(ctx context.Context) (string, error)

// Token runs the command.
func (f CommandSource) Token(ctx context.Context) (string, error) { return f(ctx) }

// TokenSource refreshes a stored sign-in. Calls are serialized, so a rotated
// refresh token is never used twice.
type TokenSource struct {
	cfg  Config
	load func() (secret.OAuthToken, error)
	save func(secret.OAuthToken) error

	mu  sync.Mutex
	tok *secret.OAuthToken
	// unsaved is set when tok was refreshed but could not be saved.
	unsaved bool
}

// NewTokenSource returns a Source that loads the stored sign-in with load and
// persists each refreshed one with save.
func NewTokenSource(cfg Config, load func() (secret.OAuthToken, error), save func(secret.OAuthToken) error) *TokenSource {
	return &TokenSource{cfg: cfg, load: load, save: save}
}

// Token returns a valid access token, refreshing and saving it when it is
// missing or about to expire.
func (s *TokenSource) Token(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tok == nil {
		tok, err := s.load()
		if err != nil {
			return "", err
		}
		s.tok = &tok
	}
	if s.unsaved {
		if err := s.save(*s.tok); err != nil {
			return "", fmt.Errorf("oauth: save refreshed token: %w", err)
		}
		s.unsaved = false
	}
	if s.tok.AccessToken != "" && s.tok.Expiry.After(s.cfg.now().Add(refreshMargin)) {
		return s.tok.AccessToken, nil
	}
	old := s.tok.RefreshToken
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {old},
		"scope":         {strings.Join(s.cfg.Provider.Scopes, " ")},
	}
	tok, err := s.cfg.tokenRequest(ctx, form, old)
	if err != nil {
		return "", fmt.Errorf("oauth: refresh token: %w", err)
	}
	if tok.RefreshToken == "" {
		tok.RefreshToken = old
	}
	s.tok = &tok
	if err := s.save(tok); err != nil {
		s.unsaved = true
		return "", fmt.Errorf("oauth: save refreshed token: %w", err)
	}
	return tok.AccessToken, nil
}
