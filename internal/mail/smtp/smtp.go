// Package smtp sends outgoing mail over SMTP with go-smtp. It never sends in
// plaintext: connections use implicit TLS or STARTTLS, and AUTH happens only
// after the channel is encrypted.
package smtp

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-sasl"
	gosmtp "github.com/emersion/go-smtp"

	"github.com/dkouvdis/dk-email/internal/config"
)

// timeout bounds one whole Send or Verify call, including dialing.
const timeout = 60 * time.Second

// Transport sends mail for one account. It is safe for concurrent use: each
// call opens its own connection.
type Transport struct {
	acct     config.Account
	password string

	// tlsConfig is a test hook merged into the TLS config, like
	// imap.Client's.
	tlsConfig *tls.Config
}

// New returns a Transport for acct. The caller applies config defaults, so
// acct.SMTPHost, SMTPPort and SMTPTLS must be set.
func New(acct config.Account, password string) *Transport {
	return &Transport{acct: acct, password: password}
}

// Send delivers msg, a complete RFC 5322 message, from the envelope sender
// from to every address in rcpts. It dials, authenticates, issues MAIL FROM,
// RCPT TO and DATA, then quits.
func (t *Transport) Send(ctx context.Context, from string, rcpts []string, msg []byte) error {
	if len(rcpts) == 0 {
		return errors.New("send: no recipients")
	}
	return t.session(ctx, func(c *gosmtp.Client) error {
		if err := c.Mail(from, nil); err != nil {
			return fmt.Errorf("sender %q rejected: %w", from, err)
		}
		for _, rcpt := range rcpts {
			if err := c.Rcpt(rcpt, nil); err != nil {
				return fmt.Errorf("recipient %q rejected: %w", rcpt, err)
			}
		}
		w, err := c.Data()
		if err != nil {
			return fmt.Errorf("DATA: %w", err)
		}
		if _, err := w.Write(msg); err != nil {
			_ = w.Close()
			return fmt.Errorf("writing message: %w", err)
		}
		if err := w.Close(); err != nil {
			return fmt.Errorf("message not accepted: %w", err)
		}
		return nil
	})
}

// Verify dials and authenticates, then quits without sending anything.
func (t *Transport) Verify(ctx context.Context) error {
	return t.session(ctx, func(*gosmtp.Client) error { return nil })
}

func (t *Transport) addr() string {
	return net.JoinHostPort(t.acct.SMTPHost, strconv.Itoa(t.acct.SMTPPort))
}

// session connects and authenticates, runs fn, then quits. Cancelling ctx or
// hitting the overall timeout closes the connection.
func (t *Transport) session(ctx context.Context, fn func(*gosmtp.Client) error) (err error) {
	if strings.TrimSpace(t.acct.SMTPHost) == "" {
		return fmt.Errorf("account %q has no smtp_host", t.acct.Name)
	}
	addr := t.addr()
	parent := ctx
	defer func() {
		if err == nil {
			return
		}
		if ctxErr := parent.Err(); ctxErr != nil && !errors.Is(err, ctxErr) {
			err = fmt.Errorf("%w (%v)", ctxErr, err)
		}
		err = fmt.Errorf("send via %s: %w", addr, err)
	}()

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	tlsConfig, err := t.newTLSConfig()
	if err != nil {
		return err
	}

	raw, err := (&net.Dialer{}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer raw.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = raw.SetDeadline(deadline)
	}
	defer context.AfterFunc(ctx, func() { raw.Close() })()

	c, err := t.handshake(ctx, raw, tlsConfig)
	if err != nil {
		return err
	}
	defer c.Close()

	if err := t.auth(c); err != nil {
		return err
	}
	if err := fn(c); err != nil {
		return err
	}
	if err := c.Quit(); err != nil {
		return fmt.Errorf("QUIT: %w", err)
	}
	return nil
}

// handshake wraps raw in TLS, immediately or through STARTTLS, and returns a
// client ready for AUTH. go-smtp's STARTTLS fails when the server doesn't
// advertise it, so nothing is sent unencrypted.
func (t *Transport) handshake(ctx context.Context, raw net.Conn, tlsConfig *tls.Config) (*gosmtp.Client, error) {
	switch t.acct.SMTPTLS {
	case config.TLSStartTLS:
		c, err := gosmtp.NewClientStartTLS(raw, tlsConfig)
		if err != nil {
			return nil, fmt.Errorf("STARTTLS with %s: %w", t.acct.SMTPHost, err)
		}
		return c, nil
	case config.TLSImplicit:
		tlsConn := tls.Client(raw, tlsConfig)
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			return nil, fmt.Errorf("TLS handshake with %s: %w", t.acct.SMTPHost, err)
		}
		return gosmtp.NewClient(tlsConn), nil
	default:
		return nil, fmt.Errorf("unknown smtp_tls mode %q", t.acct.SMTPTLS)
	}
}

// auth logs in with SASL PLAIN, redacting the password from any error.
func (t *Transport) auth(c *gosmtp.Client) error {
	err := c.Auth(sasl.NewPlainClient("", t.acct.Username, t.password))
	if err == nil {
		return nil
	}
	text := err.Error()
	if t.password != "" {
		text = strings.ReplaceAll(text, t.password, "***")
	}
	return fmt.Errorf("login to account %q (%s) failed: %s", t.acct.Name, t.addr(), text)
}

// newTLSConfig verifies the server against the system roots, or only against
// the account's tls_cert when set.
func (t *Transport) newTLSConfig() (*tls.Config, error) {
	cfg := &tls.Config{}
	if t.tlsConfig != nil {
		cfg = t.tlsConfig.Clone()
	}
	cfg.ServerName = t.acct.SMTPHost
	cfg.MinVersion = max(cfg.MinVersion, tls.VersionTLS12)
	if t.acct.TLSCert != "" {
		pool, err := config.CertPool(t.acct.TLSCert)
		if err != nil {
			return nil, fmt.Errorf("tls_cert for account %q: %w", t.acct.Name, err)
		}
		cfg.RootCAs = pool
	}
	return cfg, nil
}
