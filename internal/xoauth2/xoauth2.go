// Package xoauth2 is a stdlib-only XOAUTH2 SASL client, shared by the IMAP and
// SMTP transports. Its errors never carry the access token.
package xoauth2

import (
	"encoding/json"
	"errors"
	"strings"
	"unicode"
)

const maxCleanRunes = 200

// Mechanism is the SASL mechanism name for OAuth 2.0 bearer tokens.
const Mechanism = "XOAUTH2"

// Client is a SASL client (it satisfies sasl.Client) for XOAUTH2,
// the mechanism Exchange Online and Gmail use for IMAP and SMTP.
type Client struct {
	user, token string
	challenged  bool
	failure     *Error
}

// NewClient returns a client that authenticates user with token.
func NewClient(user, token string) *Client {
	return &Client{user: user, token: token}
}

// Start returns the mechanism and the initial response.
func (c *Client) Start() (mech string, ir []byte, err error) {
	return Mechanism, []byte("user=" + c.user + "\x01auth=Bearer " + c.token + "\x01\x01"), nil
}

// Next answers the server's error challenge with an empty response, once.
// The server then sends its tagged failure, which Err details.
func (c *Client) Next(challenge []byte) ([]byte, error) {
	if c.challenged {
		return nil, errors.New("xoauth2: unexpected challenge")
	}
	c.challenged = true
	var f struct {
		Status string `json:"status"`
		Scope  string `json:"scope"`
	}
	if json.Unmarshal(challenge, &f) != nil {
		c.failure = &Error{}
	} else {
		c.failure = &Error{Status: clean(f.Status, c.token), Scope: clean(f.Scope, c.token)}
	}
	return []byte{}, nil
}

// Err returns the error the server reported in its challenge, or nil.
func (c *Client) Err() error {
	if c.failure == nil {
		return nil
	}
	return c.failure
}

// Error is the detail of a rejected XOAUTH2 login.
type Error struct{ Status, Scope string }

// Error describes the rejection; it never contains the token.
func (e *Error) Error() string {
	s := "xoauth2: server rejected the access token"
	if e.Status != "" {
		s += " (status " + e.Status + ")"
	}
	if e.Scope != "" {
		s += " for scope " + e.Scope
	}
	return s
}

// clean makes server text safe to show: secrets redacted, control
// characters removed, whitespace collapsed and length capped.
func clean(s string, secrets ...string) string {
	for _, sec := range secrets {
		if sec != "" {
			s = strings.ReplaceAll(s, sec, "[redacted]")
		}
	}
	s = strings.Map(func(r rune) rune {
		switch {
		case unicode.IsSpace(r):
			return ' '
		case unicode.IsControl(r):
			return -1
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if runes := []rune(s); len(runes) > maxCleanRunes {
		s = string(runes[:maxCleanRunes]) + "…"
	}
	return s
}
