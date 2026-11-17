package smtp

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/emersion/go-sasl"
	gosmtp "github.com/emersion/go-smtp"

	"github.com/dkouvdis/dk-email/internal/config"
	"github.com/dkouvdis/dk-email/internal/mail"
)

const (
	testUser     = "me@example.com"
	testPassword = "s3cret-pw"
	badRcpt      = "nobody@example.com"
)

// received records what the fake server saw.
type received struct {
	mu    sync.Mutex
	auths int
	from  string
	rcpts []string
	data  []byte
	mails int
	// tokens are the XOAUTH2 tokens the server accepts; tokensSeen records
	// the accepted ones in order.
	tokens     map[string]bool
	tokensSeen []string
}

func (r *received) snapshot() received {
	r.mu.Lock()
	defer r.mu.Unlock()
	return received{auths: r.auths, from: r.from, rcpts: append([]string(nil), r.rcpts...), data: r.data, mails: r.mails, tokensSeen: append([]string(nil), r.tokensSeen...)}
}

type backend struct{ rec *received }

func (b backend) NewSession(*gosmtp.Conn) (gosmtp.Session, error) {
	return &session{rec: b.rec}, nil
}

type session struct{ rec *received }

func (s *session) AuthMechanisms() []string { return []string{sasl.Plain, "XOAUTH2"} }

// xoauth2Server is a test-only XOAUTH2 sasl.Server. A bad token gets the JSON
// error challenge, and the client's empty answer then fails the login.
type xoauth2Server struct {
	rec        *received
	challenged bool
	token      string
}

func (x *xoauth2Server) Next(resp []byte) ([]byte, bool, error) {
	if x.challenged {
		return nil, false, &gosmtp.SMTPError{Code: 535, EnhancedCode: gosmtp.EnhancedCode{5, 7, 8}, Message: "bad token " + x.token}
	}
	var user, tok string
	for _, p := range strings.Split(string(resp), "\x01") {
		if v, ok := strings.CutPrefix(p, "user="); ok {
			user = v
		}
		if v, ok := strings.CutPrefix(p, "auth=Bearer "); ok {
			tok = v
		}
	}
	x.rec.mu.Lock()
	ok := user == testUser && x.rec.tokens[tok]
	if ok {
		x.rec.tokensSeen = append(x.rec.tokensSeen, tok)
	}
	x.rec.mu.Unlock()
	if ok {
		return nil, true, nil
	}
	x.challenged, x.token = true, tok
	challenge, _ := json.Marshal(map[string]string{"status": "400", "schemes": "Bearer", "scope": "https://mail.example.com/"})
	return challenge, false, nil
}

func (s *session) Auth(mech string) (sasl.Server, error) {
	if mech == "XOAUTH2" {
		return &xoauth2Server{rec: s.rec}, nil
	}
	return sasl.NewPlainServer(func(_, user, pass string) error {
		s.rec.mu.Lock()
		s.rec.auths++
		s.rec.mu.Unlock()
		if user != testUser || pass != testPassword {
			return &gosmtp.SMTPError{Code: 535, EnhancedCode: gosmtp.EnhancedCode{5, 7, 8}, Message: "bad credentials " + pass}
		}
		return nil
	}), nil
}

func (s *session) Mail(from string, _ *gosmtp.MailOptions) error {
	s.rec.mu.Lock()
	defer s.rec.mu.Unlock()
	s.rec.mails++
	s.rec.from = from
	return nil
}

func (s *session) Rcpt(to string, _ *gosmtp.RcptOptions) error {
	if to == badRcpt {
		return &gosmtp.SMTPError{Code: 550, EnhancedCode: gosmtp.EnhancedCode{5, 1, 1}, Message: "no such user"}
	}
	s.rec.mu.Lock()
	defer s.rec.mu.Unlock()
	s.rec.rcpts = append(s.rec.rcpts, to)
	return nil
}

func (s *session) Data(r io.Reader) error {
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	s.rec.mu.Lock()
	defer s.rec.mu.Unlock()
	s.rec.data = b
	return nil
}

func (s *session) Reset()        {}
func (s *session) Logout() error { return nil }

// startServer runs a go-smtp server on 127.0.0.1. With implicit it listens
// over TLS; otherwise it offers STARTTLS when starttls is set and no TLS at
// all when it isn't.
func startServer(t *testing.T, cert tls.Certificate, implicit, starttls bool) (port int, rec *received) {
	t.Helper()
	rec = &received{}
	srv := gosmtp.NewServer(backend{rec})
	srv.Domain = "localhost"
	srv.ReadTimeout = 5 * time.Second
	srv.WriteTimeout = 5 * time.Second
	srv.AllowInsecureAuth = !implicit && !starttls
	tlsCfg := &tls.Config{Certificates: []tls.Certificate{cert}}
	if implicit || starttls {
		srv.TLSConfig = tlsCfg
	}

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	if implicit {
		l = tls.NewListener(l, tlsCfg)
	}
	go srv.Serve(l)
	t.Cleanup(func() { srv.Close() })
	return l.Addr().(*net.TCPAddr).Port, rec
}

func testTransport(t *testing.T, pool *x509.CertPool, port int, mode, password string) *Transport {
	t.Helper()
	tr := New(config.Account{
		Name:     "Test",
		Username: testUser,
		SMTPHost: "127.0.0.1",
		SMTPPort: port,
		SMTPTLS:  mode,
	}, password)
	tr.tlsConfig = &tls.Config{RootCAs: pool}
	return tr
}

var testMsg = []byte("From: me@example.com\r\nTo: you@example.com\r\nSubject: hi\r\n\r\nHello,\r\n.dot line\r\nbye\r\n")

func TestSend(t *testing.T) {
	cert, pool := selfSignedCert(t)
	tests := []struct {
		name     string
		mode     string
		implicit bool
	}{
		{"implicit TLS", config.TLSImplicit, true},
		{"STARTTLS", config.TLSStartTLS, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			port, rec := startServer(t, cert, tt.implicit, !tt.implicit)
			tr := testTransport(t, pool, port, tt.mode, testPassword)

			rcpts := []string{"you@example.com", "cc@example.com"}
			if err := tr.Send(context.Background(), testUser, rcpts, testMsg); err != nil {
				t.Fatalf("Send() error = %v", err)
			}

			got := rec.snapshot()
			if got.from != testUser {
				t.Errorf("from = %q, want %q", got.from, testUser)
			}
			if strings.Join(got.rcpts, ",") != strings.Join(rcpts, ",") {
				t.Errorf("rcpts = %q, want %q", got.rcpts, rcpts)
			}
			if !bytes.Equal(got.data, testMsg) {
				t.Errorf("data = %q, want %q", got.data, testMsg)
			}
		})
	}
}

func TestSend_AuthFailure(t *testing.T) {
	cert, pool := selfSignedCert(t)
	port, rec := startServer(t, cert, true, false)
	tr := testTransport(t, pool, port, config.TLSImplicit, "wrong-pw-1234")

	err := tr.Send(context.Background(), testUser, []string{"you@example.com"}, testMsg)
	if err == nil {
		t.Fatal("Send() error = nil, want auth failure")
	}
	for _, want := range []string{`"Test"`, "127.0.0.1", "535"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not contain %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "wrong-pw-1234") {
		t.Errorf("error %q leaks the password", err)
	}
	if got := rec.snapshot(); got.mails != 0 {
		t.Errorf("server saw %d MAIL commands after failed auth", got.mails)
	}
}

func TestSend_RejectedRecipient(t *testing.T) {
	cert, pool := selfSignedCert(t)
	port, _ := startServer(t, cert, true, false)
	tr := testTransport(t, pool, port, config.TLSImplicit, testPassword)

	err := tr.Send(context.Background(), testUser, []string{"you@example.com", badRcpt}, testMsg)
	if err == nil {
		t.Fatal("Send() error = nil, want rejected recipient")
	}
	if !strings.Contains(err.Error(), badRcpt) {
		t.Errorf("error %q does not name %q", err, badRcpt)
	}
	var smtpErr *gosmtp.SMTPError
	if !errors.As(err, &smtpErr) || smtpErr.Code != 550 {
		t.Errorf("error %v does not wrap the 550 SMTPError", err)
	}
}

func TestSend_NoRecipients(t *testing.T) {
	tr := New(config.Account{SMTPHost: "127.0.0.1", SMTPPort: 1, SMTPTLS: config.TLSImplicit}, "")
	if err := tr.Send(context.Background(), testUser, nil, testMsg); err == nil {
		t.Error("Send() with no recipients: error = nil, want error")
	}
}

func TestSend_NoSMTPHost(t *testing.T) {
	tr := New(config.Account{Name: "Test"}, "")
	err := tr.Send(context.Background(), testUser, []string{"you@example.com"}, testMsg)
	if err == nil || !strings.Contains(err.Error(), "smtp_host") {
		t.Errorf("Send() error = %v, want one mentioning smtp_host", err)
	}
}

func TestSend_StartTLSNotOffered(t *testing.T) {
	cert, pool := selfSignedCert(t)
	port, rec := startServer(t, cert, false, false)
	tr := testTransport(t, pool, port, config.TLSStartTLS, testPassword)

	err := tr.Send(context.Background(), testUser, []string{"you@example.com"}, testMsg)
	if err == nil {
		t.Fatal("Send() error = nil, want failure when STARTTLS isn't offered")
	}
	if !strings.Contains(err.Error(), "STARTTLS") {
		t.Errorf("error %q does not mention STARTTLS", err)
	}
	if got := rec.snapshot(); got.auths != 0 || got.mails != 0 {
		t.Errorf("server saw auths=%d mails=%d, want none", got.auths, got.mails)
	}
}

func TestSend_UntrustedCert(t *testing.T) {
	cert, _ := selfSignedCert(t)
	port, _ := startServer(t, cert, true, false)
	tr := testTransport(t, nil, port, config.TLSImplicit, testPassword)

	err := tr.Send(context.Background(), testUser, []string{"you@example.com"}, testMsg)
	if err == nil || !strings.Contains(err.Error(), "TLS handshake") {
		t.Errorf("Send() error = %v, want TLS handshake failure", err)
	}
}

func TestSend_UnknownMode(t *testing.T) {
	cert, pool := selfSignedCert(t)
	port, _ := startServer(t, cert, true, false)
	tr := testTransport(t, pool, port, "plain", testPassword)

	err := tr.Verify(context.Background())
	if err == nil || !strings.Contains(err.Error(), "unknown smtp_tls") {
		t.Errorf("Verify() error = %v, want unknown smtp_tls mode", err)
	}
}

func TestSend_ConnectFailure(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()

	tr := testTransport(t, nil, port, config.TLSImplicit, testPassword)
	err = tr.Verify(context.Background())
	if err == nil || !strings.Contains(err.Error(), "send via 127.0.0.1:") {
		t.Errorf("Verify() error = %v, want wrapped connect error", err)
	}
}

func TestSend_ContextCancelled(t *testing.T) {
	// A server that accepts and never speaks stalls the TLS handshake.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			t.Cleanup(func() { c.Close() })
		}
	}()

	tr := testTransport(t, nil, l.Addr().(*net.TCPAddr).Port, config.TLSImplicit, testPassword)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)

	start := time.Now()
	err = tr.Send(ctx, testUser, []string{"you@example.com"}, testMsg)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Send() error = %v, want context.Canceled", err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("Send() took %v after cancellation, want prompt return", elapsed)
	}
}

func TestVerify(t *testing.T) {
	cert, pool := selfSignedCert(t)
	port, rec := startServer(t, cert, true, false)

	if err := testTransport(t, pool, port, config.TLSImplicit, testPassword).Verify(context.Background()); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if got := rec.snapshot(); got.auths != 1 || got.mails != 0 {
		t.Errorf("server saw auths=%d mails=%d, want 1 and 0", got.auths, got.mails)
	}

	err := testTransport(t, pool, port, config.TLSImplicit, "nope").Verify(context.Background())
	if err == nil || !strings.Contains(err.Error(), `"Test"`) {
		t.Errorf("Verify() with wrong password error = %v, want one naming the account", err)
	}
}

func TestTLSCertFile(t *testing.T) {
	cert, _ := selfSignedCert(t)
	port, _ := startServer(t, cert, true, false)

	path := filepath.Join(t.TempDir(), "server.pem")
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Certificate[0]})
	if err := os.WriteFile(path, pemBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	tr := testTransport(t, nil, port, config.TLSImplicit, testPassword)
	tr.acct.TLSCert = path
	if err := tr.Verify(context.Background()); err != nil {
		t.Fatalf("Verify() with tls_cert error = %v", err)
	}

	tr.acct.TLSCert = filepath.Join(t.TempDir(), "missing.pem")
	err := tr.Verify(context.Background())
	if err == nil || !strings.Contains(err.Error(), "tls_cert") {
		t.Errorf("Verify() with missing tls_cert error = %v, want tls_cert error", err)
	}
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
		NotAfter:     time.Now().AddDate(20, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
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

func tokenTransport(t *testing.T, pool *x509.CertPool, port int, creds mail.Credentials) *Transport {
	t.Helper()
	tr := NewWithCredentials(config.Account{
		Name:     "Test",
		Username: testUser,
		SMTPHost: "127.0.0.1",
		SMTPPort: port,
		SMTPTLS:  config.TLSImplicit,
	}, creds)
	tr.tlsConfig = &tls.Config{RootCAs: pool}
	return tr
}

func TestSendAndVerify_OAuthToken(t *testing.T) {
	cert, pool := selfSignedCert(t)
	port, rec := startServer(t, cert, true, false)
	rec.tokens = map[string]bool{"tok-good": true}
	tr := tokenTransport(t, pool, port, func(context.Context) (mail.Login, error) { return mail.Login{Token: "tok-good"}, nil })

	if err := tr.Verify(context.Background()); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if err := tr.Send(context.Background(), testUser, []string{"you@example.com"}, testMsg); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	got := rec.snapshot()
	if want := []string{"tok-good", "tok-good"}; !slices.Equal(got.tokensSeen, want) {
		t.Errorf("server saw tokens %v, want %v", got.tokensSeen, want)
	}
	if got.mails != 1 {
		t.Errorf("server saw %d MAIL commands, want 1", got.mails)
	}
}

func TestSend_FetchesCredentialsForEverySend(t *testing.T) {
	cert, pool := selfSignedCert(t)
	port, rec := startServer(t, cert, true, false)
	rec.tokens = map[string]bool{"tok-1": true, "tok-2": true}
	var calls int
	tr := tokenTransport(t, pool, port, func(context.Context) (mail.Login, error) {
		calls++
		return mail.Login{Token: fmt.Sprintf("tok-%d", calls)}, nil
	})

	for i := range 2 {
		if err := tr.Send(context.Background(), testUser, []string{"you@example.com"}, testMsg); err != nil {
			t.Fatalf("Send() #%d error = %v", i+1, err)
		}
	}

	if calls != 2 {
		t.Errorf("credentials called %d times, want 2", calls)
	}
	if want := []string{"tok-1", "tok-2"}; !slices.Equal(rec.snapshot().tokensSeen, want) {
		t.Errorf("server saw tokens %v, want %v", rec.snapshot().tokensSeen, want)
	}
}

func TestSend_OAuthRejectedTokenShowsDetailWithoutToken(t *testing.T) {
	cert, pool := selfSignedCert(t)
	port, rec := startServer(t, cert, true, false)
	tr := tokenTransport(t, pool, port, func(context.Context) (mail.Login, error) { return mail.Login{Token: "tok-bad"}, nil })

	err := tr.Send(context.Background(), testUser, []string{"you@example.com"}, testMsg)

	if err == nil {
		t.Fatal("Send() error = nil, want auth failure")
	}
	for _, want := range []string{"535", "xoauth2: server rejected the access token (status 400)"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not contain %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "tok-bad") {
		t.Errorf("error %q leaks the token", err)
	}
	if got := rec.snapshot(); got.mails != 0 {
		t.Errorf("server saw %d MAIL commands after failed auth", got.mails)
	}
}

func TestSend_CredentialsErrorKeepsChain(t *testing.T) {
	cert, pool := selfSignedCert(t)
	port, _ := startServer(t, cert, true, false)
	sentinel := errors.New("refresh failed")
	tr := tokenTransport(t, pool, port, func(context.Context) (mail.Login, error) { return mail.Login{}, sentinel })

	err := tr.Send(context.Background(), testUser, []string{"you@example.com"}, testMsg)

	if !errors.Is(err, sentinel) {
		t.Fatalf("Send() error = %v, want it to wrap the credentials error", err)
	}
	for _, want := range []string{"send via", `credentials for account "Test"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not contain %q", err, want)
		}
	}
}
