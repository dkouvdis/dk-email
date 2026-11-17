package main

import (
	"context"
	"errors"
	"os/exec"
	"runtime"
	"sync"

	"github.com/dkouvdis/dk-email/internal/config"
	"github.com/dkouvdis/dk-email/internal/oauth"
	"github.com/dkouvdis/dk-email/internal/ui"
	"github.com/dkouvdis/dk-email/internal/ui/popup"
)

// openBrowser opens url in the user's browser; tests replace it.
var openBrowser = func(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default: // Linux and the BSDs
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }() // reap it; the browser outlives us anyway
	return nil
}

// runLoopback and runDeviceCode run the OAuth sign-in flows; tests replace
// them to avoid the network.
var (
	runLoopback = func(ctx context.Context, cfg oauth.Config, onURL func(string) error) (oauth.Token, error) {
		return cfg.Loopback(ctx, onURL)
	}
	runDeviceCode = func(ctx context.Context, cfg oauth.Config, show func(oauth.DeviceAuth)) (oauth.Token, error) {
		return cfg.DeviceCode(ctx, show)
	}
)

// errBrowser is what the loopback flow's URL callback returns when the
// browser couldn't be opened and the device code flow can take over.
var errBrowser = errors.New("couldn't open the browser")

// signIn builds the connect popup's OAuth sign-in. The client secret typed in
// the form wins; without one the saved one (or its command) is used, so
// signing in again keeps it.
func signIn(pw secretResolver) func(ctx context.Context, acct config.Account, clientSecret string, useCode bool, progress func(popup.SignIn)) (ui.OAuthSession, error) {
	return func(ctx context.Context, acct config.Account, clientSecret string, useCode bool, progress func(popup.SignIn)) (ui.OAuthSession, error) {
		p, err := oauth.ProviderFor(acct)
		if err != nil {
			return ui.OAuthSession{}, err
		}
		typed := clientSecret
		if clientSecret == "" {
			if clientSecret, err = pw.ResolveClientSecret(ctx, acct); err != nil {
				return ui.OAuthSession{}, err
			}
		}
		cfg := oauth.Config{Provider: p, ClientID: acct.OAuthClientID, ClientSecret: clientSecret, LoginHint: acct.Email}

		deviceCode := func() (oauth.Token, error) {
			return runDeviceCode(ctx, cfg, func(d oauth.DeviceAuth) {
				progress(popup.SignIn{UserCode: d.UserCode, VerificationURI: d.VerificationURI})
			})
		}
		var tok oauth.Token
		if useCode {
			tok, err = deviceCode()
		} else {
			tok, err = runLoopback(ctx, cfg, func(u string) error {
				progress(popup.SignIn{URL: u})
				// Google has no device code flow for mail, so its popup keeps
				// showing the address to open by hand.
				if openBrowser(u) != nil && p.DeviceURL != "" {
					return errBrowser
				}
				return nil
			})
			if errors.Is(err, errBrowser) {
				tok, err = deviceCode()
			}
		}
		if err != nil {
			return ui.OAuthSession{}, err
		}

		s := &oauthSession{tok: tok, clientSecret: typed}
		return ui.OAuthSession{
			Creds:  tokenCredentials(oauth.NewTokenSource(cfg, s.load, s.saveRefreshed)),
			Save:   s.Save,
			Forget: s.Forget,
		}, nil
	}
}

// oauthSession is a finished sign-in that isn't saved yet. Until Save runs a
// refreshed token only updates the copy in memory; afterwards it is also
// stored under the name the sign-in was saved as.
type oauthSession struct {
	// clientSecret is the one typed in the form, kept only if Save succeeds.
	clientSecret string

	mu      sync.Mutex
	tok     oauth.Token
	savedAs string
}

func (s *oauthSession) load() (oauth.Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tok, nil
}

func (s *oauthSession) saveRefreshed(t oauth.Token) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tok = t
	if s.savedAs == "" {
		return nil
	}
	return storeToken(s.savedAs, t)
}

// Save stores the token, then a typed client secret, under name. If the
// secret can't be stored the token is removed again.
func (s *oauthSession) Save(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := storeToken(name, s.tok); err != nil {
		return err
	}
	if s.clientSecret != "" {
		if err := storeClientSecret(name, s.clientSecret); err != nil {
			_ = deleteToken(name)
			return err
		}
	}
	s.savedAs = name
	return nil
}

// Forget removes what Save stored under name, ignoring errors.
func (s *oauthSession) Forget(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = deleteToken(name)
	if s.clientSecret != "" {
		_ = deleteClientSecret(name)
	}
	s.savedAs = ""
}
