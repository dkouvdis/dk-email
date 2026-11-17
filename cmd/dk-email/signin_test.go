package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/dkouvdis/dk-email/internal/config"
	"github.com/dkouvdis/dk-email/internal/mail"
	"github.com/dkouvdis/dk-email/internal/oauth"
	"github.com/dkouvdis/dk-email/internal/secret"
	"github.com/dkouvdis/dk-email/internal/ui"
	"github.com/dkouvdis/dk-email/internal/ui/popup"
)

// signInStubs replaces the browser and flow hooks and records what they saw.
type signInStubs struct {
	opened     []string
	browserErr error
	loopbackFn func(cfg oauth.Config, onURL func(string) error) (oauth.Token, error)
	deviceCfgs []oauth.Config
	loopCfgs   []oauth.Config
	progress   []popup.SignIn
}

var testToken = oauth.Token{AccessToken: "access-1", RefreshToken: "refresh-1", Expiry: time.Now().Add(time.Hour)}

func stubSignIn(t *testing.T) *signInStubs {
	t.Helper()
	s := &signInStubs{}
	oldB, oldL, oldD := openBrowser, runLoopback, runDeviceCode
	openBrowser = func(u string) error {
		s.opened = append(s.opened, u)
		return s.browserErr
	}
	runLoopback = func(_ context.Context, cfg oauth.Config, onURL func(string) error) (oauth.Token, error) {
		s.loopCfgs = append(s.loopCfgs, cfg)
		if s.loopbackFn != nil {
			return s.loopbackFn(cfg, onURL)
		}
		if err := onURL("https://auth.example/authorize?s=1"); err != nil {
			return oauth.Token{}, fmt.Errorf("oauth: %w", err)
		}
		return testToken, nil
	}
	runDeviceCode = func(_ context.Context, cfg oauth.Config, show func(oauth.DeviceAuth)) (oauth.Token, error) {
		s.deviceCfgs = append(s.deviceCfgs, cfg)
		show(oauth.DeviceAuth{UserCode: "ABCD-EFGH", VerificationURI: "https://microsoft.com/devicelogin"})
		return testToken, nil
	}
	t.Cleanup(func() { openBrowser, runLoopback, runDeviceCode = oldB, oldL, oldD })
	return s
}

func (s *signInStubs) report(p popup.SignIn) { s.progress = append(s.progress, p) }

// csResolver resolves a fixed client secret.
type csResolver struct {
	stubResolver
	clientSecret string
}

func (c csResolver) ResolveClientSecret(context.Context, config.Account) (string, error) {
	return c.clientSecret, nil
}

func googleAccount() config.Account {
	return config.Account{Name: "G", Email: "me@gmail.com", Auth: config.AuthOAuth2, OAuthProvider: config.ProviderGoogle, OAuthClientID: "gid"}
}

func TestSignIn_BrowserPathReportsTheURLAndOpensIt(t *testing.T) {
	s := stubSignIn(t)

	sess, err := signIn(stubResolver{})(context.Background(), oauthProviderAccount(), "", false, s.report)

	if err != nil || sess.Creds == nil || sess.Save == nil || sess.Forget == nil {
		t.Fatalf("session %+v, error %v", sess, err)
	}
	if want := []popup.SignIn{{URL: "https://auth.example/authorize?s=1"}}; !reflect.DeepEqual(s.progress, want) {
		t.Errorf("progress = %+v, want %+v", s.progress, want)
	}
	if want := []string{"https://auth.example/authorize?s=1"}; !slices.Equal(s.opened, want) {
		t.Errorf("opened %v, want %v", s.opened, want)
	}
	if len(s.deviceCfgs) != 0 {
		t.Error("the device code flow must not run")
	}
	if c := s.loopCfgs[0]; c.ClientID != "client-id" || c.LoginHint != "me@work.com" || c.Provider.Name != config.ProviderMicrosoft {
		t.Errorf("cfg = %+v", c)
	}
	login, err := sess.Creds(context.Background())
	if err != nil || login.Token != "access-1" {
		t.Errorf("creds = %+v, %v; want the signed-in token", login, err)
	}
}

func TestSignIn_MicrosoftBrowserFailureFallsBackToDeviceCode(t *testing.T) {
	s := stubSignIn(t)
	s.browserErr = errors.New("no display")

	_, err := signIn(stubResolver{})(context.Background(), oauthProviderAccount(), "", false, s.report)

	if err != nil {
		t.Fatal(err)
	}
	want := []popup.SignIn{{URL: "https://auth.example/authorize?s=1"}, {UserCode: "ABCD-EFGH", VerificationURI: "https://microsoft.com/devicelogin"}}
	if !reflect.DeepEqual(s.progress, want) {
		t.Errorf("progress = %+v, want %+v", s.progress, want)
	}
	if len(s.deviceCfgs) != 1 {
		t.Errorf("device code runs = %d, want 1", len(s.deviceCfgs))
	}
}

func TestSignIn_GoogleBrowserFailureKeepsWaitingForTheCallback(t *testing.T) {
	s := stubSignIn(t)
	s.browserErr = errors.New("no display")
	var urlErr error
	s.loopbackFn = func(_ oauth.Config, onURL func(string) error) (oauth.Token, error) {
		urlErr = onURL("https://accounts.example/auth")
		return testToken, nil // the user opened the address by hand
	}

	sess, err := signIn(stubResolver{})(context.Background(), googleAccount(), "typed", false, s.report)

	if err != nil || sess.Creds == nil {
		t.Fatalf("session %+v, error %v", sess, err)
	}
	if urlErr != nil {
		t.Errorf("onURL returned %v; Google has no device flow, so it must keep waiting", urlErr)
	}
	if len(s.deviceCfgs) != 0 {
		t.Error("Google must not fall back to a device code")
	}
	if want := []popup.SignIn{{URL: "https://accounts.example/auth"}}; !reflect.DeepEqual(s.progress, want) {
		t.Errorf("progress = %+v", s.progress)
	}
}

func TestSignIn_UseCodeGoesStraightToTheDeviceFlow(t *testing.T) {
	s := stubSignIn(t)

	_, err := signIn(stubResolver{})(context.Background(), oauthProviderAccount(), "", true, s.report)

	if err != nil || len(s.loopCfgs) != 0 || len(s.opened) != 0 {
		t.Fatalf("error %v, loopback runs %d, browser opens %v", err, len(s.loopCfgs), s.opened)
	}
	if want := []popup.SignIn{{UserCode: "ABCD-EFGH", VerificationURI: "https://microsoft.com/devicelogin"}}; !reflect.DeepEqual(s.progress, want) {
		t.Errorf("progress = %+v", s.progress)
	}
}

func TestSignIn_FlowErrorIsReturned(t *testing.T) {
	s := stubSignIn(t)
	want := errors.New("access_denied")
	s.loopbackFn = func(oauth.Config, func(string) error) (oauth.Token, error) { return oauth.Token{}, want }

	sess, err := signIn(stubResolver{})(context.Background(), oauthProviderAccount(), "", false, s.report)

	if !errors.Is(err, want) || sess.Creds != nil {
		t.Errorf("session %+v, error %v", sess, err)
	}
}

func TestSignIn_TypedClientSecretWinsOverTheSavedOne(t *testing.T) {
	for _, tc := range []struct{ typed, saved, want string }{
		{"typed", "saved", "typed"},
		{"", "saved", "saved"},
		{"", "", ""},
	} {
		s := stubSignIn(t)

		_, err := signIn(csResolver{clientSecret: tc.saved})(context.Background(), googleAccount(), tc.typed, false, s.report)

		if err != nil || s.loopCfgs[0].ClientSecret != tc.want {
			t.Errorf("typed %q saved %q: cfg secret %q, error %v; want %q", tc.typed, tc.saved, s.loopCfgs[0].ClientSecret, err, tc.want)
		}
	}
}

func TestSignIn_SaveStoresTheTokenThenTheTypedClientSecret(t *testing.T) {
	stubSignIn(t)
	k := stubKeychain(t, nil)
	var stored []string
	oldStore := storeToken
	storeToken = func(account string, tok oauth.Token) error {
		stored = append(stored, "store-token "+account+" "+tok.RefreshToken)
		return nil
	}
	t.Cleanup(func() { storeToken = oldStore })

	sess, err := signIn(stubResolver{})(context.Background(), googleAccount(), "typed", false, func(popup.SignIn) {})
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 0 || len(k.calls) != 0 {
		t.Fatalf("nothing may be saved before Save: %v %v", stored, k.calls)
	}

	if err := sess.Save("Gmail"); err != nil {
		t.Fatal(err)
	}

	if want := []string{"store-token Gmail refresh-1"}; !slices.Equal(stored, want) {
		t.Errorf("tokens = %v, want %v", stored, want)
	}
	if want := []string{"store-client-secret Gmail"}; !slices.Equal(k.calls, want) {
		t.Errorf("keychain = %v, want %v", k.calls, want)
	}
}

func TestSignIn_SaveWithoutTypedSecretLeavesTheSavedOneAlone(t *testing.T) {
	stubSignIn(t)
	k := stubKeychain(t, nil)
	oldStore := storeToken
	storeToken = func(string, oauth.Token) error { return nil }
	t.Cleanup(func() { storeToken = oldStore })

	sess, _ := signIn(csResolver{clientSecret: "saved"})(context.Background(), googleAccount(), "", false, func(popup.SignIn) {})
	if err := sess.Save("Gmail"); err != nil {
		t.Fatal(err)
	}
	if len(k.calls) != 0 {
		t.Errorf("keychain = %v, want no client secret written", k.calls)
	}
}

func TestSignIn_ClientSecretFailureRemovesTheToken(t *testing.T) {
	stubSignIn(t)
	k := stubKeychain(t, nil)
	k.secretErr = errors.New("keychain locked")
	oldStore := storeToken
	storeToken = func(string, oauth.Token) error { return nil }
	t.Cleanup(func() { storeToken = oldStore })

	sess, _ := signIn(stubResolver{})(context.Background(), googleAccount(), "typed", false, func(popup.SignIn) {})
	err := sess.Save("Gmail")

	if err == nil || err.Error() != "keychain locked" {
		t.Errorf("error = %v", err)
	}
	if want := []string{"store-client-secret Gmail", "delete-token Gmail"}; !slices.Equal(k.calls, want) {
		t.Errorf("keychain = %v, want %v", k.calls, want)
	}
}

func TestSignIn_ForgetDeletesWhatSaveStored(t *testing.T) {
	stubSignIn(t)
	for _, tc := range []struct {
		typed string
		want  []string
	}{
		{"typed", []string{"delete-token Gmail", "delete-client-secret Gmail"}},
		{"", []string{"delete-token Gmail"}},
	} {
		k := stubKeychain(t, nil)
		k.deleteTokenErr = errors.New("ignored")

		sess, _ := signIn(stubResolver{})(context.Background(), googleAccount(), tc.typed, false, func(popup.SignIn) {})
		sess.Forget("Gmail")

		if !slices.Equal(k.calls, tc.want) {
			t.Errorf("typed %q: keychain = %v, want %v", tc.typed, k.calls, tc.want)
		}
	}
}

// refreshingSession is a session whose token source refreshes on every use
// (each token expires at once), answered by a local token endpoint that hands
// out access-2, access-3, ...
func refreshingSession(t *testing.T) (*oauthSession, *oauth.TokenSource) {
	t.Helper()
	calls := 1
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"access_token":"access-%d","refresh_token":"refresh-%d","expires_in":1}`, calls, calls)
	}))
	t.Cleanup(srv.Close)
	s := &oauthSession{tok: oauth.Token{AccessToken: "access-1", RefreshToken: "refresh-1", Expiry: time.Now().Add(-time.Hour)}}
	cfg := oauth.Config{Provider: oauth.Provider{TokenURL: srv.URL}, ClientID: "cid", HTTPClient: srv.Client()}
	return s, oauth.NewTokenSource(cfg, s.load, s.saveRefreshed)
}

func TestSession_RefreshedTokensAreStoredUnderTheSavedNameOnly(t *testing.T) {
	var stored []string
	oldStore := storeToken
	storeToken = func(account string, tok oauth.Token) error {
		stored = append(stored, account+" "+tok.AccessToken)
		return nil
	}
	t.Cleanup(func() { storeToken = oldStore })
	s, ts := refreshingSession(t)

	// Before Save a refresh only updates the copy in memory.
	if tok, err := ts.Token(context.Background()); err != nil || tok != "access-2" || len(stored) != 0 {
		t.Fatalf("token %q, %v, stored %v; want a refresh that isn't stored", tok, err, stored)
	}

	// Save stores what the session now holds; later refreshes follow it.
	if err := s.Save("Work"); err != nil {
		t.Fatal(err)
	}
	if tok, err := ts.Token(context.Background()); err != nil || tok != "access-3" {
		t.Fatalf("token %q, %v", tok, err)
	}
	if want := []string{"Work access-2", "Work access-3"}; !slices.Equal(stored, want) {
		t.Errorf("stored = %v, want %v", stored, want)
	}
}

func TestNewConnector_WrapsMissingAndRevokedSignInsAsNeedsSignIn(t *testing.T) {
	stubDial(t)
	old := loadToken
	t.Cleanup(func() { loadToken = old })
	for _, cause := range []error{secret.ErrNoToken, oauth.ErrInvalidGrant, fmt.Errorf("oauth: refresh token: %w", oauth.ErrInvalidGrant)} {
		loadToken = func(string) (oauth.Token, error) { return oauth.Token{}, cause }

		conn, err := newConnector(stubResolver{}, slog.New(slog.DiscardHandler))(context.Background(), oauthProviderAccount())

		if !errors.Is(err, ui.ErrNeedsSignIn) || !errors.Is(err, cause) || conn != (ui.Conn{}) {
			t.Errorf("cause %v: connect = %v, %v; want ErrNeedsSignIn wrapping the cause", cause, conn, err)
		}
	}

	// Other errors stay as they are.
	other := errors.New("network down")
	loadToken = func(string) (oauth.Token, error) { return oauth.Token{}, other }
	if _, err := newConnector(stubResolver{}, slog.New(slog.DiscardHandler))(context.Background(), oauthProviderAccount()); !errors.Is(err, other) || errors.Is(err, ui.ErrNeedsSignIn) {
		t.Errorf("error = %v", err)
	}
}

const oauthConfigText = `[[account]]
name = "Corp"
email = "me@corp.com"
imap_host = "outlook.office365.com"
auth = "oauth2"
oauth_provider = "microsoft"
oauth_client_id = "cid"

[[account]]
name = "Work"
email = "me@example.com"
imap_host = "imap.example.com"
`

func writeOAuthConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(oauthConfigText), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func updateOAuth(t *testing.T, edit func(config.Account) config.Account, signedIn bool) (*keychainCalls, error) {
	t.Helper()
	path := writeOAuthConfig(t)
	k := stubKeychain(t, nil)
	o := newOnboarding(path, stubResolver{}, func(string, string) error { t.Error("password stored"); return nil }, slog.New(slog.DiscardHandler))
	return k, o.UpdateAccount(0, "Corp", edit, "", signedIn)
}

func TestOnboarding_UpdateAccountOAuthRenameCopiesThenDropsTheOldSignIn(t *testing.T) {
	k, err := updateOAuth(t, rename("Firm"), false)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"copy-oauth Corp->Firm", "delete-token Corp", "delete-client-secret Corp"}
	if !slices.Equal(k.calls, want) {
		t.Errorf("keychain = %v, want %v", k.calls, want)
	}
}

func TestOnboarding_UpdateAccountOAuthFailedWriteDeletesTheNewEntries(t *testing.T) {
	// "Work" is a duplicate name, so the config write is refused.
	k, err := updateOAuth(t, rename("Work"), false)
	if err == nil {
		t.Fatal("want an error")
	}
	want := []string{"copy-oauth Corp->Work", "delete-token Work", "delete-client-secret Work"}
	if !slices.Equal(k.calls, want) {
		t.Errorf("keychain = %v, want %v", k.calls, want)
	}
}

func TestOnboarding_UpdateAccountOAuthCopyFailureLeavesConfigUntouched(t *testing.T) {
	path := writeOAuthConfig(t)
	k := stubKeychain(t, nil)
	k.copyOAuthErr = errors.New("keychain locked")
	o := newOnboarding(path, stubResolver{}, nil, slog.New(slog.DiscardHandler))

	err := o.UpdateAccount(0, "Corp", rename("Firm"), "", false)

	if err == nil || err.Error() != "keychain locked" {
		t.Errorf("error = %v", err)
	}
	if got, _ := os.ReadFile(path); string(got) != oauthConfigText {
		t.Errorf("config changed: %s", got)
	}
	if want := []string{"copy-oauth Corp->Firm"}; !slices.Equal(k.calls, want) {
		t.Errorf("keychain = %v, want %v", k.calls, want)
	}
}

func TestOnboarding_UpdateAccountSignedInRenameCopiesOnlyTheClientSecretThenDropsTheOldSignIn(t *testing.T) {
	k, err := updateOAuth(t, rename("Firm"), true)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"copy-client-secret Corp->Firm", "delete-token Corp", "delete-client-secret Corp"}
	if !slices.Equal(k.calls, want) {
		t.Errorf("keychain = %v, want %v (no token copy: the new sign-in has its own)", k.calls, want)
	}
}

func TestOnboarding_UpdateAccountSignedInFailedWriteDeletesOnlyACopiedClientSecret(t *testing.T) {
	for _, tc := range []struct {
		copied bool
		want   []string
	}{
		{true, []string{"copy-client-secret Corp->Work", "delete-client-secret Work"}},
		{false, []string{"copy-client-secret Corp->Work"}}, // a typed secret is the UI's to forget
	} {
		path := writeOAuthConfig(t)
		k := stubKeychain(t, nil)
		k.copyClientSecretCopied = tc.copied
		o := newOnboarding(path, stubResolver{}, nil, slog.New(slog.DiscardHandler))

		// "Work" is a duplicate name, so the config write is refused.
		if err := o.UpdateAccount(0, "Corp", rename("Work"), "", true); err == nil {
			t.Fatal("want an error")
		}
		if !slices.Equal(k.calls, tc.want) {
			t.Errorf("copied %v: keychain = %v, want %v", tc.copied, k.calls, tc.want)
		}
	}
}

func TestOnboarding_UpdateAccountSignedInClientSecretCopyFailureLeavesConfigUntouched(t *testing.T) {
	path := writeOAuthConfig(t)
	k := stubKeychain(t, nil)
	k.copyOAuthErr = errors.New("keychain locked")
	o := newOnboarding(path, stubResolver{}, nil, slog.New(slog.DiscardHandler))

	err := o.UpdateAccount(0, "Corp", rename("Firm"), "", true)

	if err == nil || err.Error() != "keychain locked" {
		t.Errorf("error = %v", err)
	}
	if got, _ := os.ReadFile(path); string(got) != oauthConfigText {
		t.Errorf("config changed: %s", got)
	}
}

func TestOnboarding_UpdateAccountSwitchingPasswordAccountToOAuthDropsItsPassword(t *testing.T) {
	toOAuth := func(a config.Account) config.Account {
		a.Auth, a.OAuthProvider, a.OAuthClientID = config.AuthOAuth2, config.ProviderMicrosoft, "cid"
		return a
	}
	path := writeOAuthConfig(t)
	k := stubKeychain(t, nil)
	o := newOnboarding(path, stubResolver{}, nil, slog.New(slog.DiscardHandler))

	if err := o.UpdateAccount(1, "Work", toOAuth, "", true); err != nil {
		t.Fatal(err)
	}
	if want := []string{"delete Work"}; !slices.Equal(k.calls, want) {
		t.Errorf("keychain = %v, want %v", k.calls, want)
	}
}

func TestOnboarding_UpdateAccountSwitchingPasswordAccountToOAuthFailedWriteDeletesNothing(t *testing.T) {
	toOAuthAs := func(name string) func(config.Account) config.Account {
		return func(a config.Account) config.Account {
			a.Name = name
			a.Auth, a.OAuthProvider, a.OAuthClientID = config.AuthOAuth2, config.ProviderMicrosoft, "cid"
			return a
		}
	}
	path := writeOAuthConfig(t)
	k := stubKeychain(t, nil)
	o := newOnboarding(path, stubResolver{}, nil, slog.New(slog.DiscardHandler))

	// "Corp" is a duplicate name, so the config write is refused.
	if err := o.UpdateAccount(1, "Work", toOAuthAs("Corp"), "", true); err == nil {
		t.Fatal("want an error")
	}
	for _, c := range k.calls {
		if c == "delete Work" {
			t.Errorf("the old password was deleted: %v", k.calls)
		}
	}
}

func TestOnboarding_UpdateAccountSignedInFailedWriteLeavesCleanupToTheUI(t *testing.T) {
	k, err := updateOAuth(t, rename("Work"), true)
	if err == nil {
		t.Fatal("want an error")
	}
	if want := []string{"copy-client-secret Corp->Work"}; !slices.Equal(k.calls, want) {
		t.Errorf("keychain = %v, want %v; the UI's Forget removes the new sign-in", k.calls, want)
	}
}

func TestOnboarding_UpdateAccountSignedInSameNameTouchesNothing(t *testing.T) {
	k, err := updateOAuth(t, func(a config.Account) config.Account { a.OAuthClientID = "other"; return a }, true)
	if err != nil || len(k.calls) != 0 {
		t.Errorf("error %v, keychain %v", err, k.calls)
	}
}

func TestOnboarding_UpdateAccountSwitchingAwayFromOAuthDropsItsEntries(t *testing.T) {
	toCmd := func(a config.Account) config.Account {
		a.Auth, a.OAuthProvider, a.OAuthClientID = "", "", ""
		a.PasswordCmd = "op read op://V/I/p"
		return a
	}
	k, err := updateOAuth(t, toCmd, false)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"delete-token Corp", "delete-client-secret Corp"}; !slices.Equal(k.calls, want) {
		t.Errorf("keychain = %v, want %v", k.calls, want)
	}
}

func TestOnboarding_UpdateAccountPasswordAccountNeverTouchesOAuthEntries(t *testing.T) {
	path := writeOAuthConfig(t)
	k := stubKeychain(t, nil)
	o := newOnboarding(path, stubResolver{}, nil, slog.New(slog.DiscardHandler))

	if err := o.UpdateAccount(1, "Work", rename("Job"), "", false); err != nil {
		t.Fatal(err)
	}
	if want := []string{"copy Work->Job", "delete Work"}; !slices.Equal(k.calls, want) {
		t.Errorf("keychain = %v, want %v", k.calls, want)
	}
}

func TestOnboarding_RemoveAccountOAuth(t *testing.T) {
	oauthAcct := config.Account{Name: "Corp", Auth: config.AuthOAuth2}
	cmdAcct := config.Account{Name: "Corp", Auth: config.AuthOAuth2, PasswordCmd: "oama access me"}
	for _, tc := range []struct {
		name     string
		acct     config.Account
		deletePW bool
		want     []string
	}{
		{"oauth, delete", oauthAcct, true, []string{"delete-token Corp", "delete-client-secret Corp"}},
		{"oauth, keep", oauthAcct, false, nil},
		{"oauth with token command", cmdAcct, true, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeOAuthConfig(t)
			k := stubKeychain(t, nil)
			o := newOnboarding(path, stubResolver{}, nil, slog.New(slog.DiscardHandler))

			if err := o.RemoveAccount(0, tc.acct, tc.deletePW); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(k.calls, tc.want) {
				t.Errorf("keychain = %v, want %v", k.calls, tc.want)
			}
		})
	}
}

func TestOnboarding_RemoveAccountOAuthReturnsTheFirstErrorButTriesBoth(t *testing.T) {
	path := writeOAuthConfig(t)
	k := stubKeychain(t, nil)
	k.deleteTokenErr = errors.New("token delete failed")
	o := newOnboarding(path, stubResolver{}, nil, slog.New(slog.DiscardHandler))

	err := o.RemoveAccount(0, config.Account{Name: "Corp", Auth: config.AuthOAuth2}, true)

	if err == nil || err.Error() != "token delete failed" {
		t.Errorf("error = %v", err)
	}
	if want := []string{"delete-token Corp", "delete-client-secret Corp"}; !slices.Equal(k.calls, want) {
		t.Errorf("keychain = %v, want %v", k.calls, want)
	}
}

func TestOnboarding_DialPrefersGivenCredsOverConfigured(t *testing.T) {
	logins := stubDial(t)
	o := newOnboarding("", stubResolver{err: errors.New("must not resolve")}, nil, slog.New(slog.DiscardHandler))
	creds := func(context.Context) (mail.Login, error) { return mail.Login{Token: "fresh"}, nil }

	if _, err := o.Dial(context.Background(), config.Account{Name: "A", Email: "a@x.com", Auth: config.AuthOAuth2}, creds); err != nil {
		t.Fatal(err)
	}
	if want := []mail.Login{{Token: "fresh"}}; !reflect.DeepEqual(*logins, want) {
		t.Errorf("logins = %v, want %v", *logins, want)
	}
}
