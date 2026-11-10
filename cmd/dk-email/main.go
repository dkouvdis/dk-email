package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/dkouvdis/dk-email/internal/config"
	"github.com/dkouvdis/dk-email/internal/mail"
	"github.com/dkouvdis/dk-email/internal/mail/imap"
	"github.com/dkouvdis/dk-email/internal/secret"
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

// newConnector resolves each account's password, then logs in over IMAP.
// The debug log times both phases, never recording the password.
func newConnector(pw passwordResolver, log *slog.Logger) ui.Connector {
	return func(ctx context.Context, acct config.Account) (mail.Mailbox, error) {
		start := time.Now()
		password, err := pw.Resolve(ctx, acct)
		log.Debug("password resolved", "account", acct.Name, "ok", err == nil, "took", time.Since(start).Round(time.Millisecond))
		if errors.Is(err, secret.ErrNoPassword) {
			return nil, fmt.Errorf("%w: %w", ui.ErrNeedsPassword, err)
		}
		if err != nil {
			return nil, err
		}
		return dial(ctx, acct, password, log)
	}
}

// dial logs in over IMAP. It returns a nil interface, never a typed nil,
// on failure.
func dial(ctx context.Context, acct config.Account, password string, log *slog.Logger) (mail.Mailbox, error) {
	start := time.Now()
	c, err := imap.Dial(ctx, acct, password)
	log.Debug("imap login", "account", acct.Name, "ok", err == nil, "took", time.Since(start).Round(time.Millisecond))
	if err != nil {
		return nil, err
	}
	return c, nil
}

// newOnboarding lets the connect popup test and save accounts. A new
// account's keychain password is stored before the account is appended to
// configPath, so a saved account always has its password.
func newOnboarding(configPath string, pw passwordResolver, store func(account, password string) error, log *slog.Logger) ui.Onboarding {
	return ui.Onboarding{
		Dial: func(ctx context.Context, acct config.Account, password string) (mail.Mailbox, error) {
			acct = acct.WithDefaults()
			if password == "" {
				var err error
				if password, err = pw.Resolve(ctx, acct); err != nil {
					return nil, err
				}
			}
			return dial(ctx, acct, password, log)
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
