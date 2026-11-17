package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dkouvdis/dk-email/internal/config"
	"github.com/dkouvdis/dk-email/internal/mail"
	"github.com/dkouvdis/dk-email/internal/mail/mailtest"
	"github.com/dkouvdis/dk-email/internal/oauth"
	"github.com/dkouvdis/dk-email/internal/secret"
	"github.com/dkouvdis/dk-email/internal/ui"
)

type stubResolver struct {
	password string
	err      error
}

func (s stubResolver) Resolve(context.Context, config.Account) (string, error) {
	return s.password, s.err
}

func (s stubResolver) ResolveClientSecret(context.Context, config.Account) (string, error) {
	return "", s.err
}

func TestNewConnector_PasswordErrorSkipsLogin(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	want := errors.New("op read failed")

	conn, err := newConnector(stubResolver{err: want}, log)(context.Background(), config.Account{Name: "Personal"})

	if !errors.Is(err, want) || conn != (ui.Conn{}) {
		t.Fatalf("connect = %v, %v; want a zero Conn, %v", conn, err, want)
	}
	if !strings.Contains(buf.String(), "credentials resolved") || strings.Contains(buf.String(), "imap login") {
		t.Errorf("log = %q, want a credentials step and no login step", buf.String())
	}
}

func TestNewConnector_LoginErrorReturnsNilMailboxAndNeverLogsPassword(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().(*net.TCPAddr)
	ln.Close() // nothing listens, so the dial is refused

	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	const password = "hunter2-secret"
	acct := config.Account{Name: "Personal", Username: "me", IMAPHost: "127.0.0.1", IMAPPort: addr.Port, TLS: config.TLSImplicit}

	conn, err := newConnector(stubResolver{password: password}, log)(context.Background(), acct)

	if err == nil || conn != (ui.Conn{}) {
		t.Fatalf("connect = %#v, %v; want a zero Conn and an error", conn, err)
	}
	if !strings.Contains(buf.String(), "imap login") || !strings.Contains(buf.String(), "ok=false") {
		t.Errorf("log = %q, want a failed login step", buf.String())
	}
	if strings.Contains(buf.String(), password) || strings.Contains(err.Error(), password) {
		t.Error("password leaked into the log or error")
	}
}

func TestNewConnector_MissingPasswordIsMarkedForTheUI(t *testing.T) {
	_, err := newConnector(stubResolver{err: secret.ErrNoPassword}, slog.New(slog.DiscardHandler))(context.Background(), config.Account{Name: "Work"})

	if !errors.Is(err, ui.ErrNeedsPassword) || !errors.Is(err, secret.ErrNoPassword) {
		t.Errorf("error = %v, want both ui.ErrNeedsPassword and secret.ErrNoPassword", err)
	}
}

func TestOnboarding_AddAccountStoresPasswordThenAppends(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	stored := map[string]string{}
	store := func(account, password string) error { stored[account] = password; return nil }
	o := newOnboarding(path, stubResolver{}, store, slog.New(slog.DiscardHandler))
	acct := config.Account{Name: "Gmail", Email: "me@gmail.com", IMAPHost: "imap.gmail.com", IMAPPort: 993, TLS: config.TLSImplicit}

	if err := o.AddAccount(acct, "app-pass-123"); err != nil {
		t.Fatalf("AddAccount() error = %v", err)
	}

	if stored["Gmail"] != "app-pass-123" {
		t.Errorf("keychain = %v", stored)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), `name = "Gmail"`) || strings.Contains(string(data), "app-pass-123") {
		t.Errorf("config.toml = %q; want the account and never the password", data)
	}
	if err := o.StorePassword("Work", "x"); err != nil || stored["Work"] != "x" {
		t.Errorf("StorePassword: %v, %v", err, stored)
	}
}

func TestOnboarding_OnePasswordAccountSkipsTheKeychain(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	store := func(string, string) error { t.Error("keychain used for a 1Password account"); return nil }
	o := newOnboarding(path, stubResolver{}, store, slog.New(slog.DiscardHandler))

	err := o.AddAccount(config.Account{Name: "Work", Email: "me@example.com", IMAPHost: "h", PasswordCmd: "op read op://V/I/p"}, "")

	if err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); !strings.Contains(string(data), `password_cmd = "op read op://V/I/p"`) {
		t.Errorf("config.toml = %q", data)
	}
}

func TestOnboarding_KeychainErrorSavesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	o := newOnboarding(path, stubResolver{}, func(string, string) error { return errors.New("keychain locked") }, slog.New(slog.DiscardHandler))

	err := o.AddAccount(config.Account{Name: "Gmail", Email: "me@gmail.com", IMAPHost: "h"}, "pw")

	if err == nil || !strings.Contains(err.Error(), "keychain locked") {
		t.Errorf("error = %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("config.toml written although the password couldn't be stored")
	}
}

func TestOnboarding_DialResolvesWhenNoPasswordGiven(t *testing.T) {
	want := errors.New("op read failed")
	o := newOnboarding("", stubResolver{err: want}, nil, slog.New(slog.DiscardHandler))

	if _, err := o.Dial(context.Background(), config.Account{Name: "Work", PasswordCmd: "op read x"}, nil); !errors.Is(err, want) {
		t.Errorf("Dial() error = %v, want the resolver's error", err)
	}

	// With a typed password the resolver isn't used; the dial itself fails here.
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	conn, err := o.Dial(context.Background(), config.Account{Name: "Work", Email: "me@x", IMAPHost: "127.0.0.1", IMAPPort: port}, mail.StaticPassword("typed"))
	if err == nil || errors.Is(err, want) || conn != (ui.Conn{}) {
		t.Errorf("Dial() = %v, %v; want a connection error and a zero Conn", conn, err)
	}
}

// countingResolver counts how often the password is resolved.
type countingResolver struct {
	password string
	calls    int
}

func (c *countingResolver) Resolve(context.Context, config.Account) (string, error) {
	c.calls++
	return c.password, nil
}

func (c *countingResolver) ResolveClientSecret(context.Context, config.Account) (string, error) {
	return "", nil
}

// fakeIMAP is an imapConn that never touches the network.
type fakeIMAP struct{ *mailtest.Fake }

func (fakeIMAP) Raw(context.Context, string, mail.UID) ([]byte, error) { return nil, nil }
func (fakeIMAP) Append(context.Context, string, []byte, ...mail.Flag) error {
	return nil
}
func (fakeIMAP) SentFolder(context.Context) (string, bool, error)   { return "", false, nil }
func (fakeIMAP) DraftsFolder(context.Context) (string, bool, error) { return "", false, nil }
func (fakeIMAP) Delete(context.Context, string, []mail.UID) error   { return nil }
func (fakeIMAP) SetFlag(context.Context, string, []mail.UID, mail.Flag, bool) error {
	return nil
}

// stubDial replaces dialIMAP and records the logins its credentials yield.
func stubDial(t *testing.T) *[]mail.Login {
	t.Helper()
	var logins []mail.Login
	old := dialIMAP
	dialIMAP = func(ctx context.Context, _ config.Account, creds mail.Credentials) (imapConn, error) {
		login, err := creds(ctx)
		if err != nil {
			return nil, err
		}
		logins = append(logins, login)
		return fakeIMAP{mailtest.New()}, nil
	}
	t.Cleanup(func() { dialIMAP = old })
	return &logins
}

func TestNewConnector_NoSMTPHostMeansNoSender(t *testing.T) {
	stubDial(t)
	pw := &countingResolver{password: "pw"}

	for _, host := range []string{"", "  "} {
		conn, err := newConnector(pw, slog.New(slog.DiscardHandler))(context.Background(), config.Account{Name: "Work", SMTPHost: host})

		if err != nil || conn.Mailbox == nil {
			t.Fatalf("connect = %v, %v; want a mailbox", conn, err)
		}
		if conn.Sender != nil {
			t.Errorf("smtp_host %q: Sender = %v, want a nil interface", host, conn.Sender)
		}
	}
}

func TestNewConnector_SMTPHostBuildsSenderFromOnePasswordResolution(t *testing.T) {
	logins := stubDial(t)
	pw := &countingResolver{password: "hunter2-secret"}
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	acct := config.Account{Name: "Personal", Email: "me@example.com", SMTPHost: "smtp.example.com"}.WithDefaults()

	conn, err := newConnector(pw, log)(context.Background(), acct)

	if err != nil || conn.Mailbox == nil || conn.Sender == nil {
		t.Fatalf("connect = %+v, %v; want a mailbox and a sender", conn, err)
	}
	if pw.calls != 1 || len(*logins) != 1 || (*logins)[0].Password != "hunter2-secret" {
		t.Errorf("password resolved %d times, dialed with %v; want once", pw.calls, *logins)
	}
	if strings.Contains(buf.String(), "hunter2-secret") {
		t.Error("password leaked into the log")
	}
}

func TestOnboarding_DialBuildsSenderOnlyWithSMTPHost(t *testing.T) {
	stubDial(t)
	o := newOnboarding("", stubResolver{}, nil, slog.New(slog.DiscardHandler))

	plain, err := o.Dial(context.Background(), config.Account{Name: "A", Email: "a@x.com"}, mail.StaticPassword("typed"))
	if err != nil || plain.Mailbox == nil || plain.Sender != nil {
		t.Errorf("without smtp_host: %+v, %v; want a mailbox and no sender", plain, err)
	}
	full, err := o.Dial(context.Background(), config.Account{Name: "B", Email: "b@x.com", SMTPHost: "smtp.x.com"}, mail.StaticPassword("typed"))
	if err != nil || full.Mailbox == nil || full.Sender == nil {
		t.Errorf("with smtp_host: %+v, %v; want a mailbox and a sender", full, err)
	}
}

// stubVerifySMTP replaces verifySMTP and records the accounts and passwords
// it was asked to verify.
func stubVerifySMTP(t *testing.T, err error) *[]string {
	t.Helper()
	var calls []string
	old := verifySMTP
	verifySMTP = func(ctx context.Context, acct config.Account, creds mail.Credentials) error {
		login, cerr := creds(ctx)
		if cerr != nil {
			return cerr
		}
		calls = append(calls, acct.SMTPHost+":"+strconv.Itoa(acct.SMTPPort)+" "+acct.SMTPTLS+" "+login.Password)
		return err
	}
	t.Cleanup(func() { verifySMTP = old })
	return &calls
}

// onboard does what the connect popup does for a new account: test it with
// Dial and, with an SMTP host, VerifySMTP, and only if that works save it.
func onboard(o ui.Onboarding, acct config.Account, password string) (ui.Conn, error) {
	ctx := context.Background()
	var creds mail.Credentials
	if password != "" {
		creds = mail.StaticPassword(password)
	}
	conn, err := o.Dial(ctx, acct, creds)
	if err != nil {
		return conn, err
	}
	if o.VerifySMTP != nil && strings.TrimSpace(acct.SMTPHost) != "" {
		if err := o.VerifySMTP(ctx, acct, creds); err != nil {
			_ = conn.Mailbox.Close()
			return ui.Conn{}, err
		}
	}
	return conn, o.AddAccount(acct, password)
}

func TestOnboarding_DialNeverVerifiesSMTP(t *testing.T) {
	stubDial(t)
	calls := stubVerifySMTP(t, errors.New("must not be called"))
	o := newOnboarding("", stubResolver{}, nil, slog.New(slog.DiscardHandler))

	conn, err := o.Dial(context.Background(), config.Account{Name: "A", Email: "a@x.com", SMTPHost: "smtp.x.com"}, mail.StaticPassword("pw"))

	if err != nil || conn.Sender == nil || len(*calls) != 0 {
		t.Errorf("Dial = %+v, %v, SMTP checks %v; want a sender and no SMTP check", conn, err, *calls)
	}
}

func TestOnboarding_VerifySMTPUsesTheTypedPasswordAndDefaults(t *testing.T) {
	calls := stubVerifySMTP(t, nil)
	o := newOnboarding("", stubResolver{err: errors.New("must not resolve")}, nil, slog.New(slog.DiscardHandler))

	err := o.VerifySMTP(context.Background(), config.Account{Name: "A", Email: "a@x.com", SMTPHost: "smtp.x.com", SMTPTLS: config.TLSStartTLS}, mail.StaticPassword("typed"))

	if want := []string{"smtp.x.com:587 starttls typed"}; err != nil || !slices.Equal(*calls, want) {
		t.Errorf("verified %v (%v), want %v", *calls, err, want)
	}
}

func TestOnboarding_VerifySMTPResolvesPasswordCmdPassword(t *testing.T) {
	calls := stubVerifySMTP(t, nil)
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	o := newOnboarding("", stubResolver{password: "from-1password"}, nil, log)

	err := o.VerifySMTP(context.Background(), config.Account{Name: "A", Email: "a@x.com", PasswordCmd: "op read x", SMTPHost: "smtp.x.com"}, nil)

	if want := []string{"smtp.x.com:465 implicit from-1password"}; err != nil || !slices.Equal(*calls, want) {
		t.Errorf("verified %v (%v), want %v", *calls, err, want)
	}
	if !strings.Contains(buf.String(), "smtp login") || strings.Contains(buf.String(), "from-1password") {
		t.Errorf("log = %q, want a timed SMTP step and no password", buf.String())
	}
}

func TestOnboarding_VerifySMTPPasswordErrorSkipsTheCheck(t *testing.T) {
	calls := stubVerifySMTP(t, nil)
	want := errors.New("op read failed")
	o := newOnboarding("", stubResolver{err: want}, nil, slog.New(slog.DiscardHandler))

	err := o.VerifySMTP(context.Background(), config.Account{Name: "A", SMTPHost: "smtp.x.com"}, nil)

	if !errors.Is(err, want) || len(*calls) != 0 {
		t.Errorf("error %v, checks %v; want the resolver's error and no check", err, *calls)
	}
}

func TestOnboarding_BlankSMTPHostSavesNoSMTPKeys(t *testing.T) {
	stubDial(t)
	calls := stubVerifySMTP(t, errors.New("must not be called"))
	path := filepath.Join(t.TempDir(), "config.toml")
	o := newOnboarding(path, stubResolver{}, func(string, string) error { return nil }, slog.New(slog.DiscardHandler))

	conn, err := onboard(o, config.Account{Name: "Work", Email: "me@example.com", IMAPHost: "imap.example.com"}, "pw")

	if err != nil || conn.Mailbox == nil || conn.Sender != nil || len(*calls) != 0 {
		t.Fatalf("onboard = %+v, %v, SMTP checks %v; want a mailbox, no sender, no check", conn, err, *calls)
	}
	if data, _ := os.ReadFile(path); !strings.Contains(string(data), `name = "Work"`) || strings.Contains(string(data), "smtp_") {
		t.Errorf("config.toml = %q; want the account and no smtp_* keys", data)
	}
}

func TestOnboarding_BadSMTPLoginSavesNothing(t *testing.T) {
	stubDial(t)
	stubVerifySMTP(t, errors.New(`login to account "Gmail" failed: 535 bad credentials`))
	path := filepath.Join(t.TempDir(), "config.toml")
	stored := false
	o := newOnboarding(path, stubResolver{}, func(string, string) error { stored = true; return nil }, slog.New(slog.DiscardHandler))
	acct := config.Account{Name: "Gmail", Email: "me@gmail.com", IMAPHost: "imap.gmail.com", SMTPHost: "smtp.gmail.com", SMTPPort: 465, SMTPTLS: config.TLSImplicit}

	conn, err := onboard(o, acct, "hunter2-secret")

	if err == nil || !strings.Contains(err.Error(), "535 bad credentials") || conn != (ui.Conn{}) {
		t.Errorf("onboard = %+v, %v; want the SMTP error and a zero Conn", conn, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) || stored {
		t.Errorf("config written (%v) or password stored (%v) although SMTP failed", !os.IsNotExist(err), stored)
	}
}

func TestOnboarding_GoodSMTPLoginSavesSMTPKeysAndBuildsSender(t *testing.T) {
	stubDial(t)
	calls := stubVerifySMTP(t, nil)
	path := filepath.Join(t.TempDir(), "config.toml")
	o := newOnboarding(path, stubResolver{}, func(string, string) error { return nil }, slog.New(slog.DiscardHandler))
	acct := config.Account{Name: "Gmail", Email: "me@gmail.com", IMAPHost: "imap.gmail.com", SMTPHost: "smtp.gmail.com", SMTPPort: 465, SMTPTLS: config.TLSImplicit}

	conn, err := onboard(o, acct, "app-pass")

	if err != nil || conn.Mailbox == nil || conn.Sender == nil {
		t.Fatalf("onboard = %+v, %v; want a mailbox and a sender", conn, err)
	}
	if want := []string{"smtp.gmail.com:465 implicit app-pass"}; !slices.Equal(*calls, want) {
		t.Errorf("verified %v, want %v", *calls, want)
	}
	data, _ := os.ReadFile(path)
	for _, want := range []string{`smtp_host = "smtp.gmail.com"`, "smtp_port = 465", `smtp_tls = "implicit"`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("config.toml missing %q:\n%s", want, data)
		}
	}
	if _, err := config.Load(path); err != nil {
		t.Errorf("saved config doesn't load: %v", err)
	}
}

// keychainCalls records the keychain changes Onboarding makes.
type keychainCalls struct {
	calls []string
	// copyOAuthErr and secretErr make copyOAuth and storeClientSecret fail.
	copyOAuthErr, secretErr error
	// copyClientSecretCopied is what copyClientSecret reports; the call is
	// recorded either way.
	copyClientSecretCopied bool
	// deleteTokenErr makes deleteToken fail (after recording the call).
	deleteTokenErr error
}

// stubKeychain replaces every keychain hook, recording the calls in order, so
// a test also proves which entries were left alone.
func stubKeychain(t *testing.T, copyErr error) *keychainCalls {
	t.Helper()
	k := &keychainCalls{}
	oldDelete, oldCopy := deleteSecret, copySecret
	oldDT, oldDC, oldSC, oldCO, oldCCS := deleteToken, deleteClientSecret, storeClientSecret, copyOAuth, copyClientSecret
	deleteSecret = func(account string) error { k.calls = append(k.calls, "delete "+account); return nil }
	copySecret = func(from, to string) error { k.calls = append(k.calls, "copy "+from+"->"+to); return copyErr }
	deleteToken = func(account string) error {
		k.calls = append(k.calls, "delete-token "+account)
		return k.deleteTokenErr
	}
	deleteClientSecret = func(account string) error { k.calls = append(k.calls, "delete-client-secret "+account); return nil }
	storeClientSecret = func(account, _ string) error {
		k.calls = append(k.calls, "store-client-secret "+account)
		return k.secretErr
	}
	copyOAuth = func(from, to string) error {
		k.calls = append(k.calls, "copy-oauth "+from+"->"+to)
		return k.copyOAuthErr
	}
	copyClientSecret = func(from, to string) (bool, error) {
		k.calls = append(k.calls, "copy-client-secret "+from+"->"+to)
		return k.copyClientSecretCopied, k.copyOAuthErr
	}
	t.Cleanup(func() {
		deleteSecret, copySecret = oldDelete, oldCopy
		deleteToken, deleteClientSecret, storeClientSecret, copyOAuth, copyClientSecret = oldDT, oldDC, oldSC, oldCO, oldCCS
	})
	return k
}

const editConfig = `[[account]]
name = "Work"
email = "me@example.com"
imap_host = "imap.example.com"

[[account]]
name = "Home"
email = "me@home.org"
imap_host = "imap.home.org"
password_cmd = "op read op://V/I/p"
`

func writeEditConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(editConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func rename(name string) func(config.Account) config.Account {
	return func(a config.Account) config.Account { a.Name = name; return a }
}

func TestOnboarding_UpdateAccountRenameCopiesThenDropsOldKeychainPassword(t *testing.T) {
	path := writeEditConfig(t)
	k := stubKeychain(t, nil)
	o := newOnboarding(path, stubResolver{}, func(string, string) error { t.Error("password stored"); return nil }, slog.New(slog.DiscardHandler))

	if err := o.UpdateAccount(0, "Work", rename("Job"), "", false); err != nil {
		t.Fatal(err)
	}

	if want := []string{"copy Work->Job", "delete Work"}; !reflect.DeepEqual(k.calls, want) {
		t.Errorf("keychain = %v, want %v", k.calls, want)
	}
	cfg, err := config.Load(path)
	if err != nil || cfg.Accounts[0].Name != "Job" || cfg.Accounts[1].Name != "Home" {
		t.Errorf("config = %+v, %v", cfg, err)
	}
}

func TestOnboarding_UpdateAccountNewPasswordStoresUnderNewNameAndDropsOld(t *testing.T) {
	path := writeEditConfig(t)
	k := stubKeychain(t, nil)
	stored := map[string]string{}
	o := newOnboarding(path, stubResolver{}, func(a, p string) error { stored[a] = p; return nil }, slog.New(slog.DiscardHandler))

	if err := o.UpdateAccount(0, "Work", rename("Job"), "secret", false); err != nil {
		t.Fatal(err)
	}

	if stored["Job"] != "secret" {
		t.Errorf("stored = %v", stored)
	}
	if want := []string{"delete Work"}; !reflect.DeepEqual(k.calls, want) {
		t.Errorf("keychain = %v, want %v", k.calls, want)
	}
}

func TestOnboarding_UpdateAccountPasswordCmdAccountLeavesKeychainAlone(t *testing.T) {
	path := writeEditConfig(t)
	k := stubKeychain(t, nil)
	o := newOnboarding(path, stubResolver{}, nil, slog.New(slog.DiscardHandler))

	if err := o.UpdateAccount(1, "Home", rename("House"), "", false); err != nil {
		t.Fatal(err)
	}
	if len(k.calls) != 0 {
		t.Errorf("keychain touched: %v", k.calls)
	}
}

func TestOnboarding_UpdateAccountFailedWriteCleansUpAndKeepsConfig(t *testing.T) {
	path := writeEditConfig(t)
	k := stubKeychain(t, nil)
	o := newOnboarding(path, stubResolver{}, func(string, string) error { return nil }, slog.New(slog.DiscardHandler))

	// "Home" is a duplicate name, so the config write is refused.
	err := o.UpdateAccount(0, "Work", rename("Home"), "secret", false)

	if err == nil {
		t.Fatal("want an error")
	}
	if want := []string{"delete Home"}; !reflect.DeepEqual(k.calls, want) {
		t.Errorf("keychain = %v, want %v", k.calls, want)
	}
	if got, _ := os.ReadFile(path); string(got) != editConfig {
		t.Errorf("config changed: %s", got)
	}
}

func TestOnboarding_UpdateAccountFailedWriteRemovesTheCopy(t *testing.T) {
	path := writeEditConfig(t)
	k := stubKeychain(t, nil)
	o := newOnboarding(path, stubResolver{}, nil, slog.New(slog.DiscardHandler))

	if err := o.UpdateAccount(0, "Work", rename("Home"), "", false); err == nil {
		t.Fatal("want an error")
	}
	if want := []string{"copy Work->Home", "delete Home"}; !reflect.DeepEqual(k.calls, want) {
		t.Errorf("keychain = %v, want %v", k.calls, want)
	}
}

func TestOnboarding_UpdateAccountStaleNameWritesNothing(t *testing.T) {
	path := writeEditConfig(t)
	k := stubKeychain(t, nil)
	o := newOnboarding(path, stubResolver{}, func(string, string) error { t.Error("password stored"); return nil }, slog.New(slog.DiscardHandler))

	err := o.UpdateAccount(0, "Other", rename("Job"), "secret", false)

	if err == nil || !strings.Contains(err.Error(), "config changed on disk") || len(k.calls) != 0 {
		t.Errorf("error = %v, keychain = %v", err, k.calls)
	}
}

func TestOnboarding_UpdateAccountCopyFailureLeavesConfigUntouched(t *testing.T) {
	path := writeEditConfig(t)
	k := stubKeychain(t, errors.New("keychain locked"))
	o := newOnboarding(path, stubResolver{}, nil, slog.New(slog.DiscardHandler))

	err := o.UpdateAccount(0, "Work", rename("Job"), "", false)

	if err == nil || !strings.Contains(err.Error(), "keychain locked") {
		t.Errorf("error = %v", err)
	}
	if got, _ := os.ReadFile(path); string(got) != editConfig {
		t.Errorf("config changed: %s", got)
	}
	if want := []string{"copy Work->Job"}; !reflect.DeepEqual(k.calls, want) {
		t.Errorf("keychain = %v, want %v", k.calls, want)
	}
}

func TestOnboarding_RemoveAccount(t *testing.T) {
	tests := []struct {
		name     string
		index    int
		acct     config.Account
		deletePW bool
		want     []string
	}{
		{"keychain account, delete password", 0, config.Account{Name: "Work"}, true, []string{"delete Work"}},
		{"keychain account, keep password", 0, config.Account{Name: "Work"}, false, nil},
		{"password_cmd account", 1, config.Account{Name: "Home", PasswordCmd: "op read x"}, true, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeEditConfig(t)
			k := stubKeychain(t, nil)
			o := newOnboarding(path, stubResolver{}, nil, slog.New(slog.DiscardHandler))

			if err := o.RemoveAccount(tt.index, tt.acct, tt.deletePW); err != nil {
				t.Fatal(err)
			}

			if !reflect.DeepEqual(k.calls, tt.want) {
				t.Errorf("keychain = %v, want %v", k.calls, tt.want)
			}
			cfg, err := config.Load(path)
			if err != nil || len(cfg.Accounts) != 1 || cfg.Accounts[0].Name == tt.acct.Name {
				t.Errorf("config = %+v, %v", cfg, err)
			}
		})
	}
}

func TestOnboarding_RemoveAccountStaleKeepsPassword(t *testing.T) {
	path := writeEditConfig(t)
	k := stubKeychain(t, nil)
	o := newOnboarding(path, stubResolver{}, nil, slog.New(slog.DiscardHandler))

	if err := o.RemoveAccount(0, config.Account{Name: "Gone"}, true); err == nil || len(k.calls) != 0 {
		t.Errorf("error = %v, keychain = %v", err, k.calls)
	}
}

// oauthResolver is a secretResolver with a counted token command.
type oauthResolver struct {
	token string
	err   error
	calls int
}

func (o *oauthResolver) Resolve(context.Context, config.Account) (string, error) {
	o.calls++
	return o.token, o.err
}

func (o *oauthResolver) ResolveClientSecret(context.Context, config.Account) (string, error) {
	return "", nil
}

func TestCredentials_PasswordAccountResolvesOnce(t *testing.T) {
	pw := &countingResolver{password: "hunter2"}

	creds, err := credentials(context.Background(), pw, config.Account{Name: "Work"})
	if err != nil {
		t.Fatalf("credentials() error = %v", err)
	}
	for range 3 {
		login, err := creds(context.Background())
		if err != nil || login != (mail.Login{Password: "hunter2"}) {
			t.Fatalf("creds() = %+v, %v; want the password", login, err)
		}
	}

	if pw.calls != 1 {
		t.Errorf("password resolved %d times, want 1", pw.calls)
	}
}

func TestCredentials_OAuthWithPasswordCmdRunsTheCommandEveryTime(t *testing.T) {
	pw := &oauthResolver{token: "tok-from-cmd"}
	acct := config.Account{Name: "Work", Auth: config.AuthOAuth2, PasswordCmd: "oama access me"}

	creds, err := credentials(context.Background(), pw, acct)
	if err != nil {
		t.Fatalf("credentials() error = %v", err)
	}
	if pw.calls != 0 {
		t.Fatalf("command ran %d times before any connection, want 0", pw.calls)
	}
	for range 3 {
		login, err := creds(context.Background())
		if err != nil || login != (mail.Login{Token: "tok-from-cmd"}) {
			t.Fatalf("creds() = %+v, %v; want the token", login, err)
		}
	}

	if pw.calls != 3 {
		t.Errorf("command ran %d times, want 3", pw.calls)
	}
}

func oauthProviderAccount() config.Account {
	return config.Account{
		Name: "Work", Email: "me@work.com", Auth: config.AuthOAuth2,
		OAuthProvider: config.ProviderMicrosoft, OAuthClientID: "client-id",
	}
}

func TestCredentials_OAuthProviderUsesStoredToken(t *testing.T) {
	old := loadToken
	t.Cleanup(func() { loadToken = old })
	var loaded []string
	loadToken = func(account string) (oauth.Token, error) {
		loaded = append(loaded, account)
		return oauth.Token{AccessToken: "stored-access", RefreshToken: "r", Expiry: time.Now().Add(time.Hour)}, nil
	}

	creds, err := credentials(context.Background(), stubResolver{}, oauthProviderAccount())
	if err != nil {
		t.Fatalf("credentials() error = %v", err)
	}
	login, err := creds(context.Background())

	if err != nil || login != (mail.Login{Token: "stored-access"}) {
		t.Fatalf("creds() = %+v, %v; want the stored access token", login, err)
	}
	if !slices.Equal(loaded, []string{"Work"}) {
		t.Errorf("loadToken called for %v, want [Work]", loaded)
	}
}

func TestCredentials_OAuthProviderWithoutSignInFailsOnUse(t *testing.T) {
	old := loadToken
	t.Cleanup(func() { loadToken = old })
	loadToken = func(string) (oauth.Token, error) { return oauth.Token{}, secret.ErrNoToken }

	creds, err := credentials(context.Background(), stubResolver{}, oauthProviderAccount())
	if err != nil {
		t.Fatalf("credentials() error = %v", err)
	}
	_, err = creds(context.Background())

	if !errors.Is(err, secret.ErrNoToken) {
		t.Errorf("creds() error = %v, want secret.ErrNoToken", err)
	}
}
