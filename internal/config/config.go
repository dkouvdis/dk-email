// Package config loads and validates dk-email's config.toml.
package config

import (
	"crypto/x509"
	"errors"
	"fmt"
	"io/fs"
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
	Accounts []Account `toml:"account"`

	// Path is the file the config was loaded from.
	Path string `toml:"-"`
}

type UI struct {
	DefaultLayout   string   `toml:"default_layout"`
	RefreshInterval Duration `toml:"refresh_interval"`
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
// port for the TLS mode, and username = email. Used to dial an account
// before it's saved.
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
	if a.TLSCert != "" {
		if _, err := CertPool(a.TLSCert); err != nil {
			errs = append(errs, fmt.Errorf("tls_cert: %w", err))
		}
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
