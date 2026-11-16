package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/dkouvdis/dk-email/internal/config"
	"github.com/dkouvdis/dk-email/internal/mail"
	"github.com/dkouvdis/dk-email/internal/mail/imap"
	"github.com/dkouvdis/dk-email/internal/mail/smtp"
	"github.com/dkouvdis/dk-email/internal/secret"
	"github.com/dkouvdis/dk-email/internal/send"
	"github.com/dkouvdis/dk-email/internal/ui"
)

func main() {
	err := run(os.Args[1:], os.Stdout, os.Stderr, startUI)
	switch {
	case err == nil, errors.Is(err, flag.ErrHelp):
		return
	default:
		fmt.Fprintln(os.Stderr, "dk-email:", err)
		os.Exit(2)
	}
}

// startFunc runs the interface with a loaded config. Tests pass a stub so
// run never takes over the terminal.
type startFunc func(cfg *config.Config) error

func run(args []string, stdout, stderr io.Writer, start startFunc) error {
	fs := flag.NewFlagSet("dk-email", flag.ContinueOnError)
	fs.SetOutput(stderr)
	showVersion := fs.Bool("version", false, "print version and exit")
	configPath := fs.String("config", "", "path to config.toml (default $XDG_CONFIG_HOME/dk-email/config.toml)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *showVersion {
		fmt.Fprintln(stdout, currentVersion())
		return nil
	}

	cfg, err := loadConfig(*configPath)
	if err != nil {
		return err
	}
	return start(cfg)
}

func startUI(cfg *config.Config) error {
	log, closeLog, err := openDebugLog(os.Getenv("DK_EMAIL_DEBUG") == "1")
	if err != nil {
		return err
	}
	defer closeLog()

	resolver := secret.New()
	model := ui.New(*cfg, newConnector(resolver, log), log).
		WithOnboarding(newOnboarding(cfg.Path, resolver, secret.Store, log))
	final, err := tea.NewProgram(model).Run()
	if m, ok := final.(ui.Model); ok {
		_ = m.Close()
	}
	return err
}

// passwordResolver is the part of *secret.Resolver the connector uses.
type passwordResolver interface {
	Resolve(ctx context.Context, acct config.Account) (string, error)
}

// newConnector resolves each account's password once, then logs in over IMAP
// and, if the account has an smtp_host, sets up sending with the same
// password. The debug log times both phases, never recording the password.
func newConnector(pw passwordResolver, log *slog.Logger) ui.Connector {
	return func(ctx context.Context, acct config.Account) (ui.Conn, error) {
		start := time.Now()
		password, err := pw.Resolve(ctx, acct)
		log.Debug("password resolved", "account", acct.Name, "ok", err == nil, "took", time.Since(start).Round(time.Millisecond))
		if errors.Is(err, secret.ErrNoPassword) {
			return ui.Conn{}, fmt.Errorf("%w: %w", ui.ErrNeedsPassword, err)
		}
		if err != nil {
			return ui.Conn{}, err
		}
		return dial(ctx, acct, password, log)
	}
}

// imapConn is what dial needs from an IMAP login: the mailbox for the UI and
// the store the sender saves copies and marks answers through.
type imapConn interface {
	mail.Mailbox
	send.Store
}

// dialIMAP logs in over IMAP; tests replace it to avoid the network. On
// failure it returns a nil interface, never a typed nil.
var dialIMAP = func(ctx context.Context, acct config.Account, password string) (imapConn, error) {
	c, err := imap.Dial(ctx, acct, password)
	if err != nil {
		return nil, err
	}
	return c, nil
}

// dial logs in over IMAP and, iff the account has an smtp_host, builds its
// sender around the same connection and password. It returns a zero Conn on
// failure.
func dial(ctx context.Context, acct config.Account, password string, log *slog.Logger) (ui.Conn, error) {
	start := time.Now()
	c, err := dialIMAP(ctx, acct, password)
	log.Debug("imap login", "account", acct.Name, "ok", err == nil, "took", time.Since(start).Round(time.Millisecond))
	if err != nil {
		return ui.Conn{}, err
	}
	conn := ui.Conn{Mailbox: c}
	if strings.TrimSpace(acct.SMTPHost) != "" {
		conn.Sender = send.New(acct, smtp.New(acct, password), c, log)
	}
	return conn, nil
}

// verifySMTP tests an SMTP login (connect and authenticate, nothing sent);
// tests replace it to avoid the network.
var verifySMTP = func(ctx context.Context, acct config.Account, password string) error {
	return smtp.New(acct, password).Verify(ctx)
}

// deleteSecret and copySecret change the OS keychain; tests replace them.
var (
	deleteSecret = secret.Delete
	copySecret   = secret.Copy
)

// newOnboarding lets the connect popup test and save accounts. A new
// account's keychain password is stored before the account is appended to
// configPath, so a saved account always has its password. VerifySMTP tests a
// new account's SMTP login, resolving the password like Dial does.
func newOnboarding(configPath string, pw passwordResolver, store func(account, password string) error, log *slog.Logger) ui.Onboarding {
	return ui.Onboarding{
		Dial: func(ctx context.Context, acct config.Account, password string) (ui.Conn, error) {
			acct = acct.WithDefaults()
			if password == "" {
				var err error
				if password, err = pw.Resolve(ctx, acct); err != nil {
					return ui.Conn{}, err
				}
			}
			return dial(ctx, acct, password, log)
		},
		VerifySMTP: func(ctx context.Context, acct config.Account, password string) error {
			acct = acct.WithDefaults()
			if password == "" {
				var err error
				if password, err = pw.Resolve(ctx, acct); err != nil {
					return err
				}
			}
			start := time.Now()
			err := verifySMTP(ctx, acct, password)
			log.Debug("smtp login", "account", acct.Name, "ok", err == nil, "took", time.Since(start).Round(time.Millisecond))
			return err
		},
		AddAccount: func(acct config.Account, password string) error {
			if password != "" {
				if err := store(acct.Name, password); err != nil {
					return err
				}
			}
			return config.AppendAccount(configPath, acct)
		},
		StorePassword: store,
		UpdateAccount: func(index int, oldName string, edit func(config.Account) config.Account, password string) error {
			// Learn the new name and password source up front: the keychain
			// entry is written before the config, so a saved account always
			// has its password.
			cfg, err := config.Load(configPath)
			if err != nil {
				return err
			}
			if index < 0 || index >= len(cfg.Accounts) || cfg.Accounts[index].Name != oldName {
				return fmt.Errorf("config changed on disk: account %q not found", oldName)
			}
			edited := edit(cfg.Accounts[index])
			renamed := edited.Name != oldName
			keychain := renamed && edited.PasswordCmd == ""
			switch {
			case password != "":
				if err := store(edited.Name, password); err != nil {
					return err
				}
			case keychain:
				// Copy first, so the config is only written once the new
				// name has its password; the old entry goes afterwards.
				if err := copySecret(oldName, edited.Name); err != nil {
					return err
				}
			}
			if err := config.ReplaceAccount(configPath, index, oldName, edit); err != nil {
				if renamed && (password != "" || keychain) {
					_ = deleteSecret(edited.Name) // best effort: don't leave an orphan
				}
				return err
			}
			if keychain {
				_ = deleteSecret(oldName) // the old entry is just left over
			}
			return nil
		},
		RemoveAccount: func(index int, acct config.Account, deletePassword bool) error {
			if err := config.RemoveAccount(configPath, index, acct.Name); err != nil {
				return err
			}
			if deletePassword && acct.PasswordCmd == "" {
				return deleteSecret(acct.Name)
			}
			return nil
		},
	}
}

// loadConfig loads the config from an explicit --config path, or from the
// default config dir, creating that dir and its .gitignore on first run.
func loadConfig(path string) (*config.Config, error) {
	if path == "" {
		dir, err := config.Dir()
		if err != nil {
			return nil, err
		}
		if err := config.EnsureDir(dir); err != nil {
			return nil, err
		}
		if path, err = config.DefaultPath(); err != nil {
			return nil, err
		}
	}
	return config.Load(path)
}
