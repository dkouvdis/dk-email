package secret

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/zalando/go-keyring"

	"github.com/dkouvdis/dk-email/internal/config"
	"github.com/dkouvdis/dk-email/internal/oauth"
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

var testToken = oauth.Token{
	AccessToken:  "access-aaa",
	RefreshToken: "refresh-rrr",
	Expiry:       time.Date(2026, 11, 17, 12, 30, 0, 0, time.UTC),
}

func clientSecretAccount() config.Account {
	return config.Account{Name: "Work", OAuthClientSecretCmd: "op read op://Work/Google/client-secret"}
}

func TestResolve_PasswordCmdErrorMessageUnchanged(t *testing.T) {
	r := &Resolver{Run: fakeRunner("", "boom", errors.New("exit status 1")), Timeout: time.Second}

	_, err := r.Resolve(context.Background(), cmdAccount())

	if err == nil || !strings.HasPrefix(err.Error(), `password_cmd for account "`) {
		t.Errorf("Resolve() error = %v, want password_cmd prefix", err)
	}
}

func TestToken_RoundTrip(t *testing.T) {
	keyring.MockInit()

	if err := StoreToken("Work", testToken); err != nil {
		t.Fatalf("StoreToken() error = %v", err)
	}
	got, err := LoadToken("Work")

	if err != nil {
		t.Fatalf("LoadToken() error = %v", err)
	}
	if got.AccessToken != testToken.AccessToken || got.RefreshToken != testToken.RefreshToken || !got.Expiry.Equal(testToken.Expiry) {
		t.Errorf("LoadToken() = %+v, want %+v", got, testToken)
	}
}

func TestToken_NamespacedFromPassword(t *testing.T) {
	keyring.MockInit()
	if err := Store("Work", "pw"); err != nil {
		t.Fatal(err)
	}
	if err := StoreToken("Work", testToken); err != nil {
		t.Fatal(err)
	}
	if err := StoreClientSecret("Work", "cs"); err != nil {
		t.Fatal(err)
	}

	got, err := New().Resolve(context.Background(), config.Account{Name: "Work"})
	if err != nil || got != "pw" {
		t.Errorf("Resolve() = %q, %v; want pw", got, err)
	}
	tok, err := LoadToken("Work")
	if err != nil || tok.RefreshToken != testToken.RefreshToken {
		t.Errorf("LoadToken() = %+v, %v", tok, err)
	}
	if raw, err := keyring.Get(keychainService, "Work"); err != nil || raw != "pw" {
		t.Errorf("password entry = %q, %v; want exactly pw", raw, err)
	}

	if err := DeleteToken("Work"); err != nil {
		t.Fatal(err)
	}
	if got, err := New().Resolve(context.Background(), config.Account{Name: "Work"}); err != nil || got != "pw" {
		t.Errorf("password after DeleteToken = %q, %v", got, err)
	}
	if got, err := New().ResolveClientSecret(context.Background(), config.Account{Name: "Work"}); err != nil || got != "cs" {
		t.Errorf("client secret after DeleteToken = %q, %v", got, err)
	}

	if err := StoreToken("Work", testToken); err != nil {
		t.Fatal(err)
	}
	if err := Delete("Work"); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadToken("Work"); err != nil {
		t.Errorf("token after Delete = %v", err)
	}
}

func TestLoadToken_MissingIsErrNoToken(t *testing.T) {
	keyring.MockInit()

	_, err := LoadToken("Work")

	if !errors.Is(err, ErrNoToken) {
		t.Errorf("LoadToken() error = %v, want ErrNoToken", err)
	}
}

func TestLoadToken_CorruptJSONIsErrNoTokenWithoutLeak(t *testing.T) {
	keyring.MockInit()
	if err := keyring.Set(keychainService, "oauth:Work", "{not json secret-xyz"); err != nil {
		t.Fatal(err)
	}

	_, err := LoadToken("Work")

	if !errors.Is(err, ErrNoToken) {
		t.Fatalf("LoadToken() error = %v, want ErrNoToken", err)
	}
	if strings.Contains(err.Error(), "secret-xyz") {
		t.Errorf("error leaks stored bytes: %q", err)
	}
}

func TestLoadToken_NoRefreshTokenIsErrNoToken(t *testing.T) {
	keyring.MockInit()
	if err := keyring.Set(keychainService, "oauth:Work", `{"access_token":"a","refresh_token":""}`); err != nil {
		t.Fatal(err)
	}

	_, err := LoadToken("Work")

	if !errors.Is(err, ErrNoToken) {
		t.Errorf("LoadToken() error = %v, want ErrNoToken", err)
	}
}

func TestLoadToken_KeychainFailureIsNotErrNoToken(t *testing.T) {
	keyring.MockInitWithError(errors.New("keychain locked"))
	t.Cleanup(keyring.MockInit)

	_, err := LoadToken("Work")

	if err == nil || errors.Is(err, ErrNoToken) || !strings.Contains(err.Error(), "keychain locked") {
		t.Errorf("LoadToken() error = %v, want the keychain error", err)
	}
}

func TestStoreToken_RejectsEmptyValues(t *testing.T) {
	keyring.MockInit()

	if err := StoreToken("", testToken); err == nil {
		t.Error("StoreToken() with empty account name: error = nil")
	}
	if err := StoreToken("Work", oauth.Token{AccessToken: "a"}); err == nil {
		t.Error("StoreToken() with empty refresh token: error = nil")
	}
}

func TestStoreToken_ErrorDoesNotLeakTokens(t *testing.T) {
	keyring.MockInitWithError(errors.New("keychain locked"))
	t.Cleanup(keyring.MockInit)

	err := StoreToken("Work", testToken)

	if err == nil {
		t.Fatal("StoreToken() error = nil, want error")
	}
	for _, secret := range []string{testToken.AccessToken, testToken.RefreshToken} {
		if strings.Contains(err.Error(), secret) {
			t.Errorf("error leaks %q: %q", secret, err)
		}
	}
}

func TestDeleteToken_IgnoresMissingAndReportsFailure(t *testing.T) {
	keyring.MockInit()
	if err := DeleteToken("Work"); err != nil {
		t.Errorf("DeleteToken() of a missing entry error = %v", err)
	}
	if err := DeleteClientSecret("Work"); err != nil {
		t.Errorf("DeleteClientSecret() of a missing entry error = %v", err)
	}

	keyring.MockInitWithError(errors.New("keychain locked"))
	t.Cleanup(keyring.MockInit)
	if err := DeleteToken("Work"); err == nil || !strings.Contains(err.Error(), "keychain locked") {
		t.Errorf("DeleteToken() error = %v", err)
	}
	if err := DeleteClientSecret("Work"); err == nil || !strings.Contains(err.Error(), "keychain locked") {
		t.Errorf("DeleteClientSecret() error = %v", err)
	}
}

func TestStoreClientSecret_RejectsEmptyValues(t *testing.T) {
	keyring.MockInit()

	if err := StoreClientSecret("", "cs"); err == nil {
		t.Error("StoreClientSecret() with empty account name: error = nil")
	}
	if err := StoreClientSecret("Work", ""); err == nil {
		t.Error("StoreClientSecret() with empty secret: error = nil")
	}
}

func TestResolveClientSecret_CommandWinsOverKeychain(t *testing.T) {
	keyring.MockInit()
	if err := StoreClientSecret("Work", "from-keychain"); err != nil {
		t.Fatal(err)
	}
	var gotCmd string
	r := &Resolver{Timeout: time.Second, Run: func(_ context.Context, command string) ([]byte, []byte, error) {
		gotCmd = command
		return []byte("from-command\nextra\n"), nil, nil
	}}

	got, err := r.ResolveClientSecret(context.Background(), clientSecretAccount())

	if err != nil || got != "from-command" {
		t.Errorf("ResolveClientSecret() = %q, %v; want from-command", got, err)
	}
	if want := clientSecretAccount().OAuthClientSecretCmd; gotCmd != want {
		t.Errorf("runner got %q, want %q", gotCmd, want)
	}
}

func TestResolveClientSecret_CommandFailure(t *testing.T) {
	r := &Resolver{
		Run:     fakeRunner("stdout-secret\n", "not signed in\n", errors.New("exit status 1")),
		Timeout: time.Second,
	}

	_, err := r.ResolveClientSecret(context.Background(), clientSecretAccount())

	var cmdErr *CmdError
	if !errors.As(err, &cmdErr) {
		t.Fatalf("ResolveClientSecret() error = %v, want *CmdError", err)
	}
	if cmdErr.Key != "oauth_client_secret_cmd" {
		t.Errorf("Key = %q, want oauth_client_secret_cmd", cmdErr.Key)
	}
	msg := err.Error()
	if !strings.HasPrefix(msg, "oauth_client_secret_cmd for account") {
		t.Errorf("error %q has the wrong prefix", msg)
	}
	if !strings.Contains(msg, "not signed in") {
		t.Errorf("error %q should contain stderr", msg)
	}
	if strings.Contains(msg, "stdout-secret") {
		t.Errorf("error leaks stdout: %q", msg)
	}
}

func TestResolveClientSecret_EmptyOutputIsAnError(t *testing.T) {
	r := &Resolver{Run: fakeRunner("\n", "", nil), Timeout: time.Second}

	_, err := r.ResolveClientSecret(context.Background(), clientSecretAccount())

	if !errors.Is(err, ErrEmptyOutput) {
		t.Errorf("ResolveClientSecret() error = %v, want ErrEmptyOutput", err)
	}
}

func TestResolveClientSecret_Keychain(t *testing.T) {
	keyring.MockInit()
	acct := config.Account{Name: "Work"}

	got, err := New().ResolveClientSecret(context.Background(), acct)
	if err != nil || got != "" {
		t.Errorf("without an entry = %q, %v; want empty, nil", got, err)
	}

	if err := StoreClientSecret("Work", "cs"); err != nil {
		t.Fatal(err)
	}
	got, err = New().ResolveClientSecret(context.Background(), acct)
	if err != nil || got != "cs" {
		t.Errorf("with an entry = %q, %v; want cs", got, err)
	}
}

func TestResolveClientSecret_KeychainFailureIsAnError(t *testing.T) {
	keyring.MockInitWithError(errors.New("keychain locked"))
	t.Cleanup(keyring.MockInit)

	_, err := New().ResolveClientSecret(context.Background(), config.Account{Name: "Work"})

	if err == nil || !strings.Contains(err.Error(), "keychain locked") {
		t.Errorf("ResolveClientSecret() error = %v, want the keychain error", err)
	}
}

func TestCopyOAuth_CopiesBothAndKeepsOld(t *testing.T) {
	keyring.MockInit()
	if err := Store("Old", "pw"); err != nil {
		t.Fatal(err)
	}
	if err := StoreToken("Old", testToken); err != nil {
		t.Fatal(err)
	}
	if err := StoreClientSecret("Old", "cs"); err != nil {
		t.Fatal(err)
	}

	if err := CopyOAuth("Old", "New"); err != nil {
		t.Fatalf("CopyOAuth() error = %v", err)
	}

	for _, name := range []string{"Old", "New"} {
		if tok, err := LoadToken(name); err != nil || tok.RefreshToken != testToken.RefreshToken {
			t.Errorf("token %s = %+v, %v", name, tok, err)
		}
		if cs, err := New().ResolveClientSecret(context.Background(), config.Account{Name: name}); err != nil || cs != "cs" {
			t.Errorf("client secret %s = %q, %v", name, cs, err)
		}
	}
	if _, err := keyring.Get(keychainService, "New"); !errors.Is(err, keyring.ErrNotFound) {
		t.Errorf("password entry appeared under New: %v", err)
	}
	if pw, err := keyring.Get(keychainService, "Old"); err != nil || pw != "pw" {
		t.Errorf("password under Old = %q, %v", pw, err)
	}
}

func TestCopyOAuth_SkipsMissingAndSameName(t *testing.T) {
	keyring.MockInit()

	if err := CopyOAuth("Old", "New"); err != nil {
		t.Errorf("CopyOAuth() of nothing error = %v", err)
	}
	if _, err := LoadToken("New"); !errors.Is(err, ErrNoToken) {
		t.Errorf("a token appeared: %v", err)
	}

	if err := StoreClientSecret("Old", "cs"); err != nil {
		t.Fatal(err)
	}
	if err := CopyOAuth("Old", "New"); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadToken("New"); !errors.Is(err, ErrNoToken) {
		t.Errorf("a token appeared: %v", err)
	}
	if cs, _ := New().ResolveClientSecret(context.Background(), config.Account{Name: "New"}); cs != "cs" {
		t.Errorf("client secret = %q, want cs", cs)
	}

	if err := CopyOAuth("Old", "Old"); err != nil {
		t.Errorf("CopyOAuth() same name error = %v", err)
	}
}

func TestCopyClientSecret(t *testing.T) {
	get := func(name string) string {
		cs, _ := New().ResolveClientSecret(context.Background(), config.Account{Name: name})
		return cs
	}

	t.Run("copies only the client secret and keeps old", func(t *testing.T) {
		keyring.MockInit()
		if err := StoreToken("Old", testToken); err != nil {
			t.Fatal(err)
		}
		if err := StoreClientSecret("Old", "cs"); err != nil {
			t.Fatal(err)
		}
		copied, err := CopyClientSecret("Old", "New")
		if err != nil || !copied {
			t.Fatalf("CopyClientSecret() = %v, %v", copied, err)
		}
		if get("New") != "cs" || get("Old") != "cs" {
			t.Errorf("secrets New %q Old %q", get("New"), get("Old"))
		}
		if _, err := LoadToken("New"); !errors.Is(err, ErrNoToken) {
			t.Errorf("a token appeared: %v", err)
		}
	})

	t.Run("an existing secret under the new name wins", func(t *testing.T) {
		keyring.MockInit()
		if err := StoreClientSecret("Old", "old-cs"); err != nil {
			t.Fatal(err)
		}
		if err := StoreClientSecret("New", "typed-cs"); err != nil {
			t.Fatal(err)
		}
		copied, err := CopyClientSecret("Old", "New")
		if err != nil || copied {
			t.Fatalf("CopyClientSecret() = %v, %v", copied, err)
		}
		if get("New") != "typed-cs" {
			t.Errorf("New = %q, want typed-cs", get("New"))
		}
	})

	t.Run("missing old entry and same name copy nothing", func(t *testing.T) {
		keyring.MockInit()
		if copied, err := CopyClientSecret("Old", "New"); err != nil || copied {
			t.Errorf("missing old: %v, %v", copied, err)
		}
		if err := StoreClientSecret("Old", "cs"); err != nil {
			t.Fatal(err)
		}
		if copied, err := CopyClientSecret("Old", "Old"); err != nil || copied {
			t.Errorf("same name: %v, %v", copied, err)
		}
	})

	t.Run("keychain error", func(t *testing.T) {
		keyring.MockInitWithError(errors.New("keychain locked"))
		t.Cleanup(keyring.MockInit)
		if _, err := CopyClientSecret("Old", "New"); err == nil || !strings.Contains(err.Error(), "keychain locked") {
			t.Errorf("error = %v", err)
		}
	})
}
