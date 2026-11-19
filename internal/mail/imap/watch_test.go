package imap

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	goimap "github.com/emersion/go-imap/v2"

	"github.com/dkouvdis/dk-email/internal/config"
	"github.com/dkouvdis/dk-email/internal/mail"
)

var capsWatch = goimap.CapSet{goimap.CapIMAP4rev1: {}, goimap.CapUIDPlus: {}}

func (s *testServer) newWatcher(t *testing.T) *Watcher {
	t.Helper()
	w := NewWatcher(s.account(modeImplicit), mail.StaticPassword(testPassword), nil)
	w.tlsConfig = s.clientTLS
	t.Cleanup(func() { w.Close() })
	return w
}

// startWait runs w.Wait in a goroutine and returns its result channel.
func startWait(ctx context.Context, w *Watcher, folder string) <-chan error {
	done := make(chan error, 1)
	go func() { done <- w.Wait(ctx, folder) }()
	return done
}

// waitIdling blocks until the server has n IDLE commands running or started.
func waitIdling(t *testing.T, s *testServer, n int64) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for s.idles.Load() < n {
		if time.Now().After(deadline) {
			t.Fatalf("server saw %d IDLE commands, want %d", s.idles.Load(), n)
		}
		time.Sleep(5 * time.Millisecond)
	}
	// The server counts IDLE as it starts; give it a moment to be ready.
	time.Sleep(50 * time.Millisecond)
}

func expectDone(t *testing.T, done <-chan error, within time.Duration) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(within):
		t.Fatalf("Wait did not return within %v", within)
		return nil
	}
}

func expectBlocked(t *testing.T, done <-chan error, d time.Duration) {
	t.Helper()
	select {
	case err := <-done:
		t.Fatalf("Wait returned %v, want it to keep waiting", err)
	case <-time.After(d):
	}
}

func (s *testServer) appendInbox(t *testing.T) {
	t.Helper()
	s.appendRaw(t, "INBOX", "Subject: new\r\n\r\nbody\r\n", time.Now())
}

// expungeInbox removes the first INBOX message from a second session.
func (s *testServer) expungeInbox(t *testing.T) {
	t.Helper()
	c := s.newClient(t, modeImplicit)
	ctx := timeoutCtx(t, 10*time.Second)
	sums, err := c.Summaries(ctx, "INBOX", 0, 10)
	if err != nil || len(sums) == 0 {
		t.Fatalf("Summaries() = %v, %v", sums, err)
	}
	if err := c.Delete(ctx, "INBOX", []mail.UID{sums[0].UID}); err != nil {
		t.Fatalf("Delete() = %v", err)
	}
}

func TestWatcher_AppendWakesWait(t *testing.T) {
	srv := startServer(t, modeImplicit, capsWatch)
	w := srv.newWatcher(t)
	done := startWait(timeoutCtx(t, 10*time.Second), w, "INBOX")
	waitIdling(t, srv, 1)

	srv.appendInbox(t)

	if err := expectDone(t, done, time.Second); err != nil {
		t.Errorf("Wait() = %v, want nil", err)
	}
}

func TestWatcher_ExpungeDoesNotWake(t *testing.T) {
	srv := startServer(t, modeImplicit, capsWatch)
	w := srv.newWatcher(t)
	ctx, cancel := context.WithCancel(timeoutCtx(t, 10*time.Second))
	done := startWait(ctx, w, "INBOX")
	waitIdling(t, srv, 1)

	srv.expungeInbox(t)

	expectBlocked(t, done, 200*time.Millisecond)
	cancel()
	if err := expectDone(t, done, time.Second); !errors.Is(err, context.Canceled) {
		t.Errorf("Wait() after cancel = %v, want context.Canceled", err)
	}
}

func TestWatcher_ExpungeThenAppendWakes(t *testing.T) {
	srv := startServer(t, modeImplicit, capsWatch)
	w := srv.newWatcher(t)
	done := startWait(timeoutCtx(t, 10*time.Second), w, "INBOX")
	waitIdling(t, srv, 1)

	srv.expungeInbox(t)
	expectBlocked(t, done, 100*time.Millisecond)
	srv.appendInbox(t) // EXISTS is back to the count Wait started with

	if err := expectDone(t, done, time.Second); err != nil {
		t.Errorf("Wait() = %v, want nil", err)
	}
}

func TestWatcher_AppendBetweenWaitsWakesNextWait(t *testing.T) {
	srv := startServer(t, modeImplicit, capsWatch)
	w := srv.newWatcher(t)
	first := startWait(timeoutCtx(t, 10*time.Second), w, "INBOX")
	waitIdling(t, srv, 1)
	srv.appendInbox(t)
	if err := expectDone(t, first, time.Second); err != nil {
		t.Fatalf("first Wait() = %v", err)
	}

	srv.appendInbox(t)

	done := startWait(timeoutCtx(t, 10*time.Second), w, "INBOX")
	if err := expectDone(t, done, time.Second); err != nil {
		t.Errorf("second Wait() = %v, want nil", err)
	}
	if n := srv.ln.accepted(); n != 1 {
		t.Errorf("server accepted %d connections, want the watch connection reused", n)
	}
}

// startNoIdleServer runs a minimal implicit-TLS IMAP server that
// advertises no IDLE (the in-process server always does for IMAP4rev1).
func startNoIdleServer(t *testing.T) *Watcher {
	t.Helper()
	cert, pool := selfSignedCert(t)
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				fmt.Fprint(conn, "* OK [CAPABILITY IMAP4rev1] ready\r\n")
				r := bufio.NewReader(conn)
				for {
					line, err := r.ReadString('\n')
					if err != nil {
						return
					}
					tag, cmd, _ := strings.Cut(strings.TrimSpace(line), " ")
					switch {
					case strings.HasPrefix(cmd, "CAPABILITY"):
						fmt.Fprintf(conn, "* CAPABILITY IMAP4rev1\r\n%s OK done\r\n", tag)
					case strings.HasPrefix(cmd, "LOGIN"):
						fmt.Fprintf(conn, "%s OK [CAPABILITY IMAP4rev1] logged in\r\n", tag)
					default:
						fmt.Fprintf(conn, "%s BAD unsupported\r\n", tag)
					}
				}
			}()
		}
	}()
	addr := ln.Addr().(*net.TCPAddr)
	acct := config.Account{Name: "Test", Username: testUser, IMAPHost: addr.IP.String(), IMAPPort: addr.Port, TLS: modeImplicit}
	w := NewWatcher(acct, mail.StaticPassword(testPassword), nil)
	w.tlsConfig = &tls.Config{RootCAs: pool}
	t.Cleanup(func() { w.Close() })
	return w
}

func TestWatcher_WithoutIdleIsUnsupported(t *testing.T) {
	w := startNoIdleServer(t)

	err := w.Wait(timeoutCtx(t, 10*time.Second), "INBOX")

	if !errors.Is(err, mail.ErrUnsupported) {
		t.Errorf("Wait() = %v, want mail.ErrUnsupported", err)
	}
}

func TestWatcher_CancelledContextReturnsPromptly(t *testing.T) {
	srv := startServer(t, modeImplicit, capsWatch)
	w := srv.newWatcher(t)

	already, cancelNow := context.WithCancel(context.Background())
	cancelNow()
	if err := w.Wait(already, "INBOX"); !errors.Is(err, context.Canceled) {
		t.Errorf("Wait(cancelled ctx) = %v, want context.Canceled", err)
	}

	ctx, cancel := context.WithCancel(timeoutCtx(t, 10*time.Second))
	done := startWait(ctx, w, "INBOX")
	waitIdling(t, srv, 1)
	cancel()
	if err := expectDone(t, done, time.Second); !errors.Is(err, context.Canceled) {
		t.Errorf("Wait() = %v, want context.Canceled", err)
	}
}

func TestWatcher_ReconnectsAfterServerDropsConnection(t *testing.T) {
	srv := startServer(t, modeImplicit, capsWatch)
	w := srv.newWatcher(t)
	first := startWait(timeoutCtx(t, 10*time.Second), w, "INBOX")
	waitIdling(t, srv, 1)

	srv.ln.dropAll()

	if err := expectDone(t, first, time.Second); err == nil {
		t.Fatal("Wait() on a dropped connection = nil, want an error")
	}
	done := startWait(timeoutCtx(t, 10*time.Second), w, "INBOX")
	waitIdling(t, srv, 2)
	srv.appendInbox(t)

	if err := expectDone(t, done, time.Second); err != nil {
		t.Errorf("Wait() after reconnect = %v, want nil", err)
	}
	if n := srv.ln.accepted(); n != 2 {
		t.Errorf("server accepted %d connections, want 2", n)
	}
}

func TestWatcher_RestartsIdle(t *testing.T) {
	srv := startServer(t, modeImplicit, capsWatch)
	w := srv.newWatcher(t)
	w.idleEvery = 50 * time.Millisecond
	done := startWait(timeoutCtx(t, 10*time.Second), w, "INBOX")
	waitIdling(t, srv, 3)

	srv.appendInbox(t)

	if err := expectDone(t, done, time.Second); err != nil {
		t.Errorf("Wait() = %v, want nil", err)
	}
}

func TestWatcher_LogsNeverContainSecrets(t *testing.T) {
	srv := startServer(t, modeImplicit, capsWatch)
	srv.acceptToken("tok-secret")
	var (
		mu  sync.Mutex
		buf bytes.Buffer
	)
	log := slog.New(slog.NewTextHandler(lockedWriter{&mu, &buf}, &slog.HandlerOptions{Level: slog.LevelDebug}))

	for name, creds := range map[string]mail.Credentials{
		"password": mail.StaticPassword(testPassword),
		"oauth":    func(context.Context) (mail.Login, error) { return mail.Login{Token: "tok-secret"}, nil },
	} {
		w := NewWatcher(srv.account(modeImplicit), creds, log)
		w.tlsConfig = srv.clientTLS
		ctx, cancel := context.WithCancel(timeoutCtx(t, 10*time.Second))
		done := startWait(ctx, w, "INBOX")
		before := srv.idles.Load()
		waitIdling(t, srv, before+1)
		cancel()
		expectDone(t, done, time.Second)
		w.Close()
		_ = name
	}

	mu.Lock()
	defer mu.Unlock()
	out := buf.String()
	if !strings.Contains(out, "watch connected") || !strings.Contains(out, "watch dropped") {
		t.Errorf("log = %q, want watch connected and watch dropped records", out)
	}
	for _, secret := range []string{testPassword, "tok-secret"} {
		if strings.Contains(out, secret) {
			t.Errorf("log contains %q: %s", secret, out)
		}
	}
}

type lockedWriter struct {
	mu *sync.Mutex
	w  *bytes.Buffer
}

func (l lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

func TestWatcher_CloseIsIdempotentAndEndsWaits(t *testing.T) {
	srv := startServer(t, modeImplicit, capsWatch)
	w := srv.newWatcher(t)
	done := startWait(timeoutCtx(t, 10*time.Second), w, "INBOX")
	waitIdling(t, srv, 1)

	if err := w.Close(); err != nil {
		t.Errorf("Close() = %v", err)
	}
	if err := w.Close(); err != nil {
		t.Errorf("second Close() = %v", err)
	}

	if err := expectDone(t, done, time.Second); !errors.Is(err, mail.ErrClosed) {
		t.Errorf("pending Wait() = %v, want mail.ErrClosed", err)
	}
	if err := w.Wait(timeoutCtx(t, 10*time.Second), "INBOX"); !errors.Is(err, mail.ErrClosed) {
		t.Errorf("Wait() after Close = %v, want mail.ErrClosed", err)
	}
}
