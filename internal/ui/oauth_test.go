package ui

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/dkouvdis/dk-email/internal/config"
	"github.com/dkouvdis/dk-email/internal/mail"
	"github.com/dkouvdis/dk-email/internal/ui/popup"
)

// fakeSignIn stands in for main's OAuth sign-in: it reports a progress URL (or
// a device code), then returns a session whose Save and Forget it records.
type fakeSignIn struct {
	mu       sync.Mutex
	events   []string
	ctxs     []context.Context
	accts    []config.Account
	secrets  []string
	codes    []bool
	err      error
	saveErr  error
	holdOnce bool // the first call waits for its ctx to end after reporting
	calls    int
}

func (f *fakeSignIn) record(e string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, e)
}

func (f *fakeSignIn) log() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.events)
}

func (f *fakeSignIn) signIn(ctx context.Context, acct config.Account, clientSecret string, useCode bool, progress func(popup.SignIn)) (OAuthSession, error) {
	f.mu.Lock()
	f.calls++
	first := f.calls == 1
	f.ctxs = append(f.ctxs, ctx)
	f.accts = append(f.accts, acct)
	f.secrets = append(f.secrets, clientSecret)
	f.codes = append(f.codes, useCode)
	f.mu.Unlock()

	if useCode {
		progress(popup.SignIn{UserCode: "CODE-123", VerificationURI: "https://example.com/device"})
	} else {
		progress(popup.SignIn{URL: "https://example.com/authorize?x=1"})
	}
	if f.holdOnce && first {
		<-ctx.Done()
		return OAuthSession{}, ctx.Err()
	}
	if f.err != nil {
		return OAuthSession{}, f.err
	}
	return OAuthSession{
		Creds: func(context.Context) (mail.Login, error) { return mail.Login{Token: "tok"}, nil },
		Save: func(name string) error {
			f.record("save:" + name)
			return f.saveErr
		},
		Forget: func(name string) { f.record("forget:" + name) },
	}, nil
}

func tokenOf(t *testing.T, creds mail.Credentials) string {
	t.Helper()
	if creds == nil {
		return ""
	}
	l, err := creds(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return l.Token
}

// oauthFirstRun is a first-run model with fake onboarding and sign-in, whose
// AddAccount also logs to the sign-in's event list.
func oauthFirstRun(t *testing.T, o *fakeOnboarding, si *fakeSignIn) Model {
	t.Helper()
	on := o.onboarding()
	on.SignIn = si.signIn
	add := on.AddAccount
	on.AddAccount = func(acct config.Account, password string) error {
		if err := add(acct, password); err != nil {
			return err
		}
		si.record("add:" + acct.Name)
		return nil
	}
	m := New(config.Default(), nil, nil)
	m.every = func(time.Duration) tea.Cmd { return nil }
	m = send(t, m, size(140, 34))
	return m.WithOnboarding(on)
}

// fillOutlookOAuth fills the new-account form with an Outlook address (which
// pre-fills the servers and selects Microsoft), the OAuth 2.0 option and a
// client ID, leaving the focus on the client ID.
func fillOutlookOAuth(t *testing.T, m Model, name string) Model {
	t.Helper()
	m = typeKeys(t, m, name)
	m = send(t, m, press("tab"))
	m = typeKeys(t, m, "me@outlook.com")
	for range 5 { // username, host, port, TLS, source
		m = send(t, m, press("tab"))
	}
	m = send(t, m, press("right"), press("right"), press("tab"), press("tab"))
	return typeKeys(t, m, "client-id")
}

// toSubmitFromClientID tabs from the client ID to Submit: Tenant, SMTP host,
// port, TLS and Aliases come first.
func toSubmitFromClientID(t *testing.T, m Model) Model {
	t.Helper()
	for range 6 {
		m = send(t, m, press("tab"))
	}
	return m
}

func ctrlD() tea.KeyPressMsg { return tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl} }

func TestOAuth_NewAccountSignsInThenSavesBeforeAddingAndConnects(t *testing.T) {
	o, si := newOnboarding(), &fakeSignIn{}
	m := toSubmitFromClientID(t, fillOutlookOAuth(t, oauthFirstRun(t, o, si), "Work"))

	next, cmd := m.Update(press("enter"))
	m = next.(Model)
	first := cmd()
	p, ok := first.(signInProgressMsg)
	if !ok {
		t.Fatalf("first message = %T, want signInProgressMsg", first)
	}
	next, cmd = m.Update(p)
	m = next.(Model)
	if v := plain(m); !strings.Contains(v, "https://example.com/authorize") {
		t.Errorf("progress URL not shown in the popup:\n%s", v)
	}
	m = settle(t, m, cmd)

	if m.showConnect {
		t.Fatalf("popup still open:\n%s", plain(m))
	}
	if want := []string{"save:Work", "add:Work"}; !slices.Equal(si.log(), want) {
		t.Errorf("events = %v, want %v (Save before AddAccount)", si.log(), want)
	}
	if len(o.dialedCreds) != 1 || tokenOf(t, o.dialedCreds[0]) != "tok" {
		t.Errorf("Dial did not get the session creds: %v", o.dialedCreds)
	}
	if len(o.verifiedCreds) != 1 || tokenOf(t, o.verifiedCreds[0]) != "tok" {
		t.Errorf("VerifySMTP did not get the session creds: %v", o.verifiedCreds)
	}
	if len(o.added) != 1 || o.added[0].Name != "Work" || o.addedPasswords[0] != "" || !o.added[0].OAuth() {
		t.Errorf("AddAccount got %+v %v", o.added, o.addedPasswords)
	}
	if si.codes[0] || si.accts[0].IMAPHost != "outlook.office365.com" || si.accts[0].IMAPPort == 0 {
		t.Errorf("SignIn got useCode %v and %+v (defaults should be applied)", si.codes[0], si.accts[0])
	}
	if len(m.accts) != 1 || m.boxes[0] == nil || !strings.Contains(plain(m), "Connected Work.") && !strings.Contains(m.status, "Connected Work.") {
		t.Errorf("account not connected: accts %v status %q", m.accts, m.status)
	}
	if m.connectCancel != nil {
		t.Error("the finished test's cancel func should be cleared")
	}
}

func TestOAuth_AddAccountFailureForgetsTheSignIn(t *testing.T) {
	o, si := newOnboarding(), &fakeSignIn{}
	o.addErr = errors.New("config is read-only")
	m := toSubmitFromClientID(t, fillOutlookOAuth(t, oauthFirstRun(t, o, si), "Work"))

	m = pressSettled(t, m, "enter")

	if !m.showConnect || !strings.Contains(plain(m), "config is read-only") {
		t.Fatalf("the failure should be shown in the open popup:\n%s", plain(m))
	}
	if want := []string{"save:Work", "forget:Work"}; !slices.Equal(si.log(), want) {
		t.Errorf("events = %v, want %v", si.log(), want)
	}
	if len(m.accts) != 0 {
		t.Errorf("accounts = %v", m.accts)
	}
}

func TestOAuth_SignInErrorSavesNothing(t *testing.T) {
	o, si := newOnboarding(), &fakeSignIn{err: errors.New("access_denied")}
	m := toSubmitFromClientID(t, fillOutlookOAuth(t, oauthFirstRun(t, o, si), "Work"))

	m = pressSettled(t, m, "enter")

	if !m.showConnect || !strings.Contains(plain(m), "sign-in failed: access_denied") {
		t.Fatalf("sign-in error not shown:\n%s", plain(m))
	}
	if len(si.log()) != 0 || len(o.dialed) != 0 || len(o.added) != 0 {
		t.Errorf("nothing should happen after a failed sign-in: %v %v %v", si.log(), o.dialed, o.added)
	}
}

func TestOAuth_EscCancelsTheSignInAndIgnoresALateResult(t *testing.T) {
	o, si := newOnboarding(), &fakeSignIn{holdOnce: true}
	m := toSubmitFromClientID(t, fillOutlookOAuth(t, oauthFirstRun(t, o, si), "Work"))

	next, cmd := m.Update(press("enter"))
	m = next.(Model)
	msgs := make(chan tea.Msg, 2)
	go func() {
		msg := cmd() // the progress message
		msgs <- msg
		if p, ok := msg.(signInProgressMsg); ok {
			msgs <- p.next() // blocks until the sign-in ends
		}
	}()
	next, _ = m.Update(<-msgs)
	m = next.(Model)

	m = send(t, m, press("esc"))

	if m.showConnect || m.connectCancel != nil {
		t.Fatalf("esc should close the popup and drop the cancel func: show %v", m.showConnect)
	}
	select {
	case <-si.ctxs[0].Done():
	case <-time.After(5 * time.Second):
		t.Fatal("esc did not cancel the sign-in's context")
	}
	var late tea.Msg
	select {
	case late = <-msgs:
	case <-time.After(5 * time.Second):
		t.Fatal("no late result")
	}
	m = send(t, m, late)
	if m.showConnect || len(o.added) != 0 || len(m.accts) != 0 || len(si.log()) != 0 {
		t.Errorf("a late result must be ignored: added %v accts %v events %v", o.added, m.accts, si.log())
	}
}

func TestOAuth_UseCodeRestartsWithADeviceCode(t *testing.T) {
	o, si := newOnboarding(), &fakeSignIn{holdOnce: true}
	m := toSubmitFromClientID(t, fillOutlookOAuth(t, oauthFirstRun(t, o, si), "Work"))

	next, cmd := m.Update(press("enter"))
	m = next.(Model)
	msgs := make(chan tea.Msg, 2)
	go func() {
		msg := cmd()
		msgs <- msg
		msgs <- msg.(signInProgressMsg).next() // the cancelled first attempt's result
	}()
	next, _ = m.Update(<-msgs)
	m = next.(Model)
	first := m.connectGen

	next, cmd = m.Update(ctrlD())
	m = next.(Model)
	if m.connectGen == first {
		t.Error("the restart should start a new attempt (bump connectGen)")
	}
	m = settle(t, m, cmd)

	select {
	case <-si.ctxs[0].Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the browser attempt was not cancelled")
	}
	if late := <-msgs; late != nil {
		m = send(t, m, late) // stale: must change nothing
	}
	if len(si.codes) != 2 || si.codes[0] || !si.codes[1] {
		t.Fatalf("useCode per call = %v, want [false true]", si.codes)
	}
	if m.showConnect || len(o.added) != 1 || o.added[0].Name != "Work" {
		t.Errorf("the device code attempt should finish the job: show %v added %v", m.showConnect, o.added)
	}
}

func TestOAuth_WithoutSignInShowsAnError(t *testing.T) {
	o := newOnboarding()
	m := toSubmitFromClientID(t, fillOutlookOAuth(t, oauthFirstRun(t, o, &fakeSignIn{}), "Work"))
	m.onboard.SignIn = nil

	next, cmd := m.Update(press("enter"))
	m = next.(Model)

	if cmd != nil || m.connectCancel != nil {
		t.Error("nothing should start")
	}
	if !m.showConnect || !strings.Contains(plain(m), "OAuth sign-in isn't available.") {
		t.Errorf("error missing:\n%s", plain(m))
	}
	if len(o.dialed) != 0 {
		t.Errorf("dialed %v", o.dialed)
	}
}

// oauthEdit is an edit fixture whose account at index 1 (Beta) signs in with
// OAuth, plus a sign-in whose events also include UpdateAccount.
func oauthEdit(t *testing.T, si *fakeSignIn) (*editFixture, Model) {
	t.Helper()
	f, cfg := newEditFixture("Alpha", "Beta")
	cfg.Accounts[1].Auth, cfg.Accounts[1].OAuthProvider, cfg.Accounts[1].OAuthClientID = config.AuthOAuth2, config.ProviderMicrosoft, "client-id"
	f.signIn = si.signIn
	m := editModel(t, f, cfg)
	o := f.onboarding()
	upd := o.UpdateAccount
	o.UpdateAccount = func(i int, old string, edit func(config.Account) config.Account, pw string, signedIn bool) error {
		if err := upd(i, old, edit, pw, signedIn); err != nil {
			return err
		}
		si.record(fmt.Sprintf("update:%s:%t", old, signedIn))
		return nil
	}
	m = m.WithOnboarding(o)
	return f, cursorOn(t, m, 1)
}

// toOAuthLogin chooses OAuth 2.0 in the open edit popup (optionally renaming
// first) and leaves the focus on Submit.
func toOAuthLogin(t *testing.T, m Model, rename string) Model {
	t.Helper()
	if rename != "" {
		m = send(t, m, renameTo(rename)...)
	}
	for range 6 { // email, username, host, port, TLS, login
		m = send(t, m, press("tab"))
	}
	m = send(t, m, press("right"), press("right"), press("right"), press("tab"), press("tab"))
	if !m.connectPop.OAuth() {
		t.Fatalf("OAuth 2.0 not selected:\n%s", plain(m))
	}
	return toSubmitFromClientID(t, m)
}

func TestOAuthEdit_ReSignInRenameSavesThenUpdates(t *testing.T) {
	si := &fakeSignIn{}
	f, m := oauthEdit(t, si)
	m = send(t, m, press("E"))
	m = toOAuthLogin(t, m, "Bee")

	m = pressSettled(t, m, "enter")

	if m.showConnect {
		t.Fatalf("popup still open:\n%s", plain(m))
	}
	if want := []string{"save:Bee", "update:Beta:true"}; !slices.Equal(si.log(), want) {
		t.Errorf("events = %v, want %v", si.log(), want)
	}
	// The saved client secret is still under the old name until the save.
	if si.accts[0].Name != "Beta" {
		t.Errorf("signed in as %q, want the old name", si.accts[0].Name)
	}
	if len(f.dialed) != 1 || f.dialed[0] != "Bee" || tokenOf(t, f.dialedCreds[0]) != "tok" {
		t.Errorf("dialed %v with creds %q", f.dialed, tokenOf(t, f.dialedCreds[0]))
	}
	if len(f.updSignedIn) != 1 || !f.updSignedIn[0] || m.accts[1].Name != "Bee" {
		t.Errorf("signedIn %v account %+v", f.updSignedIn, m.accts[1])
	}
}

func TestOAuthEdit_ReSignInSameNameSavesThenUpdates(t *testing.T) {
	si := &fakeSignIn{}
	f, m := oauthEdit(t, si)
	m = send(t, m, press("E"))
	m = toOAuthLogin(t, m, "")

	m = pressSettled(t, m, "enter")

	if want := []string{"save:Beta", "update:Beta:true"}; !slices.Equal(si.log(), want) {
		t.Errorf("events = %v, want %v", si.log(), want)
	}
	if len(f.updSignedIn) != 1 || !f.updSignedIn[0] {
		t.Errorf("signedIn %v", f.updSignedIn)
	}
}

func TestOAuthEdit_UpdateFailureForgetsOnlyOnRename(t *testing.T) {
	for _, tc := range []struct {
		rename string
		want   []string
	}{
		{"Bee", []string{"save:Bee", "forget:Bee"}},
		{"", []string{"save:Beta"}}, // the new token simply replaced the old one
	} {
		si := &fakeSignIn{}
		f, m := oauthEdit(t, si)
		f.updErr = errors.New("config changed on disk")
		m = send(t, m, press("E"))
		m = toOAuthLogin(t, m, tc.rename)

		m = pressSettled(t, m, "enter")

		if !m.showConnect || !strings.Contains(plain(m), "config changed on disk") {
			t.Errorf("rename %q: error not shown:\n%s", tc.rename, plain(m))
		}
		if !slices.Equal(si.log(), tc.want) {
			t.Errorf("rename %q: events = %v, want %v", tc.rename, si.log(), tc.want)
		}
	}
}

func TestOAuthEdit_SaveAnywayWithOAuthAsksToSignIn(t *testing.T) {
	si := &fakeSignIn{}
	f, m := oauthEdit(t, si)
	f.dialErr = errors.New("dial tcp: no route to host")
	m = send(t, m, press("E"))
	m = toOAuthLogin(t, m, "Bee")
	m = pressSettled(t, m, "enter")
	if !strings.Contains(plain(m), "no route to host") {
		t.Fatalf("the test should have failed:\n%s", plain(m))
	}

	m = pressSettled(t, m, "ctrl+s")

	if !m.showConnect || !strings.Contains(plain(m), "Sign in first: Sign in & save stores the sign-in.") {
		t.Errorf("message missing:\n%s", plain(m))
	}
	if len(f.updated) != 0 {
		t.Errorf("save anyway must do nothing: %v", f.updated)
	}
}

func TestOAuthEdit_KeepCurrentDialsUnderTheOldNameWithNilCreds(t *testing.T) {
	si := &fakeSignIn{}
	f, m := oauthEdit(t, si)
	m = send(t, m, press("E"))
	m = send(t, m, renameTo("Bee")...)
	m = toSubmit(t, m)

	m = pressSettled(t, m, "enter")

	if len(f.dialed) != 1 || f.dialed[0] != "Beta" || f.dialedCreds[0] != nil {
		t.Errorf("dialed %v creds nil %v", f.dialed, f.dialedCreds[0] == nil)
	}
	if len(f.updSignedIn) != 1 || f.updSignedIn[0] {
		t.Errorf("signedIn = %v, want false", f.updSignedIn)
	}
	if si.calls != 0 {
		t.Error("keep current must not sign in")
	}
}

func TestAccount_NeedsSignInShowsThePressEHint(t *testing.T) {
	for _, tc := range []struct {
		withEdit bool
		want     string
	}{
		{true, "Alpha needs you to sign in again: press E, choose OAuth 2.0 and Sign in & save"},
		{false, "Alpha needs you to sign in again"},
	} {
		f, cfg := newEditFixture("Alpha")
		connect := func(context.Context, config.Account) (Conn, error) {
			return Conn{}, fmt.Errorf("%w: %w", ErrNeedsSignIn, errors.New("sign-in expired"))
		}
		m := New(cfg, connect, nil)
		m.every = func(time.Duration) tea.Cmd { return nil }
		m = send(t, m, size(140, 40))
		o := f.onboarding()
		if !tc.withEdit {
			o.UpdateAccount = nil
		}
		m = m.WithOnboarding(o)
		m = settle(t, m, m.Init())

		if m.status != tc.want || !m.statusErr {
			t.Errorf("withEdit %v: status %q (err %v), want %q", tc.withEdit, m.status, m.statusErr, tc.want)
		}
	}
}
