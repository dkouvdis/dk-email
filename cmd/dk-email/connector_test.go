package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/dkouvdis/dk-email/internal/config"
	"github.com/dkouvdis/dk-email/internal/mail"
	"github.com/dkouvdis/dk-email/internal/mail/mailtest"
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

func TestNewConnector_PasswordErrorSkipsLogin(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	want := errors.New("op read failed")

	conn, err := newConnector(stubResolver{err: want}, log)(context.Background(), config.Account{Name: "Personal"})

	if !errors.Is(err, want) || conn != (ui.Conn{}) {
		t.Fatalf("connect = %v, %v; want a zero Conn, %v", conn, err, want)
	}
	if !strings.Contains(buf.String(), "password resolved") || strings.Contains(buf.String(), "imap login") {
		t.Errorf("log = %q, want a password step and no login step", buf.String())
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

	if _, err := o.Dial(context.Background(), config.Account{Name: "Work", PasswordCmd: "op read x"}, ""); !errors.Is(err, want) {
		t.Errorf("Dial() error = %v, want the resolver's error", err)
	}

	// With a typed password the resolver isn't used; the dial itself fails here.
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	conn, err := o.Dial(context.Background(), config.Account{Name: "Work", Email: "me@x", IMAPHost: "127.0.0.1", IMAPPort: port}, "typed")
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

// fakeIMAP is an imapConn that never touches the network.
type fakeIMAP struct{ *mailtest.Fake }

func (fakeIMAP) Raw(context.Context, string, mail.UID) ([]byte, error) { return nil, nil }
func (fakeIMAP) Append(context.Context, string, []byte, ...mail.Flag) error {
	return nil
}
func (fakeIMAP) SentFolder(context.Context) (string, bool, error) { return "", false, nil }
func (fakeIMAP) SetFlag(context.Context, string, []mail.UID, mail.Flag, bool) error {
	return nil
}

// stubDial replaces dialIMAP and records the passwords it was given.
func stubDial(t *testing.T) *[]string {
	t.Helper()
	var passwords []string
	old := dialIMAP
	dialIMAP = func(_ context.Context, _ config.Account, password string) (imapConn, error) {
		passwords = append(passwords, password)
		return fakeIMAP{mailtest.New()}, nil
	}
	t.Cleanup(func() { dialIMAP = old })
	return &passwords
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
	passwords := stubDial(t)
	pw := &countingResolver{password: "hunter2-secret"}
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	acct := config.Account{Name: "Personal", Email: "me@example.com", SMTPHost: "smtp.example.com"}.WithDefaults()

	conn, err := newConnector(pw, log)(context.Background(), acct)

	if err != nil || conn.Mailbox == nil || conn.Sender == nil {
		t.Fatalf("connect = %+v, %v; want a mailbox and a sender", conn, err)
	}
	if pw.calls != 1 || len(*passwords) != 1 || (*passwords)[0] != "hunter2-secret" {
		t.Errorf("password resolved %d times, dialed with %v; want once", pw.calls, *passwords)
	}
	if strings.Contains(buf.String(), "hunter2-secret") {
		t.Error("password leaked into the log")
	}
}

func TestOnboarding_DialBuildsSenderOnlyWithSMTPHost(t *testing.T) {
	stubDial(t)
	o := newOnboarding("", stubResolver{}, nil, slog.New(slog.DiscardHandler))

	plain, err := o.Dial(context.Background(), config.Account{Name: "A", Email: "a@x.com"}, "typed")
	if err != nil || plain.Mailbox == nil || plain.Sender != nil {
		t.Errorf("without smtp_host: %+v, %v; want a mailbox and no sender", plain, err)
	}
	full, err := o.Dial(context.Background(), config.Account{Name: "B", Email: "b@x.com", SMTPHost: "smtp.x.com"}, "typed")
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
	verifySMTP = func(_ context.Context, acct config.Account, password string) error {
		calls = append(calls, acct.SMTPHost+":"+strconv.Itoa(acct.SMTPPort)+" "+acct.SMTPTLS+" "+password)
		return err
	}
	t.Cleanup(func() { verifySMTP = old })
	return &calls
}

// onboard does what the connect popup does for a new account: test it with
// Dial and, with an SMTP host, VerifySMTP, and only if that works save it.
func onboard(o ui.Onboarding, acct config.Account, password string) (ui.Conn, error) {
	ctx := context.Background()
	conn, err := o.Dial(ctx, acct, password)
	if err != nil {
		return conn, err
	}
	if o.VerifySMTP != nil && strings.TrimSpace(acct.SMTPHost) != "" {
		if err := o.VerifySMTP(ctx, acct, password); err != nil {
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

	conn, err := o.Dial(context.Background(), config.Account{Name: "A", Email: "a@x.com", SMTPHost: "smtp.x.com"}, "pw")

	if err != nil || conn.Sender == nil || len(*calls) != 0 {
		t.Errorf("Dial = %+v, %v, SMTP checks %v; want a sender and no SMTP check", conn, err, *calls)
	}
}

func TestOnboarding_VerifySMTPUsesTheTypedPasswordAndDefaults(t *testing.T) {
	calls := stubVerifySMTP(t, nil)
	o := newOnboarding("", stubResolver{err: errors.New("must not resolve")}, nil, slog.New(slog.DiscardHandler))

	err := o.VerifySMTP(context.Background(), config.Account{Name: "A", Email: "a@x.com", SMTPHost: "smtp.x.com", SMTPTLS: config.TLSStartTLS}, "typed")

	if want := []string{"smtp.x.com:587 starttls typed"}; err != nil || !slices.Equal(*calls, want) {
		t.Errorf("verified %v (%v), want %v", *calls, err, want)
	}
}

func TestOnboarding_VerifySMTPResolvesPasswordCmdPassword(t *testing.T) {
	calls := stubVerifySMTP(t, nil)
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	o := newOnboarding("", stubResolver{password: "from-1password"}, nil, log)

	err := o.VerifySMTP(context.Background(), config.Account{Name: "A", Email: "a@x.com", PasswordCmd: "op read x", SMTPHost: "smtp.x.com"}, "")

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

	err := o.VerifySMTP(context.Background(), config.Account{Name: "A", SMTPHost: "smtp.x.com"}, "")

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
