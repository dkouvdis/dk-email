package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/dkouvdis/dk-email/internal/config"
	"github.com/dkouvdis/dk-email/internal/mail"
	"github.com/dkouvdis/dk-email/internal/mail/mailtest"
	"github.com/dkouvdis/dk-email/internal/ui/popup"
	"github.com/dkouvdis/dk-email/internal/ui/sidebar"
	"github.com/dkouvdis/dk-email/internal/ui/theme"
	"github.com/dkouvdis/dk-email/internal/ui/threadlist"
)

// editFixture is accounts A, B and C (keyed by email, so renaming one keeps
// its mailbox) and the Onboarding that edits and removes them.
type editFixture struct {
	boxes   map[string]*mailtest.Fake
	dialErr error
	dialed  []string // account names Dial was given
	updated []string // "index/oldName"
	edits   []config.Account
	updPw   []string
	// updSignedIn is the signedIn flag of each UpdateAccount call.
	updSignedIn []bool
	dialedCreds []mail.Credentials
	// signIn, when set, is the Onboarding's SignIn.
	signIn   func(ctx context.Context, acct config.Account, clientSecret string, useCode bool, progress func(popup.SignIn)) (OAuthSession, error)
	updErr   error
	removed  []string // "index/name/deletePassword"
	removeEr error
}

func newEditFixture(names ...string) (*editFixture, config.Config) {
	f := &editFixture{boxes: map[string]*mailtest.Fake{}}
	cfg := config.Default()
	for _, n := range names {
		email := strings.ToLower(n) + "@example.com"
		box := mailtest.New()
		box.AddFolder("INBOX", mail.RoleInbox)
		box.AddMessage("INBOX", msg("Sender of "+n, "Hello "+n, fixedNow, false, "id-"+n))
		f.boxes[email] = box
		cfg.Accounts = append(cfg.Accounts, config.Account{
			Name: n, Email: email, IMAPHost: "imap." + strings.ToLower(n) + ".com", IMAPPort: 993, TLS: config.TLSImplicit, Username: email,
		})
	}
	return f, cfg
}

func (f *editFixture) connector() Connector {
	return func(_ context.Context, acct config.Account) (Conn, error) {
		return Conn{Mailbox: f.boxes[acct.Email]}, nil
	}
}

func (f *editFixture) onboarding() Onboarding {
	return Onboarding{
		SignIn: f.signIn,
		Dial: func(_ context.Context, acct config.Account, creds mail.Credentials) (Conn, error) {
			f.dialed = append(f.dialed, acct.Name)
			f.dialedCreds = append(f.dialedCreds, creds)
			if f.dialErr != nil {
				return Conn{}, f.dialErr
			}
			return Conn{Mailbox: f.boxes[acct.Email]}, nil
		},
		AddAccount:    func(config.Account, string) error { return nil },
		StorePassword: func(string, string) error { return nil },
		UpdateAccount: func(index int, oldName string, edit func(config.Account) config.Account, password string, signedIn bool) error {
			if f.updErr != nil {
				return f.updErr
			}
			f.updated = append(f.updated, fmt.Sprintf("%d/%s", index, oldName))
			f.edits = append(f.edits, edit(config.Account{Name: oldName}))
			f.updPw = append(f.updPw, password)
			f.updSignedIn = append(f.updSignedIn, signedIn)
			return nil
		},
		RemoveAccount: func(index int, acct config.Account, deletePassword bool) error {
			if f.removeEr != nil {
				return f.removeEr
			}
			f.removed = append(f.removed, fmt.Sprintf("%d/%s/%t", index, acct.Name, deletePassword))
			return nil
		},
	}
}

// editModel is a started model with every account connected and the first
// Inbox open.
func editModel(t *testing.T, f *editFixture, cfg config.Config) Model {
	t.Helper()
	m := New(cfg, f.connector(), nil)
	m.every = func(time.Duration) tea.Cmd { return nil }
	m.list = threadlist.New(theme.Default(), func() time.Time { return fixedNow })
	m = send(t, m, size(140, 40))
	m = settle(t, m, m.Init())
	return m.WithOnboarding(f.onboarding())
}

// cursorOn moves the sidebar cursor to account i's Inbox.
func cursorOn(t *testing.T, m Model, i int) Model {
	t.Helper()
	m.focus = PaneSidebar
	for range 20 {
		if a, _ := m.side.Cursor(); a == i {
			return m
		}
		m = send(t, m, press("j"))
	}
	t.Fatalf("cursor never reached account %d", i)
	return m
}

func sidebarNames(m Model) []string {
	var names []string
	for _, a := range m.side.Accounts() {
		names = append(names, a.Name)
	}
	return names
}

func renameTo(name string) []tea.Msg {
	var msgs []tea.Msg
	for range 30 {
		msgs = append(msgs, tea.KeyPressMsg{Code: tea.KeyBackspace})
	}
	for _, r := range name {
		msgs = append(msgs, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return msgs
}

// toSubmit tabs from the first field to Submit.
func toSubmit(t *testing.T, m Model) Model {
	t.Helper()
	for range 11 {
		m = send(t, m, press("tab"))
	}
	return m
}

func TestEdit_EOpensPrefilledPopupOnTheCursorAccount(t *testing.T) {
	f, cfg := newEditFixture("Alpha", "Beta", "Gamma")
	m := cursorOn(t, editModel(t, f, cfg), 1)

	m = send(t, m, press("E"))

	if !m.showConnect || m.editFor != 1 {
		t.Fatalf("showConnect %v editFor %d", m.showConnect, m.editFor)
	}
	v := plain(m)
	for _, want := range []string{"Edit account", "Beta", "beta@example.com", "imap.beta.com"} {
		if !strings.Contains(v, want) {
			t.Errorf("popup lacks %q:\n%s", want, v)
		}
	}

	m = send(t, m, press("esc"))
	if m.showConnect || m.editFor != -1 {
		t.Error("esc should close the edit popup")
	}
}

func TestEdit_UnavailableWithoutUpdateAccount(t *testing.T) {
	f, cfg := newEditFixture("Alpha")
	m := editModel(t, f, cfg)
	o := f.onboarding()
	o.UpdateAccount = nil
	m = m.WithOnboarding(o)

	m = send(t, m, press("E"))

	if m.showConnect || m.status != "Editing accounts isn't available." {
		t.Errorf("showConnect %v status %q", m.showConnect, m.status)
	}
}

func TestEdit_SuccessRenamesAndReplacesTheAccount(t *testing.T) {
	f, cfg := newEditFixture("Alpha", "Beta", "Gamma")
	m := cursorOn(t, editModel(t, f, cfg), 1)
	m.needsPassword[1] = true
	m = send(t, m, press("E"))
	m = send(t, m, renameTo("Bee")...)
	m = toSubmit(t, m)

	m = pressSettled(t, m, "enter")

	if m.showConnect || m.editFor != -1 {
		t.Fatalf("popup still open:\n%s", plain(m))
	}
	if len(f.updated) != 1 || f.updated[0] != "1/Beta" || f.edits[0].Name != "Bee" {
		t.Errorf("UpdateAccount calls %v edits %+v", f.updated, f.edits)
	}
	if len(f.dialed) != 1 {
		t.Errorf("dialed %v", f.dialed)
	}
	if m.accts[1].Name != "Bee" || m.accts[1].Email != "beta@example.com" || m.accts[1].IMAPPort != 993 {
		t.Errorf("account = %+v", m.accts[1])
	}
	if m.boxes[1] == nil || m.needsPassword[1] {
		t.Errorf("box %v needsPassword %v", m.boxes[1], m.needsPassword[1])
	}
	if names := sidebarNames(m); strings.Join(names, ",") != "Alpha,Bee,Gamma" {
		t.Errorf("sidebar accounts = %v", names)
	}
	if !strings.Contains(m.status, "Saved Bee.") {
		t.Errorf("status = %q", m.status)
	}
}

func TestEdit_KeepCurrentKeychainRenameDialsUnderTheOldName(t *testing.T) {
	f, cfg := newEditFixture("Alpha", "Beta")
	m := cursorOn(t, editModel(t, f, cfg), 1)
	m = send(t, m, press("E"))
	m = send(t, m, renameTo("Bee")...)
	m = toSubmit(t, m)

	m = pressSettled(t, m, "enter")

	if len(f.dialed) != 1 || f.dialed[0] != "Beta" {
		t.Errorf("dialed %v, want the old name (the keychain entry moves on save)", f.dialed)
	}
	if m.accts[1].Name != "Bee" {
		t.Errorf("account = %+v", m.accts[1])
	}
}

func TestEdit_FailedTestOffersSaveAnywayWhichSavesWithoutDialing(t *testing.T) {
	f, cfg := newEditFixture("Alpha", "Beta")
	f.dialErr = errors.New("dial tcp: no route to host")
	m := cursorOn(t, editModel(t, f, cfg), 1)
	m = send(t, m, press("E"))
	m = send(t, m, renameTo("Bee")...)
	m = toSubmit(t, m)

	m = pressSettled(t, m, "enter")

	if !m.showConnect || len(f.updated) != 0 {
		t.Fatalf("popup open %v, saved %v; a failed test must not save", m.showConnect, f.updated)
	}
	v := plain(m)
	if !strings.Contains(v, "no route to host") || !strings.Contains(v, "Save anyway") {
		t.Fatalf("error or save anyway hint missing:\n%s", v)
	}
	dials := len(f.dialed)

	m = pressSettled(t, m, "ctrl+s")

	if m.showConnect || len(f.updated) != 1 || f.updated[0] != "1/Beta" {
		t.Fatalf("popup open %v, saved %v", m.showConnect, f.updated)
	}
	if len(f.dialed) != dials {
		t.Error("save anyway dialed")
	}
	if m.accts[1].Name != "Bee" {
		t.Errorf("account = %+v", m.accts[1])
	}
	// Reconnected through the normal connector, so the account works again.
	if m.boxes[1] == nil || !strings.Contains(plain(m), "Bee") {
		t.Errorf("box %v\n%s", m.boxes[1], plain(m))
	}
}

func TestEdit_SaveFailureShowsErrorWithoutSaveAnyway(t *testing.T) {
	f, cfg := newEditFixture("Alpha", "Beta")
	f.updErr = errors.New("config changed on disk")
	m := cursorOn(t, editModel(t, f, cfg), 1)
	m = send(t, m, press("E"))
	m = toSubmit(t, m)

	m = pressSettled(t, m, "enter")

	v := plain(m)
	if !m.showConnect || !strings.Contains(v, "config changed on disk") || strings.Contains(v, "Save anyway") {
		t.Errorf("popup open %v:\n%s", m.showConnect, v)
	}
	if m.accts[1].Name != "Beta" {
		t.Errorf("account replaced although the save failed: %+v", m.accts[1])
	}
}

func TestEdit_ConnectErrorSuggestsE(t *testing.T) {
	f, cfg := newEditFixture("Alpha")
	f.boxes = map[string]*mailtest.Fake{}
	m := New(cfg, func(context.Context, config.Account) (Conn, error) {
		return Conn{}, errors.New("login failed")
	}, nil)
	m.every = func(time.Duration) tea.Cmd { return nil }
	m = send(t, m, size(140, 40))
	m = m.WithOnboarding(f.onboarding())
	m = settle(t, m, m.Init())

	if !strings.HasSuffix(m.status, "login failed. Press E to edit it.") {
		t.Errorf("status = %q", m.status)
	}
	// A failed account can be picked in the sidebar and edited.
	m = send(t, m, press("E"))
	if !m.showConnect || m.editFor != 0 {
		t.Errorf("E on the failed account: showConnect %v editFor %d", m.showConnect, m.editFor)
	}
}

func TestEdit_DeleteShiftsLaterAccounts(t *testing.T) {
	f, cfg := newEditFixture("Alpha", "Beta", "Gamma")
	m := cursorOn(t, editModel(t, f, cfg), 1)
	m.needsPassword[2] = true
	gammaID := m.ids[2]
	m = send(t, m, press("E"), press("shift+tab"), press("enter"))
	if v := plain(m); !strings.Contains(v, "Delete Beta?") {
		t.Fatalf("no confirmation:\n%s", v)
	}

	m = pressSettled(t, m, "y")

	if len(f.removed) != 1 || f.removed[0] != "1/Beta/true" {
		t.Fatalf("RemoveAccount calls %v", f.removed)
	}
	if m.showConnect || len(m.accts) != 2 || m.accts[0].Name != "Alpha" || m.accts[1].Name != "Gamma" {
		t.Fatalf("accounts = %+v", m.accts)
	}
	if len(m.boxes) != 2 || len(m.senders) != 2 || len(m.ids) != 2 || m.ids[1] != gammaID {
		t.Errorf("per-account slices not shifted: %d %d %v", len(m.boxes), len(m.senders), m.ids)
	}
	if m.boxes[1] != mail.Mailbox(f.boxes["gamma@example.com"]) {
		t.Error("Gamma's mailbox is not at its new index")
	}
	if len(m.needsPassword) != 1 || !m.needsPassword[1] {
		t.Errorf("needsPassword = %v, want Gamma's flag at 1", m.needsPassword)
	}
	if names := sidebarNames(m); strings.Join(names, ",") != "Alpha,Gamma" {
		t.Errorf("sidebar accounts = %v", names)
	}
	if m.status != "Deleted Beta." {
		t.Errorf("status = %q", m.status)
	}
	if a, _ := m.side.Cursor(); a != 1 {
		t.Errorf("cursor on account %d, want the next account (now 1)", a)
	}

	// Gamma still works at its new index.
	m = cursorOn(t, m, 1)
	m = pressSettled(t, m, "enter")
	if m.open.account != 1 || !strings.Contains(plain(m), "Hello Gamma") {
		t.Errorf("open %+v\n%s", m.open, plain(m))
	}
}

func TestEdit_DeleteKeepsPasswordWithK(t *testing.T) {
	f, cfg := newEditFixture("Alpha", "Beta")
	m := cursorOn(t, editModel(t, f, cfg), 1)
	m = send(t, m, press("E"), press("shift+tab"), press("enter"))

	m = pressSettled(t, m, "k")

	if len(f.removed) != 1 || f.removed[0] != "1/Beta/false" {
		t.Errorf("RemoveAccount calls %v", f.removed)
	}
}

func TestEdit_DeletingTheLastAccountOpensTheConnectPopup(t *testing.T) {
	f, cfg := newEditFixture("Alpha")
	m := editModel(t, f, cfg)
	m = send(t, m, press("E"), press("shift+tab"), press("enter"))

	m = pressSettled(t, m, "y")

	if len(m.accts) != 0 || m.open.account != -1 {
		t.Fatalf("accounts %v open %+v", m.accts, m.open)
	}
	if !m.showConnect || m.editFor != -1 || !strings.Contains(plain(m), "Connect account") {
		t.Errorf("connect popup not open like first run:\n%s", plain(m))
	}
}

func TestEdit_RemoveAccountAtClearsOrShiftsTheOpenView(t *testing.T) {
	f, cfg := newEditFixture("Alpha", "Beta", "Gamma")
	m := editModel(t, f, cfg)
	if m.open.account != 0 {
		t.Fatalf("open = %+v", m.open)
	}

	// Removing the open account clears the view.
	cleared, _ := m.removeAccountAt(0)
	if cleared.open.account != -1 || len(cleared.accts) != 2 || cleared.accts[0].Name != "Beta" {
		t.Errorf("open %+v accts %+v", cleared.open, cleared.accts)
	}

	// Removing an earlier account moves the open one down and, while its
	// first page is in flight, loads it again.
	next, cmd := m.Update(sidebar.SelectedMsg{Account: 2, Folder: "INBOX"})
	m = next.(Model)
	if !m.open.loading {
		t.Fatal("first page should be loading")
	}
	shifted, cmd := m.removeAccountAt(1)
	if shifted.open.account != 1 || shifted.accts[1].Name != "Gamma" || cmd == nil {
		t.Fatalf("open %+v accts %+v cmd %v", shifted.open, shifted.accts, cmd)
	}
	shifted = settle(t, shifted, cmd)
	if !strings.Contains(plain(shifted), "Hello Gamma") {
		t.Errorf("Gamma's threads not loaded:\n%s", plain(shifted))
	}
}

func TestEdit_LateResultsFindTheirAccountByID(t *testing.T) {
	f, cfg := newEditFixture("Alpha", "Beta", "Gamma")
	m := editModel(t, f, cfg)
	betaID, gammaID := m.ids[1], m.ids[2]
	m, _ = m.removeAccountAt(1)

	// A connect result for the deleted account is dropped and its mailbox closed.
	late := mailtest.New()
	late.AddFolder("INBOX", mail.RoleInbox)
	m = send(t, m, accountMsg{index: 1, id: betaID, conn: Conn{Mailbox: late}})
	if m.boxes[1] == mail.Mailbox(late) {
		t.Error("a result for a deleted account landed on the account that moved up")
	}

	// A result for Gamma, sent with its old index, lands on Gamma's new one.
	fresh := mailtest.New()
	fresh.AddFolder("INBOX", mail.RoleInbox)
	m = send(t, m, accountMsg{index: 2, id: gammaID, conn: Conn{Mailbox: fresh}})
	if m.boxes[1] != mail.Mailbox(fresh) {
		t.Error("Gamma's late result did not find its account")
	}

	// A refresh result for the deleted account only counts as done.
	m.refresh = refreshState{pending: 1}
	m = send(t, m, refreshMsg{account: 1, id: betaID})
	if m.refresh.pending != 0 {
		t.Errorf("pending = %d", m.refresh.pending)
	}
}
