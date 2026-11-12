package ui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/dkouvdis/dk-email/internal/config"
	"github.com/dkouvdis/dk-email/internal/mail"
	"github.com/dkouvdis/dk-email/internal/mail/mailtest"
)

// fakeOnboarding records what the connect popup asks main to do.
type fakeOnboarding struct {
	dialErr, addErr error
	box             *mailtest.Fake
	sender          mail.Sender
	dialed          []string // passwords Dial was given
	added           []config.Account
	addedPasswords  []string
	stored          map[string]string
}

func newOnboarding() *fakeOnboarding {
	box := mailtest.New()
	box.AddFolder("INBOX", mail.RoleInbox)
	box.AddMessage("INBOX", msg("Welcome", "Hello there", fixedNow, false, "h1"))
	return &fakeOnboarding{box: box, stored: map[string]string{}}
}

func (f *fakeOnboarding) onboarding() Onboarding {
	return Onboarding{
		Dial: func(_ context.Context, _ config.Account, password string) (Conn, error) {
			f.dialed = append(f.dialed, password)
			if f.dialErr != nil {
				return Conn{}, f.dialErr
			}
			return Conn{Mailbox: f.box, Sender: f.sender}, nil
		},
		AddAccount: func(acct config.Account, password string) error {
			if f.addErr != nil {
				return f.addErr
			}
			f.added = append(f.added, acct)
			f.addedPasswords = append(f.addedPasswords, password)
			return nil
		},
		StorePassword: func(account, password string) error {
			f.stored[account] = password
			return nil
		},
	}
}

func typeKeys(t *testing.T, m Model, text string) Model {
	t.Helper()
	for _, r := range text {
		m = send(t, m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return m
}

func firstRun(t *testing.T, o *fakeOnboarding, log *slog.Logger) Model {
	t.Helper()
	m := New(config.Default(), nil, log)
	m.every = func(time.Duration) tea.Cmd { return nil }
	m = send(t, m, size(140, 34))
	return m.WithOnboarding(o.onboarding())
}

// fillGmailKeychain fills the form: name, a Gmail address (which pre-fills
// the server), the keychain option and a password.
func fillGmailKeychain(t *testing.T, m Model, name, password string) Model {
	t.Helper()
	m = typeKeys(t, m, name)
	m = send(t, m, press("tab"))
	m = typeKeys(t, m, "me@gmail.com")
	for range 5 { // username, host, port, TLS, source
		m = send(t, m, press("tab"))
	}
	m = send(t, m, press("space"), press("tab"))
	return typeKeys(t, m, password)
}

func TestConnect_FirstRunOpensPopup(t *testing.T) {
	m := firstRun(t, newOnboarding(), nil)

	if !m.showConnect || !strings.Contains(plain(m), "Connect account") {
		t.Fatalf("the connect popup should open on first run:\n%s", plain(m))
	}
	if !strings.Contains(m.status, "press A to add one") {
		t.Errorf("status = %q", m.status)
	}
}

func TestConnect_SuccessSavesAndLoadsTheAccount(t *testing.T) {
	o := newOnboarding()
	var logs bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	m := firstRun(t, o, log)
	m = fillGmailKeychain(t, m, "Gmail", "s3cret-app-pass")

	next, cmd := m.Update(press("enter"))
	m = next.(Model)
	if !strings.Contains(plain(m), "Testing connection…") {
		t.Errorf("no progress while testing:\n%s", plain(m))
	}
	m = settle(t, m, cmd)

	if m.showConnect {
		t.Fatal("popup still open after a successful test")
	}
	if len(o.added) != 1 || o.added[0].Name != "Gmail" || o.added[0].IMAPHost != "imap.gmail.com" || o.added[0].PasswordCmd != "" {
		t.Fatalf("AddAccount got %+v", o.added)
	}
	if o.addedPasswords[0] != "s3cret-app-pass" || o.dialed[0] != "s3cret-app-pass" {
		t.Errorf("password not passed to Dial/AddAccount: %v / %v", o.dialed, o.addedPasswords)
	}
	v := plain(m)
	for _, want := range []string{"Gmail", "Inbox", "Hello there"} {
		if !strings.Contains(v, want) {
			t.Errorf("view missing %q after connecting:\n%s", want, v)
		}
	}
	if m.status != "Connected Gmail." {
		t.Errorf("status = %q", m.status)
	}
	if strings.Contains(logs.String(), "s3cret") {
		t.Error("the password reached the debug log")
	}
	if !strings.Contains(logs.String(), "connect test") {
		t.Errorf("connect test not logged:\n%s", logs.String())
	}
}

func TestConnect_NewAccountGetsItsSender(t *testing.T) {
	o := newOnboarding()
	o.sender = &mailtest.Sender{}
	m := firstRun(t, o, nil)
	m = fillGmailKeychain(t, m, "Gmail", "pw")
	next, cmd := m.Update(press("enter"))
	m = settle(t, next.(Model), cmd)

	if len(m.senders) != len(m.boxes) || m.senders[0] != o.sender {
		t.Fatalf("senders = %v, want the new account's sender", m.senders)
	}
	m.editorCmd = func(path string) *exec.Cmd { return exec.Command("true", path) }
	t.Setenv("TMPDIR", t.TempDir())
	next, cmd = m.Update(press("c"))
	m = next.(Model)
	if cmd == nil || m.draft == nil {
		t.Errorf("c on the new account: cmd %v, draft %v, status %q", cmd != nil, m.draft, m.status)
	}
	_ = m.Close()
}

func TestConnect_FailedTestSavesNothing(t *testing.T) {
	o := newOnboarding()
	o.dialErr = errors.New(`login to account "Gmail" failed: invalid credentials`)
	m := firstRun(t, o, nil)
	m = fillGmailKeychain(t, m, "Gmail", "wrong")

	m = pressSettled(t, m, "enter")

	if !m.showConnect || !strings.Contains(plain(m), "invalid credentials") {
		t.Errorf("error not shown in the popup:\n%s", plain(m))
	}
	if len(o.added) != 0 || len(m.accts) != 0 {
		t.Error("a failed test saved or added the account")
	}

	o.dialErr = nil // fix it and retry from the same form
	m = pressSettled(t, m, "enter")
	if m.showConnect || len(o.added) != 1 {
		t.Error("retrying after a failure didn't save")
	}
}

func TestConnect_SaveFailureClosesTheMailbox(t *testing.T) {
	o := newOnboarding()
	o.addErr = errors.New("permission denied")
	m := firstRun(t, o, nil)
	m = fillGmailKeychain(t, m, "Gmail", "pw")

	m = pressSettled(t, m, "enter")

	if !strings.Contains(plain(m), "connected, but saving failed: permission denied") {
		t.Errorf("save error not shown:\n%s", plain(m))
	}
	if _, err := o.box.Folders(context.Background()); !errors.Is(err, mail.ErrClosed) {
		t.Error("the tested mailbox wasn't closed")
	}
}

func TestConnect_CancelDropsAPendingTest(t *testing.T) {
	o := newOnboarding()
	m := firstRun(t, o, nil)
	m = fillGmailKeychain(t, m, "Gmail", "pw")
	next, cmd := m.Update(press("enter"))
	m = next.(Model)

	m = send(t, m, press("esc"))
	m = settle(t, m, cmd)

	if m.showConnect || len(m.accts) != 0 {
		t.Error("a test finishing after esc still added the account")
	}
	if _, err := o.box.Folders(context.Background()); !errors.Is(err, mail.ErrClosed) {
		t.Error("the mailbox from a cancelled test wasn't closed")
	}
}

func TestConnect_AAddsASecondAccount(t *testing.T) {
	o := newOnboarding()
	m := started(t, newFixture(), 140, 34).WithOnboarding(o.onboarding())
	if m.showConnect {
		t.Fatal("the popup shouldn't open on its own when accounts exist")
	}

	m = pressSettled(t, m, "A")
	if !m.showConnect {
		t.Fatal("A didn't open the popup")
	}
	m = typeKeys(t, m, "Personal")
	for range 8 { // to the Test & save button
		m = send(t, m, press("tab"))
	}
	m = pressSettled(t, m, "enter")
	if !strings.Contains(plain(m), `An account named "Personal" already exists`) {
		t.Errorf("duplicate name not caught:\n%s", plain(m))
	}

	m = pressSettled(t, m, "esc")
	m = pressSettled(t, m, "A")
	m = fillGmailKeychain(t, m, "Gmail", "pw")
	m = pressSettled(t, m, "enter")
	if len(m.accts) != 3 || m.side.Accounts()[2].Name != "Gmail" {
		t.Errorf("accounts = %d, sidebar %+v", len(m.accts), m.side.Accounts())
	}
	if m.open.account != 0 {
		t.Error("adding an account shouldn't switch away from the open folder")
	}
}

func TestConnect_MissingPasswordPrompt(t *testing.T) {
	f := newFixture()
	f.errs["Work"] = fmt.Errorf("%w: no password: set password_cmd or store one in the keychain", ErrNeedsPassword)
	o := newOnboarding()
	m := started(t, f, 140, 34).WithOnboarding(o.onboarding())

	if !strings.Contains(m.status, "Work needs a password: press A to enter it") {
		t.Errorf("status = %q", m.status)
	}
	m = pressSettled(t, m, "A")
	if v := plain(m); !strings.Contains(v, "Password for Work") || strings.Contains(v, "Connect account") {
		t.Fatalf("A should prompt for Work's password:\n%s", v)
	}
	m = typeKeys(t, m, "work-pass")
	m = pressSettled(t, m, "enter")

	if o.stored["Work"] != "work-pass" || len(o.added) != 0 {
		t.Errorf("stored %v, added %v; want only Work's password stored", o.stored, o.added)
	}
	if m.needsPassword[1] || m.showConnect {
		t.Error("Work still needs a password after it connected")
	}
	if f, ok := m.side.Folder(1, "INBOX"); !ok || f.Unread != 1 {
		t.Errorf("Work's folders not loaded: %+v", m.side.Accounts()[1])
	}

	m = pressSettled(t, m, "A")
	if !strings.Contains(plain(m), "Connect account") {
		t.Error("with no password missing, A should add a new account")
	}
}

func TestConnect_WithoutOnboarding(t *testing.T) {
	m := started(t, newFixture(), 140, 34)

	m = pressSettled(t, m, "A")

	if m.showConnect || m.status != "Adding accounts isn't available." {
		t.Errorf("popup %v, status %q", m.showConnect, m.status)
	}
}

func TestConnect_KeysGoToTheForm(t *testing.T) {
	m := firstRun(t, newOnboarding(), nil)

	next, cmd := m.Update(press("q"))
	if cmd != nil || !next.(Model).showConnect {
		t.Error("q should be typed into the form, not quit or close it")
	}
	if _, cmd = m.Update(press("ctrl+c")); cmd == nil {
		t.Error("ctrl+c should still quit")
	}
}

func TestConnect_PasteReachesTheForm(t *testing.T) {
	m := firstRun(t, newOnboarding(), nil)

	m = send(t, m, tea.PasteMsg{Content: "Pasted Name"})

	if !strings.Contains(plain(m), "Pasted Name") {
		t.Errorf("paste didn't reach the connect form:\n%s", plain(m))
	}
}
