package popup

import (
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
