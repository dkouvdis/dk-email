package popup

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/dkouvdis/dk-email/internal/config"
	"github.com/dkouvdis/dk-email/internal/ui/theme"
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
	c := NewConnect(theme.Default(), []string{"Personal"})
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

	c, r := keys(c, "enter")

	if r != ConnectSubmitted {
		t.Fatalf("result = %v, error %q", r, c.err)
	}
	req := c.Request()
	want := config.Account{
		Name: "Work", Email: "me@example.com", Username: "login", IMAPHost: "mail.example.com", IMAPPort: 1143,
		TLS: config.TLSStartTLS, PasswordCmd: `op read 'op://Work Vault/IMAP/password'`,
	}
	if !reflect.DeepEqual(req.Account, want) || req.Password != "" {
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
	c, r := keys(c, "enter")

	req := c.Request()
	if r != ConnectSubmitted || req.Password != "hunter2 secret" || req.Account.PasswordCmd != "" {
		t.Errorf("result %v, request %+v", r, req)
	}
	if req.Account.Username != "" || req.Account.IMAPHost != "imap.gmail.com" || req.Account.IMAPPort != 993 {
		t.Errorf("account = %+v", req.Account)
	}
	if a := req.Account; a.SMTPHost != "smtp.gmail.com" || a.SMTPPort != 465 || a.SMTPTLS != config.TLSImplicit {
		t.Errorf("SMTP = %s:%d %s, want the Gmail pre-fill", a.SMTPHost, a.SMTPPort, a.SMTPTLS)
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
	c := NewPasswordPrompt(theme.Default(), acct)
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
	if req := c.Request(); !reflect.DeepEqual(req.Account, acct) || req.Password != "app-pass" || !c.PasswordOnly() {
		t.Errorf("Request() = %+v", req)
	}
	if _, r = keys(NewPasswordPrompt(theme.Default(), acct), "tab", "enter"); r != ConnectPending {
		t.Error("an empty password must not submit")
	}
}

func TestConnect_HintsAreNotInThePopup(t *testing.T) {
	c := newConnect()
	if got := hintText(c.Hints()); got != "Next: tab | Choose: ←→ | Test & save: enter | Cancel: esc" {
		t.Errorf("hints = %q", got)
	}
	v := ansi.Strip(c.View())
	for _, bad := range []string{"esc close", "esc cancel", "tab next", "enter test"} {
		if strings.Contains(v, bad) {
			t.Errorf("view has key text %q; the hint bar shows the keys:\n%s", bad, v)
		}
	}
	if !strings.Contains(v, "[ Test & save ]") {
		t.Errorf("view lacks the button:\n%s", v)
	}

	acct := config.Account{Name: "Personal", Email: "me@gmail.com", IMAPHost: "imap.gmail.com", TLS: config.TLSImplicit}
	if got := hintText(NewPasswordPrompt(theme.Default(), acct).Hints()); got != "Test & save: enter | Cancel: esc" {
		t.Errorf("password prompt hints = %q", got)
	}
	c.SetBusy(true)
	if got := hintText(c.Hints()); got != "Cancel: esc" {
		t.Errorf("busy hints = %q", got)
	}
}

func TestConnect_FitsTheScreen(t *testing.T) {
	c := newConnect()
	if w := lipgloss.Width(c.View()); w > 71 {
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

func TestConnect_PasteIntoFields(t *testing.T) {
	c := fill(newConnect(), "Gmail")
	c = c.Paste(tea.PasteMsg{Content: "me@gmail.com\n"})
	if got := c.inputs[fEmail].Value(); got != "me@gmail.com" || c.inputs[fHost].Value() != "imap.gmail.com" {
		t.Errorf("pasted email = %q, host %q; want it trimmed and Gmail pre-filled", got, c.inputs[fHost].Value())
	}

	c, _ = keys(c, "tab", "tab", "tab", "tab", "tab", "tab") // to the reference
	c = c.Paste(tea.PasteMsg{Content: "  op://Work Vault/IMAP/password\r\n"})
	c, _ = keys(c, "enter")
	if got := c.Request().Account.PasswordCmd; got != `op read 'op://Work Vault/IMAP/password'` {
		t.Errorf("password_cmd = %q", got)
	}

	c, _ = keys(c, "shift+tab") // the source choice: pasting does nothing
	before := c.Request()
	if !reflect.DeepEqual(c.Paste(tea.PasteMsg{Content: "x"}).Request(), before) {
		t.Error("paste on a choice field changed the form")
	}
	c.SetBusy(true)
	if !reflect.DeepEqual(c.Paste(tea.PasteMsg{Content: "x"}).Request(), before) {
		t.Error("paste while testing changed the form")
	}
}

func TestConnect_GmailPrefillsSMTP(t *testing.T) {
	for _, email := range []string{"me@gmail.com", "Me@GMail.COM", "me@googlemail.com"} {
		c := fill(newConnect(), "Gmail", email)

		if h, p := c.inputs[fSMTPHost].Value(), c.inputs[fSMTPPort].Value(); h != "smtp.gmail.com" || p != "465" || c.smtpTLS != config.TLSImplicit {
			t.Errorf("%s: SMTP = %q %q %q, want smtp.gmail.com 465 implicit", email, h, p, c.smtpTLS)
		}
	}

	// Leaving Gmail clears what was filled in.
	c := fill(newConnect(), "Gmail", "me@gmail.com")
	c, _ = keys(c, "shift+tab")
	for range len("gmail.com") {
		c, _ = keys(c, "backspace")
	}
	c = typeIn(c, "example.com")
	if c.inputs[fSMTPHost].Value() != "" || c.inputs[fSMTPPort].Value() != "" {
		t.Errorf("pre-filled SMTP kept for a non-Gmail address: %q %q", c.inputs[fSMTPHost].Value(), c.inputs[fSMTPPort].Value())
	}
}

func TestConnect_GmailPrefillNeverOverwritesTypedSMTP(t *testing.T) {
	typed := func(c Connect, field int, value string) Connect {
		c.setFocus(field)
		c = typeIn(c, value)
		c.setFocus(fEmail)
		return typeIn(c, "me@gmail.com")
	}

	c := typed(newConnect(), fSMTPHost, "smtp.example.com")
	if c.inputs[fSMTPHost].Value() != "smtp.example.com" || c.inputs[fSMTPPort].Value() != "" || c.autoSMTP {
		t.Errorf("typed host: SMTP = %q %q auto %v, want it left alone", c.inputs[fSMTPHost].Value(), c.inputs[fSMTPPort].Value(), c.autoSMTP)
	}
	c = typed(newConnect(), fSMTPPort, "2525")
	if c.inputs[fSMTPHost].Value() != "" || c.inputs[fSMTPPort].Value() != "2525" {
		t.Errorf("typed port: SMTP = %q %q, want it left alone", c.inputs[fSMTPHost].Value(), c.inputs[fSMTPPort].Value())
	}

	c = newConnect()
	c.setFocus(fSMTPTLS)
	c, _ = keys(c, "right")
	c.setFocus(fEmail)
	if c = typeIn(c, "me@gmail.com"); c.inputs[fSMTPHost].Value() != "" || c.smtpTLS != config.TLSStartTLS {
		t.Errorf("chosen TLS: host %q tls %q, want it left alone", c.inputs[fSMTPHost].Value(), c.smtpTLS)
	}

	// Clearing the filled-in host opts out of sending, and sticks.
	c = fill(newConnect(), "Gmail", "me@gmail.com")
	c.setFocus(fSMTPHost)
	for range len("smtp.gmail.com") {
		c, _ = keys(c, "backspace")
	}
	c.setFocus(fEmail)
	c, _ = keys(c, "backspace")
	c = typeIn(c, "m")
	if c.inputs[fSMTPHost].Value() != "" {
		t.Errorf("host = %q, a cleared SMTP host must stay cleared", c.inputs[fSMTPHost].Value())
	}
}

func TestConnect_SMTPValidation(t *testing.T) {
	tests := []struct {
		name       string
		host, port string
		want       string // empty: valid
	}{
		{"port out of range", "smtp.example.com", "70000", "SMTP port must be a number from 1 to 65535"},
		{"port not a number", "smtp.example.com", "smtp", "SMTP port must be a number from 1 to 65535"},
		{"port without host", "", "587", "SMTP port is set but SMTP host is empty"},
		{"host only", "smtp.example.com", "", ""},
		{"blank", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := fill(newConnect(), "Work", "me@example.com", "", "mail.example.com", "")
			c.setFocus(fSMTPHost)
			c = fill(c, tt.host, tt.port)
			c.setFocus(fSecret)
			c = typeIn(c, "op://V/I/p")

			c, r := keys(c, "enter")

			if tt.want == "" && r != ConnectSubmitted || tt.want != "" && (r != ConnectPending || !strings.Contains(c.err, tt.want)) {
				t.Errorf("result %v, error %q, want %q", r, c.err, tt.want)
			}
		})
	}
}

func TestConnect_SMTPRequest(t *testing.T) {
	submit := func(host, port string, startTLS bool) config.Account {
		c := fill(newConnect(), "Work", "me@example.com", "", "mail.example.com", "")
		c.setFocus(fSMTPHost)
		c = fill(c, host, port)
		if startTLS {
			c, _ = keys(c, "right")
		}
		c.setFocus(fSecret)
		c = typeIn(c, "op://V/I/p")
		c, r := keys(c, "enter")
		if r != ConnectSubmitted {
			t.Fatalf("result %v, error %q", r, c.err)
		}
		return c.Request().Account
	}

	if a := submit("", "", true); a.SMTPHost != "" || a.SMTPPort != 0 || a.SMTPTLS != "" {
		t.Errorf("blank host: SMTP = %q %d %q, want none so no smtp_* keys are saved", a.SMTPHost, a.SMTPPort, a.SMTPTLS)
	}
	if a := submit("smtp.example.com", "", false); a.SMTPHost != "smtp.example.com" || a.SMTPPort != 0 || a.SMTPTLS != config.TLSImplicit {
		t.Errorf("blank port: SMTP = %q %d %q, want the port left to the config default", a.SMTPHost, a.SMTPPort, a.SMTPTLS)
	}
	if a := submit("smtp.example.com", "2525", true); a.SMTPPort != 2525 || a.SMTPTLS != config.TLSStartTLS {
		t.Errorf("typed: SMTP port %d tls %q, want 2525 starttls", a.SMTPPort, a.SMTPTLS)
	}
}

func TestConnect_SMTPTLSToggleChangesPortHint(t *testing.T) {
	c := newConnect()
	c.setFocus(fSMTPTLS)

	c, _ = keys(c, "right")
	if v := ansi.Strip(c.View()); c.smtpTLS != config.TLSStartTLS || c.inputs[fSMTPPort].Placeholder != "587" || !strings.Contains(v, "● starttls") {
		t.Errorf("tls %q, port hint %q after toggling:\n%s", c.smtpTLS, c.inputs[fSMTPPort].Placeholder, v)
	}
	if c, _ = keys(c, "space"); c.smtpTLS != config.TLSImplicit || c.inputs[fSMTPPort].Placeholder != "465" {
		t.Errorf("tls %q, port hint %q after toggling back", c.smtpTLS, c.inputs[fSMTPPort].Placeholder)
	}
}

func TestConnect_AliasesFieldFollowsSMTPTLS(t *testing.T) {
	c := newConnect()
	c.setFocus(fSMTPTLS)
	c, _ = keys(c, "tab")
	if c.focus != fAliases {
		t.Fatalf("focus after SMTP TLS = %d, want the Aliases field", c.focus)
	}
	c, _ = keys(c, "tab")
	if c.focus != fSubmit {
		t.Errorf("focus after Aliases = %d, want Submit", c.focus)
	}
	if !strings.Contains(ansi.Strip(newConnect().View()), "Aliases") {
		t.Error("Aliases row not shown")
	}
}

func submitAliases(smtpHost, aliases string) (Connect, ConnectResult) {
	c := fill(newConnect(), "Work", "me@example.com", "", "mail.example.com", "")
	c.setFocus(fSMTPHost)
	c = typeIn(c, smtpHost)
	c.setFocus(fAliases)
	c = typeIn(c, aliases)
	c.setFocus(fSecret)
	c = typeIn(c, "op://V/I/p")
	return keys(c, "enter")
}

func TestConnect_AliasesRequest(t *testing.T) {
	c, r := submitAliases("smtp.example.com", " hi@my.com , ,sales@my.com,")
	if r != ConnectSubmitted {
		t.Fatalf("result %v, error %q", r, c.err)
	}
	got := c.Request().Account.Aliases
	if len(got) != 2 || got[0] != (config.Alias{Email: "hi@my.com"}) || got[1] != (config.Alias{Email: "sales@my.com"}) {
		t.Errorf("aliases = %+v", got)
	}
	c, _ = submitAliases("smtp.example.com", "")
	if a := c.Request().Account.Aliases; len(a) != 0 {
		t.Errorf("empty field gave aliases %+v", a)
	}
}

func TestConnect_AliasesValidation(t *testing.T) {
	tests := []struct {
		name, host, aliases, want string
	}{
		{"bad address", "smtp.example.com", "nope", "not a valid address"},
		{"display name", "smtp.example.com", "Me <me@my.com>", "not a valid address"},
		{"duplicate", "smtp.example.com", "a@my.com, A@my.com", "listed twice"},
		{"account email", "smtp.example.com", "ME@example.com", "already the account email"},
		{"no smtp host", "", "a@my.com", "Aliases need an SMTP host"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, r := submitAliases(tt.host, tt.aliases)
			if r != ConnectPending || !strings.Contains(c.err, tt.want) {
				t.Errorf("result %v, error %q, want %q", r, c.err, tt.want)
			}
			if !strings.Contains(ansi.Strip(c.View()), tt.want) {
				t.Errorf("error not shown:\n%s", ansi.Strip(c.View()))
			}
		})
	}
}

func TestConnect_PasteIntoAliases(t *testing.T) {
	c := newConnect()
	c.setFocus(fAliases)
	c = c.Paste(tea.PasteMsg{Content: "a@my.com, b@my.com\n"})
	if got := c.inputs[fAliases].Value(); got != "a@my.com, b@my.com" {
		t.Errorf("pasted = %q", got)
	}
}

func editAccount() config.Account {
	return config.Account{
		Name: "Work", Email: "me@example.com", Username: "login", IMAPHost: "imap.example.com",
		IMAPPort: 993, TLS: config.TLSImplicit, ArchiveFolder: "Done", TLSCert: "/certs/a.pem",
		SMTPHost: "smtp.example.com", SMTPPort: 465, SMTPTLS: config.TLSImplicit, FromName: "Me",
		Aliases: []config.Alias{{Email: "Hi@Example.com", Name: "Hi there"}, {Email: "b@example.com"}},
	}
}

func newEdit(a config.Account) Connect {
	c := NewEditAccount(theme.Default(), a, []string{"Home"})
	c.SetSize(100, 40)
	return c
}

func TestEditAccount_PrefillsFields(t *testing.T) {
	c := newEdit(editAccount())

	for f, want := range map[int]string{
		fName: "Work", fEmail: "me@example.com", fUser: "login", fHost: "imap.example.com", fPort: "993",
		fSMTPHost: "smtp.example.com", fSMTPPort: "465", fAliases: "Hi@Example.com, b@example.com",
	} {
		if got := c.inputs[f].Value(); got != want {
			t.Errorf("field %d = %q, want %q", f, got, want)
		}
	}
	if c.source != sourceKeep || c.autoHost || c.autoSMTP || !c.smtpTouched {
		t.Errorf("source %d autoHost %v autoSMTP %v smtpTouched %v", c.source, c.autoHost, c.autoSMTP, c.smtpTouched)
	}
	v := ansi.Strip(c.View())
	for _, want := range []string{"Edit account", "keep current", "Current", "keychain", "Delete account"} {
		if !strings.Contains(v, want) {
			t.Errorf("view lacks %q:\n%s", want, v)
		}
	}
}

func TestEditAccount_UsernameEmptyWhenEmailAndNoSMTPPort(t *testing.T) {
	a := config.Account{Name: "W", Email: "me@example.com", Username: "me@example.com", IMAPHost: "h", IMAPPort: 143, TLS: config.TLSStartTLS}
	c := newEdit(a)

	if c.inputs[fUser].Value() != "" || c.inputs[fSMTPHost].Value() != "" || c.inputs[fSMTPPort].Value() != "" || c.inputs[fAliases].Value() != "" {
		t.Error("username or SMTP fields prefilled")
	}
	if c.tls != config.TLSStartTLS || c.inputs[fPort].Placeholder != "143" {
		t.Errorf("tls %q placeholder %q", c.tls, c.inputs[fPort].Placeholder)
	}
}

func TestEditAccount_GmailAddressDoesNotOverwrite(t *testing.T) {
	c := newEdit(editAccount())
	c, _ = keys(c, "tab") // email
	c = typeIn(c, "x@gmail.com")
	if c.inputs[fHost].Value() != "imap.example.com" || c.inputs[fSMTPHost].Value() != "smtp.example.com" {
		t.Errorf("hosts overwritten: %q %q", c.inputs[fHost].Value(), c.inputs[fSMTPHost].Value())
	}
}

func TestEditAccount_PasswordSourceCycleAndFocus(t *testing.T) {
	a := editAccount()
	a.PasswordCmd = "op read 'op://My Vault/Item/pw'"
	c := newEdit(a)
	for range 6 {
		c, _ = keys(c, "tab") // name..source
	}
	if c.focus != fSource {
		t.Fatalf("focus = %d", c.focus)
	}
	c, _ = keys(c, "tab")
	if c.focus != fSMTPHost {
		t.Errorf("focus = %d, want the secret row skipped under keep current", c.focus)
	}
	c, _ = keys(c, "shift+tab")

	c, _ = keys(c, "right")
	if c.source != sourceOnePassword || c.inputs[fSecret].Value() != "op://My Vault/Item/pw" {
		t.Errorf("source %d ref %q, want 1Password with the saved reference", c.source, c.inputs[fSecret].Value())
	}
	c, _ = keys(c, "tab")
	if c.focus != fSecret {
		t.Errorf("focus = %d, want the reference row", c.focus)
	}
	c, _ = keys(c, "shift+tab", "right")
	if c.source != sourceKeychain || c.inputs[fSecret].Value() != "" {
		t.Errorf("source %d, want keychain with an empty password", c.source)
	}
	c, r := keys(c, "tab", "enter")
	if r != ConnectPending || !strings.Contains(c.err, "Password is required") {
		t.Errorf("result %v err %q", r, c.err)
	}
	c, _ = keys(c, "shift+tab", "right", "right") // OAuth 2.0, then around
	if c.source != sourceKeep {
		t.Errorf("source %d, want it back at keep current", c.source)
	}
	if v := ansi.Strip(c.View()); !strings.Contains(v, "password_cmd") {
		t.Errorf("view doesn't say password_cmd:\n%s", v)
	}
}

func TestEditAccount_SubmitKeepsPassword(t *testing.T) {
	c := newEdit(editAccount())
	c, _ = keys(c, "tab", "tab", "tab", "tab", "tab", "tab", "tab", "tab", "tab", "tab", "tab") // ... submit
	if c.focus != fSubmit {
		t.Fatalf("focus = %d", c.focus)
	}

	c, r := keys(c, "enter")

	req := c.EditRequest()
	if r != ConnectSubmitted || !req.KeepPassword || req.Password != "" {
		t.Errorf("result %v, request %+v", r, req)
	}
	if req.Account.Name != "Work" || req.Account.Username != "login" || len(req.Account.Aliases) != 2 {
		t.Errorf("account = %+v", req.Account)
	}
}

func TestEditAccount_DuplicateNameRejectedOwnNameAllowed(t *testing.T) {
	c := newEdit(editAccount())
	if c.validate() != "" {
		t.Errorf("own name rejected: %q", c.validate())
	}
	c = typeIn(c, "") // keep focus on name
	for range len("Work") {
		c, _ = keys(c, "backspace")
	}
	c = typeIn(c, "home")
	if got := c.validate(); !strings.Contains(got, "already exists") {
		t.Errorf("validate() = %q", got)
	}
}

func TestEditAccount_DeleteRowIsLastAndOnlyInEditMode(t *testing.T) {
	c := newEdit(editAccount())
	c, _ = keys(c, "shift+tab")
	if c.focus != fDelete {
		t.Errorf("focus = %d, want the Delete row to follow Submit", c.focus)
	}
	n := newConnect()
	n, _ = keys(n, "shift+tab")
	if n.focus != fSubmit {
		t.Errorf("new account focus = %d, want Submit last", n.focus)
	}
	if strings.Contains(ansi.Strip(n.View()), "Delete account") {
		t.Error("Delete row shown for a new account")
	}
}

func TestEditAccount_DeleteConfirmKeychain(t *testing.T) {
	open := func() Connect {
		c := newEdit(editAccount())
		c, _ = keys(c, "shift+tab") // Delete row
		c, r := keys(c, "enter")
		if r != ConnectPending || !c.confirming {
			t.Fatalf("result %v confirming %v", r, c.confirming)
		}
		return c
	}
	c := open()
	if v := ansi.Strip(c.View()); !strings.Contains(v, "Delete Work?") {
		t.Errorf("no prompt:\n%s", v)
	}
	var hints []string
	for _, h := range c.Hints() {
		hints = append(hints, h.Help().Key)
	}
	if !reflect.DeepEqual(hints, []string{"y", "k", "n"}) {
		t.Errorf("hints = %v", hints)
	}

	// Fields are out of reach.
	c2, r := keys(c, "x", "tab")
	if r != ConnectPending || c2.inputs[fName].Value() != "Work" || !c2.confirming {
		t.Error("a key reached the form while confirming")
	}

	if _, r := keys(open(), "y"); r != ConnectDelete {
		t.Errorf("y: result %v", r)
	}
	c, r = keys(open(), "y")
	if !c.DeletePassword() {
		t.Error("y should delete the password too")
	}
	c, r = keys(open(), "k")
	if r != ConnectDelete || c.DeletePassword() {
		t.Errorf("k: result %v, deletePassword %v", r, c.DeletePassword())
	}
	for _, k := range []string{"n", "esc"} {
		c, r = keys(open(), k)
		if r != ConnectPending || c.confirming {
			t.Errorf("%s: result %v confirming %v", k, r, c.confirming)
		}
	}
}

func TestEditAccount_DeleteConfirmPasswordCmdHasNoKeepOption(t *testing.T) {
	a := editAccount()
	a.PasswordCmd = "op read x"
	c := newEdit(a)
	c, _ = keys(c, "shift+tab", "enter")

	if c2, r := keys(c, "k"); r != ConnectPending || !c2.confirming {
		t.Errorf("k: result %v", r)
	}
	c, r := keys(c, "y")
	if r != ConnectDelete || c.DeletePassword() {
		t.Errorf("y: result %v deletePassword %v", r, c.DeletePassword())
	}
}

func TestEditAccount_SaveAnyway(t *testing.T) {
	c := newEdit(editAccount())
	if _, r := keys(c, "ctrl+s"); r != ConnectPending {
		t.Error("ctrl+s saved without a failed test")
	}
	hasHint := func(c Connect) bool {
		for _, h := range c.Hints() {
			if h.Help().Key == "ctrl+s" {
				return true
			}
		}
		return false
	}

	c.SetTestFailed("dial: no route")
	if !hasHint(c) {
		t.Error("no save anyway hint after a failed test")
	}
	if _, r := keys(c, "ctrl+s"); r != ConnectSaveAnyway {
		t.Errorf("ctrl+s result = %v", r)
	}

	c = typeIn(c, "x")
	if hasHint(c) {
		t.Error("save anyway still offered after an edit")
	}
	if _, r := keys(c, "ctrl+s"); r != ConnectPending {
		t.Error("ctrl+s saved after the flag was cleared")
	}

	// Config-level validation still applies.
	c.SetTestFailed("boom")
	c.inputs[fHost].SetValue("")
	if c, r := keys(c, "ctrl+s"); r != ConnectPending || !strings.Contains(c.err, "IMAP host is required") {
		t.Errorf("result %v err %q", r, c.err)
	}
}

func TestSaveAnyway_NotOfferedForNewAccountOrPasswordPrompt(t *testing.T) {
	c := fill(newConnect(), "Work", "me@example.com", "", "h", "")
	c, _ = keys(c, "tab", "tab")
	c = typeIn(c, "op://V/I/p")
	c.SetTestFailed("nope")
	if _, r := keys(c, "ctrl+s"); r != ConnectPending {
		t.Error("new account offered save anyway")
	}
	p := NewPasswordPrompt(theme.Default(), editAccount())
	p.SetTestFailed("nope")
	if _, r := keys(p, "ctrl+s"); r != ConnectPending {
		t.Error("password prompt offered save anyway")
	}
}

func TestApplyEdit(t *testing.T) {
	// formOf is what the form submits for old, with change applied to the
	// prefilled fields.
	formOf := func(old config.Account, change func(*Connect)) EditRequest {
		c := newEdit(old.WithDefaults())
		if change != nil {
			change(&c)
		}
		return c.EditRequest()
	}
	raw := config.Account{
		Name: "Work", Email: "me@example.com", IMAPHost: "imap.example.com", TLSCert: "certs/a.pem",
		ArchiveFolder: "Done", PasswordCmd: "op read x", SMTPHost: "smtp.example.com", FromName: "Me",
		Aliases: []config.Alias{{Email: "hi@example.com", Name: "Hi"}},
	}

	t.Run("no change keeps raw values", func(t *testing.T) {
		if got := ApplyEdit(raw, formOf(raw, nil)); !reflect.DeepEqual(got, raw) {
			t.Errorf("got %+v\nwant %+v", got, raw)
		}
	})
	t.Run("rename and alias names follow the email case-insensitively", func(t *testing.T) {
		req := formOf(raw, func(c *Connect) {
			c.inputs[fName].SetValue("Job")
			c.inputs[fAliases].SetValue("HI@example.com, new@example.com")
		})
		got := ApplyEdit(raw, req)
		want := raw
		want.Name = "Job"
		want.Aliases = []config.Alias{{Email: "HI@example.com", Name: "Hi"}, {Email: "new@example.com"}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %+v\nwant %+v", got, want)
		}
	})
	t.Run("explicit non default port stays, tls change writes tls", func(t *testing.T) {
		req := formOf(raw, func(c *Connect) {
			c.inputs[fPort].SetValue("1143")
			c.tls = config.TLSStartTLS
		})
		got := ApplyEdit(raw, req)
		if got.IMAPPort != 1143 || got.TLS != config.TLSStartTLS {
			t.Errorf("port %d tls %q", got.IMAPPort, got.TLS)
		}
	})
	t.Run("explicit default port stays when it was written", func(t *testing.T) {
		old := raw
		old.IMAPPort, old.TLS, old.SMTPPort, old.SMTPTLS = 993, config.TLSImplicit, 465, config.TLSImplicit
		if got := ApplyEdit(old, formOf(old, nil)); !reflect.DeepEqual(got, old) {
			t.Errorf("got %+v", got)
		}
	})
	t.Run("removing SMTP host clears the sending keys", func(t *testing.T) {
		yes := true
		old := raw
		old.SaveSent = &yes
		req := formOf(old, func(c *Connect) {
			c.inputs[fSMTPHost].SetValue("")
			c.inputs[fSMTPPort].SetValue("")
		})
		got := ApplyEdit(old, req)
		if got.SMTPHost != "" || got.SMTPPort != 0 || got.SMTPTLS != "" || got.FromName != "" || got.Aliases != nil || got.SaveSent != nil {
			t.Errorf("sending keys left: %+v", got)
		}
		if got.ArchiveFolder != "Done" || got.TLSCert != "certs/a.pem" {
			t.Errorf("other keys lost: %+v", got)
		}
	})
	t.Run("adding SMTP host with default port writes none", func(t *testing.T) {
		old := raw
		old.SMTPHost, old.FromName, old.Aliases = "", "", nil
		req := formOf(old, func(c *Connect) {
			c.inputs[fSMTPHost].SetValue("smtp.new.com")
			c.inputs[fSMTPPort].SetValue("465")
		})
		got := ApplyEdit(old, req)
		if got.SMTPHost != "smtp.new.com" || got.SMTPPort != 0 || got.SMTPTLS != "" {
			t.Errorf("got %+v", got)
		}
	})
	t.Run("username equal to email is dropped", func(t *testing.T) {
		req := formOf(raw, func(c *Connect) { c.inputs[fUser].SetValue("me@example.com") })
		if got := ApplyEdit(raw, req); got.Username != "" {
			t.Errorf("username = %q", got.Username)
		}
	})
	t.Run("1Password and keychain sources", func(t *testing.T) {
		req := formOf(raw, func(c *Connect) {
			c.source = sourceOnePassword
			c.inputs[fSecret].SetValue("op://V/I/p")
		})
		if got := ApplyEdit(raw, req); got.PasswordCmd != "op read op://V/I/p" {
			t.Errorf("PasswordCmd = %q", got.PasswordCmd)
		}
		req = formOf(raw, func(c *Connect) {
			c.source = sourceKeychain
			c.inputs[fSecret].SetValue("pw")
		})
		if got := ApplyEdit(raw, req); got.PasswordCmd != "" || req.Password != "pw" {
			t.Errorf("PasswordCmd = %q password %q", got.PasswordCmd, req.Password)
		}
	})
}

// toSource moves a new or edit form to the wanted password source by pressing
// right on the Login row.
func toSource(t *testing.T, c Connect, want int) Connect {
	t.Helper()
	c.setFocus(fSource)
	for range 4 {
		if c.source == want {
			return c
		}
		c, _ = keys(c, "right")
	}
	t.Fatalf("source %d not reachable", want)
	return c
}

func oauthEdit(provider string) config.Account {
	a := editAccount()
	a.Auth, a.OAuthProvider, a.OAuthClientID = config.AuthOAuth2, provider, "client-1"
	return a
}

func fieldsOf(c Connect) []int { return c.focusable() }

func hasField(c Connect, f int) bool {
	for _, g := range c.focusable() {
		if g == f {
			return true
		}
	}
	return false
}

func TestConnect_SourceCycle(t *testing.T) {
	c := newConnect()
	c.setFocus(fSource)
	var got []int
	for range 4 {
		got = append(got, c.source)
		c, _ = keys(c, "right")
	}
	if want := []int{sourceOnePassword, sourceKeychain, sourceOAuth, sourceOnePassword}; !reflect.DeepEqual(got, want) {
		t.Errorf("new cycle = %v, want %v", got, want)
	}

	e := newEdit(editAccount())
	e.setFocus(fSource)
	got = nil
	for range 5 {
		got = append(got, e.source)
		e, _ = keys(e, "right")
	}
	if want := []int{sourceKeep, sourceOnePassword, sourceKeychain, sourceOAuth, sourceKeep}; !reflect.DeepEqual(got, want) {
		t.Errorf("edit cycle = %v, want %v", got, want)
	}

	v := ansi.Strip(toSource(t, newConnect(), sourceOAuth).View())
	for _, want := range []string{"Login", "1Password", "keychain", "OAuth 2.0"} {
		if !strings.Contains(v, want) {
			t.Errorf("view lacks %q:\n%s", want, v)
		}
	}
	if !strings.Contains(ansi.Strip(newEdit(editAccount()).View()), "keep current") {
		t.Error("edit view lacks keep current")
	}
}

func TestConnect_FieldVisibilityAndFocusOrder(t *testing.T) {
	tests := []struct {
		name     string
		source   int
		provider string
		want     []int
		absent   []int
	}{
		{"1password", sourceOnePassword, config.ProviderMicrosoft, []int{fSecret}, []int{fProvider, fClientID, fTenant, fClientSecret}},
		{"keychain", sourceKeychain, config.ProviderGoogle, []int{fSecret}, []int{fProvider, fClientID, fTenant, fClientSecret}},
		{"keep", sourceKeep, config.ProviderGoogle, nil, []int{fSecret, fProvider, fClientID, fTenant, fClientSecret}},
		{"oauth microsoft", sourceOAuth, config.ProviderMicrosoft, []int{fProvider, fClientID, fTenant}, []int{fSecret, fClientSecret}},
		{"oauth google", sourceOAuth, config.ProviderGoogle, []int{fProvider, fClientID, fClientSecret}, []int{fSecret, fTenant}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newEdit(editAccount())
			c.source, c.provider = tt.source, tt.provider
			for _, f := range tt.want {
				if !hasField(c, f) {
					t.Errorf("field %d not focusable", f)
				}
			}
			for _, f := range tt.absent {
				if hasField(c, f) {
					t.Errorf("field %d focusable", f)
				}
			}
			// Focus order follows the field constants, which the view follows.
			fs := fieldsOf(c)
			for i := 1; i < len(fs); i++ {
				if fs[i] <= fs[i-1] {
					t.Errorf("focus order not ascending: %v", fs)
				}
			}
			v := ansi.Strip(c.View())
			for f, label := range map[int]string{fProvider: "Provider", fClientID: "Client ID", fTenant: "Tenant", fClientSecret: "Secret"} {
				if shown := strings.Contains(v, label+" "); shown != hasField(c, f) {
					t.Errorf("%q shown=%v focusable=%v:\n%s", label, shown, hasField(c, f), v)
				}
			}
		})
	}

	c := toSource(t, newConnect(), sourceOAuth)
	c, _ = keys(c, "tab")
	if c.focus != fProvider {
		t.Errorf("focus after Login = %d, want Provider", c.focus)
	}
	if c, _ = keys(c, "tab"); c.focus != fClientID || !c.inputs[fClientID].Focused() {
		t.Errorf("focus = %d, want Client ID", c.focus)
	}
}

func TestConnect_OAuthPresets(t *testing.T) {
	for _, domain := range []string{"outlook.com", "hotmail.com", "live.com", "msn.com", "office365.com"} {
		c := fill(newConnect(), "MS", "me@"+domain)
		if h, p := c.inputs[fHost].Value(), c.inputs[fPort].Value(); h != "outlook.office365.com" || p != "993" || c.tls != config.TLSImplicit {
			t.Errorf("%s: IMAP = %q %q %q", domain, h, p, c.tls)
		}
		if h, p := c.inputs[fSMTPHost].Value(), c.inputs[fSMTPPort].Value(); h != "smtp.office365.com" || p != "587" || c.smtpTLS != config.TLSStartTLS || c.inputs[fSMTPPort].Placeholder != "587" {
			t.Errorf("%s: SMTP = %q %q %q", domain, h, p, c.smtpTLS)
		}
		if c.provider != config.ProviderMicrosoft {
			t.Errorf("%s: provider = %q", domain, c.provider)
		}
	}

	c := fill(newConnect(), "G", "me@gmail.com")
	if c.provider != config.ProviderGoogle || c.inputs[fSMTPHost].Value() != "smtp.gmail.com" || c.inputs[fSMTPPort].Value() != "465" || c.smtpTLS != config.TLSImplicit {
		t.Errorf("gmail: provider %q SMTP %q %q %q", c.provider, c.inputs[fSMTPHost].Value(), c.inputs[fSMTPPort].Value(), c.smtpTLS)
	}
}

func TestConnect_CustomDomainFollowsProviderChoice(t *testing.T) {
	c := fill(newConnect(), "Work", "me@corp.example")
	if c.inputs[fHost].Value() != "" || c.inputs[fSMTPHost].Value() != "" {
		t.Fatal("a custom domain without OAuth must not pre-fill")
	}
	c = toSource(t, c, sourceOAuth)
	if c.inputs[fHost].Value() != "outlook.office365.com" || c.inputs[fSMTPHost].Value() != "smtp.office365.com" || c.smtpTLS != config.TLSStartTLS {
		t.Errorf("Microsoft preset missing: %q %q %q", c.inputs[fHost].Value(), c.inputs[fSMTPHost].Value(), c.smtpTLS)
	}

	// Switching preset replaces the auto-filled values.
	c.setFocus(fProvider)
	c, _ = keys(c, "right")
	if c.provider != config.ProviderGoogle || !c.providerTouched {
		t.Fatalf("provider %q touched %v", c.provider, c.providerTouched)
	}
	if c.inputs[fHost].Value() != "imap.gmail.com" || c.inputs[fSMTPHost].Value() != "smtp.gmail.com" || c.inputs[fSMTPPort].Value() != "465" || c.smtpTLS != config.TLSImplicit {
		t.Errorf("Google preset missing: %q %q %q %q", c.inputs[fHost].Value(), c.inputs[fSMTPHost].Value(), c.inputs[fSMTPPort].Value(), c.smtpTLS)
	}
	c, _ = keys(c, "space")
	if c.provider != config.ProviderMicrosoft || c.inputs[fHost].Value() != "outlook.office365.com" || c.inputs[fSMTPPort].Value() != "587" {
		t.Errorf("toggle back: %q %q %q", c.provider, c.inputs[fHost].Value(), c.inputs[fSMTPPort].Value())
	}

	// Leaving OAuth for a source without a preset clears what was filled.
	c = toSource(t, c, sourceOnePassword)
	if c.inputs[fHost].Value() != "" || c.inputs[fPort].Value() != "" || c.inputs[fSMTPHost].Value() != "" || c.inputs[fSMTPPort].Value() != "" {
		t.Errorf("preset kept after leaving OAuth: %q %q", c.inputs[fHost].Value(), c.inputs[fSMTPHost].Value())
	}
}

func TestConnect_PresetNeverOverwritesTypedHost(t *testing.T) {
	c := fill(newConnect(), "Work", "me@corp.example", "", "mail.corp.example")
	c.setFocus(fSMTPHost)
	c = typeIn(c, "smtp.corp.example")
	c = toSource(t, c, sourceOAuth)
	c.setFocus(fProvider)
	c, _ = keys(c, "right")
	if c.inputs[fHost].Value() != "mail.corp.example" || c.inputs[fSMTPHost].Value() != "smtp.corp.example" {
		t.Errorf("typed hosts overwritten: %q %q", c.inputs[fHost].Value(), c.inputs[fSMTPHost].Value())
	}
	c.setFocus(fEmail)
	c = typeIn(c, "x")
	c = c.Paste(tea.PasteMsg{Content: "@outlook.com"})
	if c.inputs[fHost].Value() != "mail.corp.example" {
		t.Errorf("host = %q after a detected domain", c.inputs[fHost].Value())
	}
}

func TestConnect_ProviderFromEmailUnlessTouched(t *testing.T) {
	c := fill(newConnect(), "Work", "me@gmail.com")
	if c.provider != config.ProviderGoogle {
		t.Errorf("provider = %q, want google", c.provider)
	}
	c = toSource(t, c, sourceOAuth)
	if c.provider != config.ProviderGoogle {
		t.Errorf("provider = %q after choosing OAuth", c.provider)
	}

	c = newConnect()
	c.setFocus(fProvider)
	c, _ = keys(c, "right") // google, chosen by hand
	c.setFocus(fEmail)
	c = typeIn(c, "me@outlook.com")
	if c.provider != config.ProviderGoogle {
		t.Errorf("provider = %q, a touched provider must not follow the email", c.provider)
	}
}

func TestEditAccount_OAuthNeverAutoFills(t *testing.T) {
	c := newEdit(oauthEdit(config.ProviderMicrosoft))
	c = toSource(t, c, sourceOAuth)
	c.setFocus(fProvider)
	c, _ = keys(c, "right")
	c.setFocus(fEmail)
	c = typeIn(c, "x@outlook.com")
	if c.autoHost || c.autoSMTP || c.inputs[fHost].Value() != "imap.example.com" || c.inputs[fSMTPHost].Value() != "smtp.example.com" {
		t.Errorf("edit auto-filled: %q %q auto %v %v", c.inputs[fHost].Value(), c.inputs[fSMTPHost].Value(), c.autoHost, c.autoSMTP)
	}
}

func oauthForm(t *testing.T, provider string) Connect {
	t.Helper()
	c := fill(newConnect(), "Work", "me@example.com", "", "mail.example.com", "")
	c = toSource(t, c, sourceOAuth)
	if provider == config.ProviderGoogle {
		c.setFocus(fProvider)
		c, _ = keys(c, "right")
	}
	return c
}

func TestConnect_OAuthValidation(t *testing.T) {
	c := oauthForm(t, config.ProviderMicrosoft)
	if err := c.validate(); err != "Client ID is required: register an app first (see docs/oauth.md)" {
		t.Errorf("validate() = %q", err)
	}
	c.inputs[fClientID].SetValue("abc")
	if err := c.validate(); err != "" {
		t.Errorf("Microsoft with a client ID: %q", err)
	}

	g := oauthForm(t, config.ProviderGoogle)
	g.inputs[fClientID].SetValue("abc")
	if err := g.validate(); err != "Google needs the client secret from your OAuth client" {
		t.Errorf("validate() = %q", err)
	}
	g.inputs[fClientSecret].SetValue("s3cret")
	if err := g.validate(); err != "" {
		t.Errorf("Google with a secret: %q", err)
	}

	// Editing a Google OAuth account: empty means keep the saved secret.
	e := newEdit(oauthEdit(config.ProviderGoogle))
	e = toSource(t, e, sourceOAuth)
	if err := e.validate(); err != "" {
		t.Errorf("edit Google keeps secret: %q", err)
	}
	if got := e.inputs[fClientSecret].Placeholder; got != "leave empty to keep the saved one" {
		t.Errorf("placeholder = %q", got)
	}
	// Switching an existing Microsoft account to Google needs the secret.
	m := newEdit(oauthEdit(config.ProviderMicrosoft))
	m = toSource(t, m, sourceOAuth)
	m.setFocus(fProvider)
	m, _ = keys(m, "right")
	if err := m.validate(); !strings.Contains(err, "Google needs the client secret") {
		t.Errorf("microsoft to google: %q", err)
	}

	// Enter on the button reports it in the form.
	c, r := keys(oauthForm(t, config.ProviderMicrosoft), "tab", "tab", "tab", "tab", "tab", "tab", "tab", "tab", "enter") // ... submit
	if r != ConnectPending || !strings.Contains(c.err, "Client ID is required") {
		t.Errorf("result %v err %q", r, c.err)
	}
}

func TestConnect_OAuthRequest(t *testing.T) {
	tests := []struct {
		name, tenant string
		want         string
	}{
		{"empty", "", ""},
		{"common", "  Common ", ""},
		{"specific", " contoso.onmicrosoft.com ", "contoso.onmicrosoft.com"},
	}
	for _, tt := range tests {
		t.Run("microsoft "+tt.name, func(t *testing.T) {
			c := oauthForm(t, config.ProviderMicrosoft)
			c.inputs[fClientID].SetValue(" abc ")
			c.inputs[fTenant].SetValue(tt.tenant)
			c.inputs[fSecret].SetValue("ignored")
			req := c.Request()
			a := req.Account
			if !c.OAuth() || a.Auth != config.AuthOAuth2 || a.OAuthProvider != config.ProviderMicrosoft || a.OAuthClientID != "abc" || a.OAuthTenant != tt.want || a.PasswordCmd != "" || req.Password != "" || req.ClientSecret != "" {
				t.Errorf("Request() = %+v", req)
			}
		})
	}

	g := oauthForm(t, config.ProviderGoogle)
	g.inputs[fClientID].SetValue("abc")
	g.inputs[fTenant].SetValue("contoso")
	g.inputs[fClientSecret].SetValue(" s3 cret ")
	req := g.Request()
	if a := req.Account; a.OAuthProvider != config.ProviderGoogle || a.OAuthTenant != "" || req.ClientSecret != " s3 cret " || a.Auth != config.AuthOAuth2 {
		t.Errorf("Request() = %+v", req)
	}
	if er := g.EditRequest(); er.ClientSecret != " s3 cret " || er.KeepPassword {
		t.Errorf("EditRequest() = %+v", er)
	}
	if newConnect().OAuth() {
		t.Error("OAuth() true for a new form")
	}
}

func TestConnect_OAuthSecretIsMasked(t *testing.T) {
	g := oauthForm(t, config.ProviderGoogle)
	g.setFocus(fClientSecret)
	g = typeIn(g, "topsecret")
	if v := ansi.Strip(g.View()); strings.Contains(v, "topsecret") {
		t.Errorf("client secret shown:\n%s", v)
	}
}

func TestApplyEdit_OAuth(t *testing.T) {
	formOf := func(old config.Account, change func(*Connect)) EditRequest {
		c := newEdit(old.WithDefaults())
		change(&c)
		return c.EditRequest()
	}
	pw := editAccount()
	pw.PasswordCmd = "op read x"

	t.Run("password to OAuth", func(t *testing.T) {
		req := formOf(pw, func(c *Connect) {
			*c = toSource(t, *c, sourceOAuth)
			c.inputs[fClientID].SetValue("cid")
			c.inputs[fTenant].SetValue("contoso")
		})
		got := ApplyEdit(pw, req)
		if got.PasswordCmd != "" || got.Auth != config.AuthOAuth2 || got.OAuthProvider != config.ProviderMicrosoft || got.OAuthClientID != "cid" || got.OAuthTenant != "contoso" {
			t.Errorf("got %+v", got)
		}
		if got.ArchiveFolder != "Done" || got.TLSCert != "/certs/a.pem" {
			t.Errorf("other keys lost: %+v", got)
		}
	})
	t.Run("OAuth to keychain clears the OAuth fields", func(t *testing.T) {
		old := oauthEdit(config.ProviderGoogle)
		old.OAuthClientSecretCmd = "cmd"
		old.OAuthTenant = "x"
		req := formOf(old, func(c *Connect) { *c = toSource(t, *c, sourceKeychain); c.inputs[fSecret].SetValue("pw") })
		got := ApplyEdit(old, req)
		if got.Auth != "" || got.OAuthProvider != "" || got.OAuthClientID != "" || got.OAuthTenant != "" || got.OAuthClientSecretCmd != "" || got.PasswordCmd != "" {
			t.Errorf("got %+v", got)
		}
	})
	t.Run("keep leaves everything", func(t *testing.T) {
		old := oauthEdit(config.ProviderGoogle)
		old.OAuthClientSecretCmd = "cmd"
		if got := ApplyEdit(old, formOf(old, func(*Connect) {})); !reflect.DeepEqual(got, old) {
			t.Errorf("got %+v", got)
		}
	})
	t.Run("auth password is preserved", func(t *testing.T) {
		old := pw
		old.Auth = config.AuthPassword
		req := formOf(old, func(c *Connect) { *c = toSource(t, *c, sourceKeychain); c.inputs[fSecret].SetValue("pw") })
		if got := ApplyEdit(old, req); got.Auth != config.AuthPassword {
			t.Errorf("Auth = %q", got.Auth)
		}
		req = formOf(old, func(c *Connect) { *c = toSource(t, *c, sourceOAuth); c.inputs[fClientID].SetValue("c") })
		if got := ApplyEdit(old, req); got.Auth != config.AuthOAuth2 {
			t.Errorf("Auth = %q, OAuth must replace password", got.Auth)
		}
	})
	t.Run("client secret cmd kept only for Google without a new secret", func(t *testing.T) {
		old := oauthEdit(config.ProviderGoogle)
		old.OAuthClientSecretCmd = "cmd"
		edit := func(secret string) config.Account {
			return ApplyEdit(old, formOf(old, func(c *Connect) {
				*c = toSource(t, *c, sourceOAuth)
				c.inputs[fClientSecret].SetValue(secret)
			}))
		}
		if got := edit(""); got.OAuthClientSecretCmd != "cmd" {
			t.Errorf("cmd = %q, want it kept", got.OAuthClientSecretCmd)
		}
		if got := edit("new"); got.OAuthClientSecretCmd != "" {
			t.Errorf("cmd = %q, want it cleared by a new secret", got.OAuthClientSecretCmd)
		}
		ms := ApplyEdit(old, formOf(old, func(c *Connect) {
			*c = toSource(t, *c, sourceOAuth)
			c.setFocus(fProvider)
			*c, _ = keys(*c, "right")
		}))
		if ms.OAuthProvider != config.ProviderMicrosoft || ms.OAuthClientSecretCmd != "" {
			t.Errorf("microsoft: %+v", ms)
		}
	})
}

func TestEditAccount_OAuthPrefill(t *testing.T) {
	a := oauthEdit(config.ProviderGoogle)
	c := newEdit(a.WithDefaults())
	if c.provider != config.ProviderGoogle || !c.providerTouched || c.inputs[fClientID].Value() != "client-1" || c.inputs[fTenant].Value() != "" {
		t.Errorf("provider %q touched %v client %q tenant %q", c.provider, c.providerTouched, c.inputs[fClientID].Value(), c.inputs[fTenant].Value())
	}
	if v := ansi.Strip(c.View()); !strings.Contains(v, "OAuth 2.0 (google)") {
		t.Errorf("current row:\n%s", v)
	}

	m := oauthEdit(config.ProviderMicrosoft)
	m.OAuthTenant = "contoso"
	if c = newEdit(m); c.inputs[fTenant].Value() != "contoso" || c.provider != config.ProviderMicrosoft {
		t.Errorf("tenant %q provider %q", c.inputs[fTenant].Value(), c.provider)
	}
	if c = newEdit(oauthEdit(config.ProviderMicrosoft).WithDefaults()); c.inputs[fTenant].Value() != "" {
		t.Errorf("tenant %q, want the common default left empty", c.inputs[fTenant].Value())
	}
	if v := ansi.Strip(c.View()); !strings.Contains(v, "OAuth 2.0 (microsoft)") {
		t.Errorf("current row:\n%s", v)
	}

	cmd := oauthEdit(config.ProviderMicrosoft)
	cmd.PasswordCmd = "get-token"
	c = newEdit(cmd)
	if c.inputs[fClientID].Value() != "" || c.providerTouched {
		t.Errorf("token command account prefilled: client %q touched %v", c.inputs[fClientID].Value(), c.providerTouched)
	}
	if v := ansi.Strip(c.View()); !strings.Contains(v, "token command") {
		t.Errorf("current row:\n%s", v)
	}
	if v := ansi.Strip(newEdit(editAccount()).View()); !strings.Contains(v, "keychain") {
		t.Errorf("current row:\n%s", v)
	}
}

func TestConnect_OAuthNoteAndButton(t *testing.T) {
	c := oauthForm(t, config.ProviderMicrosoft)
	v := ansi.Strip(c.View())
	for _, want := range []string{"Signs in with your browser; tokens go in the OS keychain", "[ Sign in & save ]"} {
		if !strings.Contains(v, want) {
			t.Errorf("view lacks %q:\n%s", want, v)
		}
	}
	if strings.Contains(v, "[ Test & save ]") {
		t.Error("OAuth form shows the test button")
	}
	if got := c.inputs[fClientID].Placeholder; got != "your app's client ID" {
		t.Errorf("client ID placeholder = %q", got)
	}
	if got := c.inputs[fTenant].Placeholder; got != "common" {
		t.Errorf("tenant placeholder = %q", got)
	}
	if got := c.inputs[fClientSecret].Placeholder; got != "client secret (Google Cloud)" {
		t.Errorf("secret placeholder = %q", got)
	}
}

func TestConnect_SignInProgress(t *testing.T) {
	long := "https://login.microsoftonline.com/common/oauth2/v2.0/authorize?client_id=0123456789abcdef&redirect_uri=http%3A%2F%2F127.0.0.1%3A53682&state=" + strings.Repeat("abcdef0123", 12)
	c := oauthForm(t, config.ProviderMicrosoft)
	c.SetSignIn(SignIn{URL: long})

	v := ansi.Strip(c.View())
	if !c.busy || strings.Contains(v, "Testing connection") {
		t.Errorf("busy %v, view:\n%s", c.busy, v)
	}
	for _, want := range []string{"Waiting for you to sign in in your browser…", "If it didn't open, visit:"} {
		if !strings.Contains(v, want) {
			t.Errorf("view lacks %q:\n%s", want, v)
		}
	}
	// The whole URL is present across the wrapped lines.
	var joined strings.Builder
	in := false
	for _, l := range strings.Split(v, "\n") {
		l = strings.Trim(l, "│ ")
		if strings.HasPrefix(l, "https://") {
			in = true
		}
		if in && l != "" && !strings.HasPrefix(l, "╰") && !strings.HasPrefix(l, "└") {
			joined.WriteString(l)
		}
	}
	if !strings.Contains(joined.String(), long) {
		t.Errorf("URL cut or missing:\n%s", v)
	}
	if got := hintText(c.Hints()); got != "Cancel: esc | Use a code instead: ctrl+d" {
		t.Errorf("hints = %q", got)
	}
	if _, r := keys(c, "ctrl+d"); r != ConnectUseCode {
		t.Errorf("ctrl+d result = %v", r)
	}
	if c2, r := keys(c, "x", "tab"); r != ConnectPending || c2.inputs[fName].Value() != "Work" {
		t.Error("a key got through while signing in")
	}
	if _, r := keys(c, "esc"); r != ConnectCancelled {
		t.Error("esc should cancel the sign-in")
	}

	c.SetBusy(false)
	if v := ansi.Strip(c.View()); strings.Contains(v, "Waiting for you") || (SignIn{}) != c.signIn {
		t.Errorf("sign-in not cleared:\n%s", v)
	}
	if _, r := keys(c, "ctrl+d"); r != ConnectPending {
		t.Error("ctrl+d acted when not busy")
	}

	// Google has no device code, and a device code sign-in has no URL to swap.
	g := oauthForm(t, config.ProviderGoogle)
	g.SetSignIn(SignIn{URL: "https://accounts.google.com/o/oauth2/auth"})
	if _, r := keys(g, "ctrl+d"); r != ConnectPending {
		t.Errorf("google ctrl+d result = %v", r)
	}
	if got := hintText(g.Hints()); got != "Cancel: esc" {
		t.Errorf("google hints = %q", got)
	}

	d := oauthForm(t, config.ProviderMicrosoft)
	d.SetSignIn(SignIn{UserCode: "ABCD-EFGH", VerificationURI: "https://microsoft.com/devicelogin"})
	v = ansi.Strip(d.View())
	for _, want := range []string{"Go to https://microsoft.com/devicelogin", "and enter the code", "ABCD-EFGH"} {
		if !strings.Contains(v, want) {
			t.Errorf("view lacks %q:\n%s", want, v)
		}
	}
	if _, r := keys(d, "ctrl+d"); r != ConnectPending {
		t.Errorf("device code ctrl+d result = %v", r)
	}
	if got := hintText(d.Hints()); got != "Cancel: esc" {
		t.Errorf("device hints = %q", got)
	}
}

func TestEditAccount_DeleteConfirmOAuth(t *testing.T) {
	open := func(a config.Account) Connect {
		c := newEdit(a)
		c, _ = keys(c, "shift+tab", "enter")
		return c
	}
	c := open(oauthEdit(config.ProviderMicrosoft))
	if got := hintText(c.Hints()); got != "Delete + sign-in: y | Delete, keep sign-in: k | Cancel: n" {
		t.Errorf("hints = %q", got)
	}
	if c, r := keys(c, "y"); r != ConnectDelete || !c.DeletePassword() {
		t.Errorf("y: result %v deletePassword %v", r, c.DeletePassword())
	}
	if c, r := keys(open(oauthEdit(config.ProviderMicrosoft)), "k"); r != ConnectDelete || c.DeletePassword() {
		t.Errorf("k: result %v deletePassword %v", r, c.DeletePassword())
	}

	cmd := oauthEdit(config.ProviderMicrosoft)
	cmd.PasswordCmd = "get-token"
	if got := hintText(open(cmd).Hints()); got != "Delete: y | Cancel: n" {
		t.Errorf("token command hints = %q", got)
	}
}

func TestConnect_RendersWithin80x24(t *testing.T) {
	check := func(t *testing.T, c Connect, button string) {
		t.Helper()
		c.SetSize(80, 24)
		v := c.View()
		if h := lipgloss.Height(v); h > 24 {
			t.Errorf("height = %d", h)
		}
		for _, l := range strings.Split(v, "\n") {
			if w := lipgloss.Width(l); w > 80 {
				t.Errorf("line is %d wide: %q", w, ansi.Strip(l))
			}
		}
		if !strings.Contains(ansi.Strip(v), button) {
			t.Errorf("button %q not visible:\n%s", button, ansi.Strip(v))
		}
	}
	for _, src := range []int{sourceOnePassword, sourceKeychain, sourceOAuth} {
		for _, p := range []string{config.ProviderMicrosoft, config.ProviderGoogle} {
			t.Run(fmt.Sprintf("new source %d %s", src, p), func(t *testing.T) {
				c := newConnect()
				c.source, c.provider = src, p
				button := "[ Test & save ]"
				if src == sourceOAuth {
					button = "[ Sign in & save ]"
				}
				check(t, c, button)
			})
		}
	}
	for _, src := range []int{sourceKeep, sourceOnePassword, sourceKeychain, sourceOAuth} {
		for _, p := range []string{config.ProviderMicrosoft, config.ProviderGoogle} {
			t.Run(fmt.Sprintf("edit source %d %s", src, p), func(t *testing.T) {
				c := newEdit(oauthEdit(p))
				c.source = src
				button := "[ Test & save ]"
				if src == sourceOAuth {
					button = "[ Sign in & save ]"
				}
				check(t, c, button)
				if v := ansi.Strip(c.View()); !strings.Contains(v, "[ Delete account ]") {
					t.Errorf("delete row cut:\n%s", v)
				}
			})
		}
	}

	// A long URL keeps its end visible at 80x24.
	c := oauthForm(t, config.ProviderMicrosoft)
	url := "https://example.com/authorize?" + strings.Repeat("0123456789", 20) + "END"
	c.SetSignIn(SignIn{URL: url})
	c.SetSize(80, 24)
	if v := ansi.Strip(c.View()); !strings.Contains(v, "END") {
		t.Errorf("URL end cut at 80x24:\n%s", v)
	}
}
