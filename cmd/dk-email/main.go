package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
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

	_ = *configPath // consumed once config loading lands (T2/T8)
	fmt.Fprintln(stderr, "dk-email: the terminal interface is not implemented yet")
	return nil
}
