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

	mb, err := newConnector(stubResolver{err: want}, log)(context.Background(), config.Account{Name: "Personal"})

	if !errors.Is(err, want) || mb != nil {
		t.Fatalf("connect = %v, %v; want nil, %v", mb, err, want)
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

	mb, err := newConnector(stubResolver{password: password}, log)(context.Background(), acct)

	if err == nil || mb != nil {
		t.Fatalf("connect = %#v, %v; want a nil mailbox and an error", mb, err)
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
	mb, err := o.Dial(context.Background(), config.Account{Name: "Work", Email: "me@x", IMAPHost: "127.0.0.1", IMAPPort: port}, "typed")
	if err == nil || errors.Is(err, want) || mb != nil {
		t.Errorf("Dial() = %v, %v; want a connection error and a nil mailbox", mb, err)
	}
}
