package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/dkouvdis/dk-email/internal/config"
	"github.com/dkouvdis/dk-email/internal/mail"
	"github.com/dkouvdis/dk-email/internal/ui/popup"
)

// ErrNeedsPassword marks a connect error for an account with no password
// anywhere, so A can prompt for one. main wraps secret.ErrNoPassword in it.
var ErrNeedsPassword = errors.New("needs password")

// ErrNeedsSignIn marks a connect error for an OAuth account whose stored
// sign-in is missing, expired or revoked, so E can offer a new sign-in. main
// wraps secret.ErrNoToken and oauth.ErrInvalidGrant in it.
var ErrNeedsSignIn = errors.New("needs sign-in")

// OAuthSession is a finished OAuth sign-in that isn't saved yet.
type OAuthSession struct {
	// Creds logs in with the new token, refreshing it when needed.
	Creds mail.Credentials
	// Save stores the sign-in (and a typed client secret) in the keychain
	// under the account name; refreshed tokens are saved there from then on.
	Save func(name string) error
	// Forget removes what Save stored, best effort.
	Forget func(name string)
}

// Onboarding is how the connect popup tests and saves accounts. main
// provides it so the UI never touches passwords, IMAP or the config file.
type Onboarding struct {
	// Dial logs in. Non-nil creds are used as is (a typed password, or a
	// fresh OAuth sign-in); nil means the account's configured credentials:
	// password_cmd, the keychain, or the stored OAuth sign-in.
	Dial func(ctx context.Context, acct config.Account, creds mail.Credentials) (Conn, error)
	// VerifySMTP tests a new account's SMTP login before it's saved. It is
	// given the same creds as Dial: nil means the account's configured
	// credentials. Nil skips the test.
	VerifySMTP func(ctx context.Context, acct config.Account, creds mail.Credentials) error
	// SignIn runs an OAuth sign-in for acct: in the browser, or with a device
	// code when useCode is set. clientSecret is the one typed in the form, or
	// "". progress reports what the popup should show; it may be called from
	// another goroutine. Nil disables OAuth in the popup.
	SignIn func(ctx context.Context, acct config.Account, clientSecret string, useCode bool, progress func(popup.SignIn)) (OAuthSession, error)
	// AddAccount saves a new account: password, if any, to the keychain,
	// then the account to config.toml.
	AddAccount func(acct config.Account, password string) error
	// StorePassword saves a saved account's password to the keychain.
	StorePassword func(account, password string) error
	// UpdateAccount saves an edited account in place. edit maps the account
	// as written in config.toml to the new one. A non-empty password goes to
	// the keychain under the new name first; a keychain password that isn't
	// replaced follows a rename. signedIn is true when a fresh OAuth sign-in
	// was just saved under the edited name. Nil disables editing.
	UpdateAccount func(index int, oldName string, edit func(config.Account) config.Account, password string, signedIn bool) error
	// RemoveAccount deletes an account from config.toml and, with
	// deletePassword, its keychain password. Nil disables deleting.
	RemoveAccount func(index int, acct config.Account, deletePassword bool) error
}

func (o Onboarding) ready() bool {
	return o.Dial != nil && o.AddAccount != nil && o.StorePassword != nil
}

// connectedMsg reports a connect-popup test. index is the account a
// password was entered for, or -1 for a new account. gen drops results
// from a popup that was closed meanwhile.
type connectedMsg struct {
	gen     int
	index   int
	acct    config.Account
	conn    Conn
	folders []mail.Folder
	err     error
	took    time.Duration
}

// WithOnboarding enables the connect popup. With no accounts configured it
// opens at once (first run).
func (m Model) WithOnboarding(o Onboarding) Model {
	m.onboard = o
	if len(m.accts) == 0 && o.ready() {
		m.status = "No accounts yet: press A to add one."
		m, _ = m.openConnect()
	}
	return m
}

// openConnect prompts for the first missing password, or else opens the
// form for a new account.
func (m Model) openConnect() (Model, tea.Cmd) {
	if !m.onboard.ready() {
		m.setStatus("Adding accounts isn't available.")
		return m, nil
	}
	m.connectFor, m.editFor = -1, -1
	for i := range m.accts {
		if m.needsPassword[i] {
			m.connectFor = i
			break
		}
	}
	if m.connectFor >= 0 {
		m.connectPop = popup.NewPasswordPrompt(m.theme, m.accts[m.connectFor])
	} else {
		names := make([]string, len(m.accts))
		for i, a := range m.accts {
			names[i] = a.Name
		}
		m.connectPop = popup.NewConnect(m.theme, names)
	}
	m.connectPop.SetSize(m.panes.Popup.W, m.panes.Popup.H)
	m.showConnect = true
	m.connectGen++
	m.log.Debug("popup opened", "popup", "connect", "password only", m.connectPop.PasswordOnly())
	return m, nil
}

func (m Model) handleConnectKey(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	var r popup.ConnectResult
	m.connectPop, r = m.connectPop.Update(msg)
	switch r {
	case popup.ConnectCancelled:
		m.showConnect, m.editFor = false, -1
		m.connectGen++ // a test still running is ignored when it finishes
		m.endTest()    // and a sign-in still waiting stops, freeing its port
		m.log.Debug("popup closed", "popup", "connect")
	case popup.ConnectSubmitted, popup.ConnectSaveAnyway, popup.ConnectDelete:
		if m.editFor >= 0 {
			return m.handleEditResult(r)
		}
		return m.startConnectTest(false)
	case popup.ConnectUseCode:
		// Stop the browser sign-in and start over with a device code.
		m.endTest()
		m.connectGen++
		if m.editFor >= 0 {
			return m.startEditTest(true)
		}
		return m.startConnectTest(true)
	}
	return m, nil
}

// beginTest cancels any earlier connect or edit test and returns the context
// for the next one; endTest cancels and forgets it.
func (m *Model) beginTest() context.Context {
	m.endTest()
	ctx, cancel := context.WithCancel(context.Background())
	m.connectCancel = cancel
	return ctx
}

func (m *Model) endTest() {
	if m.connectCancel != nil {
		m.connectCancel()
		m.connectCancel = nil
	}
}

// startConnectTest tests and saves the new account (or the password) in the
// popup.
func (m Model) startConnectTest(useCode bool) (Model, tea.Cmd) {
	if m.connectFor < 0 && m.connectPop.OAuth() && m.onboard.SignIn == nil {
		m.connectPop.SetError("OAuth sign-in isn't available.")
		return m, nil
	}
	m.connectPop.SetBusy(true)
	m.connectGen++
	ctx := m.beginTest()
	return m, testAndSave(ctx, m.onboard, m.connectGen, m.connectFor, m.connectPop.Request(), useCode)
}

// signInProgressMsg reports what a running OAuth sign-in wants the popup to
// show. next reads the following message of the same test.
type signInProgressMsg struct {
	gen    int
	signIn popup.SignIn
	next   tea.Cmd
}

func (m Model) handleSignInProgress(msg signInProgressMsg) (Model, tea.Cmd) {
	if msg.gen == m.connectGen && m.showConnect {
		m.connectPop.SetSignIn(msg.signIn)
	}
	return m, msg.next // keep reading, so the test's goroutine can finish
}

// withProgress runs work, which reports sign-in progress, in a goroutine. The
// returned cmd yields each progress message and finally work's result, one
// per call; every progress message carries the cmd for the next one.
func withProgress(gen int, work func(progress func(popup.SignIn)) tea.Msg) tea.Cmd {
	msgs := make(chan tea.Msg, 16)
	var next tea.Cmd = func() tea.Msg { return <-msgs }
	return func() tea.Msg {
		go func() {
			msgs <- work(func(s popup.SignIn) {
				msgs <- signInProgressMsg{gen: gen, signIn: s, next: next}
			})
		}()
		return <-msgs
	}
}

// testAndSave logs in and lists folders, and only if both work saves the
// account (or its password). A new account with an SMTP host must also pass
// VerifySMTP. On any failure nothing is saved. A new OAuth account signs in
// first, and its sign-in is stored just before the account is added.
func testAndSave(parent context.Context, o Onboarding, gen, index int, req popup.ConnectRequest, useCode bool) tea.Cmd {
	if index < 0 && o.SignIn != nil && req.Account.OAuth() {
		return withProgress(gen, func(progress func(popup.SignIn)) tea.Msg {
			return connectTest(parent, o, gen, index, req, true, useCode, progress)
		})
	}
	return func() tea.Msg {
		return connectTest(parent, o, gen, index, req, false, false, nil)
	}
}

func connectTest(parent context.Context, o Onboarding, gen, index int, req popup.ConnectRequest, oauth, useCode bool, progress func(popup.SignIn)) tea.Msg {
	start := time.Now()
	res := connectedMsg{gen: gen, index: index, acct: req.Account}
	fail := func(err error) tea.Msg {
		closeConn(res.conn)
		res.conn = Conn{}
		res.err, res.took = err, time.Since(start)
		return res
	}

	var creds mail.Credentials
	var sess OAuthSession
	if oauth {
		signCtx, cancel := context.WithTimeout(parent, signInTimeout)
		var err error
		sess, err = o.SignIn(signCtx, req.Account.WithDefaults(), req.ClientSecret, useCode, progress)
		cancel()
		if err != nil {
			return fail(fmt.Errorf("sign-in failed: %w", err))
		}
		creds = sess.Creds
	} else if req.Password != "" {
		creds = mail.StaticPassword(req.Password)
	}

	ctx, cancel := context.WithTimeout(parent, connectTimeout)
	defer cancel()
	conn, err := o.Dial(ctx, req.Account, creds)
	if err == nil && conn.Mailbox == nil {
		err = errors.New("connector returned no mailbox")
	}
	if err != nil {
		return fail(err)
	}
	res.conn = conn
	if index < 0 && o.VerifySMTP != nil && strings.TrimSpace(req.Account.SMTPHost) != "" {
		if err := o.VerifySMTP(ctx, req.Account, creds); err != nil {
			return fail(fmt.Errorf("IMAP login worked, but SMTP failed: %w", err))
		}
	}
	if res.folders, err = conn.Mailbox.Folders(ctx); err != nil {
		return fail(fmt.Errorf("list folders: %w", err))
	}
	switch {
	case index >= 0:
		err = o.StorePassword(req.Account.Name, req.Password)
	case oauth:
		// The sign-in goes to the keychain before the config, so a saved
		// account always has one; it is taken back if the config fails.
		if err = sess.Save(req.Account.Name); err == nil {
			if err = o.AddAccount(req.Account, ""); err != nil {
				sess.Forget(req.Account.Name)
			}
		}
	default:
		err = o.AddAccount(req.Account, req.Password)
	}
	if err != nil {
		return fail(fmt.Errorf("connected, but saving failed: %w", err))
	}
	res.took = time.Since(start)
	return res
}

func (m Model) handleConnected(msg connectedMsg) (Model, tea.Cmd) {
	if msg.gen != m.connectGen || !m.showConnect {
		closeConn(msg.conn)
		return m, nil
	}
	m.endTest()
	m.log.Debug("connect test", "ok", msg.err == nil, "new", msg.index < 0, "took", msg.took.Round(time.Millisecond))
	if msg.err != nil {
		m.connectPop.SetBusy(false)
		m.connectPop.SetError(msg.err.Error())
		return m, nil
	}
	m.showConnect = false
	m.statusErr = false

	index := msg.index
	if index < 0 {
		m.accts = append(m.accts, msg.acct.WithDefaults())
		m.boxes = append(m.boxes, nil)
		m.senders = append(m.senders, nil)
		m.ids = append(m.ids, m.newID())
		m.side, index = m.side.AddAccount(msg.acct.Name, nil)
	} else {
		delete(m.needsPassword, index)
		m.stopWatch(m.ids[index])
		if old := m.boxes[index]; old != nil {
			_ = old.Close()
		}
	}
	m, cmd := m.handleAccount(accountMsg{index: index, id: m.ids[index], conn: msg.conn, folders: msg.folders, took: msg.took})
	done := fmt.Sprintf("Connected %s.", m.accts[index].Name)
	if m.open.account == index && m.open.loading {
		m.open.notice = done // shown once its Inbox has loaded
	} else {
		m.setStatus(done)
	}
	return m, cmd
}
