package imap

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	goimap "github.com/emersion/go-imap/v2"

	"github.com/dkouvdis/dk-email/internal/config"
	"github.com/dkouvdis/dk-email/internal/mail"
)

var bg = context.Background()

func timeoutCtx(t *testing.T, d time.Duration) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(bg, d)
	t.Cleanup(cancel)
	return ctx
}

func TestFolders_ListsSeededFoldersWithRolesAndCounts(t *testing.T) {
	tests := []struct {
		name string
		mode string
		caps goimap.CapSet
	}{
		{"implicit TLS with LIST-STATUS", modeImplicit, capsListStatus},
		{"implicit TLS with per-folder STATUS", modeImplicit, capsListOnly},
		{"implicit TLS with plain LIST", modeImplicit, capsPlainList},
		{"STARTTLS with LIST-STATUS", modeStartTLS, capsListStatus},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := startServer(t, tt.mode, tt.caps)
			c := srv.newClient(t, tt.mode)

			got, err := c.Folders(timeoutCtx(t, 10*time.Second))

			if err != nil {
				t.Fatalf("Folders() error = %v", err)
			}
			if !slices.Equal(got, wantSeededFolders) {
				t.Errorf("Folders() = %+v, want %+v", got, wantSeededFolders)
			}
		})
	}
}

func TestFolders_ConnectsLazilyAndReusesOneConnection(t *testing.T) {
	srv := startServer(t, modeImplicit, capsListStatus)

	c := srv.newClient(t, modeImplicit)

	if n := srv.ln.accepted(); n != 0 {
		t.Fatalf("New opened %d connections, want 0", n)
	}
	for i := range 3 {
		if _, err := c.Folders(timeoutCtx(t, 10*time.Second)); err != nil {
			t.Fatalf("Folders() call %d error = %v", i, err)
		}
	}
	if n := srv.ln.accepted(); n != 1 {
		t.Errorf("accepted %d connections after 3 calls, want 1", n)
	}
}

func TestFolders_ReconnectsAfterServerDropsConnection(t *testing.T) {
	srv := startServer(t, modeImplicit, capsListStatus)
	c := srv.newClient(t, modeImplicit)
	if _, err := c.Folders(timeoutCtx(t, 10*time.Second)); err != nil {
		t.Fatalf("first Folders() error = %v", err)
	}

	srv.ln.dropAll()
	got, err := c.Folders(timeoutCtx(t, 10*time.Second))

	if err != nil {
		t.Fatalf("Folders() after drop error = %v", err)
	}
	if !slices.Equal(got, wantSeededFolders) {
		t.Errorf("Folders() after drop = %+v, want %+v", got, wantSeededFolders)
	}
	if n := srv.ln.accepted(); n != 2 {
		t.Errorf("accepted %d connections, want 2", n)
	}
}

func TestFolders_ReconnectsAfterCallTimesOut(t *testing.T) {
	srv := startServer(t, modeImplicit, capsListStatus)
	c := srv.newClient(t, modeImplicit)
	srv.hangList.Store(true)

	start := time.Now()
	_, err := c.Folders(timeoutCtx(t, 150*time.Millisecond))

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Folders() error = %v, want context.DeadlineExceeded", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("Folders() took %v after a 150ms deadline", elapsed)
	}

	srv.hangList.Store(false)
	got, err := c.Folders(timeoutCtx(t, 10*time.Second))
	if err != nil {
		t.Fatalf("Folders() after timeout error = %v", err)
	}
	if !slices.Equal(got, wantSeededFolders) {
		t.Errorf("Folders() after timeout = %+v, want %+v", got, wantSeededFolders)
	}
}

func TestFolders_CancelDuringCall(t *testing.T) {
	srv := startServer(t, modeImplicit, capsListStatus)
	c := srv.newClient(t, modeImplicit)
	srv.hangList.Store(true)
	ctx, cancel := context.WithCancel(bg)
	time.AfterFunc(100*time.Millisecond, cancel)

	_, err := c.Folders(ctx)

	if !errors.Is(err, context.Canceled) {
		t.Errorf("Folders() error = %v, want context.Canceled", err)
	}
}

func TestFolders_CancelledContextDoesNotConnect(t *testing.T) {
	srv := startServer(t, modeImplicit, capsListStatus)
	c := srv.newClient(t, modeImplicit)
	ctx, cancel := context.WithCancel(bg)
	cancel()

	_, err := c.Folders(ctx)

	if !errors.Is(err, context.Canceled) {
		t.Errorf("Folders() error = %v, want context.Canceled", err)
	}
	if n := srv.ln.accepted(); n != 0 {
		t.Errorf("accepted %d connections for a cancelled context, want 0", n)
	}
}

func TestDial_ConnectsAndLogsIn(t *testing.T) {
	srv := startServer(t, modeImplicit, capsListStatus)

	c, err := dial(timeoutCtx(t, 10*time.Second), newTrustingClient(srv, modeImplicit, testPassword))

	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	t.Cleanup(func() { c.Close() })
	if n := srv.ln.accepted(); n != 1 {
		t.Errorf("accepted %d connections, want 1", n)
	}
}

func TestDial_RejectsBadPasswordWithClearError(t *testing.T) {
	const wrong = "wr0ng-p4ssw0rd"
	for _, mode := range []string{modeImplicit, modeStartTLS} {
		t.Run(mode, func(t *testing.T) {
			srv := startServer(t, mode, capsListStatus)

			c, err := dial(timeoutCtx(t, 10*time.Second), newTrustingClient(srv, mode, wrong))

			if err == nil {
				c.Close()
				t.Fatal("Dial() error = nil, want login failure")
			}
			msg := err.Error()
			acct := srv.account(mode)
			server := net.JoinHostPort(acct.IMAPHost, strconv.Itoa(acct.IMAPPort))
			if want := fmt.Sprintf(`login to account "Test" (%s) failed`, server); !strings.Contains(msg, want) {
				t.Errorf("error %q does not contain %q (account, host:port and failed login)", msg, want)
			}
			if !strings.Contains(msg, "Authentication failed") {
				t.Errorf("error %q does not include the server's response text", msg)
			}
			if strings.Contains(msg, wrong) {
				t.Errorf("error %q contains the password", msg)
			}
			if strings.Contains(msg, "Hint:") {
				t.Errorf("error %q has a Gmail hint for a non-Gmail host", msg)
			}
		})
	}
}

func TestFolders_BadPasswordErrorFromLazyConnect(t *testing.T) {
	srv := startServer(t, modeImplicit, capsListStatus)
	c := newTrustingClient(srv, modeImplicit, "nope")
	t.Cleanup(func() { c.Close() })

	_, err := c.Folders(timeoutCtx(t, 10*time.Second))

	acct := srv.account(modeImplicit)
	server := net.JoinHostPort(acct.IMAPHost, strconv.Itoa(acct.IMAPPort))
	if want := fmt.Sprintf(`login to account "Test" (%s) failed`, server); err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("Folders() error = %v, want a login failure containing %q", err, want)
	}
}

func TestLoginError_RedactsPasswordEchoedByServer(t *testing.T) {
	c := New(config.Account{Name: "Personal", IMAPHost: "imap.example.com", IMAPPort: 993}, "hunter2")
	serverErr := &goimap.Error{Type: goimap.StatusResponseTypeNo, Text: "bad login for hunter2"}

	got := c.loginError(serverErr).Error()

	if strings.Contains(got, "hunter2") {
		t.Errorf("loginError() = %q, contains the password", got)
	}
	if want := `login to account "Personal" (imap.example.com:993) failed: bad login for ***`; got != want {
		t.Errorf("loginError() = %q, want %q", got, want)
	}
}

func TestLoginError_NonServerErrorNamesHostAndPort(t *testing.T) {
	c := New(config.Account{Name: "Personal", IMAPHost: "imap.example.com", IMAPPort: 993}, "hunter2")
	cause := errors.New("connection reset")

	err := c.loginError(cause)

	got := err.Error()
	if strings.Contains(got, "hunter2") {
		t.Errorf("loginError() = %q, contains the password", got)
	}
	if want := `login to account "Personal" (imap.example.com:993) failed: connection reset`; got != want {
		t.Errorf("loginError() = %q, want %q", got, want)
	}
	if !errors.Is(err, cause) {
		t.Errorf("loginError() = %v, does not wrap the cause", err)
	}
}

func TestLoginError_AppendsHintAfterMessageWithoutPassword(t *testing.T) {
	c := New(config.Account{Name: "Personal", IMAPHost: "imap.gmail.com", IMAPPort: 993}, "hunter2")
	serverErr := &goimap.Error{
		Type: goimap.StatusResponseTypeNo,
		Code: goimap.ResponseCodeAuthenticationFailed,
		Text: "Invalid credentials hunter2",
	}

	got := c.loginError(serverErr).Error()

	if strings.Contains(got, "hunter2") {
		t.Errorf("loginError() = %q, contains the password", got)
	}
	prefix := `login to account "Personal" (imap.gmail.com:993) failed: Invalid credentials ***. Hint: For Gmail use an app password`
	if !strings.HasPrefix(got, prefix) {
		t.Errorf("loginError() = %q, want prefix %q", got, prefix)
	}
}

func TestLoginHint(t *testing.T) {
	const (
		appPasswordHint = "Gmail requires an app password when 2-Step Verification is on. " +
			"Create one at https://myaccount.google.com/apppasswords, store it in 1Password, " +
			"and point password_cmd at it (passkeys can't be used for IMAP)"
		gmailHint = "For Gmail use an app password from https://myaccount.google.com/apppasswords, " +
			"not your Google password. Google Workspace (work/school) accounts don't allow app passwords " +
			"and need OAuth, which dk-email doesn't support yet"
		webLoginHint = "sign in to this account once in a web browser to clear Google's security check, then retry"
	)
	tests := []struct {
		name string
		host string
		code goimap.ResponseCode
		text string
		want string
	}{
		{"app password required on Gmail", "imap.gmail.com", "", "Application-specific password required: https://support.google.com/accounts/answer/185833 (Failure)", appPasswordHint},
		{"app password required on any host", "mail.example.com", "ALERT", "Application-specific password required", appPasswordHint},
		{"app password text is case-insensitive", "mail.example.com", "", "APPLICATION-SPECIFIC PASSWORD REQUIRED", appPasswordHint},
		{"app password wins over invalid credentials", "imap.gmail.com", goimap.ResponseCodeAuthenticationFailed, "Invalid credentials: Application-specific password required", appPasswordHint},
		{"Gmail with authentication failed code", "imap.gmail.com", goimap.ResponseCodeAuthenticationFailed, "Authentication failed", gmailHint},
		{"Gmail with invalid credentials text", "imap.gmail.com", "", "Invalid credentials (Failure)", gmailHint},
		{"Googlemail host", "imap.googlemail.com", goimap.ResponseCodeAuthenticationFailed, "nope", gmailHint},
		{"Gmail host is case-insensitive", "IMAP.Gmail.COM", "", "invalid CREDENTIALS", gmailHint},
		{"non-Gmail host with invalid credentials", "mail.example.com", "", "Invalid credentials", ""},
		{"non-Gmail host with authentication failed code", "mail.example.com", goimap.ResponseCodeAuthenticationFailed, "Authentication failed", ""},
		{"Gmail host with unrelated failure", "imap.gmail.com", "", "Too many simultaneous connections", ""},
		{"web login required", "imap.gmail.com", "WEBALERT", "Web login required.", webLoginHint},
		{"web login text is case-insensitive", "mail.example.com", "", "WEB LOGIN REQUIRED", webLoginHint},
		{"no hint otherwise", "mail.example.com", "", "bad login", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := loginHint(tt.host, tt.code, tt.text); got != tt.want {
				t.Errorf("loginHint(%q, %q, %q) = %q, want %q", tt.host, tt.code, tt.text, got, tt.want)
			}
		})
	}
}

func TestDial_StartTLSRefusedByServerFailsBeforeLogin(t *testing.T) {
	srv := startServer(t, modePlain, capsListStatus)

	c, err := dial(timeoutCtx(t, 10*time.Second), newTrustingClient(srv, modeStartTLS, testPassword))

	if err == nil {
		c.Close()
		t.Fatal("Dial() error = nil, want STARTTLS failure")
	}
	if !strings.Contains(err.Error(), "STARTTLS") {
		t.Errorf("Dial() error = %q, want it to mention STARTTLS", err)
	}
}

func TestDial_RejectsUnknownTLSMode(t *testing.T) {
	srv := startServer(t, modeImplicit, capsListStatus)

	_, err := dial(timeoutCtx(t, 10*time.Second), newTrustingClient(srv, "sometimes", testPassword))

	if err == nil || !strings.Contains(err.Error(), `unknown tls mode "sometimes"`) {
		t.Errorf("Dial() error = %v, want unknown tls mode", err)
	}
}

func TestDial_UntrustedCertificateFails(t *testing.T) {
	srv := startServer(t, modeImplicit, capsListStatus)
	c := New(srv.account(modeImplicit), testPassword)

	_, err := dial(timeoutCtx(t, 10*time.Second), c)

	if err == nil || !strings.Contains(err.Error(), "TLS handshake") {
		t.Errorf("Dial() error = %v, want a TLS handshake failure", err)
	}
}

func TestDial_ConnectionRefused(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().(*net.TCPAddr)
	ln.Close()
	acct := config.Account{Name: "Test", IMAPHost: "127.0.0.1", IMAPPort: addr.Port, TLS: modeImplicit}

	_, err = Dial(timeoutCtx(t, 10*time.Second), acct, testPassword)

	if err == nil || !strings.Contains(err.Error(), "connect to") {
		t.Errorf("Dial() error = %v, want a connect failure", err)
	}
}

func TestDial_HonoursDeadlineWhenServerNeverGreets(t *testing.T) {
	cert, pool := selfSignedCert(t)
	serverTLS := &tls.Config{Certificates: []tls.Certificate{cert}}
	tests := []struct {
		name      string
		mode      string
		serverTLS *tls.Config
	}{
		{"implicit TLS, no handshake", modeImplicit, nil},
		{"implicit TLS, handshake then silence", modeImplicit, serverTLS},
		{"STARTTLS, silent greeting", modeStartTLS, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host, port := silentListener(t, tt.serverTLS)
			c := New(config.Account{Name: "Test", Username: testUser, IMAPHost: host, IMAPPort: port, TLS: tt.mode}, testPassword)
			c.tlsConfig = &tls.Config{RootCAs: pool}
			ctx, cancel := context.WithTimeout(bg, 100*time.Millisecond)
			defer cancel()

			start := time.Now()
			_, err := dial(ctx, c)

			if elapsed := time.Since(start); elapsed > time.Second {
				t.Errorf("Dial() took %v with a 100ms deadline", elapsed)
			}
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("Dial() error = %v, want context.DeadlineExceeded", err)
			}
			if c.conn != nil {
				t.Error("failed Dial left a connection open")
			}
		})
	}
}

func TestDial_HonoursCancellationWhileWaitingForGreeting(t *testing.T) {
	host, port := silentListener(t, nil)
	c := New(config.Account{Name: "Test", Username: testUser, IMAPHost: host, IMAPPort: port, TLS: modeStartTLS}, testPassword)
	ctx, cancel := context.WithCancel(bg)
	time.AfterFunc(50*time.Millisecond, cancel)

	start := time.Now()
	_, err := dial(ctx, c)

	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("Dial() took %v after cancellation", elapsed)
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Dial() error = %v, want context.Canceled", err)
	}
}

func TestClose_IsIdempotentAndLaterCallsReturnErrClosed(t *testing.T) {
	srv := startServer(t, modeImplicit, capsListStatus)
	c := srv.newClient(t, modeImplicit)
	if _, err := c.Folders(timeoutCtx(t, 10*time.Second)); err != nil {
		t.Fatalf("Folders() error = %v", err)
	}

	err1 := c.Close()
	err2 := c.Close()
	_, foldersErr := c.Folders(bg)

	if err1 != nil || err2 != nil {
		t.Errorf("Close() = %v, %v; want nil, nil", err1, err2)
	}
	if !errors.Is(foldersErr, mail.ErrClosed) {
		t.Errorf("Folders() after Close error = %v, want mail.ErrClosed", foldersErr)
	}
}

func TestClose_WithoutConnectionMakesLaterCallsFail(t *testing.T) {
	c := New(config.Account{Name: "Test"}, "pw")

	if err := c.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if _, err := c.Folders(bg); !errors.Is(err, mail.ErrClosed) {
		t.Errorf("Folders() error = %v, want mail.ErrClosed", err)
	}
}

func TestClose_AfterServerDroppedConnection(t *testing.T) {
	srv := startServer(t, modeImplicit, capsListStatus)
	c := srv.newClient(t, modeImplicit)
	if _, err := c.Folders(timeoutCtx(t, 10*time.Second)); err != nil {
		t.Fatalf("Folders() error = %v", err)
	}
	srv.ln.dropAll()
	<-c.conn.Closed()

	if err := c.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}
}

func TestUnsupportedMethods_ReturnErrUnsupported(t *testing.T) {
	c := New(config.Account{Name: "Test"}, "pw")
	calls := map[string]func() error{
		"Summaries": func() error { _, err := c.Summaries(bg, "INBOX", 0, 10); return err },
		"Message":   func() error { _, err := c.Message(bg, "INBOX", 1); return err },
		"SetFlag":   func() error { return c.SetFlag(bg, "INBOX", []mail.UID{1}, mail.FlagSeen, true) },
		"Move":      func() error { return c.Move(bg, "INBOX", []mail.UID{1}, "Archive") },
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			err := call()

			if !errors.Is(err, errors.ErrUnsupported) {
				t.Errorf("%s() error = %v, want errors.ErrUnsupported", name, err)
			}
		})
	}
}

func TestNewTLSConfig_EnforcesServerNameAndMinimumVersion(t *testing.T) {
	base := &tls.Config{MinVersion: tls.VersionTLS10, ServerName: "other.example.com"}
	c := New(config.Account{IMAPHost: "imap.example.com"}, "pw")
	c.tlsConfig = base

	got := c.newTLSConfig()

	if got.ServerName != "imap.example.com" {
		t.Errorf("ServerName = %q, want imap.example.com", got.ServerName)
	}
	if got.MinVersion != tls.VersionTLS12 {
		t.Errorf("MinVersion = %#x, want TLS 1.2", got.MinVersion)
	}
	if base.MinVersion != tls.VersionTLS10 || base.ServerName != "other.example.com" {
		t.Error("newTLSConfig mutated the injected base config")
	}
}

func TestNewTLSConfig_DefaultsToTLS12(t *testing.T) {
	c := New(config.Account{IMAPHost: "imap.example.com"}, "pw")

	got := c.newTLSConfig()

	if got.MinVersion != tls.VersionTLS12 || got.ServerName != "imap.example.com" {
		t.Errorf("newTLSConfig() = {MinVersion: %#x, ServerName: %q}", got.MinVersion, got.ServerName)
	}
}

func newTrustingClient(srv *testServer, mode, password string) *Client {
	c := New(srv.account(mode), password)
	c.tlsConfig = srv.clientTLS
	return c
}
