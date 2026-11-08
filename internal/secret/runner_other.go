//go:build !unix

package secret

import (
	"bytes"
	"context"
	"os/exec"
	"time"
)

// shellRunner is a best-effort fallback; only unix platforms are supported releases.
func shellRunner(ctx context.Context, command string) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.WaitDelay = 2 * time.Second

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}
