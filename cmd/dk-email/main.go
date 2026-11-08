package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/dkouvdis/dk-email/internal/config"
)

func main() {
	err := run(os.Args[1:], os.Stdout, os.Stderr)
	switch {
	case err == nil, errors.Is(err, flag.ErrHelp):
		return
	default:
		fmt.Fprintln(os.Stderr, "dk-email:", err)
		os.Exit(2)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
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
	fmt.Fprintf(stderr, "dk-email: loaded %d account(s) from %s; the terminal interface is not implemented yet\n", len(cfg.Accounts), cfg.Path)
	return nil
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
