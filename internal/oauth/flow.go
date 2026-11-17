package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/dkouvdis/dk-email/internal/secret"
)

const (
	successPage = "Signed in to dk-email. You can close this tab."
	failurePage = "Sign-in failed. Return to dk-email."
)

func randomString(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("oauth: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

type loopbackResult struct {
	tok secret.OAuthToken
	err error
}

// Loopback signs in with the authorization code flow and PKCE. It listens on
// the loopback interface for the redirect and calls onURL with the
// authorization URL; the caller opens the browser and may show the URL.
func (c Config) Loopback(ctx context.Context, onURL func(authURL string) error) (secret.OAuthToken, error) {
	l4, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return secret.OAuthToken{}, fmt.Errorf("oauth: %w", err)
	}
	port := l4.Addr().(*net.TCPAddr).Port
	// Browsers may resolve localhost to IPv6; best effort.
	l6, _ := net.Listen("tcp", net.JoinHostPort("::1", strconv.Itoa(port)))

	verifier, err := randomString(32)
	if err != nil {
		l4.Close()
		if l6 != nil {
			l6.Close()
		}
		return secret.OAuthToken{}, err
	}
	state, err := randomString(16)
	if err != nil {
		l4.Close()
		if l6 != nil {
			l6.Close()
		}
		return secret.OAuthToken{}, err
	}
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	redirect := "http://localhost:" + strconv.Itoa(port) + "/"

	done := make(chan loopbackResult, 1)
	srv := &http.Server{
		ReadHeaderTimeout: 10 * time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c.handleCallback(ctx, w, r, state, verifier, redirect, done)
		}),
	}
	go srv.Serve(l4)
	if l6 != nil {
		go srv.Serve(l6)
	}
	defer func() {
		sctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if srv.Shutdown(sctx) != nil {
			srv.Close()
		}
	}()

	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {c.ClientID},
		"redirect_uri":          {redirect},
		"scope":                 {strings.Join(c.Provider.Scopes, " ")},
		"state":                 {state},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}
	if c.LoginHint != "" {
		q.Set("login_hint", c.LoginHint)
	}
	for k, v := range c.Provider.AuthParams {
		q.Set(k, v)
	}
	if err := onURL(c.Provider.AuthURL + "?" + q.Encode()); err != nil {
		return secret.OAuthToken{}, fmt.Errorf("oauth: %w", err)
	}

	select {
	case res := <-done:
		return res.tok, res.err
	case <-ctx.Done():
		return secret.OAuthToken{}, ctx.Err()
	}
}

func (c Config) handleCallback(ctx context.Context, w http.ResponseWriter, r *http.Request, state, verifier, redirect string, done chan<- loopbackResult) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	q := r.URL.Query()
	if subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(state)) != 1 {
		http.Error(w, "state mismatch", http.StatusBadRequest)
		return
	}
	var res loopbackResult
	switch {
	case q.Get("error") != "":
		res.err = &Error{Code: clean(q.Get("error")), Description: clean(q.Get("error_description"))}
	case q.Get("code") != "":
		code := q.Get("code")
		form := url.Values{
			"grant_type":    {"authorization_code"},
			"code":          {code},
			"redirect_uri":  {redirect},
			"code_verifier": {verifier},
		}
		res.tok, res.err = c.tokenRequest(ctx, form, code, verifier)
	default:
		http.Error(w, "missing code", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	// Only the first completed callback counts.
	select {
	case done <- res:
		if res.err != nil {
			fmt.Fprint(w, failurePage)
		} else {
			fmt.Fprint(w, successPage)
		}
	default:
		fmt.Fprint(w, failurePage)
	}
}

// DeviceAuth is what the user needs to complete a device code sign-in.
type DeviceAuth struct {
	UserCode        string
	VerificationURI string
	Message         string
	ExpiresIn       time.Duration
}

type deviceResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	VerificationURL string `json:"verification_url"`
	ExpiresIn       int64  `json:"expires_in"`
	Interval        int64  `json:"interval"`
	Message         string `json:"message"`
}

// DeviceCode signs in with the device code flow. show is called once with the
// code and address the user must visit; the token is then polled for.
func (c Config) DeviceCode(ctx context.Context, show func(DeviceAuth)) (secret.OAuthToken, error) {
	if c.Provider.DeviceURL == "" {
		return secret.OAuthToken{}, fmt.Errorf("oauth: %s has no device code sign-in", c.Provider.Name)
	}
	form := url.Values{"client_id": {c.ClientID}, "scope": {strings.Join(c.Provider.Scopes, " ")}}
	secrets := []string(nil)
	if c.ClientSecret != "" {
		form.Set("client_secret", c.ClientSecret)
		secrets = append(secrets, c.ClientSecret)
	}
	var dr deviceResponse
	if err := c.post(ctx, c.Provider.DeviceURL, form, &dr, secrets...); err != nil {
		return secret.OAuthToken{}, err
	}
	uri := dr.VerificationURI
	if uri == "" {
		uri = dr.VerificationURL
	}
	expires := time.Duration(dr.ExpiresIn) * time.Second
	show(DeviceAuth{UserCode: dr.UserCode, VerificationURI: uri, Message: dr.Message, ExpiresIn: expires})

	interval := time.Duration(dr.Interval) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}
	start := c.now()
	for {
		if err := c.wait(ctx, interval); err != nil {
			return secret.OAuthToken{}, err
		}
		if expires > 0 && c.now().Sub(start) >= expires {
			return secret.OAuthToken{}, errors.New("oauth: device code expired")
		}
		tok, err := c.tokenRequest(ctx, url.Values{
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
			"device_code": {dr.DeviceCode},
		}, dr.DeviceCode)
		if err == nil {
			return tok, nil
		}
		var oe *Error
		if errors.As(err, &oe) {
			switch oe.Code {
			case "authorization_pending":
				continue
			case "slow_down":
				interval += 5 * time.Second
				continue
			}
		}
		return secret.OAuthToken{}, err
	}
}
