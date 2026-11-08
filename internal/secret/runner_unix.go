//go:build unix

package secret

import (
	"bytes"
	"context"
	"os/exec"
	"syscall"
	"time"
)

// shellRunner runs command with `sh -c`, inheriting the user's environment
// and with stdin closed, so it can never read from the TUI's terminal.
func shellRunner(ctx context.Context, command string) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	// Own process group, so a timeout kills pipelines and children such as `op`, not just sh.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = 2 * time.Second

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}
