package ui

import (
	"context"
	"errors"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/dkouvdis/dk-email/internal/config"
	"github.com/dkouvdis/dk-email/internal/mail"
	"github.com/dkouvdis/dk-email/internal/ui/popup"
)

// ErrNeedsPassword marks a connect error for an account with no password
// anywhere, so A can prompt for one. main wraps secret.ErrNoPassword in it.
var ErrNeedsPassword = errors.New("needs password")

// Onboarding is how the connect popup tests and saves accounts. main
// provides it so the UI never touches passwords, IMAP or the config file.
type Onboarding struct {
	// Dial logs in. A non-empty password is used as is; otherwise the
	// account's password_cmd (or keychain entry) supplies it.
	Dial func(ctx context.Context, acct config.Account, password string) (mail.Mailbox, error)
	// AddAccount saves a new account: password, if any, to the keychain,
	// then the account to config.toml.
	AddAccount func(acct config.Account, password string) error
	// StorePassword saves a saved account's password to the keychain.
	StorePassword func(account, password string) error
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
	mb      mail.Mailbox
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
	m.connectFor = -1
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
		m.showConnect = false
		m.connectGen++ // a test still running is ignored when it finishes
		m.log.Debug("popup closed", "popup", "connect")
	case popup.ConnectSubmitted:
		m.connectPop.SetBusy(true)
		m.connectGen++
		return m, testAndSave(m.onboard, m.connectGen, m.connectFor, m.connectPop.Request())
	}
	return m, nil
}

// testAndSave logs in and lists folders, and only if both work saves the
// account (or its password). On any failure nothing is saved.
func testAndSave(o Onboarding, gen, index int, req popup.ConnectRequest) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), connectTimeout)
		defer cancel()
		start := time.Now()
		res := connectedMsg{gen: gen, index: index, acct: req.Account}
		fail := func(err error) tea.Msg {
			if res.mb != nil {
				_ = res.mb.Close()
				res.mb = nil
			}
			res.err, res.took = err, time.Since(start)
			return res
		}

		mb, err := o.Dial(ctx, req.Account, req.Password)
		if err == nil && mb == nil {
			err = errors.New("connector returned no mailbox")
		}
		if err != nil {
			return fail(err)
		}
		res.mb = mb
		if res.folders, err = mb.Folders(ctx); err != nil {
			return fail(fmt.Errorf("list folders: %w", err))
		}
		if index >= 0 {
			err = o.StorePassword(req.Account.Name, req.Password)
		} else {
			err = o.AddAccount(req.Account, req.Password)
		}
		if err != nil {
			return fail(fmt.Errorf("connected, but saving failed: %w", err))
		}
		res.took = time.Since(start)
		return res
	}
}

func (m Model) handleConnected(msg connectedMsg) (Model, tea.Cmd) {
	if msg.gen != m.connectGen || !m.showConnect {
		if msg.mb != nil {
			_ = msg.mb.Close()
		}
		return m, nil
	}
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
		m.side, index = m.side.AddAccount(msg.acct.Name, nil)
	} else {
		delete(m.needsPassword, index)
		if old := m.boxes[index]; old != nil {
			_ = old.Close()
		}
	}
	m, cmd := m.handleAccount(accountMsg{index: index, mb: msg.mb, folders: msg.folders, took: msg.took})
	done := fmt.Sprintf("Connected %s.", m.accts[index].Name)
	if m.open.account == index && m.open.loading {
		m.open.notice = done // shown once its Inbox has loaded
	} else {
		m.setStatus(done)
	}
	return m, cmd
}
