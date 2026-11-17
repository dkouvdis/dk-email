// Package oauth implements OAuth 2.0 sign-in (loopback and device code flows)
// and access token refresh for XOAUTH2 mail accounts.
package oauth

import (
	"fmt"
	"net/url"

	"github.com/dkouvdis/dk-email/internal/config"
)

// Provider is an OAuth 2.0 authorization server.
type Provider struct {
	// Name is "microsoft" or "google".
	Name     string
	AuthURL  string
	TokenURL string
	// DeviceURL is empty when the provider has no device code flow we can use.
	DeviceURL string
	Scopes    []string
	// AuthParams are extra authorization request parameters.
	AuthParams map[string]string
}

// ProviderFor returns the OAuth provider an account is configured for.
func ProviderFor(acct config.Account) (Provider, error) {
	switch acct.OAuthProvider {
	case config.ProviderMicrosoft:
		tenant := acct.OAuthTenant
		if tenant == "" {
			tenant = "common"
		}
		base := "https://login.microsoftonline.com/" + url.PathEscape(tenant) + "/oauth2/v2.0/"
		return Provider{
			Name:      config.ProviderMicrosoft,
			AuthURL:   base + "authorize",
			TokenURL:  base + "token",
			DeviceURL: base + "devicecode",
			Scopes: []string{
				"https://outlook.office.com/IMAP.AccessAsUser.All",
				"https://outlook.office.com/SMTP.Send",
				"offline_access",
			},
		}, nil
	case config.ProviderGoogle:
		return Provider{
			Name:     config.ProviderGoogle,
			AuthURL:  "https://accounts.google.com/o/oauth2/v2/auth",
			TokenURL: "https://oauth2.googleapis.com/token",
			// Google's device flow does not allow the mail scope.
			Scopes: []string{"https://mail.google.com/"},
			// Always issue a refresh token.
			AuthParams: map[string]string{"access_type": "offline", "prompt": "consent"},
		}, nil
	}
	return Provider{}, fmt.Errorf("unknown oauth_provider %q", acct.OAuthProvider)
}
