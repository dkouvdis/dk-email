package compose

import (
	"fmt"
	"strings"

	"github.com/dkouvdis/dk-email/internal/mail"
)

// Format renders d as the editor file: the To, Cc, Bcc and Subject header
// lines (always all four, in that order), a blank line, then the body.
// Addresses are written as the user would type them, so non-ASCII names stay
// readable; Parse reads them back.
func Format(d mail.Draft) string {
	var b strings.Builder
	b.WriteString("To: " + formatAddresses(d.To) + "\n")
	b.WriteString("Cc: " + formatAddresses(d.Cc) + "\n")
	b.WriteString("Bcc: " + formatAddresses(d.Bcc) + "\n")
	b.WriteString("Subject: " + singleLine(d.Subject) + "\n")
	b.WriteString("\n")
	b.WriteString(d.Body)
	return b.String()
}

// Parse reads the editor file back into a copy of base, replacing To, Cc, Bcc,
// Subject and Body. Every other field of base is kept.
//
// Header lines run until the first blank line and everything after it is the
// body, exactly as written. A header line starting with a space or tab
// continues the previous header, so a long recipient list may be wrapped. A
// file without a blank line is all headers, with an empty body. The errors
// are written to be shown to the user as they are.
func Parse(text string, base mail.Draft) (mail.Draft, error) {
	text = strings.ReplaceAll(text, "\r\n", "\n")

	head, body, _ := strings.Cut(text, "\n\n")
	if strings.HasPrefix(text, "\n") {
		head, body = "", text[1:]
	}

	type field struct {
		line  int
		value string
	}
	fields := map[string]*field{}
	var last *field
	for i, line := range strings.Split(head, "\n") {
		n := i + 1
		if line == "" {
			continue
		}
		if line[0] == ' ' || line[0] == '\t' {
			if last == nil {
				return base, fmt.Errorf("line %d: unexpected indented line before any header", n)
			}
			last.value += " " + strings.TrimSpace(line)
			continue
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			return base, fmt.Errorf("line %d: missing ':' in header line %q", n, line)
		}
		name = strings.TrimSpace(name)
		key := strings.ToLower(name)
		switch key {
		case "to", "cc", "bcc", "subject":
		default:
			return base, fmt.Errorf("unknown header %q on line %d (only To, Cc, Bcc and Subject are allowed)", name, n)
		}
		if prev, dup := fields[key]; dup {
			return base, fmt.Errorf("duplicate header %q on line %d (first on line %d)", name, n, prev.line)
		}
		last = &field{line: n, value: strings.TrimSpace(value)}
		fields[key] = last
	}

	get := func(key string) string {
		if f := fields[key]; f != nil {
			return f.value
		}
		return ""
	}

	d := base
	var err error
	if d.To, err = parseAddresses("To", get("to")); err != nil {
		return base, err
	}
	if d.Cc, err = parseAddresses("Cc", get("cc")); err != nil {
		return base, err
	}
	if d.Bcc, err = parseAddresses("Bcc", get("bcc")); err != nil {
		return base, err
	}
	if len(d.To)+len(d.Cc)+len(d.Bcc) == 0 {
		return base, fmt.Errorf("no recipients: add an address to To, Cc or Bcc")
	}
	d.Subject = get("subject")
	d.Body = body
	return d, nil
}
