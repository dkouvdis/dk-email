package popup

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/dkouvdis/dk-email/internal/config"
)

func ck(k string) tea.KeyPressMsg {
	switch k {
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "shift+tab":
		return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	}
	return tea.KeyPressMsg{Code: []rune(k)[0], Text: k}
}

func keys(c Connect, ks ...string) (Connect, ConnectResult) {
	var r ConnectResult
	for _, k := range ks {
		c, r = c.Update(ck(k))
	}
	return c, r
}

func typeIn(c Connect, s string) Connect {
	for _, r := range s {
		c, _ = c.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return c
}

// fill types into the field with focus, then tabs to the next one.
func fill(c Connect, values ...string) Connect {
	for _, v := range values {
		c = typeIn(c, v)
		c, _ = keys(c, "tab")
	}
	return c
}

func newConnect() Connect {
	c := NewConnect([]string{"Personal"})
	c.SetSize(100, 40)
	return c
}

func TestConnect_GmailPrefill(t *testing.T) {
	c := fill(newConnect(), "Gmail", "me@gmail.com")

	if got := c.inputs[fHost].Value(); got != "imap.gmail.com" {
		t.Errorf("host = %q, want imap.gmail.com", got)
	}
	if got := c.inputs[fPort].Value(); got != "993" || c.tls != config.TLSImplicit {
		t.Errorf("port %q tls %q, want 993 implicit", got, c.tls)
	}

	// Changing to another domain clears what was filled in automatically.
	c, _ = keys(c, "shift+tab")
	for range len("gmail.com") {
		c, _ = keys(c, "backspace")
	}
	c = typeIn(c, "example.com")
	if c.inputs[fHost].Value() != "" || c.inputs[fPort].Value() != "" {
		t.Errorf("auto-filled host/port kept for a non-Gmail address: %q %q", c.inputs[fHost].Value(), c.inputs[fPort].Value())
	}

	// A host typed by hand is never overwritten.
	c = newConnect()
	c = fill(c, "Work", "")
	c, _ = keys(c, "tab")
	c = typeIn(c, "mail.example.com")
	c, _ = keys(c, "shift+tab", "shift+tab")
	c = typeIn(c, "me@googlemail.com")
	if got := c.inputs[fHost].Value(); got != "mail.example.com" {
		t.Errorf("host = %q, a typed host must win over the Gmail pre-fill", got)
	}
}

func TestConnect_Validation(t *testing.T) {
	tests := []struct {
		name   string
		values []string // name, email, username, host, port
		want   string
	}{
		{"name required", []string{"", "me@example.com", "", "h", ""}, "Name is required"},
		{"duplicate name", []string{"personal", "me@example.com", "", "h", ""}, `An account named "personal" already exists`},
		{"email", []string{"Work", "nope", "", "h", ""}, "Email must be an address"},
		{"host required", []string{"Work", "me@example.com", "", "", ""}, "IMAP host is required"},
		{"port range", []string{"Work", "me@example.com", "", "h", "70000"}, "Port must be a number from 1 to 65535"},
		{"ref required", []string{"Work", "me@example.com", "", "h", ""}, "1Password reference is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := fill(newConnect(), tt.values...)
			c, _ = keys(c, "tab", "tab") // TLS, source
			if tt.name != "ref required" {
				c = typeIn(c, "op://V/I/p")
			}
			c, _ = keys(c, "tab")

			c, r := keys(c, "enter")

			if r != ConnectPending || !strings.Contains(c.err, tt.want) {
				t.Errorf("result %v, error %q, want %q", r, c.err, tt.want)
			}
			if !strings.Contains(ansi.Strip(c.View()), tt.want) {
				t.Errorf("error not shown in the popup:\n%s", ansi.Strip(c.View()))
			}
		})
	}
}

func TestConnect_SubmitOnePasswordReference(t *testing.T) {
	c := fill(newConnect(), "Work", "me@example.com", "login", "mail.example.com", "1143")
	c, _ = keys(c, "right")      // TLS: starttls
	c, _ = keys(c, "tab", "tab") // source stays 1Password
	c = typeIn(c, "op://Work Vault/IMAP/password")
	c, _ = keys(c, "tab")

	c, r := keys(c, "enter")

	if r != ConnectSubmitted {
		t.Fatalf("result = %v, error %q", r, c.err)
	}
	req := c.Request()
	want := config.Account{
		Name: "Work", Email: "me@example.com", Username: "login", IMAPHost: "mail.example.com", IMAPPort: 1143,
		TLS: config.TLSStartTLS, PasswordCmd: `op read 'op://Work Vault/IMAP/password'`,
	}
	if req.Account != want || req.Password != "" {
		t.Errorf("Request() = %+v, want %+v and no password", req, want)
	}
}

func TestConnect_SubmitKeychainPasswordIsMasked(t *testing.T) {
	c := fill(newConnect(), "Gmail", "me@gmail.com", "", "", "")
	c, _ = keys(c, "tab", "space") // source: keychain
	c, _ = keys(c, "tab")
	c = typeIn(c, "hunter2 secret")

	if v := ansi.Strip(c.View()); strings.Contains(v, "hunter2") || !strings.Contains(v, "keychain") {
		t.Errorf("password shown or keychain not explained:\n%s", v)
	}
	c, _ = keys(c, "tab")
	c, r := keys(c, "enter")

	req := c.Request()
	if r != ConnectSubmitted || req.Password != "hunter2 secret" || req.Account.PasswordCmd != "" {
		t.Errorf("result %v, request %+v", r, req)
	}
	if req.Account.Username != "" || req.Account.IMAPHost != "imap.gmail.com" || req.Account.IMAPPort != 993 {
		t.Errorf("account = %+v", req.Account)
	}
}

func TestConnect_EnterAdvancesAndEscCancels(t *testing.T) {
	c := newConnect()
	c = typeIn(c, "Work")
	c, r := keys(c, "enter")
	if r != ConnectPending || c.focus != fEmail {
		t.Errorf("enter on a field: result %v, focus %d; want the next field", r, c.focus)
	}
	if _, r = keys(c, "q"); r != ConnectPending {
		t.Error("q should be typed, not close the form")
	}
	if _, r = keys(c, "esc"); r != ConnectCancelled {
		t.Error("esc should cancel")
	}
}

func TestConnect_BusyIgnoresInputAndShowsError(t *testing.T) {
	c := newConnect()
	c.SetBusy(true)
	c = typeIn(c, "xyz")
	if c.inputs[fName].Value() != "" || !strings.Contains(ansi.Strip(c.View()), "Testing connection…") {
		t.Error("typing while testing should be ignored, and the popup should say it's testing")
	}
	if _, r := keys(c, "esc"); r != ConnectCancelled {
		t.Error("esc should still cancel while testing")
	}

	c.SetBusy(false)
	c.SetError("login failed: invalid credentials")
	if v := ansi.Strip(c.View()); !strings.Contains(v, "login failed: invalid credentials") || strings.Contains(v, "Testing") {
		t.Errorf("view:\n%s", v)
	}
}

func TestConnect_PasswordOnlyMode(t *testing.T) {
	acct := config.Account{Name: "Personal", Email: "me@gmail.com", IMAPHost: "imap.gmail.com", IMAPPort: 993, TLS: config.TLSImplicit}
	c := NewPasswordPrompt(acct)
	c.SetSize(100, 40)

	v := ansi.Strip(c.View())
	for _, want := range []string{"Password for Personal", "me@gmail.com", "imap.gmail.com", "keychain"} {
		if !strings.Contains(v, want) {
			t.Errorf("view missing %q:\n%s", want, v)
		}
	}
	c = typeIn(c, "app-pass")
	c, _ = keys(c, "down", "down") // only the password and the button take focus
	c, _ = keys(c, "shift+tab", "tab")
	c, r := keys(c, "enter")

	if r != ConnectSubmitted {
		t.Fatalf("result = %v, error %q", r, c.err)
	}
	if req := c.Request(); req.Account != acct || req.Password != "app-pass" || !c.PasswordOnly() {
		t.Errorf("Request() = %+v", req)
	}
	if _, r = keys(NewPasswordPrompt(acct), "tab", "enter"); r != ConnectPending {
		t.Error("an empty password must not submit")
	}
}

func TestConnect_FitsTheScreen(t *testing.T) {
	c := newConnect()
	if w := lipgloss.Width(c.View()); w > 70 {
		t.Errorf("width = %d, want a compact form", w)
	}
	c.SetSize(50, 12)
	v := c.View()
	if w, h := lipgloss.Width(v), lipgloss.Height(v); w > 50 || h > 12 {
		t.Errorf("view is %dx%d, want it within 50x12", w, h)
	}
}

func TestShellQuote(t *testing.T) {
	tests := map[string]string{
		"op://Personal/Gmail/app-password": "op://Personal/Gmail/app-password",
		"op://Work Vault/IMAP/password":    "'op://Work Vault/IMAP/password'",
		"it's":                             `'it'\''s'`,
		"":                                 "''",
	}
	for in, want := range tests {
		if got := shellQuote(in); got != want {
			t.Errorf("shellQuote(%q) = %s, want %s", in, got, want)
		}
	}
}
