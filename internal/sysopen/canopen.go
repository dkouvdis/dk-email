package sysopen

import (
	"os"
	"os/exec"
	"runtime"
)

// CanOpen reports whether Open would show something to the user here.
func CanOpen() bool {
	return canOpen(runtime.GOOS, os.Getenv, exec.LookPath)
}

func canOpen(goos string, getenv func(string) string, lookPath func(string) (string, error)) bool {
	// Over SSH the app would open on the remote machine's screen.
	if getenv("SSH_CONNECTION") != "" || getenv("SSH_TTY") != "" {
		return false
	}
	switch goos {
	case "darwin":
		_, err := lookPath("open")
		return err == nil
	case "windows":
		return true
	default:
		if getenv("DISPLAY") == "" && getenv("WAYLAND_DISPLAY") == "" {
			return false
		}
		_, err := lookPath("xdg-open")
		return err == nil
	}
}
