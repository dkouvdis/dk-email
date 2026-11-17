package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func readConfig(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

const editBase = `# my config
[ui]
default_layout = "list"

# first account
[[account]]
name = "A"
email = "a@example.com"
imap_host = "imap.a.com"
# inside A
archive_folder = "Done"

# about B
[[account]] # trailing
name = "B"
email = "b@example.com"
imap_host = "imap.b.com"

# trailing comment
`

func TestReplaceAccount_Golden(t *testing.T) {
	path := writeConfig(t, editBase)
	if err := ReplaceAccount(path, 1, "B", func(a Account) Account {
		a.Name = "B2"
		return a
	}); err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(editBase, `[[account]] # trailing
name = "B"
email = "b@example.com"
imap_host = "imap.b.com"
`, `[[account]]
name = "B2"
email = "b@example.com"
imap_host = "imap.b.com"
`, 1)
	if got := readConfig(t, path); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestReplaceAccount_FirstBlockKeepsCommentsAboveNextHeader(t *testing.T) {
	path := writeConfig(t, editBase)
	if err := ReplaceAccount(path, 0, "A", func(a Account) Account {
		a.IMAPHost = "imap.new.com"
		return a
	}); err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(editBase, "imap_host = \"imap.a.com\"\n# inside A\narchive_folder = \"Done\"\n",
		"imap_host = \"imap.new.com\"\narchive_folder = \"Done\"\n", 1)
	if got := readConfig(t, path); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestReplaceAccount_UIAfterAccounts(t *testing.T) {
	in := "[[account]]\nname = \"A\"\nemail = \"a@x.com\"\nimap_host = \"h\"\n\n[ui]\ndefault_layout = \"list\"\n"
	path := writeConfig(t, in)
	if err := ReplaceAccount(path, 0, "A", func(a Account) Account { a.Name = "Z"; return a }); err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(in, `name = "A"`, `name = "Z"`, 1)
	if got := readConfig(t, path); got != want {
		t.Errorf("got:\n%s", got)
	}
}

func TestReplaceAccount_CRLF(t *testing.T) {
	in := strings.ReplaceAll(editBase, "\n", "\r\n")
	path := writeConfig(t, in)
	if err := ReplaceAccount(path, 1, "B", func(a Account) Account { a.Name = "B2"; return a }); err != nil {
		t.Fatal(err)
	}
	got := readConfig(t, path)
	if strings.Count(got, "\n") != strings.Count(got, "\r\n") {
		t.Errorf("mixed line endings: %q", got)
	}
	if !strings.Contains(got, "name = \"B2\"\r\n") || !strings.HasSuffix(got, "# trailing comment\r\n") {
		t.Errorf("unexpected result: %q", got)
	}
}

func TestReplaceAccount_RawValuesSurvive(t *testing.T) {
	path := writeConfig(t, strings.Replace(editBase, `archive_folder = "Done"`, `tls_cert = "certs/a.pem"`, 1))
	if err := os.Mkdir(filepath.Join(filepath.Dir(path), "certs"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), "certs", "a.pem"), testCertPEM(t), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := ReplaceAccount(path, 0, "A", func(a Account) Account { return a }); err != nil {
		t.Fatal(err)
	}
	got := readConfig(t, path)
	if !strings.Contains(got, `tls_cert = "certs/a.pem"`) {
		t.Errorf("relative tls_cert rewritten: %s", got)
	}
	if strings.Contains(got, "imap_port") || strings.Contains(got, "username") {
		t.Errorf("defaults written out: %s", got)
	}
	after, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before.Accounts, after.Accounts) {
		t.Errorf("accounts changed:\n%+v\n%+v", before.Accounts, after.Accounts)
	}
}

func TestReplaceAccount_Rejected(t *testing.T) {
	tests := []struct {
		name  string
		index int
		old   string
		edit  func(Account) Account
		want  string
	}{
		{"stale name", 0, "Nope", func(a Account) Account { return a }, "config changed on disk"},
		{"index out of range", 5, "A", func(a Account) Account { return a }, "config changed on disk"},
		{"negative index", -1, "A", func(a Account) Account { return a }, "config changed on disk"},
		{"invalid result", 0, "A", func(a Account) Account {
			a.Aliases = []Alias{{Email: "x@example.com"}}
			return a
		}, "smtp_host"},
		{"duplicate name", 0, "A", func(a Account) Account { a.Name = "B"; return a }, "duplicate"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeConfig(t, editBase)
			err := ReplaceAccount(path, tt.index, tt.old, tt.edit)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want it to contain %q", err, tt.want)
			}
			if got := readConfig(t, path); got != editBase {
				t.Errorf("file changed:\n%s", got)
			}
		})
	}
}

func TestRemoveAccount_Golden(t *testing.T) {
	path := writeConfig(t, editBase)
	if err := RemoveAccount(path, 0, "A"); err != nil {
		t.Fatal(err)
	}
	want := `# my config
[ui]
default_layout = "list"

# first account
# about B
[[account]] # trailing
name = "B"
email = "b@example.com"
imap_host = "imap.b.com"

# trailing comment
`
	if got := readConfig(t, path); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestRemoveAccount_LastBlock(t *testing.T) {
	path := writeConfig(t, editBase)
	if err := RemoveAccount(path, 1, "B"); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil || len(cfg.Accounts) != 1 || cfg.Accounts[0].Name != "A" {
		t.Fatalf("Load() = %+v, %v", cfg, err)
	}
	got := readConfig(t, path)
	if !strings.HasSuffix(got, "# about B\n# trailing comment\n") {
		t.Errorf("trailing comments lost: %q", got)
	}
}

func TestRemoveAccount_OnlyAccount(t *testing.T) {
	path := writeConfig(t, "[ui]\ndefault_layout = \"list\"\n\n[[account]]\nname = \"A\"\nemail = \"a@x.com\"\nimap_host = \"h\"\n")
	if err := RemoveAccount(path, 0, "A"); err != nil {
		t.Fatal(err)
	}
	if got := readConfig(t, path); got != "[ui]\ndefault_layout = \"list\"\n" {
		t.Errorf("got %q", got)
	}
}

func TestRemoveAccount_StaleLeavesFile(t *testing.T) {
	path := writeConfig(t, editBase)
	if err := RemoveAccount(path, 1, "A"); err == nil {
		t.Fatal("want error")
	}
	if got := readConfig(t, path); got != editBase {
		t.Errorf("file changed: %s", got)
	}
}

func TestReplaceAccount_OAuth(t *testing.T) {
	path := writeConfig(t, editBase)
	if err := ReplaceAccount(path, 1, "B", func(a Account) Account {
		a.Auth, a.OAuthProvider, a.OAuthClientID, a.OAuthClientSecretCmd = AuthOAuth2, ProviderGoogle, "gid", "op read op://P/G/secret"
		return a
	}); err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(editBase, `[[account]] # trailing
name = "B"
email = "b@example.com"
imap_host = "imap.b.com"
`, `[[account]]
name = "B"
email = "b@example.com"
imap_host = "imap.b.com"
auth = "oauth2"
oauth_provider = "google"
oauth_client_id = "gid"
oauth_client_secret_cmd = "op read op://P/G/secret"
`, 1)
	got := readConfig(t, path)
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	if _, err := Load(path); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if err := ReplaceAccount(path, 1, "B", func(a Account) Account { return a }); err != nil {
		t.Fatal(err)
	}
	if again := readConfig(t, path); again != got {
		t.Errorf("identity replace changed the file:\n%s\nwant:\n%s", again, got)
	}
}
