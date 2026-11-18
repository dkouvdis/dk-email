// Package sysopen opens files and URLs with the system's default app.
package sysopen

import (
	"os/exec"
	"runtime"
)

// Open hands target (a file path or URL) to the system's default app. It
// returns once the app has started; it doesn't wait for it to exit.
func Open(target string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", target)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	default: // Linux and the BSDs
		cmd = exec.Command("xdg-open", target)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }() // reap it; the app outlives us anyway
	return nil
}
