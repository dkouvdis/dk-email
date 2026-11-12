package compose

import (
	"fmt"
	netmail "net/mail"
	"strings"

	"github.com/dkouvdis/dk-email/internal/mail"
)

// Fields are the composer's editable values, as the user sees them.
type Fields struct{ To, Cc, Bcc, Subject, Body string }

// FieldsOf formats d for the composer: addresses as comma-separated lists
// the way the user would type them (so non-ASCII names stay readable), the
// subject on one line and the body as is.
func FieldsOf(d mail.Draft) Fields {
	return Fields{
		To:      formatAddresses(d.To),
		Cc:      formatAddresses(d.Cc),
		Bcc:     formatAddresses(d.Bcc),
		Subject: singleLine(d.Subject),
		Body:    d.Body,
	}
}

// Draft parses f into a copy of base, replacing only To, Cc, Bcc, Subject and
// Body. Every other field of base is kept; on error base is returned
// unchanged. The errors are written to be shown to the user as they are.
func (f Fields) Draft(base mail.Draft) (mail.Draft, error) {
	return f.draft(base, true)
}

// DraftForSave is like Draft but allows a draft with no recipients, so a
// work in progress can be saved.
func (f Fields) DraftForSave(base mail.Draft) (mail.Draft, error) {
	return f.draft(base, false)
}

func (f Fields) draft(base mail.Draft, requireRecipients bool) (mail.Draft, error) {
	d := base
	var err error
	if d.To, err = parseAddresses("To", strings.TrimSpace(f.To)); err != nil {
		return base, err
	}
	if d.Cc, err = parseAddresses("Cc", strings.TrimSpace(f.Cc)); err != nil {
		return base, err
	}
	if d.Bcc, err = parseAddresses("Bcc", strings.TrimSpace(f.Bcc)); err != nil {
		return base, err
	}
	if requireRecipients && len(d.To)+len(d.Cc)+len(d.Bcc) == 0 {
		return base, fmt.Errorf("no recipients: add an address to To, Cc or Bcc")
	}
	d.Subject = strings.TrimSpace(singleLine(f.Subject))
	d.Body = strings.ReplaceAll(f.Body, "\r\n", "\n")
	return d, nil
}

func formatAddresses(l []mail.Address) string {
	parts := make([]string, len(l))
	for i, a := range l {
		parts[i] = formatAddress(a)
	}
	return strings.Join(parts, ", ")
}

func formatAddress(a mail.Address) string {
	name := singleLine(a.Name)
	if name == "" || a.Email == "" {
		if a.Email != "" {
			return a.Email
		}
		return name
	}
	if needsQuotes(name) {
		name = `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(name) + `"`
	}
	return name + " <" + a.Email + ">"
}

// needsQuotes reports whether a display name must be a quoted-string. Only
// ASCII is checked: UTF-8 text is a valid atom.
func needsQuotes(name string) bool {
	if name != strings.TrimSpace(name) {
		return true
	}
	for _, r := range name {
		if r < 0x80 && (strings.ContainsRune("()<>[]:;@\\,.\"", r) || r < ' ' || r == 0x7f) {
			return true
		}
	}
	return false
}

// singleLine keeps a value on one line so it can't inject extra headers.
func singleLine(s string) string {
	return strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ").Replace(s)
}

func parseAddresses(header, value string) ([]mail.Address, error) {
	if value == "" {
		return nil, nil
	}
	list, err := netmail.ParseAddressList(value)
	if err != nil {
		return nil, fmt.Errorf("%s: invalid address list %q: %w", header, value, err)
	}
	out := make([]mail.Address, len(list))
	for i, a := range list {
		out[i] = mail.Address{Name: a.Name, Email: a.Address}
	}
	return out, nil
}
