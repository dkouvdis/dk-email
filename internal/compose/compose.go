// Package compose builds outgoing messages: the empty new-message draft, the
// editor file the user edits (Format and Parse), and the RFC 5322 bytes that
// are sent (Build). It is pure: no network, no UI.
package compose

import (
	"crypto/rand"
	"encoding/hex"
	"strings"

	"github.com/dkouvdis/dk-email/internal/mail"
)

// New returns an empty draft from the account identity.
func New(from mail.Address) mail.Draft {
	return mail.Draft{From: from}
}

// MessageID returns a fresh, random Message-ID in the form the rest of the
// app stores ids (mail.Summary.MessageID): without angle brackets, as
// "<32 hex chars>@<domain of from>". Build adds the brackets on the wire.
func MessageID(from mail.Address) string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("compose: crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b[:]) + "@" + domain(from.Email)
}

// domain is the part after the last "@", or "localhost" when it is missing or
// has characters that don't belong in a Message-ID.
func domain(email string) string {
	i := strings.LastIndexByte(email, '@')
	if i < 0 {
		return "localhost"
	}
	d := email[i+1:]
	if d == "" {
		return "localhost"
	}
	for _, r := range d {
		ok := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '-'
		if !ok {
			return "localhost"
		}
	}
	return d
}
