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
	fSecret // the 1Password reference or the keychain password
	fSubmit
	fieldCount
)

const (
	sourceOnePassword = iota
	sourceKeychain
)

const (
	connectInnerW = 58
	labelW        = 10
)

// ConnectResult is what a key press did to the form.
type ConnectResult int

const (
	ConnectPending ConnectResult = iota
	ConnectSubmitted
	ConnectCancelled
)

// ConnectRequest is a submitted form. Password is set only for the keychain
// option; with 1Password the account's PasswordCmd reads it.
type ConnectRequest struct {
	Account  config.Account
	Password string
}

// Connect is the connect-account form, or, from NewPasswordPrompt, a prompt
// for a saved account's missing password.
type Connect struct {
	inputs   [fieldCount]textinput.Model // only the text fields are used
	focus    int
	tls      string
	source   int
	autoHost bool // the host and port came from the Gmail pre-fill

	passwordOnly bool
	account      config.Account // the saved account in password-only mode
	existing     []string       // lowercased names already in use

	busy       bool
	err        string
	maxW, maxH int

	st   helpStyles
	keys struct{ next, prev, submit, cancel, toggle key.Binding }
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

func newForm(th theme.Theme) Connect {
	c := Connect{tls: config.TLSImplicit, st: newHelpStyles(th)}
	placeholders := map[int]string{
		fName:  "Personal",
		fEmail: "me@example.com",
		fUser:  "same as email",
		fHost:  "imap.example.com",
		fPort:  "993",
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
	c.updateSecretInput()
	c.keys.next = key.NewBinding(key.WithKeys("tab", "down"))
	c.keys.prev = key.NewBinding(key.WithKeys("shift+tab", "up"))
	c.keys.submit = key.NewBinding(key.WithKeys("enter"))
	c.keys.cancel = key.NewBinding(key.WithKeys("esc"))
	c.keys.toggle = key.NewBinding(key.WithKeys("left", "right", "space", "h", "l"))
	return c
}

func isInput(f int) bool { return f != fTLS && f != fSource && f != fSubmit }

// PasswordOnly reports whether this is a password prompt for a saved account.
func (c Connect) PasswordOnly() bool { return c.passwordOnly }

// SetSize caps the popup at maxW × maxH cells.
func (c *Connect) SetSize(maxW, maxH int) { c.maxW, c.maxH = maxW, maxH }

// SetBusy shows that the connection is being tested; input is ignored meanwhile.
func (c *Connect) SetBusy(busy bool) {
	c.busy = busy
	if busy {
		c.err = ""
	}
}

// SetError shows why testing or saving failed.
func (c *Connect) SetError(msg string) { c.err = msg }

func (c Connect) focusable() []int {
	if c.passwordOnly {
		return []int{fSecret, fSubmit}
	}
	all := make([]int, 0, fieldCount)
	for i := range fieldCount {
		all = append(all, i)
	}
	return all
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
	if key.Matches(msg, c.keys.cancel) {
		return c, ConnectCancelled
	}
	if c.busy {
		return c, ConnectPending
	}
	switch {
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
	case key.Matches(msg, c.keys.submit):
		c.move(1)
	case (c.focus == fTLS || c.focus == fSource) && key.Matches(msg, c.keys.toggle):
		c.toggle()
	case isInput(c.focus):
		before := c.inputs[c.focus].Value()
		c.inputs[c.focus], _ = c.inputs[c.focus].Update(msg)
		if c.inputs[c.focus].Value() != before {
			c.edited(c.focus)
		}
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
	case fSource:
		c.source = 1 - c.source
		c.inputs[fSecret].SetValue("")
		c.updateSecretInput()
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

// edited reacts to typing: a Gmail address fills in Gmail's server unless
// a host was typed by hand, and leaving Gmail clears what it filled in.
func (c *Connect) edited(f int) {
	switch f {
	case fHost, fPort:
		c.autoHost = false
	case fEmail:
		email := strings.ToLower(strings.TrimSpace(c.inputs[fEmail].Value()))
		_, domain, _ := strings.Cut(email, "@")
		gmail := domain == "gmail.com" || domain == "googlemail.com"
		switch {
		case gmail && (c.autoHost || c.inputs[fHost].Value() == ""):
			c.inputs[fHost].SetValue("imap.gmail.com")
			c.inputs[fPort].SetValue("993")
			c.tls = config.TLSImplicit
			c.inputs[fPort].Placeholder = "993"
			c.autoHost = true
		case !gmail && c.autoHost:
			c.inputs[fHost].SetValue("")
			c.inputs[fPort].SetValue("")
			c.autoHost = false
		}
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
	if p := c.value(fPort); p != "" {
		if n, err := strconv.Atoi(p); err != nil || n < 1 || n > 65535 {
			return "Port must be a number from 1 to 65535"
		}
	}
	switch {
	case c.source == sourceOnePassword && c.value(fSecret) == "":
		return "1Password reference is required, e.g. op://Vault/Item/password"
	case c.source == sourceKeychain && c.inputs[fSecret].Value() == "":
		return "Password is required"
	}
	return ""
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
// so config defaults apply when it's saved.
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
	if c.source == sourceOnePassword {
		a.PasswordCmd = "op read " + shellQuote(c.value(fSecret))
		return ConnectRequest{Account: a}
	}
	return ConnectRequest{Account: a, Password: c.inputs[fSecret].Value()}
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
	if c.passwordOnly {
		title = "Password for " + c.account.Name
	}
	badge := c.st.badge.Render("esc close")
	t := c.st.title.Render(title)
	header := t + strings.Repeat(" ", max(innerW-lipgloss.Width(t)-lipgloss.Width(badge), 1)) + badge

	lines := []string{ansi.Truncate(header, innerW, ""), ""}
	if c.passwordOnly {
		lines = append(lines, c.readOnly("Email", c.account.Email, innerW), c.readOnly("Server", fmt.Sprintf("%s:%d", c.account.IMAPHost, c.account.WithDefaults().IMAPPort), innerW), "")
	} else {
		for _, f := range []int{fName, fEmail, fUser, fHost, fPort} {
			lines = append(lines, c.inputRow(f, innerW))
		}
		lines = append(lines,
			c.choiceRow(fTLS, "TLS", []string{"implicit", "starttls"}, map[bool]int{true: 0, false: 1}[c.tls == config.TLSImplicit]),
			c.choiceRow(fSource, "Password", []string{"1Password", "keychain"}, c.source),
		)
	}
	lines = append(lines, c.inputRow(fSecret, innerW))
	note := "Stored as password_cmd = \"op read <reference>\""
	if c.source == sourceKeychain {
		note = "Saved in the OS keychain, never in config.toml"
	}
	lines = append(lines, strings.Repeat(" ", labelW+1)+c.st.dim.Render(ansi.Truncate(note, innerW-labelW-1, "…")), "")

	button := "[ Test & save ]"
	switch {
	case c.busy:
		lines = append(lines, c.st.dim.Render("Testing connection…"))
	case c.focus == fSubmit:
		lines = append(lines, c.st.badge.Render(button))
	default:
		lines = append(lines, button)
	}
	if c.err != "" {
		for _, l := range strings.Split(lipgloss.NewStyle().Width(innerW).Render(c.err), "\n") {
			lines = append(lines, c.st.alert.Render(strings.TrimRight(l, " ")))
		}
	}
	footer := "tab next · ←→ choose · enter test & save · esc cancel"
	if c.passwordOnly {
		footer = "enter test & save · esc cancel"
	}
	lines = append(lines, "", c.st.dim.Render(ansi.Truncate(footer, innerW, "…")))

	if room := c.maxH - 2; len(lines) > room {
		lines = append(lines[:room-1], lines[len(lines)-1])
	}
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, innerW, "")
	}
	return c.st.box.Width(innerW + helpChromeCols).Render(strings.Join(lines, "\n"))
}

func (c Connect) label(f int, text string) string {
	l := fmt.Sprintf("%-*s", labelW, text)
	if f == c.focus {
		return c.st.section.Render(l) + " "
	}
	return c.st.dim.Render(l) + " "
}

func (c Connect) inputRow(f, innerW int) string {
	names := map[int]string{fName: "Name", fEmail: "Email", fUser: "Username", fHost: "IMAP host", fPort: "Port", fSecret: "Reference"}
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

func (c Connect) readOnly(name, value string, innerW int) string {
	return c.st.dim.Render(fmt.Sprintf("%-*s", labelW, name)) + " " + ansi.Truncate(value, innerW-labelW-1, "…")
}
