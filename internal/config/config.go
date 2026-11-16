// Package config loads and validates dk-email's config.toml.
package config

import (
	"crypto/x509"
	"errors"
	"fmt"
	"io/fs"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

const (
	LayoutSplit = "split"
	LayoutList  = "list"

	TLSImplicit = "implicit"
	TLSStartTLS = "starttls"
)

type Config struct {
	UI       UI        `toml:"ui"`
	Colors   Colors    `toml:"colors"`
	Accounts []Account `toml:"account"`

	// Path is the file the config was loaded from.
	Path string `toml:"-"`
}

type UI struct {
	DefaultLayout   string   `toml:"default_layout"`
	RefreshInterval Duration `toml:"refresh_interval"`
}

// Alias is another address the account can send as, such as one on the user's own domain.
type Alias struct {
	Email string `toml:"email"`
	// Name is optional and falls back to from_name; empty both means address only.
	Name string `toml:"name"`
}

// Account describes one IMAP account. There is deliberately no password field:
// passwords come from PasswordCmd or the OS keychain.
type Account struct {
	Name          string `toml:"name"`
	Email         string `toml:"email"`
	Username      string `toml:"username"`
	IMAPHost      string `toml:"imap_host"`
	IMAPPort      int    `toml:"imap_port"`
	TLS           string `toml:"tls"`
	PasswordCmd   string `toml:"password_cmd"`
	ArchiveFolder string `toml:"archive_folder"`
	// TLSCert is a PEM file of certificates to trust for this account instead
	// of the system roots, e.g. Proton Mail Bridge's exported cert.pem. Load
	// makes it absolute: "~/" is expanded and a relative path is resolved
	// from the config file's folder.
	TLSCert string `toml:"tls_cert"`

	// SMTPHost enables sending for the account; empty keeps it read-only.
	// SMTPPort and SMTPTLS default (to 465/587 and "implicit") only when it
	// is set, and FromName and SaveSent need it.
	SMTPHost string `toml:"smtp_host"`
	SMTPPort int    `toml:"smtp_port"`
	SMTPTLS  string `toml:"smtp_tls"`
	// FromName is the display name on outgoing mail; empty means address only.
	FromName string `toml:"from_name"`
	// Aliases are other addresses the account sends as through the same SMTP
	// login. They need smtp_host.
	Aliases []Alias `toml:"aliases"`
	// SaveSent overrides whether a copy of sent mail is appended to the Sent
	// folder; nil means use the SaveSentCopy default.
	SaveSent *bool `toml:"save_sent"`
}

// SaveSentCopy reports whether sent mail should be appended to the Sent
// folder over IMAP. An explicit save_sent wins; otherwise it is false for
// imap.gmail.com, because Gmail already files SMTP-submitted mail in Sent,
// and true everywhere else.
func (a Account) SaveSentCopy() bool {
	if a.SaveSent != nil {
		return *a.SaveSent
	}
	return !strings.EqualFold(strings.TrimSpace(a.IMAPHost), "imap.gmail.com")
}

func (a Account) smtpEnabled() bool {
	return strings.TrimSpace(a.SMTPHost) != ""
}

// Duration reads TOML strings such as "120s" or "2m".
type Duration struct{ time.Duration }

func (d *Duration) UnmarshalText(b []byte) error {
	v, err := time.ParseDuration(string(b))
	if err != nil {
		return fmt.Errorf("invalid duration %q (use a value like \"120s\" or \"2m\")", b)
	}
	d.Duration = v
	return nil
}

func (d Duration) MarshalText() ([]byte, error) {
	return []byte(d.String()), nil
}

// Default returns the config used when no file exists.
func Default() Config {
	return Config{UI: UI{
		DefaultLayout:   LayoutSplit,
		RefreshInterval: Duration{120 * time.Second},
	}}
}

// Load reads and validates the config at path. A missing file is not an
// error: it yields Default() with no accounts (first run).
func Load(path string) (*Config, error) {
	cfg := Default()
	cfg.Path = path

	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return &cfg, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	return parse(path, data)
}

// parse decodes and validates config file contents. path is used for
// messages and to resolve relative tls_cert paths.
func parse(path string, data []byte) (*Config, error) {
	cfg := Default()
	cfg.Path = path

	md, err := toml.Decode(string(data), &cfg)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	unknownErr := checkUndecoded(md.Undecoded())

	for i := range cfg.Accounts {
		cfg.Accounts[i].applyDefaults()
		cfg.Accounts[i].TLSCert = resolvePath(cfg.Accounts[i].TLSCert, filepath.Dir(path))
	}
	if err := errors.Join(unknownErr, cfg.validate()); err != nil {
		return nil, fmt.Errorf("invalid config %s: %w", path, err)
	}
	return &cfg, nil
}

// checkUndecoded rejects unknown keys so typos such as "pasword_cmd" fail
// loudly instead of silently falling back to the keychain.
func checkUndecoded(keys []toml.Key) error {
	var errs []error
	for _, k := range keys {
		if len(k) == 2 && k[0] == "account" && k[1] == "password" {
			errs = append(errs, errors.New(`"password" is not supported: passwords are never stored in the config file; set password_cmd (e.g. "op read op://…") or omit it to use the OS keychain`))
			continue
		}
		errs = append(errs, fmt.Errorf("unknown key %q", k.String()))
	}
	return errors.Join(errs...)
}

// WithDefaults returns a with the defaults Load applies: implicit TLS, the
// port for the TLS mode, and username = email, plus the SMTP TLS mode and
// port when smtp_host is set. Used to dial an account before it's saved.
func (a Account) WithDefaults() Account {
	a.applyDefaults()
	return a
}

func (a *Account) applyDefaults() {
	if a.TLS == "" {
		a.TLS = TLSImplicit
	}
	if a.IMAPPort == 0 {
		switch a.TLS {
		case TLSImplicit:
			a.IMAPPort = 993
		case TLSStartTLS:
			a.IMAPPort = 143
		}
	}
	if a.Username == "" {
		a.Username = a.Email
	}
	if !a.smtpEnabled() {
		return
	}
	if a.SMTPTLS == "" {
		a.SMTPTLS = TLSImplicit
	}
	if a.SMTPPort == 0 {
		switch a.SMTPTLS {
		case TLSImplicit:
			a.SMTPPort = 465
		case TLSStartTLS:
			a.SMTPPort = 587
		}
	}
}

func (c *Config) validate() error {
	var errs []error

	switch c.UI.DefaultLayout {
	case LayoutSplit, LayoutList:
	default:
		errs = append(errs, fmt.Errorf("ui.default_layout must be %q or %q, got %q", LayoutSplit, LayoutList, c.UI.DefaultLayout))
	}
	if c.UI.RefreshInterval.Duration <= 0 {
		errs = append(errs, fmt.Errorf("ui.refresh_interval must be positive, got %s", c.UI.RefreshInterval))
	}
	errs = append(errs, c.Colors.validate()...)

	seen := make(map[string]bool, len(c.Accounts))
	for i, a := range c.Accounts {
		label := fmt.Sprintf("account %q", a.Name)
		if strings.TrimSpace(a.Name) == "" {
			label = fmt.Sprintf("account #%d", i+1)
		}
		for _, err := range a.validate() {
			errs = append(errs, fmt.Errorf("%s: %w", label, err))
		}
		key := strings.ToLower(strings.TrimSpace(a.Name))
		if key != "" && seen[key] {
			errs = append(errs, fmt.Errorf("duplicate account name %q", a.Name))
		}
		seen[key] = true
	}
	return errors.Join(errs...)
}

func (a Account) validate() []error {
	var errs []error
	if strings.TrimSpace(a.Name) == "" {
		errs = append(errs, errors.New("name is required"))
	}
	if strings.TrimSpace(a.IMAPHost) == "" {
		errs = append(errs, errors.New("imap_host is required"))
	}
	if strings.TrimSpace(a.Username) == "" {
		errs = append(errs, errors.New("username or email is required"))
	}
	switch a.TLS {
	case TLSImplicit, TLSStartTLS:
	default:
		errs = append(errs, fmt.Errorf("tls must be %q or %q, got %q", TLSImplicit, TLSStartTLS, a.TLS))
	}
	if a.IMAPPort < 1 || a.IMAPPort > 65535 {
		errs = append(errs, fmt.Errorf("imap_port must be between 1 and 65535, got %d", a.IMAPPort))
	}
	errs = append(errs, a.validateSMTP()...)
	if a.TLSCert != "" {
		if _, err := CertPool(a.TLSCert); err != nil {
			errs = append(errs, fmt.Errorf("tls_cert: %w", err))
		}
	}
	return errs
}

func (a Account) validateSMTP() []error {
	var errs []error
	if !a.smtpEnabled() {
		for _, k := range []struct {
			key string
			set bool
		}{
			{"smtp_port", a.SMTPPort != 0},
			{"smtp_tls", a.SMTPTLS != ""},
			{"from_name", a.FromName != ""},
			{"aliases", len(a.Aliases) > 0},
			{"save_sent", a.SaveSent != nil},
		} {
			if k.set {
				errs = append(errs, fmt.Errorf("%s is set but smtp_host is empty", k.key))
			}
		}
		return errs
	}
	errs = append(errs, a.validateAliases()...)
	switch a.SMTPTLS {
	case TLSImplicit, TLSStartTLS:
	default:
		errs = append(errs, fmt.Errorf("smtp_tls must be %q or %q, got %q", TLSImplicit, TLSStartTLS, a.SMTPTLS))
	}
	if a.SMTPPort < 1 || a.SMTPPort > 65535 {
		errs = append(errs, fmt.Errorf("smtp_port must be between 1 and 65535, got %d", a.SMTPPort))
	}
	return errs
}

// validateAliases checks each alias is a bare address and that none repeats
// another alias or the account email, ignoring case.
func (a Account) validateAliases() []error {
	var errs []error
	seen := map[string]bool{}
	if e := strings.ToLower(strings.TrimSpace(a.Email)); e != "" {
		seen[e] = true
	}
	for i, al := range a.Aliases {
		email := strings.TrimSpace(al.Email)
		parsed, err := mail.ParseAddress(email)
		if err != nil || parsed.Address != email {
			errs = append(errs, fmt.Errorf("aliases[%d]: email %q is not a valid address", i, al.Email))
			continue
		}
		key := strings.ToLower(email)
		switch {
		case key == strings.ToLower(strings.TrimSpace(a.Email)):
			errs = append(errs, fmt.Errorf("aliases[%d]: %q is already the account email", i, al.Email))
		case seen[key]:
			errs = append(errs, fmt.Errorf("aliases[%d]: %q is listed twice", i, al.Email))
		}
		seen[key] = true
	}
	return errs
}

// CertPool reads a PEM file into a pool holding only its certificates.
func CertPool(path string) (*x509.CertPool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(data) {
		return nil, fmt.Errorf("%s contains no PEM certificate", path)
	}
	return pool, nil
}

// resolvePath expands a leading "~/" and makes a relative path relative to
// base. An empty path stays empty.
func resolvePath(p, base string) string {
	switch {
	case p == "":
		return ""
	case p == "~" || strings.HasPrefix(p, "~/"):
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	case !filepath.IsAbs(p):
		return filepath.Join(base, p)
	}
	return p
}
