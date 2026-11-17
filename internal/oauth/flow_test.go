package oauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/dkouvdis/dk-email/internal/secret"
)

// browse simulates the browser: it GETs the redirect URI (on 127.0.0.1) with
// the given query and returns status and body.
func browse(t *testing.T, redirect string, q url.Values) (int, string) {
	t.Helper()
	u, err := url.Parse(redirect)
	if err != nil {
		t.Error(err)
		return 0, ""
	}
	u.Host = "127.0.0.1:" + u.Port()
	u.RawQuery = q.Encode()
	resp, err := http.Get(u.String())
	if err != nil {
		t.Error(err)
		return 0, ""
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

type loopResult struct {
	tok secret.OAuthToken
	err error
}

// runLoopback starts Loopback in a goroutine; onURL gets the parsed auth URL.
func runLoopback(ctx context.Context, cfg Config, onURL func(u *url.URL) error) <-chan loopResult {
	ch := make(chan loopResult, 1)
	go func() {
		tok, err := cfg.Loopback(ctx, func(raw string) error {
			u, perr := url.Parse(raw)
			if perr != nil {
				return perr
			}
			return onURL(u)
		})
		ch <- loopResult{tok, err}
	}()
	return ch
}

func wait(t *testing.T, ch <-chan loopResult) loopResult {
	t.Helper()
	select {
	case r := <-ch:
		return r
	case <-time.After(5 * time.Second):
		t.Fatal("Loopback did not return")
		return loopResult{}
	}
}

func TestLoopback_Success(t *testing.T) {
	for _, withSecret := range []bool{false, true} {
		name := "public client"
		if withSecret {
			name = "client secret"
		}
		t.Run(name, func(t *testing.T) {
			rec, cfg := newServer(t, func(_ int, _ url.Values, w http.ResponseWriter) {
				writeJSON(w, 200, map[string]any{"access_token": "acc", "refresh_token": "ref", "expires_in": 1800})
			})
			cfg.LoginHint = "me@example.com"
			cfg.Provider.AuthParams = map[string]string{"access_type": "offline", "prompt": "consent"}
			if withSecret {
				cfg.ClientSecret = "shh"
			}
			var redirect, state string
			var challenge string
			ch := runLoopback(context.Background(), cfg, func(u *url.URL) error {
				q := u.Query()
				if got := u.Scheme + "://" + u.Host + u.Path; got != cfg.Provider.AuthURL {
					t.Errorf("auth endpoint = %q", got)
				}
				checks := map[string]string{
					"response_type": "code", "client_id": "client-id", "scope": "scope.a scope.b",
					"code_challenge_method": "S256", "login_hint": "me@example.com",
					"access_type": "offline", "prompt": "consent",
				}
				for k, v := range checks {
					if q.Get(k) != v {
						t.Errorf("%s = %q, want %q", k, q.Get(k), v)
					}
				}
				redirect, state, challenge = q.Get("redirect_uri"), q.Get("state"), q.Get("code_challenge")
				ru, err := url.Parse(redirect)
				if err != nil || ru.Scheme != "http" || ru.Hostname() != "localhost" || ru.Port() == "" || ru.Path != "/" {
					t.Errorf("redirect_uri = %q", redirect)
				}
				if state == "" || challenge == "" {
					t.Error("missing state or challenge")
				}
				go func() {
					code, body := browse(t, redirect, url.Values{"code": {"the-code"}, "state": {state}})
					if code != 200 || !strings.Contains(body, "Signed in to dk-email. You can close this tab.") {
						t.Errorf("browser got %d %q", code, body)
					}
				}()
				return nil
			})
			r := wait(t, ch)
			if r.err != nil {
				t.Fatal(r.err)
			}
			if r.tok.AccessToken != "acc" || r.tok.RefreshToken != "ref" || !r.tok.Expiry.Equal(fixedNow.Add(30*time.Minute)) {
				t.Errorf("token = %+v", r.tok)
			}
			f := rec.form(0)
			for k, v := range map[string]string{
				"grant_type": "authorization_code", "code": "the-code", "redirect_uri": redirect, "client_id": "client-id",
			} {
				if f.Get(k) != v {
					t.Errorf("%s = %q, want %q", k, f.Get(k), v)
				}
			}
			sum := sha256.Sum256([]byte(f.Get("code_verifier")))
			if f.Get("code_verifier") == "" || base64.RawURLEncoding.EncodeToString(sum[:]) != challenge {
				t.Errorf("verifier %q does not match challenge %q", f.Get("code_verifier"), challenge)
			}
			_, has := f["client_secret"]
			if has != withSecret || (withSecret && f.Get("client_secret") != "shh") {
				t.Errorf("client_secret present=%v", has)
			}
		})
	}
}

func TestLoopback_WrongStateKeepsWaiting(t *testing.T) {
	_, cfg := newServer(t, func(_ int, _ url.Values, w http.ResponseWriter) {
		writeJSON(w, 200, map[string]any{"access_token": "acc", "expires_in": 60})
	})
	ch := runLoopback(context.Background(), cfg, func(u *url.URL) error {
		redirect, state := u.Query().Get("redirect_uri"), u.Query().Get("state")
		go func() {
			code, body := browse(t, redirect, url.Values{"code": {"x"}, "state": {"forged"}})
			if code != 400 || !strings.Contains(body, "state mismatch") {
				t.Errorf("forged: %d %q", code, body)
			}
			if code, _ := browse(t, redirect, url.Values{"error": {"access_denied"}}); code != 400 {
				t.Errorf("no state: %d", code)
			}
			browse(t, redirect, url.Values{"code": {"real"}, "state": {state}})
		}()
		return nil
	})
	r := wait(t, ch)
	if r.err != nil || r.tok.AccessToken != "acc" {
		t.Fatalf("got %+v, %v", r.tok, r.err)
	}
}

func TestLoopback_ProviderError(t *testing.T) {
	rec, cfg := newServer(t, func(int, url.Values, http.ResponseWriter) { t.Error("unexpected token request") })
	ch := runLoopback(context.Background(), cfg, func(u *url.URL) error {
		go browse(t, u.Query().Get("redirect_uri"), url.Values{
			"error": {"access_denied"}, "error_description": {"user said no"}, "state": {u.Query().Get("state")},
		})
		return nil
	})
	r := wait(t, ch)
	var oe *Error
	if !errors.As(r.err, &oe) || oe.Code != "access_denied" || oe.Description != "user said no" {
		t.Fatalf("err = %v", r.err)
	}
	if rec.count() != 0 {
		t.Error("token endpoint was called")
	}
}

func TestLoopback_OnURLError(t *testing.T) {
	_, cfg := newServer(t, func(int, url.Values, http.ResponseWriter) {})
	want := errors.New("no browser")
	ch := runLoopback(context.Background(), cfg, func(*url.URL) error { return want })
	r := wait(t, ch)
	if !errors.Is(r.err, want) || !strings.HasPrefix(r.err.Error(), "oauth: ") {
		t.Fatalf("err = %v", r.err)
	}
}

func TestLoopback_ContextCancel(t *testing.T) {
	_, cfg := newServer(t, func(int, url.Values, http.ResponseWriter) {})
	ctx, cancel := context.WithCancel(context.Background())
	ch := runLoopback(ctx, cfg, func(*url.URL) error {
		go func() {
			time.Sleep(20 * time.Millisecond)
			cancel()
		}()
		return nil
	})
	if r := wait(t, ch); !errors.Is(r.err, context.Canceled) {
		t.Fatalf("err = %v", r.err)
	}
}

func TestLoopback_ExchangeFailureRedacted(t *testing.T) {
	var verifier string
	_, cfg := newServer(t, func(_ int, f url.Values, w http.ResponseWriter) {
		verifier = f.Get("code_verifier")
		writeJSON(w, 400, map[string]any{
			"error":             "invalid_request",
			"error_description": "bad code the-code-123 verifier " + verifier + " secret shh-456",
		})
	})
	cfg.ClientSecret = "shh-456"
	var body string
	done := make(chan struct{})
	ch := runLoopback(context.Background(), cfg, func(u *url.URL) error {
		go func() {
			defer close(done)
			_, body = browse(t, u.Query().Get("redirect_uri"), url.Values{"code": {"the-code-123"}, "state": {u.Query().Get("state")}})
		}()
		return nil
	})
	r := wait(t, ch)
	<-done
	if r.err == nil || !strings.Contains(r.err.Error(), "[redacted]") {
		t.Fatalf("err = %v", r.err)
	}
	mustNotContain(t, r.err.Error(), "the-code-123", verifier, "shh-456")
	if !strings.Contains(body, "Sign-in failed. Return to dk-email.") {
		t.Errorf("browser body = %q", body)
	}
}

func TestDeviceCode_Success(t *testing.T) {
	var sleeps []time.Duration
	rec, cfg := newServer(t, func(n int, f url.Values, w http.ResponseWriter) {})
	rec.h = func(n int, f url.Values, w http.ResponseWriter) {
		switch n {
		case 1:
			writeJSON(w, 200, map[string]any{
				"device_code": "dev-code", "user_code": "ABCD", "verification_uri": "https://example.com/device",
				"expires_in": 900, "interval": 2, "message": "Go sign in",
			})
		case 2, 3:
			writeJSON(w, 400, map[string]any{"error": "authorization_pending"})
		default:
			writeJSON(w, 200, map[string]any{"access_token": "acc", "refresh_token": "ref", "expires_in": 60})
		}
	}
	cfg.ClientSecret = "shh"
	cfg.sleep = func(ctx context.Context, d time.Duration) error { sleeps = append(sleeps, d); return ctx.Err() }
	var shown []DeviceAuth
	tok, err := cfg.DeviceCode(context.Background(), func(d DeviceAuth) { shown = append(shown, d) })
	if err != nil {
		t.Fatal(err)
	}
	if tok.AccessToken != "acc" || tok.RefreshToken != "ref" || !tok.Expiry.Equal(fixedNow.Add(time.Minute)) {
		t.Errorf("token = %+v", tok)
	}
	want := DeviceAuth{UserCode: "ABCD", VerificationURI: "https://example.com/device", Message: "Go sign in", ExpiresIn: 900 * time.Second}
	if len(shown) != 1 || shown[0] != want {
		t.Errorf("shown = %+v", shown)
	}
	if len(sleeps) != 3 {
		t.Fatalf("sleeps = %v", sleeps)
	}
	for _, d := range sleeps {
		if d != 2*time.Second {
			t.Errorf("sleep %v", d)
		}
	}
	if f := rec.form(0); f.Get("client_id") != "client-id" || f.Get("scope") != "scope.a scope.b" {
		t.Errorf("device form = %v", f)
	}
	f := rec.form(3)
	for k, v := range map[string]string{
		"grant_type": "urn:ietf:params:oauth:grant-type:device_code", "device_code": "dev-code",
		"client_id": "client-id", "client_secret": "shh",
	} {
		if f.Get(k) != v {
			t.Errorf("%s = %q, want %q", k, f.Get(k), v)
		}
	}
}

func TestDeviceCode_VerificationURLFallbackAndDefaultInterval(t *testing.T) {
	var sleeps []time.Duration
	_, cfg := newServer(t, func(n int, f url.Values, w http.ResponseWriter) {
		if n == 1 {
			writeJSON(w, 200, map[string]any{"device_code": "dev-code-1", "user_code": "U", "verification_url": "https://g.example/d", "expires_in": 600})
			return
		}
		writeJSON(w, 200, map[string]any{"access_token": "a", "expires_in": 60})
	})
	cfg.sleep = func(ctx context.Context, d time.Duration) error { sleeps = append(sleeps, d); return ctx.Err() }
	var shown DeviceAuth
	if _, err := cfg.DeviceCode(context.Background(), func(d DeviceAuth) { shown = d }); err != nil {
		t.Fatal(err)
	}
	if shown.VerificationURI != "https://g.example/d" {
		t.Errorf("uri = %q", shown.VerificationURI)
	}
	if len(sleeps) != 1 || sleeps[0] != 5*time.Second {
		t.Errorf("sleeps = %v", sleeps)
	}
}

func TestDeviceCode_SlowDown(t *testing.T) {
	var sleeps []time.Duration
	_, cfg := newServer(t, func(n int, f url.Values, w http.ResponseWriter) {
		switch n {
		case 1:
			writeJSON(w, 200, map[string]any{"device_code": "dev-code-1", "user_code": "U", "verification_uri": "u", "expires_in": 900, "interval": 1})
		case 2:
			writeJSON(w, 400, map[string]any{"error": "slow_down"})
		case 3:
			writeJSON(w, 400, map[string]any{"error": "slow_down"})
		default:
			writeJSON(w, 200, map[string]any{"access_token": "a", "expires_in": 60})
		}
	})
	cfg.sleep = func(ctx context.Context, d time.Duration) error { sleeps = append(sleeps, d); return ctx.Err() }
	if _, err := cfg.DeviceCode(context.Background(), func(DeviceAuth) {}); err != nil {
		t.Fatal(err)
	}
	want := []time.Duration{time.Second, 6 * time.Second, 11 * time.Second}
	if len(sleeps) != len(want) {
		t.Fatalf("sleeps = %v", sleeps)
	}
	for i := range want {
		if sleeps[i] != want[i] {
			t.Errorf("sleeps = %v, want %v", sleeps, want)
			break
		}
	}
}

func TestDeviceCode_TerminalErrors(t *testing.T) {
	for _, code := range []string{"expired_token", "access_denied"} {
		t.Run(code, func(t *testing.T) {
			_, cfg := newServer(t, func(n int, f url.Values, w http.ResponseWriter) {
				if n == 1 {
					writeJSON(w, 200, map[string]any{"device_code": "dev-secret", "user_code": "U", "verification_uri": "u", "expires_in": 900, "interval": 1})
					return
				}
				writeJSON(w, 400, map[string]any{"error": code, "error_description": "echo dev-secret"})
			})
			_, err := cfg.DeviceCode(context.Background(), func(DeviceAuth) {})
			var oe *Error
			if !errors.As(err, &oe) || oe.Code != code {
				t.Fatalf("err = %v", err)
			}
			mustNotContain(t, err.Error(), "dev-secret")
		})
	}
}

func TestDeviceCode_DeviceEndpointError(t *testing.T) {
	_, cfg := newServer(t, func(_ int, _ url.Values, w http.ResponseWriter) {
		writeJSON(w, 400, map[string]any{"error": "invalid_client", "error_description": "nope"})
	})
	_, err := cfg.DeviceCode(context.Background(), func(DeviceAuth) { t.Error("show called") })
	var oe *Error
	if !errors.As(err, &oe) || oe.Code != "invalid_client" {
		t.Fatalf("err = %v", err)
	}
}

func TestDeviceCode_NoDeviceURL(t *testing.T) {
	rec, cfg := newServer(t, func(int, url.Values, http.ResponseWriter) {})
	cfg.Provider.DeviceURL = ""
	cfg.Provider.Name = "google"
	_, err := cfg.DeviceCode(context.Background(), func(DeviceAuth) {})
	if err == nil || err.Error() != "oauth: google has no device code sign-in" {
		t.Fatalf("err = %v", err)
	}
	if rec.count() != 0 {
		t.Error("HTTP request made")
	}
}

func TestDeviceCode_Expires(t *testing.T) {
	now := fixedNow
	_, cfg := newServer(t, func(n int, f url.Values, w http.ResponseWriter) {
		if n == 1 {
			writeJSON(w, 200, map[string]any{"device_code": "dev-code-1", "user_code": "U", "verification_uri": "u", "expires_in": 12, "interval": 5})
			return
		}
		writeJSON(w, 400, map[string]any{"error": "authorization_pending"})
	})
	cfg.Now = func() time.Time { return now }
	cfg.sleep = func(ctx context.Context, d time.Duration) error { now = now.Add(d); return ctx.Err() }
	_, err := cfg.DeviceCode(context.Background(), func(DeviceAuth) {})
	if err == nil || err.Error() != "oauth: device code expired" {
		t.Fatalf("err = %v", err)
	}
}

func TestDeviceCode_ContextCancel(t *testing.T) {
	_, cfg := newServer(t, func(n int, f url.Values, w http.ResponseWriter) {
		writeJSON(w, 200, map[string]any{"device_code": "dev-code-1", "user_code": "U", "verification_uri": "u", "expires_in": 900, "interval": 1})
	})
	ctx, cancel := context.WithCancel(context.Background())
	cfg.sleep = func(ctx context.Context, d time.Duration) error { cancel(); return ctx.Err() }
	_, err := cfg.DeviceCode(ctx, func(DeviceAuth) {})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}
