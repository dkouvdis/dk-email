package oauth

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dkouvdis/dk-email/internal/secret"
)

func TestClean(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		secrets []string
		want    string
	}{
		{"plain", "hello", nil, "hello"},
		{"redacts every occurrence", "a SECRET b SECRET", []string{"SECRET"}, "a [redacted] b [redacted]"},
		{"ignores empty secret", "abc", []string{""}, "abc"},
		{"multiple secrets", "x one y two", []string{"one", "two"}, "x [redacted] y [redacted]"},
		{"control characters", "a\x1b[31mb\x00c", nil, "a[31mbc"},
		{"whitespace collapsed", "a \n\t b   c", nil, "a b c"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := clean(tt.in, tt.secrets...); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
	long := clean(strings.Repeat("é", 300))
	if r := []rune(long); len(r) != 201 || r[200] != '…' {
		t.Errorf("truncation: %d runes", len(r))
	}
}

func TestError(t *testing.T) {
	if got := (&Error{Code: "access_denied"}).Error(); got != "oauth: access_denied" {
		t.Errorf("got %q", got)
	}
	if got := (&Error{Code: "x", Description: "why"}).Error(); got != "oauth: x: why" {
		t.Errorf("got %q", got)
	}
	if !errors.Is(&Error{Code: "invalid_grant"}, ErrInvalidGrant) {
		t.Error("invalid_grant should match ErrInvalidGrant")
	}
	if errors.Is(&Error{Code: "other"}, ErrInvalidGrant) {
		t.Error("other code should not match")
	}
}

func TestCommandSource(t *testing.T) {
	var s Source = CommandSource(func(context.Context) (string, error) { return "tok", nil })
	got, err := s.Token(context.Background())
	if err != nil || got != "tok" {
		t.Fatalf("got %q, %v", got, err)
	}
	want := errors.New("boom")
	s = CommandSource(func(context.Context) (string, error) { return "", want })
	if _, err := s.Token(context.Background()); !errors.Is(err, want) {
		t.Fatalf("err = %v", err)
	}
}

func okToken(w http.ResponseWriter, access, refresh string) {
	body := map[string]any{"access_token": access, "expires_in": 3600}
	if refresh != "" {
		body["refresh_token"] = refresh
	}
	writeJSON(w, 200, body)
}

func loader(tok secret.OAuthToken, err error) func() (secret.OAuthToken, error) {
	return func() (secret.OAuthToken, error) { return tok, err }
}

func noSave(secret.OAuthToken) error { return nil }

func TestTokenSource_CachedValid(t *testing.T) {
	rec, cfg := newServer(t, func(int, url.Values, http.ResponseWriter) { t.Error("unexpected HTTP") })
	tok := secret.OAuthToken{AccessToken: "cached", RefreshToken: "r", Expiry: fixedNow.Add(10 * time.Minute)}
	s := NewTokenSource(cfg, loader(tok, nil), noSave)
	for range 2 {
		got, err := s.Token(context.Background())
		if err != nil || got != "cached" {
			t.Fatalf("got %q, %v", got, err)
		}
	}
	if rec.count() != 0 {
		t.Errorf("requests = %d", rec.count())
	}
}

func TestTokenSource_RefreshAndSave(t *testing.T) {
	rec, cfg := newServer(t, func(_ int, _ url.Values, w http.ResponseWriter) { okToken(w, "new-access", "new-refresh") })
	cfg.ClientSecret = "csecret"
	old := secret.OAuthToken{AccessToken: "old", RefreshToken: "old-refresh", Expiry: fixedNow.Add(time.Minute)}
	var saved []secret.OAuthToken
	returned := false
	s := NewTokenSource(cfg, loader(old, nil), func(tok secret.OAuthToken) error {
		if returned {
			t.Error("save called after Token returned")
		}
		saved = append(saved, tok)
		return nil
	})
	got, err := s.Token(context.Background())
	returned = true
	if err != nil || got != "new-access" {
		t.Fatalf("got %q, %v", got, err)
	}
	f := rec.form(0)
	want := map[string]string{
		"grant_type": "refresh_token", "refresh_token": "old-refresh", "client_id": "client-id",
		"scope": "scope.a scope.b", "client_secret": "csecret",
	}
	for k, v := range want {
		if f.Get(k) != v {
			t.Errorf("%s = %q, want %q", k, f.Get(k), v)
		}
	}
	if len(saved) != 1 || saved[0].RefreshToken != "new-refresh" || saved[0].AccessToken != "new-access" ||
		!saved[0].Expiry.Equal(fixedNow.Add(time.Hour)) {
		t.Errorf("saved = %+v", saved)
	}
	// now cached
	if got, _ := s.Token(context.Background()); got != "new-access" || rec.count() != 1 {
		t.Errorf("second call: %q, %d requests", got, rec.count())
	}
}

func TestTokenSource_NoClientSecretWhenEmpty(t *testing.T) {
	rec, cfg := newServer(t, func(_ int, _ url.Values, w http.ResponseWriter) { okToken(w, "a", "r") })
	s := NewTokenSource(cfg, loader(secret.OAuthToken{RefreshToken: "r0"}, nil), noSave)
	if _, err := s.Token(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := rec.form(0)["client_secret"]; ok {
		t.Error("client_secret sent although empty")
	}
}

func TestTokenSource_KeepsOldRefreshToken(t *testing.T) {
	_, cfg := newServer(t, func(_ int, _ url.Values, w http.ResponseWriter) { okToken(w, "a2", "") })
	var saved secret.OAuthToken
	s := NewTokenSource(cfg, loader(secret.OAuthToken{RefreshToken: "keep-me"}, nil), func(tok secret.OAuthToken) error {
		saved = tok
		return nil
	})
	if _, err := s.Token(context.Background()); err != nil {
		t.Fatal(err)
	}
	if saved.RefreshToken != "keep-me" {
		t.Errorf("refresh token = %q", saved.RefreshToken)
	}
}

func TestTokenSource_ExpiresInDefault(t *testing.T) {
	_, cfg := newServer(t, func(_ int, _ url.Values, w http.ResponseWriter) {
		writeJSON(w, 200, map[string]any{"access_token": "a"})
	})
	var saved secret.OAuthToken
	s := NewTokenSource(cfg, loader(secret.OAuthToken{RefreshToken: "refresh-tok-1"}, nil), func(tok secret.OAuthToken) error {
		saved = tok
		return nil
	})
	if _, err := s.Token(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !saved.Expiry.Equal(fixedNow.Add(time.Hour)) {
		t.Errorf("expiry = %v", saved.Expiry)
	}
}

func TestTokenSource_LoadErrorPassesThrough(t *testing.T) {
	_, cfg := newServer(t, func(int, url.Values, http.ResponseWriter) { t.Error("unexpected HTTP") })
	s := NewTokenSource(cfg, loader(secret.OAuthToken{}, secret.ErrNoToken), noSave)
	_, err := s.Token(context.Background())
	if !errors.Is(err, secret.ErrNoToken) {
		t.Fatalf("err = %v", err)
	}
}

func TestTokenSource_InvalidGrant(t *testing.T) {
	_, cfg := newServer(t, func(_ int, _ url.Values, w http.ResponseWriter) {
		writeJSON(w, 400, map[string]any{"error": "invalid_grant", "error_description": "revoked"})
	})
	s := NewTokenSource(cfg, loader(secret.OAuthToken{RefreshToken: "refresh-tok-1"}, nil), noSave)
	_, err := s.Token(context.Background())
	if !errors.Is(err, ErrInvalidGrant) {
		t.Fatalf("err = %v", err)
	}
	var oe *Error
	if !errors.As(err, &oe) || oe.Code != "invalid_grant" {
		t.Errorf("want *Error in chain, got %v", err)
	}
}

func TestTokenSource_ErrorsNeverLeakSecrets(t *testing.T) {
	t.Run("non-JSON 500 body", func(t *testing.T) {
		_, cfg := newServer(t, func(_ int, _ url.Values, w http.ResponseWriter) {
			http.Error(w, "boom refresh-secret-1 csecret-9", 500)
		})
		cfg.ClientSecret = "csecret-9"
		s := NewTokenSource(cfg, loader(secret.OAuthToken{RefreshToken: "refresh-secret-1"}, nil), noSave)
		_, err := s.Token(context.Background())
		if err == nil || !strings.Contains(err.Error(), "500") {
			t.Fatalf("err = %v", err)
		}
		mustNotContain(t, err.Error(), "refresh-secret-1", "csecret-9")
	})
	t.Run("description echoes secrets", func(t *testing.T) {
		_, cfg := newServer(t, func(_ int, _ url.Values, w http.ResponseWriter) {
			writeJSON(w, 400, map[string]any{"error": "invalid_request", "error_description": "bad refresh-secret-1 and csecret-9"})
		})
		cfg.ClientSecret = "csecret-9"
		s := NewTokenSource(cfg, loader(secret.OAuthToken{RefreshToken: "refresh-secret-1"}, nil), noSave)
		_, err := s.Token(context.Background())
		if err == nil || !strings.Contains(err.Error(), "[redacted]") {
			t.Fatalf("err = %v", err)
		}
		mustNotContain(t, err.Error(), "refresh-secret-1", "csecret-9")
	})
	t.Run("missing access token", func(t *testing.T) {
		_, cfg := newServer(t, func(_ int, _ url.Values, w http.ResponseWriter) {
			writeJSON(w, 200, map[string]any{"refresh_token": "x"})
		})
		s := NewTokenSource(cfg, loader(secret.OAuthToken{RefreshToken: "refresh-tok-1"}, nil), noSave)
		_, err := s.Token(context.Background())
		if err == nil || !strings.Contains(err.Error(), "no access_token") {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestTokenSource_SaveFailure(t *testing.T) {
	rec, cfg := newServer(t, func(_ int, _ url.Values, w http.ResponseWriter) { okToken(w, "a", "r2") })
	saveErr := errors.New("keychain locked")
	saves := 0
	var saveResult error = saveErr
	s := NewTokenSource(cfg, loader(secret.OAuthToken{RefreshToken: "refresh-tok-1"}, nil), func(secret.OAuthToken) error {
		saves++
		return saveResult
	})
	got, err := s.Token(context.Background())
	if got != "" || !errors.Is(err, saveErr) || !strings.Contains(err.Error(), "oauth: save refreshed token") {
		t.Fatalf("got %q, %v", got, err)
	}
	// Still failing: the save is retried and the error returned again.
	got, err = s.Token(context.Background())
	if got != "" || !errors.Is(err, saveErr) || !strings.Contains(err.Error(), "oauth: save refreshed token") {
		t.Fatalf("second call: %q, %v", got, err)
	}
	if saves != 2 {
		t.Errorf("saves = %d, want 2", saves)
	}
	// Save works again: the cached token is returned without another refresh.
	saveResult = nil
	got, err = s.Token(context.Background())
	if err != nil || got != "a" {
		t.Fatalf("third call: %q, %v", got, err)
	}
	if saves != 3 {
		t.Errorf("saves = %d, want 3", saves)
	}
	got, err = s.Token(context.Background())
	if err != nil || got != "a" || saves != 3 {
		t.Fatalf("fourth call: %q, %v, saves %d", got, err, saves)
	}
	if rec.count() != 1 {
		t.Errorf("requests = %d", rec.count())
	}
}

func TestTokenSource_Concurrent(t *testing.T) {
	rec, cfg := newServer(t, func(_ int, _ url.Values, w http.ResponseWriter) {
		time.Sleep(20 * time.Millisecond)
		okToken(w, "fresh", "r2")
	})
	s := NewTokenSource(cfg, loader(secret.OAuthToken{RefreshToken: "r", Expiry: fixedNow.Add(-time.Hour)}, nil), noSave)
	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if got, err := s.Token(context.Background()); err != nil || got != "fresh" {
				t.Errorf("got %q, %v", got, err)
			}
		}()
	}
	wg.Wait()
	if rec.count() != 1 {
		t.Errorf("refresh requests = %d, want 1", rec.count())
	}
}
