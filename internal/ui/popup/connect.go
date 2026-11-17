package popup

import (
	"fmt"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/charmbracelet/x/ansi"
	"github.com/dkouvdis/dk-email/internal/ui/theme"

	"github.com/dkouvdis/dk-email/internal/config"
)

// Form fields, in focus order.
const (
	fName = iota
	fEmail
	fUser
	fHost
	fPort
	fTLS
	fSource
	fSecret       // the 1Password reference or the keychain password
	fProvider     // OAuth only: Microsoft or Google
	fClientID     // OAuth only
	fTenant       // OAuth with Microsoft only
	fClientSecret // OAuth with Google only
	fSMTPHost
	fSMTPPort
	fSMTPTLS
	fAliases
	fSubmit
	fDelete // edit mode only
	fieldCount
)

const (
	sourceOnePassword = iota
	sourceKeychain
	sourceKeep // edit mode only: leave the password as it is
	sourceOAuth
)

const (
	connectInnerW = 67 // fits the edit form's Login row and the OAuth note
	labelW        = 10
)

// ConnectResult is what a key press did to the form.
type ConnectResult int

const (
	ConnectPending ConnectResult = iota
	ConnectSubmitted
	ConnectCancelled
	ConnectDelete     // the edited account was deleted; see DeletePassword
	ConnectSaveAnyway // save the edit without a working login test
	ConnectUseCode    // during a Microsoft browser sign-in, the user asked for a device code instead
)

// ConnectRequest is a submitted form. Password is set only for the keychain
// option; with 1Password the account's PasswordCmd reads it. ClientSecret is
// the Google client secret typed in the form, for the keychain; empty
// otherwise.
type ConnectRequest struct {
	Account      config.Account
	Password     string
	ClientSecret string
}

// EditRequest is a submitted edit form. Account holds the form's fields
// only (alias emails without names); ApplyEdit merges it into the saved
// account. Password is set only for the keychain option, and KeepPassword
// means the password source wasn't touched. ClientSecret is the Google client
// secret typed in the form, for the keychain; empty otherwise.
type EditRequest struct {
	Account      config.Account
	Password     string
	KeepPassword bool
	ClientSecret string
}

// SignIn is what the form shows while an OAuth sign-in runs. URL is the
// browser address, shown in case the browser didn't open; UserCode and
// VerificationURI are set instead for a Microsoft device code sign-in.
type SignIn struct{ URL, UserCode, VerificationURI string }

// Connect is the connect-account form, or, from NewPasswordPrompt, a prompt
// for a saved account's missing password.
type Connect struct {
	inputs   [fieldCount]textinput.Model // only the text fields are used
	focus    int
	tls      string
	source   int
	autoHost bool // the host and port came from a provider preset

	smtpTLS     string
	autoSMTP    bool // the SMTP fields came from a provider preset
	smtpTouched bool // the user edited an SMTP field, so a preset never fills it

	provider        string // config.ProviderMicrosoft or ProviderGoogle, for OAuth
	providerTouched bool   // the provider was chosen by hand, so the email never selects it

	passwordOnly bool
	account      config.Account // the saved account in password-only mode
	existing     []string       // lowercased names already in use

	editing    bool
	orig       config.Account // the account being edited, as loaded
	testFailed bool           // a failed test offers ctrl+s to save anyway
	confirming bool           // asking whether to delete the account
	deletePW   bool           // the delete was confirmed with y on a keychain account

	busy       bool
	signIn     SignIn // set while busy with an OAuth sign-in
	err        string
	maxW, maxH int

	st   helpStyles
	keys struct{ next, prev, submit, cancel, toggle, saveAnyway, useCode key.Binding }
}

// NewConnect is an empty form for a new account. existing are the account
// names already taken.
func NewConnect(th theme.Theme, existing []string) Connect {
	c := newForm(th)
	for _, n := range existing {
		c.existing = append(c.existing, strings.ToLower(strings.TrimSpace(n)))
	}
	c.setFocus(fName)
	return c
}

// NewPasswordPrompt asks for a saved account's password, to keep in the
// keychain. Only the password can be edited.
func NewPasswordPrompt(th theme.Theme, acct config.Account) Connect {
	c := newForm(th)
	c.passwordOnly, c.account = true, acct
	c.source = sourceKeychain
	c.setFocus(fSecret)
	c.updateSecretInput()
	return c
}

// NewEditAccount is the form prefilled from acct, as loaded (defaults
// applied). existing are the other accounts' names. The password source
// starts on Keep current, and a Delete account row follows Submit.
func NewEditAccount(th theme.Theme, acct config.Account, existing []string) Connect {
	c := NewConnect(th, existing)
	c.editing, c.orig = true, acct
	c.source = sourceKeep
	c.autoHost = false // a saved host is never replaced by a preset
	c.tls, c.smtpTLS = acct.TLS, config.TLSImplicit
	c.inputs[fName].SetValue(acct.Name)
	c.inputs[fEmail].SetValue(acct.Email)
	if acct.Username != acct.Email {
		c.inputs[fUser].SetValue(acct.Username)
	}
	c.inputs[fHost].SetValue(acct.IMAPHost)
	c.inputs[fPort].SetValue(strconv.Itoa(acct.IMAPPort))
	c.inputs[fPort].Placeholder = defaultPort(acct.TLS, "993", "143")
	if acct.SMTPHost != "" {
		c.smtpTLS = acct.SMTPTLS
		c.inputs[fSMTPHost].SetValue(acct.SMTPHost)
		c.inputs[fSMTPPort].SetValue(strconv.Itoa(acct.SMTPPort))
		emails := make([]string, len(acct.Aliases))
		for i, a := range acct.Aliases {
			emails[i] = a.Email
		}
		c.inputs[fAliases].SetValue(strings.Join(emails, ", "))
	}
	c.inputs[fSMTPPort].Placeholder = defaultPort(c.smtpTLS, "465", "587")
	c.smtpTouched = true // a preset never overwrites a saved account
	if acct.OAuth() && acct.PasswordCmd == "" {
		if acct.OAuthProvider == config.ProviderGoogle || acct.OAuthProvider == config.ProviderMicrosoft {
			c.provider, c.providerTouched = acct.OAuthProvider, true
		}
		c.inputs[fClientID].SetValue(acct.OAuthClientID)
		if !strings.EqualFold(acct.OAuthTenant, oauthDefaultTenant) {
			c.inputs[fTenant].SetValue(acct.OAuthTenant) // the loaded account has the default applied
		}
	}
	if acct.OAuth() && acct.OAuthProvider == config.ProviderGoogle {
		c.inputs[fClientSecret].Placeholder = "leave empty to keep the saved one"
	}
	if ref, ok := opReference(acct.PasswordCmd); ok {
		c.inputs[fSecret].SetValue(ref) // kept for when 1Password is chosen
	}
	c.updateSecretInput()
	return c
}

func defaultPort(tls, implicit, starttls string) string {
	if tls == config.TLSStartTLS {
		return starttls
	}
	return implicit
}

// opReference extracts <ref> from `op read <ref>`, undoing shellQuote.
func opReference(cmd string) (string, bool) {
	ref, ok := strings.CutPrefix(strings.TrimSpace(cmd), "op read ")
	if !ok {
		return "", false
	}
	ref = strings.TrimSpace(ref)
	if len(ref) >= 2 && ref[0] == '\'' && ref[len(ref)-1] == '\'' {
		ref = strings.ReplaceAll(ref[1:len(ref)-1], `'\''`, "'")
	}
	return ref, ref != ""
}

func newForm(th theme.Theme) Connect {
	c := Connect{tls: config.TLSImplicit, smtpTLS: config.TLSImplicit, provider: config.ProviderMicrosoft, st: newHelpStyles(th)}
	placeholders := map[int]string{
		fName:  "Personal",
		fEmail: "me@example.com",
		fUser:  "same as email",
		fHost:  "imap.example.com",
		fPort:  "993",

		fClientID:     "your app's client ID",
		fTenant:       oauthDefaultTenant,
		fClientSecret: "client secret (Google Cloud)",

		fSMTPHost: "optional, enables sending",
		fSMTPPort: "465",
		fAliases:  "optional: me@mydomain.com, hi@mydomain.com",
	}
	for i := range fieldCount {
		if !isInput(i) {
			continue
		}
		in := textinput.New()
		in.Prompt = ""
		in.Placeholder = placeholders[i]
		c.inputs[i] = in
	}
	c.inputs[fClientSecret].EchoMode = textinput.EchoPassword
	c.updateSecretInput()
	c.keys.next = key.NewBinding(key.WithKeys("tab", "down"))
	c.keys.prev = key.NewBinding(key.WithKeys("shift+tab", "up"))
	c.keys.submit = key.NewBinding(key.WithKeys("enter"))
	c.keys.cancel = key.NewBinding(key.WithKeys("esc"))
	c.keys.toggle = key.NewBinding(key.WithKeys("left", "right", "space", "h", "l"))
	c.keys.saveAnyway = key.NewBinding(key.WithKeys("ctrl+s"))
	c.keys.useCode = key.NewBinding(key.WithKeys("ctrl+d"))
	return c
}

func isInput(f int) bool {
	return f != fTLS && f != fSource && f != fProvider && f != fSMTPTLS && f != fSubmit && f != fDelete
}

// PasswordOnly reports whether this is a password prompt for a saved account.
func (c Connect) PasswordOnly() bool { return c.passwordOnly }

// SetSize caps the popup at maxW × maxH cells.
func (c *Connect) SetSize(maxW, maxH int) { c.maxW, c.maxH = maxW, maxH }

// SetBusy shows that the connection is being tested; input is ignored meanwhile.
// SetBusy(false) also clears any sign-in progress.
func (c *Connect) SetBusy(busy bool) {
	c.busy = busy
	if busy {
		c.err = ""
	} else {
		c.signIn = SignIn{}
	}
}

// SetSignIn shows sign-in progress; it implies busy. SetBusy(false) clears it.
func (c *Connect) SetSignIn(s SignIn) {
	c.SetBusy(true)
	c.signIn = s
}

// OAuth reports whether the OAuth 2.0 source is selected, so the caller runs
// a sign-in instead of a password test.
func (c Connect) OAuth() bool { return c.source == sourceOAuth }

// SetError shows why testing or saving failed.
func (c *Connect) SetError(msg string) {
	c.err = msg
	c.confirming = false
}

// SetTestFailed shows why the login test failed and, when editing, offers
// ctrl+s to save anyway until a field changes. New accounts don't get it.
func (c *Connect) SetTestFailed(msg string) {
	c.err = msg
	c.testFailed = c.editing && !c.passwordOnly
}

// DeletePassword reports whether a ConnectDelete also removes the keychain
// password or OAuth sign-in: only for a keychain or OAuth account, and not
// when kept with k.
func (c Connect) DeletePassword() bool { return c.deletePW }

func (c Connect) focusable() []int {
	if c.passwordOnly {
		return []int{fSecret, fSubmit}
	}
	all := make([]int, 0, fieldCount)
	for i := range fieldCount {
		if c.visible(i) {
			all = append(all, i)
		}
	}
	return all
}

// visible reports whether form field f is shown for the chosen source.
func (c Connect) visible(f int) bool {
	oauth := c.source == sourceOAuth
	switch f {
	case fDelete:
		return c.editing
	case fSecret:
		return c.source != sourceKeep && !oauth
	case fProvider, fClientID:
		return oauth
	case fTenant:
		return oauth && c.provider == config.ProviderMicrosoft
	case fClientSecret:
		return oauth && c.provider == config.ProviderGoogle
	}
	return true
}

func (c *Connect) setFocus(f int) {
	c.focus = f
	for i := range fieldCount {
		if !isInput(i) {
			continue
		}
		if i == f {
			c.inputs[i].Focus()
		} else {
			c.inputs[i].Blur()
		}
	}
}

func (c *Connect) move(step int) {
	fs := c.focusable()
	i := 0
	for j, f := range fs {
		if f == c.focus {
			i = j
		}
	}
	c.setFocus(fs[(i+step+len(fs))%len(fs)])
}

func (c Connect) Update(msg tea.KeyPressMsg) (Connect, ConnectResult) {
	if c.confirming && !c.busy {
		return c.confirm(msg)
	}
	if key.Matches(msg, c.keys.cancel) {
		return c, ConnectCancelled
	}
	if c.busy {
		if key.Matches(msg, c.keys.useCode) && c.signIn.URL != "" && c.provider == config.ProviderMicrosoft {
			return c, ConnectUseCode
		}
		return c, ConnectPending
	}
	switch {
	case key.Matches(msg, c.keys.saveAnyway):
		if !c.testFailed {
			return c, ConnectPending
		}
		if err := c.validate(); err != "" {
			c.err, c.testFailed = err, false
			return c, ConnectPending
		}
		return c, ConnectSaveAnyway
	case key.Matches(msg, c.keys.next):
		c.move(1)
	case key.Matches(msg, c.keys.prev):
		c.move(-1)
	case key.Matches(msg, c.keys.submit) && (c.focus == fSubmit || c.focus == fSecret):
		if err := c.validate(); err != "" {
			c.err = err
			return c, ConnectPending
		}
		c.err = ""
		return c, ConnectSubmitted
	case key.Matches(msg, c.keys.submit) && c.focus == fDelete:
		c.confirming = true
	case key.Matches(msg, c.keys.submit):
		c.move(1)
	case (c.focus == fTLS || c.focus == fSource || c.focus == fProvider || c.focus == fSMTPTLS) && key.Matches(msg, c.keys.toggle):
		c.toggle()
		c.testFailed = false
	case isInput(c.focus):
		before := c.inputs[c.focus].Value()
		c.inputs[c.focus], _ = c.inputs[c.focus].Update(msg)
		if c.inputs[c.focus].Value() != before {
			c.testFailed = false
			c.edited(c.focus)
		}
	}
	return c, ConnectPending
}

// confirm handles keys while asking whether to delete the account. No key
// reaches the fields. A keychain or OAuth account can keep its stored
// secrets with k.
func (c Connect) confirm(msg tea.KeyPressMsg) (Connect, ConnectResult) {
	keychain := c.orig.PasswordCmd == ""
	switch msg.String() {
	case "y":
		c.deletePW = keychain
		return c, ConnectDelete
	case "k":
		if keychain {
			c.deletePW = false
			return c, ConnectDelete
		}
	case "n", "esc":
		c.confirming = false
	}
	return c, ConnectPending
}

// Paste inserts pasted text into the focused field, without line breaks.
func (c Connect) Paste(msg tea.PasteMsg) Connect {
	if c.busy || !isInput(c.focus) {
		return c
	}
	before := c.inputs[c.focus].Value()
	c.inputs[c.focus], _ = c.inputs[c.focus].Update(tea.PasteMsg{Content: singleLine(msg.Content)})
	if c.inputs[c.focus].Value() != before {
		c.testFailed = false
		c.edited(c.focus)
	}
	return c
}

// singleLine drops surrounding whitespace and any line breaks, which a
// copied reference often ends with.
func singleLine(s string) string {
	return strings.NewReplacer("\r", "", "\n", "").Replace(strings.TrimSpace(s))
}

func (c *Connect) toggle() {
	switch c.focus {
	case fTLS:
		if c.tls == config.TLSImplicit {
			c.tls = config.TLSStartTLS
			c.inputs[fPort].Placeholder = "143"
		} else {
			c.tls = config.TLSImplicit
			c.inputs[fPort].Placeholder = "993"
		}
	case fSMTPTLS:
		if c.smtpTLS == config.TLSImplicit {
			c.smtpTLS = config.TLSStartTLS
			c.inputs[fSMTPPort].Placeholder = "587"
		} else {
			c.smtpTLS = config.TLSImplicit
			c.inputs[fSMTPPort].Placeholder = "465"
		}
		c.edited(fSMTPTLS)
	case fSource:
		if c.editing {
			// Keep current, 1Password, keychain, OAuth 2.0, as shown.
			c.source = map[int]int{sourceKeep: sourceOnePassword, sourceOnePassword: sourceKeychain, sourceKeychain: sourceOAuth, sourceOAuth: sourceKeep}[c.source]
		} else {
			c.source = map[int]int{sourceOnePassword: sourceKeychain, sourceKeychain: sourceOAuth, sourceOAuth: sourceOnePassword}[c.source]
		}
		c.inputs[fSecret].SetValue("")
		if ref, ok := opReference(c.orig.PasswordCmd); c.editing && c.source == sourceOnePassword && ok {
			c.inputs[fSecret].SetValue(ref)
		}
		c.updateSecretInput()
		c.applyPreset()
	case fProvider:
		if c.provider == config.ProviderMicrosoft {
			c.provider = config.ProviderGoogle
		} else {
			c.provider = config.ProviderMicrosoft
		}
		c.providerTouched = true
		c.applyPreset()
	}
}

func (c *Connect) updateSecretInput() {
	in := &c.inputs[fSecret]
	if c.source == sourceKeychain {
		in.EchoMode = textinput.EchoPassword
		in.Placeholder = "app password"
	} else {
		in.EchoMode = textinput.EchoNormal
		in.Placeholder = "op://Vault/Item/password"
	}
}

// edited reacts to typing: a known mail domain fills in its provider's
// servers unless they were typed by hand. The SMTP fields are pre-filled only
// until the user has touched one of them, so clearing the filled-in host to
// opt out of sending sticks.
func (c *Connect) edited(f int) {
	switch f {
	case fHost, fPort:
		c.autoHost = false
	case fSMTPHost, fSMTPPort, fSMTPTLS:
		c.autoSMTP, c.smtpTouched = false, true
	case fEmail:
		c.applyPreset()
	}
}

// oauthDefaultTenant is Microsoft's multi-tenant endpoint, the config default.
const oauthDefaultTenant = "common"

// serverPreset is the server settings a provider's accounts use.
type serverPreset struct {
	imapHost, smtpHost, smtpPort, smtpTLS string
}

var presets = map[string]serverPreset{
	config.ProviderGoogle:    {"imap.gmail.com", "smtp.gmail.com", "465", config.TLSImplicit},
	config.ProviderMicrosoft: {"outlook.office365.com", "smtp.office365.com", "587", config.TLSStartTLS},
}

// domainProviders maps the mail domains of each provider's consumer accounts.
var domainProviders = map[string]string{
	"gmail.com":      config.ProviderGoogle,
	"googlemail.com": config.ProviderGoogle,
	"outlook.com":    config.ProviderMicrosoft,
	"hotmail.com":    config.ProviderMicrosoft,
	"live.com":       config.ProviderMicrosoft,
	"msn.com":        config.ProviderMicrosoft,
	"office365.com":  config.ProviderMicrosoft,
}

// applyPreset fills in, replaces or clears the servers of the provider the
// email domain names, or, for a custom domain with OAuth chosen, the selected
// provider. It never touches a host the user typed, and it follows the email
// to select the provider until the user picks one.
func (c *Connect) applyPreset() {
	email := strings.ToLower(strings.TrimSpace(c.inputs[fEmail].Value()))
	_, domain, _ := strings.Cut(email, "@")
	detected := domainProviders[domain]
	if detected != "" && !c.providerTouched {
		c.provider = detected
	}
	want := detected
	if want == "" && c.source == sourceOAuth {
		want = c.provider
	}
	p, ok := presets[want]
	switch {
	case ok && (c.autoHost || c.inputs[fHost].Value() == ""):
		c.inputs[fHost].SetValue(p.imapHost)
		c.inputs[fPort].SetValue("993")
		c.tls = config.TLSImplicit
		c.inputs[fPort].Placeholder = "993"
		c.autoHost = true
	case !ok && c.autoHost:
		c.inputs[fHost].SetValue("")
		c.inputs[fPort].SetValue("")
		c.autoHost = false
	}
	switch {
	case ok && (c.autoSMTP || !c.smtpTouched):
		c.inputs[fSMTPHost].SetValue(p.smtpHost)
		c.inputs[fSMTPPort].SetValue(p.smtpPort)
		c.smtpTLS = p.smtpTLS
		c.inputs[fSMTPPort].Placeholder = p.smtpPort
		c.autoSMTP = true
	case !ok && c.autoSMTP:
		c.inputs[fSMTPHost].SetValue("")
		c.inputs[fSMTPPort].SetValue("")
		c.autoSMTP = false
	}
}

func (c Connect) value(f int) string { return strings.TrimSpace(c.inputs[f].Value()) }

func (c Connect) validate() string {
	if c.passwordOnly {
		if c.inputs[fSecret].Value() == "" {
			return "Password is required"
		}
		return ""
	}
	name := c.value(fName)
	switch {
	case name == "":
		return "Name is required"
	case containsFold(c.existing, name):
		return fmt.Sprintf("An account named %q already exists", name)
	case !strings.Contains(c.value(fEmail), "@"):
		return "Email must be an address such as me@example.com"
	case c.value(fHost) == "":
		return "IMAP host is required"
	}
	if !validPort(c.value(fPort)) {
		return "Port must be a number from 1 to 65535"
	}
	switch {
	case !validPort(c.value(fSMTPPort)):
		return "SMTP port must be a number from 1 to 65535"
	case c.value(fSMTPHost) == "" && c.value(fSMTPPort) != "":
		return "SMTP port is set but SMTP host is empty"
	}
	if aliases := c.aliases(); len(aliases) > 0 {
		if c.value(fSMTPHost) == "" {
			return "Aliases need an SMTP host"
		}
		a := config.Account{Email: c.value(fEmail), Aliases: aliases}
		if errs := a.AliasErrors(); len(errs) > 0 {
			return errs[0].Error()
		}
	}
	switch {
	case c.source == sourceOnePassword && c.value(fSecret) == "":
		return "1Password reference is required, e.g. op://Vault/Item/password"
	case c.source == sourceKeychain && c.inputs[fSecret].Value() == "":
		return "Password is required"
	case c.source == sourceOAuth && c.value(fClientID) == "":
		return "Client ID is required: register an app first (see docs/oauth.md)"
	case c.source == sourceOAuth && c.provider == config.ProviderGoogle && c.inputs[fClientSecret].Value() == "" && !c.savedGoogleSecret():
		return "Google needs the client secret from your OAuth client"
	}
	return ""
}

// savedGoogleSecret reports whether the edited account already signs in to
// Google, so an empty client secret field means keep the saved one.
func (c Connect) savedGoogleSecret() bool {
	return c.editing && c.orig.OAuth() && c.orig.OAuthProvider == config.ProviderGoogle
}

// aliases parses the Aliases field: comma-separated, trimmed, empties dropped.
func (c Connect) aliases() []config.Alias {
	var out []config.Alias
	for _, e := range strings.Split(c.inputs[fAliases].Value(), ",") {
		if e = strings.TrimSpace(e); e != "" {
			out = append(out, config.Alias{Email: e})
		}
	}
	return out
}

// validPort accepts an empty port, which means the default.
func validPort(p string) bool {
	if p == "" {
		return true
	}
	n, err := strconv.Atoi(p)
	return err == nil && n >= 1 && n <= 65535
}

func containsFold(names []string, name string) bool {
	name = strings.ToLower(name)
	for _, n := range names {
		if n == name {
			return true
		}
	}
	return false
}

// Request is the submitted account. Optional fields left empty stay empty,
// so config defaults apply when it's saved. The SMTP port and TLS are
// included only with an SMTP host; without one the account can't send.
func (c Connect) Request() ConnectRequest {
	if c.passwordOnly {
		return ConnectRequest{Account: c.account, Password: c.inputs[fSecret].Value()}
	}
	a := config.Account{
		Name:     c.value(fName),
		Email:    c.value(fEmail),
		IMAPHost: c.value(fHost),
		TLS:      c.tls,
	}
	if u := c.value(fUser); u != a.Email {
		a.Username = u
	}
	a.IMAPPort, _ = strconv.Atoi(c.value(fPort))
	if a.SMTPHost = c.value(fSMTPHost); a.SMTPHost != "" {
		a.SMTPPort, _ = strconv.Atoi(c.value(fSMTPPort))
		a.SMTPTLS = c.smtpTLS
		a.Aliases = c.aliases()
	}
	switch c.source {
	case sourceOnePassword:
		a.PasswordCmd = "op read " + shellQuote(c.value(fSecret))
		return ConnectRequest{Account: a}
	case sourceOAuth:
		a.Auth, a.OAuthProvider, a.OAuthClientID = config.AuthOAuth2, c.provider, c.value(fClientID)
		req := ConnectRequest{Account: a}
		switch c.provider {
		case config.ProviderMicrosoft:
			if t := c.value(fTenant); t != "" && !strings.EqualFold(t, oauthDefaultTenant) {
				req.Account.OAuthTenant = t
			}
		case config.ProviderGoogle:
			req.ClientSecret = c.inputs[fClientSecret].Value()
		}
		return req
	}
	return ConnectRequest{Account: a, Password: c.inputs[fSecret].Value()}
}

// EditRequest is the submitted edit form.
func (c Connect) EditRequest() EditRequest {
	r := c.Request()
	return EditRequest{Account: r.Account, Password: r.Password, KeepPassword: c.source == sourceKeep, ClientSecret: r.ClientSecret}
}

// ApplyEdit returns old with the form's fields applied, for writing back to
// config.toml. Keys the form doesn't show (tls_cert, archive_folder,
// save_sent, alias names, ...) stay as they were. old is the account as
// written in the file, so a port or TLS mode that only repeats a default
// stays unwritten. Without an SMTP host the sending keys are cleared, as
// the config requires.
func ApplyEdit(old config.Account, r EditRequest) config.Account {
	f := r.Account
	a := old
	a.Name, a.Email, a.Username, a.IMAPHost = f.Name, f.Email, f.Username, f.IMAPHost
	if f.Username == f.Email {
		a.Username = ""
	}

	eff := old.WithDefaults()
	a.TLS, a.IMAPPort = f.TLS, f.IMAPPort
	if old.TLS == "" && f.TLS == config.TLSImplicit {
		a.TLS = ""
	}
	if old.IMAPPort == 0 && eff.TLS == f.TLS && f.IMAPPort == eff.IMAPPort {
		a.IMAPPort = 0
	}

	if f.SMTPHost == "" {
		a.SMTPHost, a.SMTPPort, a.SMTPTLS = "", 0, ""
		a.FromName, a.Aliases, a.SaveSent = "", nil, nil
	} else {
		sm := old
		sm.SMTPHost = f.SMTPHost
		sm = sm.WithDefaults()
		a.SMTPHost, a.SMTPTLS, a.SMTPPort = f.SMTPHost, f.SMTPTLS, f.SMTPPort
		if old.SMTPTLS == "" && f.SMTPTLS == config.TLSImplicit {
			a.SMTPTLS = ""
		}
		if old.SMTPPort == 0 && sm.SMTPTLS == f.SMTPTLS && f.SMTPPort == sm.SMTPPort {
			a.SMTPPort = 0
		}
		names := map[string]string{}
		for _, al := range old.Aliases {
			names[strings.ToLower(al.Email)] = al.Name
		}
		a.Aliases = nil
		for _, al := range f.Aliases {
			a.Aliases = append(a.Aliases, config.Alias{Email: al.Email, Name: names[strings.ToLower(al.Email)]})
		}
	}

	if !r.KeepPassword {
		a.PasswordCmd = f.PasswordCmd
		a.Auth, a.OAuthProvider, a.OAuthClientID, a.OAuthTenant = f.Auth, f.OAuthProvider, f.OAuthClientID, f.OAuthTenant
		if a.Auth == "" && old.Auth == config.AuthPassword {
			a.Auth = config.AuthPassword
		}
		if f.Auth == config.AuthOAuth2 && f.OAuthProvider == config.ProviderGoogle && r.ClientSecret == "" {
			a.OAuthClientSecretCmd = old.OAuthClientSecretCmd
		} else {
			a.OAuthClientSecretCmd = ""
		}
	}
	return a
}

// shellQuote quotes s for sh -c, leaving plain references unquoted.
func shellQuote(s string) string {
	safe := s != "" && strings.IndexFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("/:._-@+=", r))
	}) < 0
	if safe {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// View renders the form, cut to fit SetSize.
func (c Connect) View() string {
	innerW := min(connectInnerW, c.maxW-helpChromeCols)
	if innerW < 30 || c.maxH < 8 {
		return ""
	}
	title := "Connect account"
	switch {
	case c.passwordOnly:
		title = "Password for " + c.account.Name
	case c.editing:
		title = "Edit account"
	}
	lines := []string{ansi.Truncate(c.st.title.Render(title), innerW, ""), ""}
	if c.passwordOnly {
		lines = append(lines, c.readOnly("Email", c.account.Email, innerW), c.readOnly("Server", fmt.Sprintf("%s:%d", c.account.IMAPHost, c.account.WithDefaults().IMAPPort), innerW), "")
	} else {
		for _, f := range []int{fName, fEmail, fUser, fHost, fPort} {
			lines = append(lines, c.inputRow(f, innerW))
		}
		lines = append(lines, c.choiceRow(fTLS, "TLS", []string{"implicit", "starttls"}, map[bool]int{true: 0, false: 1}[c.tls == config.TLSImplicit]))
		if c.editing {
			lines = append(lines, c.choiceRow(fSource, "Login", []string{"keep current", "1Password", "keychain", "OAuth 2.0"}, map[int]int{sourceKeep: 0, sourceOnePassword: 1, sourceKeychain: 2, sourceOAuth: 3}[c.source]))
		} else {
			lines = append(lines, c.choiceRow(fSource, "Login", []string{"1Password", "keychain", "OAuth 2.0"}, map[int]int{sourceOnePassword: 0, sourceKeychain: 1, sourceOAuth: 2}[c.source]))
		}
	}
	note := "Stored as password_cmd = \"op read <reference>\""
	switch c.source {
	case sourceKeep:
		current := "keychain"
		switch {
		case c.orig.OAuth() && c.orig.PasswordCmd != "":
			current = "token command"
		case c.orig.OAuth() && c.orig.OAuthProvider != "":
			current = "OAuth 2.0 (" + c.orig.OAuthProvider + ")"
		case c.orig.OAuth():
			current = "OAuth 2.0"
		case c.orig.PasswordCmd != "":
			current = "password_cmd"
		}
		lines = append(lines, c.readOnly("Current", current, innerW))
		note = "The saved password isn't changed"
	case sourceKeychain:
		lines = append(lines, c.inputRow(fSecret, innerW))
		note = "Saved in the OS keychain, never in config.toml"
	case sourceOAuth:
		lines = append(lines, c.oauthBox(innerW)...)
		note = ""
	default:
		lines = append(lines, c.inputRow(fSecret, innerW))
	}
	if note != "" {
		lines = append(lines, strings.Repeat(" ", labelW+1)+c.st.dim.Render(ansi.Truncate(note, innerW-labelW-1, "…")))
	}
	if !c.passwordOnly {
		lines = append(lines,
			c.inputRow(fSMTPHost, innerW),
			c.inputRow(fSMTPPort, innerW),
			c.choiceRow(fSMTPTLS, "SMTP TLS", []string{"implicit", "starttls"}, map[bool]int{true: 0, false: 1}[c.smtpTLS == config.TLSImplicit]),
			c.inputRow(fAliases, innerW),
		)
	}
	lines = append(lines, "")

	button := "[ Test & save ]"
	if c.source == sourceOAuth {
		button = "[ Sign in & save ]"
	}
	signing := c.busy && c.signIn != (SignIn{})
	switch {
	case signing:
		lines = append(lines, c.signInLines(innerW)...)
	case c.busy:
		lines = append(lines, c.st.dim.Render("Testing connection…"))
	case c.focus == fSubmit:
		lines = append(lines, c.st.badge.Render(button))
	default:
		lines = append(lines, button)
	}
	if c.editing && !c.busy {
		switch {
		case c.confirming:
			lines = append(lines, c.st.alert.Render(fmt.Sprintf("Delete %s?", c.orig.Name)))
		case c.focus == fDelete:
			lines = append(lines, c.st.alert.Reverse(true).Padding(0, 1).Render("[ Delete account ]"))
		default:
			lines = append(lines, c.st.alert.Render("[ Delete account ]"))
		}
	}
	if c.err != "" {
		for _, l := range strings.Split(lipgloss.NewStyle().Width(innerW).Render(c.err), "\n") {
			lines = append(lines, c.st.alert.Render(strings.TrimRight(l, " ")))
		}
	}

	if room := c.maxH - 2; len(lines) > room {
		if signing {
			// Keep the sign-in status, which can't be cut, and drop form rows.
			n := len(c.signInLines(innerW))
			lines = append(lines[:max(room-n, 0)], lines[len(lines)-n:]...)
		}
		lines = lines[:min(len(lines), room)]
	}
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, innerW, "")
	}
	return c.st.box.Width(innerW + helpChromeCols).Render(strings.Join(lines, "\n"))
}

// Hints are the keys that currently work, for the hint bar.
func (c Connect) Hints() []key.Binding {
	b := func(k, desc string) key.Binding { return key.NewBinding(key.WithKeys(k), key.WithHelp(k, desc)) }
	cancel := b("esc", "cancel")
	switch {
	case c.busy && c.signIn.URL != "" && c.provider == config.ProviderMicrosoft:
		return []key.Binding{cancel, b("ctrl+d", "use a code instead")}
	case c.busy:
		return []key.Binding{cancel}
	case c.confirming && c.orig.PasswordCmd == "" && c.orig.OAuth():
		return []key.Binding{b("y", "delete + sign-in"), b("k", "delete, keep sign-in"), b("n", "cancel")}
	case c.confirming && c.orig.PasswordCmd == "":
		return []key.Binding{b("y", "delete + password"), b("k", "delete, keep password"), b("n", "cancel")}
	case c.confirming:
		return []key.Binding{b("y", "delete"), b("n", "cancel")}
	case c.passwordOnly:
		return []key.Binding{b("enter", "test & save"), cancel}
	}
	submit := "test & save"
	if c.source == sourceOAuth {
		submit = "sign in & save"
	}
	hints := []key.Binding{b("tab", "next"), b("←→", "choose"), b("enter", submit)}
	if c.testFailed {
		hints = append(hints, b("ctrl+s", "save anyway"))
	}
	return append(hints, cancel)
}

func (c Connect) label(f int, text string) string {
	l := fmt.Sprintf("%-*s", labelW, text)
	if f == c.focus {
		return c.st.section.Render(l) + " "
	}
	return c.st.dim.Render(l) + " "
}

func (c Connect) inputRow(f, innerW int) string {
	names := map[int]string{fName: "Name", fEmail: "Email", fUser: "Username", fHost: "IMAP host", fPort: "Port", fSecret: "Reference", fClientID: "Client ID", fTenant: "Tenant", fClientSecret: "Secret", fSMTPHost: "SMTP host", fSMTPPort: "SMTP port", fAliases: "Aliases"}
	if f == fSecret && c.source == sourceKeychain {
		names[fSecret] = "Password"
	}
	in := c.inputs[f]
	in.SetWidth(max(innerW-labelW-2, 1))
	return c.label(f, names[f]) + in.View()
}

func (c Connect) choiceRow(f int, name string, options []string, selected int) string {
	parts := make([]string, len(options))
	for i, o := range options {
		if i == selected {
			parts[i] = c.st.key.Render("● " + o)
		} else {
			parts[i] = c.st.dim.Render("○ " + o)
		}
	}
	return c.label(f, name) + strings.Join(parts, "   ")
}

// oauthBox frames the OAuth fields in a box titled OAuth 2.0, so they read
// as part of that login choice.
func (c Connect) oauthBox(innerW int) []string {
	w := innerW - 4 // the side borders and their padding
	rows := []string{
		c.choiceRow(fProvider, "Provider", []string{"Microsoft", "Google"}, map[bool]int{true: 0, false: 1}[c.provider == config.ProviderMicrosoft]),
		c.inputRow(fClientID, w),
	}
	for _, f := range []int{fTenant, fClientSecret} {
		if c.visible(f) {
			rows = append(rows, c.inputRow(f, w))
		}
	}
	rows = append(rows, strings.Repeat(" ", labelW+1)+c.st.dim.Render(ansi.Truncate("Browser sign-in; tokens are kept in the OS keychain", w-labelW-1, "…")))

	title := "╭─ OAuth 2.0 "
	out := []string{c.st.dim.Render(title + strings.Repeat("─", max(innerW-ansi.StringWidth(title)-1, 0)) + "╮")}
	side := c.st.dim.Render("│")
	for _, r := range rows {
		r = ansi.Truncate(r, w, "")
		out = append(out, side+" "+r+strings.Repeat(" ", max(w-ansi.StringWidth(r), 0))+" "+side)
	}
	return append(out, c.st.dim.Render("╰"+strings.Repeat("─", innerW-2)+"╯"))
}

func (c Connect) readOnly(name, value string, innerW int) string {
	return c.st.dim.Render(fmt.Sprintf("%-*s", labelW, name)) + " " + ansi.Truncate(value, innerW-labelW-1, "…")
}

// signInLines is the status shown while an OAuth sign-in runs. Long
// addresses are wrapped, never truncated, so they stay copyable.
func (c Connect) signInLines(innerW int) []string {
	var lines []string
	wrap := func(text string) {
		for _, l := range strings.Split(ansi.Hardwrap(text, innerW, true), "\n") {
			lines = append(lines, l)
		}
	}
	if c.signIn.URL != "" {
		lines = append(lines, c.st.dim.Render("Waiting for you to sign in in your browser…"), c.st.dim.Render("If it didn't open, visit:"))
		wrap(c.signIn.URL)
		return lines
	}
	wrap("Go to " + c.signIn.VerificationURI)
	return append(lines, "and enter the code "+c.st.badge.Render(c.signIn.UserCode))
}
