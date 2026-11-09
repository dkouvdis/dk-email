package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	tea "charm.land/bubbletea/v2"

	"github.com/dkouvdis/dk-email/internal/config"
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

	_, err = tea.NewProgram(ui.New(*cfg, log)).Run()
	return err
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
