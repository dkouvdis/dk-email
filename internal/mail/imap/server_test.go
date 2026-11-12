package imap

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"log"
	"math/big"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	goimap "github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"

	"github.com/dkouvdis/dk-email/internal/config"
	"github.com/dkouvdis/dk-email/internal/mail"
)

const (
	testUser     = "user@example.com"
	testPassword = "s3cret-p4ss"
)

// Transport modes for the in-process server.
const (
	modeImplicit = config.TLSImplicit // TLS from the first byte
	modeStartTLS = config.TLSStartTLS // plaintext greeting, then STARTTLS
	modePlain    = "plain"            // no TLS at all
)

type mailboxSpec struct {
	name  string
	attrs []goimap.MailboxAttr
	// seen and unseen are the number of messages appended with and without \Seen.
	seen, unseen int
	// noselect mailboxes are only listed, not created.
	noselect bool
}

// seededMailboxes is in the order the test server lists them. imapmemserver
// v2.0.0-beta.8 ignores CreateOptions.SpecialUse (mailboxes never report
// special-use attributes), so testSession reports these attributes itself.
var seededMailboxes = []mailboxSpec{
	{name: "INBOX", seen: 1, unseen: 2},
	{name: "Sent", attrs: []goimap.MailboxAttr{goimap.MailboxAttrSent}, seen: 1},
	{name: "Trash", attrs: []goimap.MailboxAttr{goimap.MailboxAttrTrash}},
	{name: "Archive", attrs: []goimap.MailboxAttr{goimap.MailboxAttrArchive}, unseen: 2},
	{name: "[Gmail]", attrs: []goimap.MailboxAttr{goimap.MailboxAttrNoSelect}, noselect: true},
	{name: "[Gmail]/All Mail", attrs: []goimap.MailboxAttr{goimap.MailboxAttrAll}, seen: 2, unseen: 2},
	{name: "[Gmail]/Starred", attrs: []goimap.MailboxAttr{goimap.MailboxAttrFlagged}},
	{name: "Receipts", unseen: 1},
}

var wantSeededFolders = []mail.Folder{
	{Name: "INBOX", Delimiter: "/", Role: mail.RoleInbox, Unread: 2, Total: 3},
	{Name: "Sent", Delimiter: "/", Role: mail.RoleSent, Unread: 0, Total: 1},
	{Name: "Trash", Delimiter: "/", Role: mail.RoleTrash, Unread: 0, Total: 0},
	{Name: "Archive", Delimiter: "/", Role: mail.RoleArchive, Unread: 2, Total: 2},
	{Name: "[Gmail]/All Mail", Delimiter: "/", Role: mail.RoleAll, Unread: 2, Total: 4},
	{Name: "[Gmail]/Starred", Delimiter: "/", Role: mail.RoleNone, Unread: 0, Total: 0},
	{Name: "Receipts", Delimiter: "/", Role: mail.RoleNone, Unread: 1, Total: 1},
}

// Capability sets that make the server take each of the client's list paths.
var (
	capsListStatus = goimap.CapSet{goimap.CapIMAP4rev1: {}, goimap.CapIMAP4rev2: {}, goimap.CapSpecialUse: {}}
	capsListOnly   = goimap.CapSet{goimap.CapIMAP4rev1: {}, goimap.CapListExtended: {}, goimap.CapSpecialUse: {}}
	capsPlainList  = goimap.CapSet{goimap.CapIMAP4rev1: {}}
)

type testServer struct {
	host, port string
	clientTLS  *tls.Config
	certPEM    []byte // the server's self-signed certificate, as a tls_cert file holds it
	ln         *trackingListener
	user       *imapmemserver.User

	// hangList makes LIST block until the test ends.
	hangList atomic.Bool
	// noSentRole makes LIST report "Sent" without its \Sent attribute.
	noSentRole atomic.Bool
	release    chan struct{}
}

// startServer runs an in-memory IMAP server on 127.0.0.1 with the seeded mailboxes.
func startServer(t *testing.T, mode string, caps goimap.CapSet) *testServer {
	t.Helper()

	cert, pool := selfSignedCert(t)
	serverTLS := &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}

	rawLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	ln := &trackingListener{Listener: rawLn}
	host, port, err := net.SplitHostPort(rawLn.Addr().String())
	if err != nil {
		t.Fatalf("split address: %v", err)
	}

	user := imapmemserver.NewUser(testUser, testPassword)
	seedMailboxes(t, user)

	s := &testServer{
		host:      host,
		port:      port,
		clientTLS: &tls.Config{RootCAs: pool},
		certPEM:   pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Certificate[0]}),
		ln:        ln,
		user:      user,
		release:   make(chan struct{}),
	}

	opts := &imapserver.Options{
		NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return &testSession{UserSession: imapmemserver.NewUserSession(user), user: user, srv: s}, nil, nil
		},
		Caps:   caps,
		Logger: log.New(io.Discard, "", 0),
	}
	var serveLn net.Listener = ln
	switch mode {
	case modeImplicit:
		serveLn = tls.NewListener(ln, serverTLS)
	case modeStartTLS:
		opts.TLSConfig = serverTLS
	}

	srv := imapserver.New(opts)
	go srv.Serve(serveLn)
	t.Cleanup(func() { srv.Close() })
	// Registered last, so it runs first and unblocks any hung handler before srv.Close waits.
	t.Cleanup(func() { close(s.release) })
	return s
}

func (s *testServer) account(mode string) config.Account {
	p, _ := strconv.Atoi(s.port)
	return config.Account{
		Name:     "Test",
		Email:    testUser,
		Username: testUser,
		IMAPHost: s.host,
		IMAPPort: p,
		TLS:      mode,
	}
}

// newClient returns a lazily connecting Client that trusts the test certificate.
func (s *testServer) newClient(t *testing.T, mode string) *Client {
	t.Helper()
	c := New(s.account(mode), testPassword)
	c.tlsConfig = s.clientTLS
	t.Cleanup(func() { c.Close() })
	return c
}

func seedMailboxes(t *testing.T, user *imapmemserver.User) {
	t.Helper()
	for _, m := range seededMailboxes {
		if m.noselect {
			continue
		}
		if err := user.Create(m.name, nil); err != nil {
			t.Fatalf("create %q: %v", m.name, err)
		}
		for i := 0; i < m.seen+m.unseen; i++ {
			var flags []goimap.Flag
			if i < m.seen {
				flags = []goimap.Flag{goimap.FlagSeen}
			}
			body := "Subject: message " + strconv.Itoa(i) + "\r\n\r\nbody\r\n"
			if _, err := user.Append(m.name, newLiteral(body), &goimap.AppendOptions{Flags: flags}); err != nil {
				t.Fatalf("append to %q: %v", m.name, err)
			}
		}
	}
}

type literal struct {
	io.Reader
	size int64
}

func newLiteral(s string) literal {
	return literal{Reader: strings.NewReader(s), size: int64(len(s))}
}

func (l literal) Size() int64 { return l.size }

// testSession serves the seeded mailboxes through imapmemserver but reports
// special-use attributes itself, and can hang LIST.
type testSession struct {
	*imapmemserver.UserSession
	user *imapmemserver.User
	srv  *testServer
}

func (s *testSession) List(w *imapserver.ListWriter, ref string, patterns []string, options *goimap.ListOptions) error {
	if s.srv.hangList.Load() {
		<-s.srv.release
		return nil
	}
	if len(patterns) == 0 {
		return w.WriteList(&goimap.ListData{Attrs: []goimap.MailboxAttr{goimap.MailboxAttrNoSelect}, Delim: '/'})
	}
	for _, m := range seededMailboxes {
		matched := false
		for _, p := range patterns {
			matched = matched || imapserver.MatchList(m.name, '/', ref, p)
		}
		if !matched {
			continue
		}
		data := &goimap.ListData{Mailbox: m.name, Delim: '/', Attrs: m.attrs}
		if m.name == "Sent" && s.srv.noSentRole.Load() {
			data.Attrs = nil
		}
		if options.ReturnStatus != nil && !m.noselect {
			status, err := s.user.Status(m.name, options.ReturnStatus)
			if err != nil {
				return err
			}
			data.Status = status
		}
		if err := w.WriteList(data); err != nil {
			return err
		}
	}
	return nil
}

// trackingListener remembers accepted connections so tests can count and drop them.
type trackingListener struct {
	net.Listener

	mu    sync.Mutex
	conns []net.Conn
}

func (l *trackingListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err == nil {
		l.mu.Lock()
		l.conns = append(l.conns, conn)
		l.mu.Unlock()
	}
	return conn, err
}

func (l *trackingListener) accepted() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.conns)
}

func (l *trackingListener) dropAll() {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, c := range l.conns {
		c.Close()
	}
}

// silentListener accepts connections, optionally completes a TLS handshake,
// and then never sends anything.
func silentListener(t *testing.T, serverTLS *tls.Config) (host string, port int) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	var (
		mu    sync.Mutex
		conns []net.Conn
	)
	t.Cleanup(func() {
		ln.Close()
		mu.Lock()
		defer mu.Unlock()
		for _, c := range conns {
			c.Close()
		}
	})
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			if serverTLS != nil {
				conn = tls.Server(conn, serverTLS)
				go conn.(*tls.Conn).Handshake()
			}
			mu.Lock()
			conns = append(conns, conn)
			mu.Unlock()
		}
	}()
	addr := ln.Addr().(*net.TCPAddr)
	return addr.IP.String(), addr.Port
}

func selfSignedCert(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "dk-email test"},
		NotBefore:    time.Now().Add(-time.Hour),
		// Valid for 20 years like Proton Mail Bridge's cert, which macOS's
		// verifier rejects; tls_cert must not depend on that verifier.
		NotAfter:    time.Now().AddDate(20, 0, 0),
		KeyUsage:    x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, pool
}
