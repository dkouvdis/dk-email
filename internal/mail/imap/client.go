// Package imap implements mail.Mailbox on top of an IMAP server.
//
// A Client keeps one lazily opened connection per account. Calls are
// serialized by a mutex, a broken connection is dropped, and the next call
// reconnects. go-imap's client methods take no context, so cancellation and
// deadlines are enforced by closing the TCP connection when the context ends.
package imap

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	goimap "github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"

	"github.com/dkouvdis/dk-email/internal/config"
	"github.com/dkouvdis/dk-email/internal/mail"
)

// closeTimeout bounds how long Close waits for the server to acknowledge LOGOUT.
const closeTimeout = 5 * time.Second

var _ mail.Mailbox = (*Client)(nil)

// Client is a mail.Mailbox for one IMAP account.
type Client struct {
	acct     config.Account
	password string

	// tlsConfig is a base TLS configuration. It is only set from tests, to
	// trust a self-signed certificate.
	tlsConfig *tls.Config

	mu     sync.Mutex
	conn   *imapclient.Client
	raw    net.Conn // the TCP connection under conn, closed to abort a call
	closed bool
	// uidValidity remembers each folder's last seen UIDVALIDITY.
	uidValidity map[string]uint32
}

// finalError marks an error from a healthy connection, so do neither drops
// the connection nor retries.
type finalError struct{ err error }

func (e finalError) Error() string { return e.err.Error() }
func (e finalError) Unwrap() error { return e.err }

// New returns a Client for acct. It does no network I/O; the connection is
// opened by the first call.
func New(acct config.Account, password string) *Client {
	return &Client{acct: acct, password: password}
}

// Dial is New followed by an eager connect and login, so bad settings and
// credentials surface immediately.
func Dial(ctx context.Context, acct config.Account, password string) (*Client, error) {
	return dial(ctx, New(acct, password))
}

func dial(ctx context.Context, c *Client) (*Client, error) {
	if err := c.do(ctx, "Dial", func(*imapclient.Client) error { return nil }); err != nil {
		return nil, err
	}
	return c, nil
}

// Close logs out and closes the connection. It is safe to call more than once;
// later calls on the Client return mail.ErrClosed.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	if c.conn == nil {
		return nil
	}

	conn, raw := c.conn, c.raw
	c.conn, c.raw = nil, nil

	select {
	case <-conn.Closed():
	default:
		t := time.AfterFunc(closeTimeout, func() { raw.Close() })
		defer t.Stop()
		// The connection is going away either way, so a failed LOGOUT is not an error.
		_ = conn.Logout().Wait()
	}
	_ = conn.Close()
	return nil
}

func (c *Client) Message(ctx context.Context, folder string, uid mail.UID) (mail.Message, error) {
	return mail.Message{}, fmt.Errorf("imap: Message: %w", errors.ErrUnsupported)
}

func (c *Client) SetFlag(ctx context.Context, folder string, uids []mail.UID, f mail.Flag, on bool) error {
	return fmt.Errorf("imap: SetFlag: %w", errors.ErrUnsupported)
}

func (c *Client) Move(ctx context.Context, from string, uids []mail.UID, to string) error {
	return fmt.Errorf("imap: Move: %w", errors.ErrUnsupported)
}

// do runs fn on the account's connection, opening it if needed. If a
// connection that was already open fails, do reconnects and runs fn once more,
// so fn must be safe to repeat.
func (c *Client) do(ctx context.Context, op string, fn func(*imapclient.Client) error) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return fmt.Errorf("imap: %s: %w", op, mail.ErrClosed)
	}

	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("imap: %s: %w", op, err)
		}
		conn, reused, err := c.connectLocked(ctx)
		if err != nil {
			return fmt.Errorf("imap: %s: %w", op, err)
		}

		err = c.runLocked(ctx, conn, fn)
		if err == nil {
			return nil
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("imap: %s: %w", op, ctxErr)
		}
		var serverErr *goimap.Error
		var final finalError
		if errors.As(err, &serverErr) || errors.As(err, &final) {
			return fmt.Errorf("imap: %s: %w", op, err)
		}
		c.dropLocked()
		if !reused {
			return fmt.Errorf("imap: %s: %w", op, err)
		}
	}
}

func (c *Client) runLocked(ctx context.Context, conn *imapclient.Client, fn func(*imapclient.Client) error) error {
	raw := c.raw
	stop := context.AfterFunc(ctx, func() { raw.Close() })
	err := fn(conn)
	if !stop() {
		// The context ended while fn ran, so the connection was closed under it.
		c.dropLocked()
	}
	return err
}

func (c *Client) dropLocked() {
	if c.conn == nil {
		return
	}
	_ = c.conn.Close()
	c.conn, c.raw = nil, nil
}

// connectLocked returns the live connection, opening one if needed. reused
// reports whether the connection predates this call.
func (c *Client) connectLocked(ctx context.Context) (conn *imapclient.Client, reused bool, err error) {
	if c.conn != nil {
		select {
		case <-c.conn.Closed():
			c.dropLocked()
		default:
			return c.conn, true, nil
		}
	}

	conn, raw, err := c.open(ctx)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, false, ctxErr
		}
		return nil, false, err
	}
	c.conn, c.raw = conn, raw
	return conn, false, nil
}

func (c *Client) open(ctx context.Context) (*imapclient.Client, net.Conn, error) {
	addr := net.JoinHostPort(c.acct.IMAPHost, strconv.Itoa(c.acct.IMAPPort))
	raw, err := (&net.Dialer{}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, nil, fmt.Errorf("connect to %s: %w", addr, err)
	}

	stop := context.AfterFunc(ctx, func() { raw.Close() })
	conn, err := c.handshake(ctx, raw)
	if !stop() && err == nil {
		err = ctx.Err()
		_ = conn.Close()
	}
	if err != nil {
		raw.Close()
		return nil, nil, err
	}
	return conn, raw, nil
}

// handshake upgrades raw to TLS, waits for the greeting and logs in. It
// leaves raw open on failure; the caller closes it.
func (c *Client) handshake(ctx context.Context, raw net.Conn) (*imapclient.Client, error) {
	opts := &imapclient.Options{TLSConfig: c.newTLSConfig()}

	var conn *imapclient.Client
	switch c.acct.TLS {
	case config.TLSStartTLS:
		var err error
		if conn, err = imapclient.NewStartTLS(raw, opts); err != nil {
			return nil, fmt.Errorf("STARTTLS with %s: %w", c.acct.IMAPHost, err)
		}
	case config.TLSImplicit, "":
		tlsConn := tls.Client(raw, opts.TLSConfig)
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			return nil, fmt.Errorf("TLS handshake with %s: %w", c.acct.IMAPHost, err)
		}
		conn = imapclient.New(tlsConn, opts)
	default:
		return nil, fmt.Errorf("unknown tls mode %q", c.acct.TLS)
	}

	if err := conn.WaitGreeting(); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("waiting for greeting from %s: %w", c.acct.IMAPHost, err)
	}
	if err := conn.Login(c.acct.Username, c.password).Wait(); err != nil {
		_ = conn.Close()
		return nil, c.loginError(err)
	}
	return conn, nil
}

func (c *Client) newTLSConfig() *tls.Config {
	cfg := &tls.Config{}
	if c.tlsConfig != nil {
		cfg = c.tlsConfig.Clone()
	}
	cfg.ServerName = c.acct.IMAPHost
	cfg.MinVersion = max(cfg.MinVersion, tls.VersionTLS12)
	return cfg
}

func (c *Client) loginError(err error) error {
	server := net.JoinHostPort(c.acct.IMAPHost, strconv.Itoa(c.acct.IMAPPort))
	var serverErr *goimap.Error
	if !errors.As(err, &serverErr) {
		return fmt.Errorf("login to account %q (%s) failed: %w", c.acct.Name, server, err)
	}
	text := serverErr.Text
	if c.password != "" {
		text = strings.ReplaceAll(text, c.password, "***")
	}
	msg := fmt.Sprintf("login to account %q (%s) failed: %s", c.acct.Name, server, text)
	if hint := loginHint(c.acct.IMAPHost, serverErr.Code, text); hint != "" {
		msg += ". Hint: " + hint
	}
	return errors.New(msg)
}

// loginHint returns actionable advice for known Gmail login failures, or "".
func loginHint(host string, code goimap.ResponseCode, text string) string {
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, "application-specific password required"):
		return "Gmail requires an app password when 2-Step Verification is on. " +
			"Create one at https://myaccount.google.com/apppasswords, store it in 1Password, " +
			"and point password_cmd at it (passkeys can't be used for IMAP)"
	case isGmailHost(host) && (strings.EqualFold(string(code), string(goimap.ResponseCodeAuthenticationFailed)) ||
		strings.Contains(lower, "invalid credentials")):
		return "For Gmail use an app password from https://myaccount.google.com/apppasswords, " +
			"not your Google password. Google Workspace (work/school) accounts don't allow app passwords " +
			"and need OAuth, which dk-email doesn't support yet"
	case strings.Contains(lower, "web login required"):
		return "sign in to this account once in a web browser to clear Google's security check, then retry"
	}
	return ""
}

func isGmailHost(host string) bool {
	return strings.EqualFold(host, "imap.gmail.com") || strings.EqualFold(host, "imap.googlemail.com")
}
