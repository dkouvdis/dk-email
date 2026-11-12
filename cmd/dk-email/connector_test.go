package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net"
	"os"
	"path/filepath"
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
