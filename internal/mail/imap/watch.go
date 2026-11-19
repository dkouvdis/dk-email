package imap

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	goimap "github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"

	"github.com/dkouvdis/dk-email/internal/config"
	"github.com/dkouvdis/dk-email/internal/mail"
)

var _ mail.Watcher = (*Watcher)(nil)

// defaultIdleEvery is how often IDLE is ended and restarted. Gmail ends an
// IDLE after 29 minutes.
const defaultIdleEvery = 25 * time.Minute

// Watcher is a mail.Watcher: it waits for new mail in one folder with IDLE
// over a connection of its own, so the Client's serialized connection stays
// free.
type Watcher struct {
	acct  config.Account
	creds mail.Credentials
	log   *slog.Logger // debug log; never nil, discards by default

	// tlsConfig is a base TLS configuration. It is only set from tests, to
	// trust a self-signed certificate.
	tlsConfig *tls.Config
	// idleEvery is how long one IDLE command runs before it is restarted.
	// Gmail ends IDLE after 29 minutes, so the default stays below that.
	idleEvery time.Duration

	mu     sync.Mutex // guards closed and cancel
	closed bool
	cancel context.CancelFunc // ends the Wait in progress

	// waitMu serializes Wait and Close, and guards conn, raw and folder.
	waitMu sync.Mutex
	conn   *imapclient.Client
	raw    net.Conn // the TCP connection under conn
	folder string   // the folder selected on conn, or ""

	// Handlers run on go-imap's reader goroutine, so hmu guards the counts.
	hmu     sync.Mutex
	n       uint32 // messages in the folder, as last reported
	arrived bool   // a message arrived since the last Wait returned
	signal  chan struct{}
}

// NewWatcher returns a Watcher for acct. It does no network I/O; the first
// Wait connects. A nil log discards.
func NewWatcher(acct config.Account, creds mail.Credentials, log *slog.Logger) *Watcher {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Watcher{
		acct:      acct,
		creds:     creds,
		log:       log,
		idleEvery: defaultIdleEvery,
		signal:    make(chan struct{}, 1),
	}
}

// Close logs out and closes the connection. It is safe to call more than
// once; it ends a Wait in progress, and later Waits return mail.ErrClosed.
func (w *Watcher) Close() error {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil
	}
	w.closed = true
	if w.cancel != nil {
		w.cancel()
	}
	w.mu.Unlock()

	w.waitMu.Lock()
	defer w.waitMu.Unlock()
	if w.conn == nil {
		return nil
	}
	conn, raw := w.conn, w.raw
	w.conn, w.raw, w.folder = nil, nil, ""

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

// Wait implements mail.Watcher.
func (w *Watcher) Wait(ctx context.Context, folder string) error {
	w.waitMu.Lock()
	defer w.waitMu.Unlock()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return fmt.Errorf("imap: watch: %w", mail.ErrClosed)
	}
	w.cancel = cancel
	w.mu.Unlock()

	err := w.wait(ctx, folder)
	if err == nil {
		return nil
	}
	if errors.Is(err, mail.ErrUnsupported) {
		return fmt.Errorf("imap: watch: %w", err)
	}
	w.dropLocked()
	w.log.Debug("watch dropped", "account", w.acct.Name, "err", err)
	w.mu.Lock()
	closed := w.closed
	w.mu.Unlock()
	if closed {
		return fmt.Errorf("imap: watch: %w", mail.ErrClosed)
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Errorf("imap: watch: %w", ctxErr)
	}
	return fmt.Errorf("imap: watch: %w", err)
}

func (w *Watcher) dropLocked() {
	if w.conn == nil {
		return
	}
	_ = w.conn.Close()
	w.conn, w.raw, w.folder = nil, nil, ""
}

// guard runs fn, which blocks on the connection, and closes the connection
// if ctx ends meanwhile, as go-imap's methods take no context.
func (w *Watcher) guard(ctx context.Context, fn func() error) error {
	raw := w.raw
	stop := context.AfterFunc(ctx, func() { raw.Close() })
	err := fn()
	if !stop() && err == nil {
		err = ctx.Err()
	}
	return err
}

func (w *Watcher) wait(ctx context.Context, folder string) error {
	if w.conn != nil {
		select {
		case <-w.conn.Closed():
			w.log.Debug("watch connection closed by server", "account", w.acct.Name)
			w.dropLocked()
		default:
		}
	}
	if w.conn == nil {
		if err := w.connect(ctx); err != nil {
			return err
		}
	}
	conn := w.conn

	if w.folder != folder {
		var data *goimap.SelectData
		err := w.guard(ctx, func() (err error) {
			data, err = conn.Select(folder, &goimap.SelectOptions{ReadOnly: true}).Wait()
			return err
		})
		if err != nil {
			return fmt.Errorf("examine %q: %w", folder, err)
		}
		w.folder = folder
		w.hmu.Lock()
		w.n, w.arrived = data.NumMessages, false
		w.hmu.Unlock()
	} else if err := w.guard(ctx, func() error { return conn.Noop().Wait() }); err != nil {
		return fmt.Errorf("noop: %w", err)
	}
	if w.takeArrived() {
		return nil
	}
	return w.idle(ctx, conn)
}

func (w *Watcher) connect(ctx context.Context) error {
	// A throwaway Client carries the dial and login code both share.
	dialer := &Client{acct: w.acct, creds: w.creds, tlsConfig: w.tlsConfig}
	conn, raw, err := dialer.open(ctx, &imapclient.UnilateralDataHandler{
		Mailbox: w.onMailbox,
		Expunge: w.onExpunge,
	})
	if err != nil {
		return err
	}
	if !conn.Caps().Has(goimap.CapIdle) {
		_ = conn.Close()
		w.log.Debug("watch unsupported: no IDLE", "account", w.acct.Name)
		return mail.ErrUnsupported
	}
	w.conn, w.raw, w.folder = conn, raw, ""
	w.log.Debug("watch connected", "account", w.acct.Name)
	return nil
}

func (w *Watcher) onMailbox(data *imapclient.UnilateralDataMailbox) {
	if data.NumMessages == nil {
		return
	}
	w.hmu.Lock()
	if *data.NumMessages > w.n {
		w.arrived = true
	}
	w.n = *data.NumMessages
	w.hmu.Unlock()
	select {
	case w.signal <- struct{}{}:
	default:
	}
}

func (w *Watcher) onExpunge(uint32) {
	w.hmu.Lock()
	if w.n > 0 {
		w.n--
	}
	w.hmu.Unlock()
}

// takeArrived reports whether mail arrived, and resets the flag.
func (w *Watcher) takeArrived() bool {
	w.hmu.Lock()
	defer w.hmu.Unlock()
	arrived := w.arrived
	w.arrived = false
	return arrived
}

// idle runs IDLE until mail arrives, restarting it every idleEvery.
func (w *Watcher) idle(ctx context.Context, conn *imapclient.Client) error {
	var cmd *imapclient.IdleCommand
	start := func() error {
		return w.guard(ctx, func() (err error) {
			cmd, err = conn.Idle()
			return err
		})
	}
	end := func() error {
		return w.guard(ctx, func() error {
			if err := cmd.Close(); err != nil {
				return err
			}
			return cmd.Wait()
		})
	}

	if err := start(); err != nil {
		return fmt.Errorf("idle: %w", err)
	}
	timer := time.NewTimer(w.idleEvery)
	defer timer.Stop()
	for {
		select {
		case <-w.signal:
			if !w.takeArrived() {
				continue
			}
			if err := end(); err != nil {
				return fmt.Errorf("end idle: %w", err)
			}
			w.takeArrived()
			w.log.Debug("watch woke", "account", w.acct.Name)
			return nil
		case <-timer.C:
			if err := end(); err != nil {
				return fmt.Errorf("end idle: %w", err)
			}
			if err := start(); err != nil {
				return fmt.Errorf("restart idle: %w", err)
			}
			w.log.Debug("watch idle restarted", "account", w.acct.Name)
			timer.Reset(w.idleEvery)
		case <-ctx.Done():
			w.raw.Close()
			return ctx.Err()
		case <-conn.Closed():
			return errors.New("connection closed")
		}
	}
}
