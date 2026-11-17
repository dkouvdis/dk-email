//go:build unix

package secret

import (
	"context"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestShellRunner_CapturesStdout(t *testing.T) {
	stdout, _, err := shellRunner(context.Background(), `printf 'pw\n'`)

	if err != nil || string(stdout) != "pw\n" {
		t.Errorf("shellRunner() = %q, %v; want %q", stdout, err, "pw\n")
	}
}

func TestShellRunner_InheritsEnvironment(t *testing.T) {
	t.Setenv("DK_EMAIL_TEST_VAR", "from-env")

	stdout, _, err := shellRunner(context.Background(), `printf %s "$DK_EMAIL_TEST_VAR"`)

	if err != nil || string(stdout) != "from-env" {
		t.Errorf("shellRunner() = %q, %v; want %q", stdout, err, "from-env")
	}
}

func TestCommandEnv_DropsGPGTTY(t *testing.T) {
	in := []string{"A=1", "GPG_TTY=/dev/ttys001", "GPG_TTY_X=1", "B=2"}

	got := commandEnv(in)

	want := []string{"A=1", "GPG_TTY_X=1", "B=2"}
	if !slices.Equal(got, want) {
		t.Errorf("commandEnv() = %q, want %q", got, want)
	}
	if in[1] != "GPG_TTY=/dev/ttys001" {
		t.Errorf("commandEnv() modified its input: %q", in)
	}
}

func TestShellRunner_DropsGPGTTY(t *testing.T) {
	t.Setenv("GPG_TTY", "/dev/ttys001")
	t.Setenv("DK_EMAIL_TEST_VAR", "from-env")

	stdout, _, err := shellRunner(context.Background(), `printf '%s %s' "${GPG_TTY-unset}" "$DK_EMAIL_TEST_VAR"`)

	if err != nil || string(stdout) != "unset from-env" {
		t.Errorf("shellRunner() = %q, %v; want %q", stdout, err, "unset from-env")
	}
}

func TestShellRunner_ReportsFailureWithStderr(t *testing.T) {
	_, stderr, err := shellRunner(context.Background(), `echo "not signed in" >&2; exit 3`)

	if err == nil {
		t.Fatal("shellRunner() error = nil, want exit error")
	}
	if !strings.Contains(string(stderr), "not signed in") {
		t.Errorf("stderr = %q, want it captured", stderr)
	}
}

// A pipeline keeps a grandchild alive after sh is killed; the whole process
// group must be killed so the call returns promptly on timeout.
func TestShellRunner_TimeoutKillsProcessGroup(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()

	_, _, err := shellRunner(ctx, `sleep 30 | cat`)

	if err == nil {
		t.Fatal("shellRunner() error = nil, want timeout error")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("shellRunner() took %s after timeout, want prompt return", elapsed)
	}
}

func TestResolve_RealShellCommand(t *testing.T) {
	r := New()
	acct := cmdAccount()
	acct.PasswordCmd = `printf '%s\n' ` + testPassword

	got, err := r.Resolve(context.Background(), acct)

	if err != nil || got != testPassword {
		t.Errorf("Resolve() = %q, %v; want %q", got, err, testPassword)
	}
}

// exitError returns a real *exec.ExitError with status 1.
func exitError(t *testing.T) error {
	t.Helper()
	err := exec.Command("sh", "-c", "exit 1").Run()
	if err == nil {
		t.Fatal("expected an exit error")
	}
	return err
}
