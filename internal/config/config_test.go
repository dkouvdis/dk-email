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
	"reflect"
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
	want := UI{DefaultLayout: "split", RefreshInterval: Duration{120 * time.Second}}
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
	wantUI := UI{DefaultLayout: "list", RefreshInterval: Duration{90 * time.Second}}
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
		if !reflect.DeepEqual(cfg.Accounts[i], want) {
			t.Errorf("Accounts[%d] = %+v, want %+v", i, cfg.Accounts[i], want)
		}
	}
}

func TestLoad_PartialUIKeepsOtherDefaults(t *testing.T) {
	path := writeConfig(t, "[ui]\nrefresh_interval = \"30s\"\n")

	cfg, err := Load(path)

	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	want := UI{DefaultLayout: "split", RefreshInterval: Duration{30 * time.Second}}
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
	path := writeConfig(t, "[ui]\ndefault_layout = \"list\"\nrefresh_interval = = 3\n")

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
		{"non-positive refresh", "[ui]\nrefresh_interval = \"0s\"\n", "ui.refresh_interval must be positive"},
		{"unparsable refresh", "[ui]\nrefresh_interval = \"soon\"\n", "invalid duration"},
		{"colour number out of range", "[colors]\naccent = 256\n", "colors.accent must be an ANSI colour number 0-255"},
		{"colour name", "[colors]\nstar = \"yellow\"\n", `colors.star must be an ANSI colour number 0-255 or a hex colour like "#ff00ff", got "yellow"`},
		{"short hex", "[colors]\nerror = \"#ff\"\n", "colors.error must be"},
		{"bad hex digit", "[colors]\nhint = \"#gg0000\"\n", "colors.hint must be"},
		{"unknown colour role", "[colors]\nbackground = \"0\"\n", `unknown key "colors.background"`},
		{"colour wrong type", "[colors]\nmuted = true\n", "colour must be a number or a string"},
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

func TestLoad_Colors(t *testing.T) {
	path := writeConfig(t, "[colors]\naccent = 5\non_accent = \"15\"\nmuted = \"#888\"\nborder = \"#3c3c3c\"\n")

	cfg, err := Load(path)

	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	want := Colors{Accent: "5", OnAccent: "15", Muted: "#888", Border: "#3c3c3c"}
	if cfg.Colors != want {
		t.Errorf("Colors = %+v, want %+v", cfg.Colors, want)
	}
}

func TestLoad_ReportsEveryProblemAtOnce(t *testing.T) {
	path := writeConfig(t, "[ui]\nrefresh_interval = \"0s\"\n[[account]]\nname = \"A\"\nimap_hots = \"h\"\n")

	_, err := Load(path)

	if err == nil {
		t.Fatal("Load() error = nil, want errors")
	}
	for _, want := range []string{"ui.refresh_interval", `unknown key "account.imap_hots"`, `account "A": imap_host is required`, `account "A": username or email is required`} {
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

const smtpBase = "[[account]]\nname = \"A\"\nemail = \"a@x.com\"\nimap_host = \"h\"\n"

func TestLoad_NoSMTPKeysLeavesSendingDisabled(t *testing.T) {
	cfg, err := Load(writeConfig(t, smtpBase))

	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	got := cfg.Accounts[0]
	if got.SMTPHost != "" || got.SMTPPort != 0 || got.SMTPTLS != "" || got.FromName != "" || got.SaveSent != nil {
		t.Errorf("SMTP fields set on an account without smtp keys: %+v", got)
	}
}

func TestLoad_SMTPDefaults(t *testing.T) {
	tests := []struct {
		name     string
		keys     string
		wantPort int
		wantTLS  string
	}{
		{"implicit by default on 465", `smtp_host = "s"`, 465, "implicit"},
		{"explicit implicit on 465", "smtp_host = \"s\"\nsmtp_tls = \"implicit\"", 465, "implicit"},
		{"starttls on 587", "smtp_host = \"s\"\nsmtp_tls = \"starttls\"", 587, "starttls"},
		{"explicit port kept", "smtp_host = \"s\"\nsmtp_tls = \"starttls\"\nsmtp_port = 2525", 2525, "starttls"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := Load(writeConfig(t, smtpBase+tt.keys+"\n"))

			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			got := cfg.Accounts[0]
			if got.SMTPHost != "s" || got.SMTPPort != tt.wantPort || got.SMTPTLS != tt.wantTLS {
				t.Errorf("got host=%q port=%d tls=%q, want port=%d tls=%q", got.SMTPHost, got.SMTPPort, got.SMTPTLS, tt.wantPort, tt.wantTLS)
			}
		})
	}
}

func TestAccount_WithDefaultsSMTP(t *testing.T) {
	if got := (Account{SMTPHost: "s"}).WithDefaults(); got.SMTPPort != 465 || got.SMTPTLS != TLSImplicit {
		t.Errorf("with smtp_host: port=%d tls=%q, want 465 implicit", got.SMTPPort, got.SMTPTLS)
	}
	if got := (Account{}).WithDefaults(); got.SMTPPort != 0 || got.SMTPTLS != "" {
		t.Errorf("without smtp_host: port=%d tls=%q, want none", got.SMTPPort, got.SMTPTLS)
	}
}

func TestAccount_SaveSentCopy(t *testing.T) {
	yes, no := true, false
	tests := []struct {
		name string
		acct Account
		want bool
	}{
		{"gmail defaults to false", Account{IMAPHost: "imap.gmail.com"}, false},
		{"gmail host ignores case and spaces", Account{IMAPHost: " IMAP.Gmail.com "}, false},
		{"other host defaults to true", Account{IMAPHost: "imap.example.com"}, true},
		{"explicit true wins on gmail", Account{IMAPHost: "imap.gmail.com", SaveSent: &yes}, true},
		{"explicit false wins elsewhere", Account{IMAPHost: "imap.example.com", SaveSent: &no}, false},
		{"exchange online defaults to false", Account{IMAPHost: "outlook.office365.com"}, false},
		{"outlook.com imap defaults to false", Account{IMAPHost: "imap-mail.outlook.com"}, false},
		{"office365 smtp host defaults to false", Account{IMAPHost: "mail.example.com", SMTPHost: "smtp.office365.com"}, false},
		{"outlook.com smtp host ignores case and spaces", Account{IMAPHost: "mail.example.com", SMTPHost: " SMTP-Mail.Outlook.com "}, false},
		{"gmail smtp alone does not count", Account{IMAPHost: "mail.example.com", SMTPHost: "smtp.gmail.com"}, true},
		{"explicit true wins on exchange online", Account{IMAPHost: "outlook.office365.com", SaveSent: &yes}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.acct.SaveSentCopy(); got != tt.want {
				t.Errorf("SaveSentCopy() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLoad_SMTPValidationErrors(t *testing.T) {
	tests := []struct {
		name string
		keys string
		want []string
	}{
		{"port without host", "smtp_port = 465", []string{`account "A": smtp_port is set but smtp_host is empty`}},
		{"tls without host", `smtp_tls = "implicit"`, []string{`account "A": smtp_tls is set but smtp_host is empty`}},
		{"from_name without host", `from_name = "Me"`, []string{`account "A": from_name is set but smtp_host is empty`}},
		{"save_sent without host", "save_sent = false", []string{`account "A": save_sent is set but smtp_host is empty`}},
		{"every orphan key reported", "smtp_port = 465\nsmtp_tls = \"bogus\"\nfrom_name = \"Me\"\nsave_sent = true", []string{
			"smtp_port is set but smtp_host is empty", "smtp_tls is set but smtp_host is empty",
			"from_name is set but smtp_host is empty", "save_sent is set but smtp_host is empty"}},
		{"unknown smtp_tls", "smtp_host = \"s\"\nsmtp_tls = \"ssl\"", []string{`account "A": smtp_tls must be "implicit" or "starttls", got "ssl"`}},
		{"port too high", "smtp_host = \"s\"\nsmtp_port = 70000", []string{`account "A": smtp_port must be between 1 and 65535, got 70000`}},
		{"negative port", "smtp_host = \"s\"\nsmtp_port = -1", []string{"smtp_port must be between 1 and 65535, got -1"}},
		{"tls and port together", "smtp_host = \"s\"\nsmtp_tls = \"ssl\"\nsmtp_port = 99999", []string{
			`smtp_tls must be "implicit" or "starttls", got "ssl"`, "smtp_port must be between 1 and 65535, got 99999"}},
		{"smtp errors join imap errors", "smtp_host = \"s\"\nsmtp_port = 99999\ntls = \"ssl\"", []string{
			`tls must be "implicit" or "starttls"`, "smtp_port must be between 1 and 65535, got 99999"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(writeConfig(t, smtpBase+tt.keys+"\n"))

			if err == nil {
				t.Fatalf("Load() error = nil, want %q", tt.want)
			}
			for _, want := range tt.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error = %q, want it to contain %q", err, want)
				}
			}
		})
	}
}

func TestLoad_Aliases(t *testing.T) {
	cfg, err := Load(writeConfig(t, smtpBase+"smtp_host = \"s\"\naliases = [{ email = \"c@x.com\", name = \"C\" }, { email = \"b@x.com\" }]\n"))

	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	want := []Alias{{Email: "c@x.com", Name: "C"}, {Email: "b@x.com"}}
	if got := cfg.Accounts[0].Aliases; !reflect.DeepEqual(got, want) {
		t.Errorf("Aliases = %+v, want %+v", got, want)
	}
}

func TestLoad_NoAliasesIsNil(t *testing.T) {
	cfg, err := Load(writeConfig(t, smtpBase))

	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := cfg.Accounts[0].Aliases; got != nil {
		t.Errorf("Aliases = %+v, want nil", got)
	}
}

func TestLoad_AliasValidationErrors(t *testing.T) {
	tests := []struct {
		name string
		keys string
		want string
	}{
		{"without host", `aliases = [{ email = "a@x.com" }]`, "aliases is set but smtp_host is empty"},
		{"not an address", "smtp_host = \"s\"\naliases = [{ email = \"nope\" }]", `aliases[0]: email "nope" is not a valid address`},
		{"display name form", "smtp_host = \"s\"\naliases = [{ email = \"A <a@x.com>\" }]", `aliases[0]: email "A <a@x.com>" is not a valid address`},
		{"empty", "smtp_host = \"s\"\naliases = [{ name = \"A\" }]", `aliases[0]: email "" is not a valid address`},
		{"account email", "smtp_host = \"s\"\naliases = [{ email = \"A@X.com\" }]", `aliases[0]: "A@X.com" is already the account email`},
		{"listed twice", "smtp_host = \"s\"\naliases = [{ email = \"b@x.com\" }, { email = \"B@x.com\" }]", `aliases[1]: "B@x.com" is listed twice`},
		{"unknown key", "smtp_host = \"s\"\naliases = [{ email = \"b@x.com\", nmae = \"B\" }]", "nmae"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(writeConfig(t, smtpBase+tt.keys+"\n"))

			if err == nil {
				t.Fatalf("Load() error = nil, want %q", tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %q, want it to contain %q", err, tt.want)
			}
		})
	}
}

func TestLoad_OAuth(t *testing.T) {
	tests := []struct {
		name       string
		keys       string
		wantAuth   string
		wantOAuth  bool
		wantTenant string
	}{
		{"microsoft defaults the tenant", "auth = \"oauth2\"\noauth_provider = \"microsoft\"\noauth_client_id = \"cid\"", AuthOAuth2, true, "common"},
		{"microsoft keeps its tenant", "auth = \"oauth2\"\noauth_provider = \"microsoft\"\noauth_client_id = \"cid\"\noauth_tenant = \"contoso.onmicrosoft.com\"", AuthOAuth2, true, "contoso.onmicrosoft.com"},
		{"google with a secret command", "auth = \"oauth2\"\noauth_provider = \"google\"\noauth_client_id = \"cid\"\noauth_client_secret_cmd = \"op read op://P/G/secret\"", AuthOAuth2, true, ""},
		{"external token command", "auth = \"oauth2\"\npassword_cmd = \"oama access me@x.com\"", AuthOAuth2, true, ""},
		{"explicit password auth", "auth = \"password\"", AuthPassword, false, ""},
		{"no auth key", "", "", false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := Load(writeConfig(t, smtpBase+tt.keys+"\n"))

			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			a := cfg.Accounts[0]
			if a.Auth != tt.wantAuth {
				t.Errorf("Auth = %q, want %q", a.Auth, tt.wantAuth)
			}
			if a.OAuth() != tt.wantOAuth {
				t.Errorf("OAuth() = %v, want %v", a.OAuth(), tt.wantOAuth)
			}
			if a.OAuthTenant != tt.wantTenant {
				t.Errorf("OAuthTenant = %q, want %q", a.OAuthTenant, tt.wantTenant)
			}
		})
	}
}

func TestLoad_OAuthValidationErrors(t *testing.T) {
	const ms = "auth = \"oauth2\"\noauth_provider = \"microsoft\"\noauth_client_id = \"cid\"\n"
	tests := []struct {
		name string
		keys string
		want []string
	}{
		{"unknown auth", `auth = "kerberos"`, []string{`account "A": auth must be "password" or "oauth2", got "kerberos"`}},
		{"unknown auth skips other auth checks", "auth = \"kerberos\"\noauth_provider = \"google\"", []string{`auth must be "password" or "oauth2", got "kerberos"`}},
		{"provider without oauth2", `oauth_provider = "google"`, []string{`account "A": oauth_provider is set but auth is not "oauth2"`}},
		{"client id without oauth2", `oauth_client_id = "cid"`, []string{`oauth_client_id is set but auth is not "oauth2"`}},
		{"tenant without oauth2", `oauth_tenant = "common"`, []string{`oauth_tenant is set but auth is not "oauth2"`}},
		{"secret cmd without oauth2", `oauth_client_secret_cmd = "x"`, []string{`oauth_client_secret_cmd is set but auth is not "oauth2"`}},
		{"orphan keys with explicit password auth", "auth = \"password\"\noauth_provider = \"google\"", []string{`oauth_provider is set but auth is not "oauth2"`}},
		{"provider with password_cmd", "auth = \"oauth2\"\npassword_cmd = \"c\"\noauth_provider = \"google\"", []string{`oauth_provider is set with password_cmd: an oauth2 account takes its token from password_cmd or the oauth_* keys, not both`}},
		{"client id with password_cmd", "auth = \"oauth2\"\npassword_cmd = \"c\"\noauth_client_id = \"cid\"", []string{`oauth_client_id is set with password_cmd: an oauth2 account takes its token from password_cmd or the oauth_* keys, not both`}},
		{"tenant with password_cmd", "auth = \"oauth2\"\npassword_cmd = \"c\"\noauth_tenant = \"t\"", []string{`oauth_tenant is set with password_cmd: an oauth2 account takes its token from password_cmd or the oauth_* keys, not both`}},
		{"secret cmd with password_cmd", "auth = \"oauth2\"\npassword_cmd = \"c\"\noauth_client_secret_cmd = \"x\"", []string{`oauth_client_secret_cmd is set with password_cmd: an oauth2 account takes its token from password_cmd or the oauth_* keys, not both`}},
		{"missing provider", "auth = \"oauth2\"\noauth_client_id = \"cid\"", []string{`oauth_provider is required when auth is "oauth2" (or set password_cmd to an external token command)`}},
		{"bad provider", "auth = \"oauth2\"\noauth_provider = \"yahoo\"\noauth_client_id = \"cid\"", []string{`oauth_provider must be "microsoft" or "google", got "yahoo"`}},
		{"missing client id", "auth = \"oauth2\"\noauth_provider = \"google\"", []string{`oauth_client_id is required when auth is "oauth2"`}},
		{"blank client id", "auth = \"oauth2\"\noauth_provider = \"google\"\noauth_client_id = \"  \"", []string{`oauth_client_id is required when auth is "oauth2"`}},
		{"tenant with google", "auth = \"oauth2\"\noauth_provider = \"google\"\noauth_client_id = \"cid\"\noauth_tenant = \"common\"", []string{`oauth_tenant is only used with oauth_provider "microsoft"`}},
		{"secret cmd with microsoft", ms + `oauth_client_secret_cmd = "x"`, []string{`oauth_client_secret_cmd is only used with oauth_provider "google"`}},
		{"several errors reported", "auth = \"oauth2\"\noauth_provider = \"google\"\noauth_tenant = \"common\"", []string{
			`oauth_client_id is required when auth is "oauth2"`, `oauth_tenant is only used with oauth_provider "microsoft"`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(writeConfig(t, smtpBase+tt.keys+"\n"))

			if err == nil {
				t.Fatalf("Load() error = nil, want %q", tt.want)
			}
			for _, want := range tt.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error = %q, want it to contain %q", err, want)
				}
			}
		})
	}
}

func TestLoad_RejectsPlaintextOAuthClientSecretWithHint(t *testing.T) {
	const secret = "GOCSPX-supersecretvalue"
	path := writeConfig(t, smtpBase+"oauth_client_secret = \""+secret+"\"\n")

	_, err := Load(path)

	if err == nil {
		t.Fatal("Load() error = nil, want error for oauth_client_secret key")
	}
	if !strings.Contains(err.Error(), "oauth_client_secret_cmd") {
		t.Errorf("error = %q, want a hint to use oauth_client_secret_cmd", err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Errorf("error leaks the secret: %q", err)
	}
}

func TestLoad_NotifyDefaultsOn(t *testing.T) {
	want := Notify{Bell: true, Desktop: true, Title: true}
	if got := Default().Notify; got != want {
		t.Errorf("Default().Notify = %+v, want %+v", got, want)
	}
	cfg, err := Load(writeConfig(t, "[ui]\nrefresh_interval = \"30s\"\n"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Notify != want {
		t.Errorf("Notify = %+v, want %+v", cfg.Notify, want)
	}
}

func TestLoad_NotifyPartialKeepsOthersOn(t *testing.T) {
	cfg, err := Load(writeConfig(t, "[notify]\nbell = false\n"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	want := Notify{Bell: false, Desktop: true, Title: true}
	if cfg.Notify != want {
		t.Errorf("Notify = %+v, want %+v", cfg.Notify, want)
	}
}

func TestLoad_NotifyRejectsUnknownKey(t *testing.T) {
	_, err := Load(writeConfig(t, "[notify]\nsound = true\n"))
	if err == nil || !strings.Contains(err.Error(), "notify.sound") {
		t.Errorf("Load() error = %v, want unknown key notify.sound", err)
	}
}
