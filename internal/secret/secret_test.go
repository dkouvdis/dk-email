package secret

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/zalando/go-keyring"

	"github.com/dkouvdis/dk-email/internal/config"
)

const testPassword = "abcd-efgh-ijkl-mnop"

func fakeRunner(stdout, stderr string, err error) Runner {
	return func(context.Context, string) ([]byte, []byte, error) {
		return []byte(stdout), []byte(stderr), err
	}
}

func cmdAccount() config.Account {
	return config.Account{Name: "Personal", PasswordCmd: "op read op://Personal/Gmail/app-password"}
}

func TestResolve_PasswordCmdUsesFirstLineOfStdout(t *testing.T) {
	tests := []struct {
		name   string
		stdout string
	}{
		{"trailing newline", testPassword + "\n"},
		{"crlf", testPassword + "\r\n"},
		{"no newline", testPassword},
		{"pass-style extra lines", testPassword + "\nlogin: me@gmail.com\nurl: gmail.com\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &Resolver{Run: fakeRunner(tt.stdout, "", nil), Timeout: time.Second}

			got, err := r.Resolve(context.Background(), cmdAccount())

			if err != nil {
				t.Fatalf("Resolve() error = %v", err)
			}
			if got != testPassword {
				t.Errorf("Resolve() = %q, want %q", got, testPassword)
			}
		})
	}
}

func TestResolve_KeepsSpacesInsidePassword(t *testing.T) {
	r := &Resolver{Run: fakeRunner("  abcd efgh ijkl mnop \n", "", nil), Timeout: time.Second}

	got, err := r.Resolve(context.Background(), cmdAccount())

	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got != "  abcd efgh ijkl mnop " {
		t.Errorf("Resolve() = %q, want spaces preserved", got)
	}
}

func TestResolve_PassesCommandToRunner(t *testing.T) {
	var gotCmd string
	r := &Resolver{Timeout: time.Second, Run: func(_ context.Context, command string) ([]byte, []byte, error) {
		gotCmd = command
		return []byte(testPassword), nil, nil
	}}

	if _, err := r.Resolve(context.Background(), cmdAccount()); err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if want := cmdAccount().PasswordCmd; gotCmd != want {
		t.Errorf("runner got %q, want %q", gotCmd, want)
	}
}

func TestResolve_PasswordCmdTakesPrecedenceOverKeychain(t *testing.T) {
	keyring.MockInit()
	if err := Store("Personal", "from-keychain"); err != nil {
		t.Fatal(err)
	}
	r := &Resolver{Run: fakeRunner("from-command\n", "", nil), Timeout: time.Second}

	got, err := r.Resolve(context.Background(), cmdAccount())

	if err != nil || got != "from-command" {
		t.Errorf("Resolve() = %q, %v; want %q", got, err, "from-command")
	}
}

func TestResolve_CommandFailureIncludesStderrButNotStdout(t *testing.T) {
	r := &Resolver{
		Run:     fakeRunner(testPassword+"\n", "[ERROR] 2026/11/08 not signed in\n", errors.New("exit status 1")),
		Timeout: time.Second,
	}

	_, err := r.Resolve(context.Background(), cmdAccount())

	var cmdErr *CmdError
	if !errors.As(err, &cmdErr) {
		t.Fatalf("Resolve() error = %v, want *CmdError", err)
	}
	msg := err.Error()
	for _, want := range []string{`"Personal"`, "exit status 1", "not signed in"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q should contain %q", msg, want)
		}
	}
	if strings.Contains(msg, testPassword) {
		t.Errorf("error leaks the password: %q", msg)
	}
}

func TestResolve_EmptyOutputIsAnError(t *testing.T) {
	for _, stdout := range []string{"", "\n", "\r\n", "\nsecond-line\n"} {
		t.Run(stdout, func(t *testing.T) {
			r := &Resolver{Run: fakeRunner(stdout, "", nil), Timeout: time.Second}

			_, err := r.Resolve(context.Background(), cmdAccount())

			if !errors.Is(err, ErrEmptyOutput) {
				t.Errorf("Resolve() error = %v, want ErrEmptyOutput", err)
			}
		})
	}
}

func TestResolve_TimesOut(t *testing.T) {
	r := &Resolver{Timeout: 20 * time.Millisecond, Run: func(ctx context.Context, _ string) ([]byte, []byte, error) {
		<-ctx.Done()
		return nil, nil, ctx.Err()
	}}

	_, err := r.Resolve(context.Background(), cmdAccount())

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Resolve() error = %v, want DeadlineExceeded", err)
	}
	if !strings.Contains(err.Error(), "timed out after 20ms") {
		t.Errorf("error = %q, want it to mention the timeout", err)
	}
}

func TestResolve_ParentCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := &Resolver{Timeout: time.Minute, Run: func(ctx context.Context, _ string) ([]byte, []byte, error) {
		<-ctx.Done()
		return nil, nil, ctx.Err()
	}}

	_, err := r.Resolve(ctx, cmdAccount())

	if !errors.Is(err, context.Canceled) {
		t.Errorf("Resolve() error = %v, want context.Canceled", err)
	}
}

func TestResolve_StderrIsSanitizedAndTruncated(t *testing.T) {
	stderr := "\x1b[31mred\x1b[0m\x07 " + strings.Repeat("x", 1000)
	r := &Resolver{Run: fakeRunner("", stderr, errors.New("exit status 1")), Timeout: time.Second}

	_, err := r.Resolve(context.Background(), cmdAccount())

	msg := err.Error()
	if strings.ContainsAny(msg, "\x1b\x07") {
		t.Errorf("error contains control characters: %q", msg)
	}
	if len(msg) > 400 {
		t.Errorf("error is %d bytes, want stderr truncated", len(msg))
	}
}

func TestResolve_KeychainWhenNoPasswordCmd(t *testing.T) {
	keyring.MockInit()
	if err := Store("Work", testPassword); err != nil {
		t.Fatalf("Store() error = %v", err)
	}

	got, err := New().Resolve(context.Background(), config.Account{Name: "Work"})

	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got != testPassword {
		t.Errorf("Resolve() = %q, want %q", got, testPassword)
	}
}

func TestResolve_MissingKeychainEntryIsErrNoPassword(t *testing.T) {
	keyring.MockInit()

	_, err := New().Resolve(context.Background(), config.Account{Name: "Nobody"})

	if !errors.Is(err, ErrNoPassword) {
		t.Errorf("Resolve() error = %v, want ErrNoPassword", err)
	}
}

func TestResolve_KeychainFailureIsNotErrNoPassword(t *testing.T) {
	keyring.MockInitWithError(errors.New("keychain locked"))
	t.Cleanup(keyring.MockInit)

	_, err := New().Resolve(context.Background(), config.Account{Name: "Work"})

	if err == nil || errors.Is(err, ErrNoPassword) || !strings.Contains(err.Error(), "keychain locked") {
		t.Errorf("Resolve() error = %v, want the keychain error", err)
	}
}

func TestStore_RejectsEmptyValues(t *testing.T) {
	keyring.MockInit()

	if err := Store("", testPassword); err == nil {
		t.Error("Store() with empty account name: error = nil")
	}
	if err := Store("Work", ""); err == nil {
		t.Error("Store() with empty password: error = nil")
	}
}

func TestStore_ErrorDoesNotLeakPassword(t *testing.T) {
	keyring.MockInitWithError(errors.New("keychain locked"))
	t.Cleanup(keyring.MockInit)

	err := Store("Work", testPassword)

	if err == nil {
		t.Fatal("Store() error = nil, want error")
	}
	if strings.Contains(err.Error(), testPassword) {
		t.Errorf("error leaks the password: %q", err)
	}
}

func TestDelete_RemovesEntryAndIgnoresMissing(t *testing.T) {
	keyring.MockInit()
	if err := Store("Work", testPassword); err != nil {
		t.Fatal(err)
	}

	if err := Delete("Work"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := New().Resolve(context.Background(), config.Account{Name: "Work"}); !errors.Is(err, ErrNoPassword) {
		t.Errorf("entry still there: %v", err)
	}
	if err := Delete("Work"); err != nil {
		t.Errorf("Delete() of a missing entry error = %v", err)
	}
}

func TestDelete_KeychainFailureIsAnError(t *testing.T) {
	keyring.MockInitWithError(errors.New("keychain locked"))
	t.Cleanup(keyring.MockInit)

	if err := Delete("Work"); err == nil || !strings.Contains(err.Error(), "keychain locked") {
		t.Errorf("Delete() error = %v", err)
	}
}

func TestCopy_StoresUnderNewNameAndKeepsOld(t *testing.T) {
	keyring.MockInit()
	if err := Store("Old", testPassword); err != nil {
		t.Fatal(err)
	}

	if err := Copy("Old", "New"); err != nil {
		t.Fatalf("Copy() error = %v", err)
	}

	r := New()
	for _, name := range []string{"Old", "New"} {
		if got, err := r.Resolve(context.Background(), config.Account{Name: name}); err != nil || got != testPassword {
			t.Errorf("entry %s = %q, %v", name, got, err)
		}
	}
}

func TestCopy_NothingToCopyIsNotAnError(t *testing.T) {
	keyring.MockInit()

	if err := Copy("Old", "New"); err != nil {
		t.Errorf("Copy() error = %v", err)
	}
	if _, err := New().Resolve(context.Background(), config.Account{Name: "New"}); !errors.Is(err, ErrNoPassword) {
		t.Errorf("an entry appeared: %v", err)
	}
}
