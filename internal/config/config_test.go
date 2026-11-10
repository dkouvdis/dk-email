package config

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoad_MissingFileReturnsDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")

	cfg, err := Load(path)

	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.Accounts) != 0 {
		t.Errorf("Accounts = %v, want none", cfg.Accounts)
	}
	want := UI{DefaultLayout: "split", WideBreakpoint: 120, ListWidth: 35, RefreshInterval: Duration{120 * time.Second}}
	if cfg.UI != want {
		t.Errorf("UI = %+v, want %+v", cfg.UI, want)
	}
	if cfg.Path != path {
		t.Errorf("Path = %q, want %q", cfg.Path, path)
	}
}

func TestLoad_FullExample(t *testing.T) {
	cfg, err := Load("testdata/full.toml")

	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	wantUI := UI{DefaultLayout: "list", WideBreakpoint: 140, ListWidth: 40, RefreshInterval: Duration{90 * time.Second}}
	if cfg.UI != wantUI {
		t.Errorf("UI = %+v, want %+v", cfg.UI, wantUI)
	}
	wantAccounts := []Account{
		{
			Name: "Personal", Email: "me@gmail.com", Username: "me@gmail.com",
			IMAPHost: "imap.gmail.com", IMAPPort: 993, TLS: "implicit",
			PasswordCmd: "op read op://Personal/Gmail/app-password",
		},
		{
			Name: "Work", Email: "me@example.com", Username: "me@example.com",
			IMAPHost: "mail.example.com", IMAPPort: 143, TLS: "starttls",
			ArchiveFolder: "Archive",
		},
	}
	if len(cfg.Accounts) != len(wantAccounts) {
		t.Fatalf("got %d accounts, want %d", len(cfg.Accounts), len(wantAccounts))
	}
	for i, want := range wantAccounts {
		if cfg.Accounts[i] != want {
			t.Errorf("Accounts[%d] = %+v, want %+v", i, cfg.Accounts[i], want)
		}
	}
}

func TestLoad_PartialUIKeepsOtherDefaults(t *testing.T) {
	path := writeConfig(t, "[ui]\nlist_width = 50\n")

	cfg, err := Load(path)

	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	want := UI{DefaultLayout: "split", WideBreakpoint: 120, ListWidth: 50, RefreshInterval: Duration{120 * time.Second}}
	if cfg.UI != want {
		t.Errorf("UI = %+v, want %+v", cfg.UI, want)
	}
}

func TestLoad_AccountDefaults(t *testing.T) {
	tests := []struct {
		name     string
		account  string
		wantPort int
		wantTLS  string
		wantUser string
	}{
		{"tls defaults to implicit on 993", `name = "A"` + "\n" + `email = "a@x.com"` + "\n" + `imap_host = "h"`, 993, "implicit", "a@x.com"},
		{"starttls defaults to 143", `name = "A"` + "\n" + `email = "a@x.com"` + "\n" + `imap_host = "h"` + "\n" + `tls = "starttls"`, 143, "starttls", "a@x.com"},
		{"explicit port and username kept", `name = "A"` + "\n" + `email = "a@x.com"` + "\n" + `username = "alice"` + "\n" + `imap_host = "h"` + "\n" + `imap_port = 1993`, 1993, "implicit", "alice"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeConfig(t, "[[account]]\n"+tt.account+"\n")

			cfg, err := Load(path)

			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			got := cfg.Accounts[0]
			if got.IMAPPort != tt.wantPort || got.TLS != tt.wantTLS || got.Username != tt.wantUser {
				t.Errorf("got port=%d tls=%q user=%q, want port=%d tls=%q user=%q",
					got.IMAPPort, got.TLS, got.Username, tt.wantPort, tt.wantTLS, tt.wantUser)
			}
		})
	}
}

func TestLoad_InvalidTOMLReportsLine(t *testing.T) {
	path := writeConfig(t, "[ui]\nlist_width = 40\nwide_breakpoint = = 3\n")

	_, err := Load(path)

	if err == nil {
		t.Fatal("Load() error = nil, want parse error")
	}
	if !strings.Contains(err.Error(), "line 3") {
		t.Errorf("error = %q, want it to mention line 3", err)
	}
}

func TestLoad_RejectsPlaintextPasswordWithHint(t *testing.T) {
	const secret = "hunter2-app-password"
	path := writeConfig(t, `[[account]]
name = "A"
email = "a@x.com"
imap_host = "h"
password = "`+secret+`"
`)

	_, err := Load(path)

	if err == nil {
		t.Fatal("Load() error = nil, want error for password key")
	}
	if !strings.Contains(err.Error(), "password_cmd") {
		t.Errorf("error = %q, want a hint to use password_cmd", err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Errorf("error leaks the password: %q", err)
	}
}

func TestLoad_RejectsUnknownKeys(t *testing.T) {
	path := writeConfig(t, `[[account]]
name = "A"
email = "a@x.com"
imap_host = "h"
pasword_cmd = "op read x"
`)

	_, err := Load(path)

	if err == nil {
		t.Fatal("Load() error = nil, want error for unknown key")
	}
	if !strings.Contains(err.Error(), "pasword_cmd") {
		t.Errorf("error = %q, want it to name the unknown key", err)
	}
}

func TestLoad_ValidationErrors(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{"missing name", "[[account]]\nemail = \"a@x.com\"\nimap_host = \"h\"\n", "name is required"},
		{"missing host", "[[account]]\nname = \"A\"\nemail = \"a@x.com\"\n", "imap_host is required"},
		{"missing username and email", "[[account]]\nname = \"A\"\nimap_host = \"h\"\n", "username or email is required"},
		{"unknown tls", "[[account]]\nname = \"A\"\nemail = \"a@x.com\"\nimap_host = \"h\"\ntls = \"ssl\"\n", `tls must be "implicit" or "starttls"`},
		{"port out of range", "[[account]]\nname = \"A\"\nemail = \"a@x.com\"\nimap_host = \"h\"\nimap_port = 70000\n", "imap_port must be between 1 and 65535"},
		{"duplicate names ignore case", "[[account]]\nname = \"Work\"\nemail = \"a@x.com\"\nimap_host = \"h\"\n[[account]]\nname = \"work\"\nemail = \"b@x.com\"\nimap_host = \"h\"\n", `duplicate account name "work"`},
		{"unknown layout", "[ui]\ndefault_layout = \"grid\"\n", `ui.default_layout must be "split" or "list"`},
		{"breakpoint below forced list width", "[ui]\nwide_breakpoint = 60\n", "ui.wide_breakpoint must be at least 80"},
		{"list width too small", "[ui]\nlist_width = 5\n", "ui.list_width must be between 10 and 90"},
		{"list width too large", "[ui]\nlist_width = 95\n", "ui.list_width must be between 10 and 90"},
		{"non-positive refresh", "[ui]\nrefresh_interval = \"0s\"\n", "ui.refresh_interval must be positive"},
		{"unparsable refresh", "[ui]\nrefresh_interval = \"soon\"\n", "invalid duration"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeConfig(t, tt.content)

			_, err := Load(path)

			if err == nil {
				t.Fatalf("Load() error = nil, want %q", tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %q, want it to contain %q", err, tt.want)
			}
		})
	}
}

func TestLoad_ReportsEveryProblemAtOnce(t *testing.T) {
	path := writeConfig(t, "[ui]\nlist_width = 5\n[[account]]\nname = \"A\"\nimap_hots = \"h\"\n")

	_, err := Load(path)

	if err == nil {
		t.Fatal("Load() error = nil, want errors")
	}
	for _, want := range []string{"ui.list_width", `unknown key "account.imap_hots"`, `account "A": imap_host is required`, `account "A": username or email is required`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to contain %q", err, want)
		}
	}
}

func TestLoad_UnreadableFile(t *testing.T) {
	_, err := Load(t.TempDir())

	if err == nil {
		t.Fatal("Load() on a directory error = nil, want error")
	}
}

// testCertPEM writes a self-signed certificate as PEM and returns its bytes.
func testCertPEM(t *testing.T) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "127.0.0.1"}, NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func TestLoad_TLSCertResolvesPaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cert := testCertPEM(t)
	if err := os.WriteFile(filepath.Join(home, "bridge.pem"), cert, 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name, value string
		want        func(cfgDir string) string
	}{
		{"home", "~/bridge.pem", func(string) string { return filepath.Join(home, "bridge.pem") }},
		{"absolute", filepath.Join(home, "bridge.pem"), func(string) string { return filepath.Join(home, "bridge.pem") }},
		{"relative to the config folder", "certs/bridge.pem", func(dir string) string { return filepath.Join(dir, "certs", "bridge.pem") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeConfig(t, "[[account]]\nname = \"Proton\"\nemail = \"me@pm.me\"\nimap_host = \"127.0.0.1\"\nimap_port = 1143\ntls = \"starttls\"\ntls_cert = \""+tt.value+"\"\n")
			dir := filepath.Dir(path)
			if err := os.MkdirAll(filepath.Join(dir, "certs"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "certs", "bridge.pem"), cert, 0o600); err != nil {
				t.Fatal(err)
			}

			cfg, err := Load(path)

			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if got := cfg.Accounts[0].TLSCert; got != tt.want(dir) {
				t.Errorf("TLSCert = %q, want %q", got, tt.want(dir))
			}
		})
	}
}

func TestLoad_TLSCertMustBeReadablePEM(t *testing.T) {
	dir := t.TempDir()
	garbage := filepath.Join(dir, "garbage.pem")
	if err := os.WriteFile(garbage, []byte("not a certificate"), 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct{ value, want string }{
		{filepath.Join(dir, "missing.pem"), "no such file"},
		{garbage, "contains no PEM certificate"},
	}
	for _, tt := range tests {
		path := writeConfig(t, "[[account]]\nname = \"Proton\"\nemail = \"me@pm.me\"\nimap_host = \"127.0.0.1\"\ntls_cert = \""+tt.value+"\"\n")

		_, err := Load(path)

		if err == nil || !strings.Contains(err.Error(), `account "Proton": tls_cert: `) || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("Load() error = %v, want a tls_cert error containing %q", err, tt.want)
		}
	}
}

func TestCertPool(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.pem")
	if err := os.WriteFile(path, testCertPEM(t), 0o600); err != nil {
		t.Fatal(err)
	}
	if pool, err := CertPool(path); err != nil || pool == nil {
		t.Errorf("CertPool() = %v, %v", pool, err)
	}
}

func TestResolvePath(t *testing.T) {
	if got := resolvePath("", "/base"); got != "" {
		t.Errorf("empty = %q", got)
	}
	if got := resolvePath("~", "/base"); got == "~" || got == "" {
		t.Errorf("~ = %q, want the home directory", got)
	}
}

func TestAccount_WithDefaults(t *testing.T) {
	a := Account{Email: "me@example.com", TLS: TLSStartTLS}

	got := a.WithDefaults()

	if got.IMAPPort != 143 || got.Username != "me@example.com" || a.IMAPPort != 0 {
		t.Errorf("WithDefaults() = %+v (original %+v)", got, a)
	}
}
