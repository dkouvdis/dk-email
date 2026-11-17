package oauth

import (
	"reflect"
	"testing"

	"github.com/dkouvdis/dk-email/internal/config"
)

func TestProviderFor(t *testing.T) {
	ms := func(tenant string) string { return "https://login.microsoftonline.com/" + tenant + "/oauth2/v2.0/" }
	msScopes := []string{
		"https://outlook.office.com/IMAP.AccessAsUser.All",
		"https://outlook.office.com/SMTP.Send",
		"offline_access",
	}
	tests := []struct {
		name string
		acct config.Account
		want Provider
	}{
		{
			name: "microsoft default tenant",
			acct: config.Account{OAuthProvider: config.ProviderMicrosoft},
			want: Provider{Name: "microsoft", AuthURL: ms("common") + "authorize", TokenURL: ms("common") + "token", DeviceURL: ms("common") + "devicecode", Scopes: msScopes},
		},
		{
			name: "microsoft custom tenant",
			acct: config.Account{OAuthProvider: config.ProviderMicrosoft, OAuthTenant: "contoso.com"},
			want: Provider{Name: "microsoft", AuthURL: ms("contoso.com") + "authorize", TokenURL: ms("contoso.com") + "token", DeviceURL: ms("contoso.com") + "devicecode", Scopes: msScopes},
		},
		{
			name: "microsoft tenant is path escaped",
			acct: config.Account{OAuthProvider: config.ProviderMicrosoft, OAuthTenant: "a/b c"},
			want: Provider{Name: "microsoft", AuthURL: ms("a%2Fb%20c") + "authorize", TokenURL: ms("a%2Fb%20c") + "token", DeviceURL: ms("a%2Fb%20c") + "devicecode", Scopes: msScopes},
		},
		{
			name: "google",
			acct: config.Account{OAuthProvider: config.ProviderGoogle},
			want: Provider{
				Name:       "google",
				AuthURL:    "https://accounts.google.com/o/oauth2/v2/auth",
				TokenURL:   "https://oauth2.googleapis.com/token",
				Scopes:     []string{"https://mail.google.com/"},
				AuthParams: map[string]string{"access_type": "offline", "prompt": "consent"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ProviderFor(tt.acct)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestProviderFor_Unknown(t *testing.T) {
	_, err := ProviderFor(config.Account{OAuthProvider: "yahoo"})
	if err == nil || err.Error() != `unknown oauth_provider "yahoo"` {
		t.Fatalf("err = %v", err)
	}
}
