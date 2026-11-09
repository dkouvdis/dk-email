package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net"
	"strings"
	"testing"

	"github.com/dkouvdis/dk-email/internal/config"
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
