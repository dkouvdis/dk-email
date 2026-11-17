package oauth

import (
	"encoding/json"
	"errors"
)

// XOAuth2 is the SASL mechanism name for OAuth 2.0 bearer tokens.
const XOAuth2 = "XOAUTH2"

// XOAuth2Client is a SASL client (it satisfies sasl.Client) for XOAUTH2,
// the mechanism Exchange Online and Gmail use for IMAP and SMTP.
type XOAuth2Client struct {
	user, token string
	challenged  bool
	failure     *XOAuth2Error
}

// NewXOAuth2Client returns a client that authenticates user with token.
func NewXOAuth2Client(user, token string) *XOAuth2Client {
	return &XOAuth2Client{user: user, token: token}
}

// Start returns the mechanism and the initial response.
func (c *XOAuth2Client) Start() (mech string, ir []byte, err error) {
	return XOAuth2, []byte("user=" + c.user + "\x01auth=Bearer " + c.token + "\x01\x01"), nil
}

// Next answers the server's error challenge with an empty response, once.
// The server then sends its tagged failure, which Err details.
func (c *XOAuth2Client) Next(challenge []byte) ([]byte, error) {
	if c.challenged {
		return nil, errors.New("xoauth2: unexpected challenge")
	}
	c.challenged = true
	var f struct {
		Status string `json:"status"`
		Scope  string `json:"scope"`
	}
	if json.Unmarshal(challenge, &f) != nil {
		c.failure = &XOAuth2Error{}
	} else {
		c.failure = &XOAuth2Error{Status: clean(f.Status, c.token), Scope: clean(f.Scope, c.token)}
	}
	return []byte{}, nil
}

// Err returns the error the server reported in its challenge, or nil.
func (c *XOAuth2Client) Err() error {
	if c.failure == nil {
		return nil
	}
	return c.failure
}

// XOAuth2Error is the detail of a rejected XOAUTH2 login.
type XOAuth2Error struct{ Status, Scope string }

// Error describes the rejection; it never contains the token.
func (e *XOAuth2Error) Error() string {
	s := "xoauth2: server rejected the access token"
	if e.Status != "" {
		s += " (status " + e.Status + ")"
	}
	if e.Scope != "" {
		s += " for scope " + e.Scope
	}
	return s
}
