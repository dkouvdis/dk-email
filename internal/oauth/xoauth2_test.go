package oauth

import (
	"errors"
	"strings"
	"testing"

	"github.com/emersion/go-sasl"
)

var _ sasl.Client = (*XOAuth2Client)(nil)

func TestXOAuth2_Start(t *testing.T) {
	c := NewXOAuth2Client("me@example.com", "tok123")
	mech, ir, err := c.Start()
	if err != nil {
		t.Fatalf("Start error: %v", err)
	}
	if mech != "XOAUTH2" {
		t.Errorf("mech = %q", mech)
	}
	want := "user=me@example.com\x01auth=Bearer tok123\x01\x01"
	if string(ir) != want {
		t.Errorf("ir = %q, want %q", ir, want)
	}
}

func TestXOAuth2_ErrorChallenge(t *testing.T) {
	c := NewXOAuth2Client("me@example.com", "tok123")
	resp, err := c.Next([]byte(`{"status":"400","schemes":"Bearer","scope":"https://mail.google.com/"}`))
	if err != nil {
		t.Fatalf("Next error: %v", err)
	}
	if resp == nil || len(resp) != 0 {
		t.Errorf("resp = %#v, want non-nil empty", resp)
	}
	var xe *XOAuth2Error
	if !errors.As(c.Err(), &xe) {
		t.Fatalf("Err() = %#v, want *XOAuth2Error", c.Err())
	}
	if xe.Status != "400" || xe.Scope != "https://mail.google.com/" {
		t.Errorf("failure = %+v", xe)
	}
	want := "xoauth2: server rejected the access token (status 400) for scope https://mail.google.com/"
	if c.Err().Error() != want {
		t.Errorf("message = %q, want %q", c.Err().Error(), want)
	}
}

func TestXOAuth2_NonJSONChallenge(t *testing.T) {
	c := NewXOAuth2Client("me@example.com", "tok123")
	resp, err := c.Next([]byte("secret-raw-garbage"))
	if err != nil {
		t.Fatalf("Next error: %v", err)
	}
	if resp == nil || len(resp) != 0 {
		t.Errorf("resp = %#v, want non-nil empty", resp)
	}
	if c.Err() == nil {
		t.Fatal("Err() = nil")
	}
	if got := c.Err().Error(); got != "xoauth2: server rejected the access token" {
		t.Errorf("message = %q", got)
	}
	if strings.Contains(c.Err().Error(), "garbage") {
		t.Error("raw challenge echoed")
	}
}

func TestXOAuth2_SecondChallengeFails(t *testing.T) {
	c := NewXOAuth2Client("me@example.com", "tok123")
	if _, err := c.Next([]byte(`{"status":"401"}`)); err != nil {
		t.Fatalf("first Next error: %v", err)
	}
	resp, err := c.Next([]byte("more-stuff"))
	if err == nil {
		t.Fatal("second Next returned nil error")
	}
	if resp != nil {
		t.Errorf("resp = %#v, want nil", resp)
	}
	if strings.Contains(err.Error(), "more-stuff") {
		t.Error("error echoes challenge")
	}
}

func TestXOAuth2_NoChallengeNoErr(t *testing.T) {
	c := NewXOAuth2Client("me@example.com", "tok123")
	if c.Err() != nil {
		t.Errorf("Err() = %#v, want nil", c.Err())
	}
}

func TestXOAuth2_ErrorNeverLeaksToken(t *testing.T) {
	const tok = "supersecrettoken"
	c := NewXOAuth2Client("me@example.com", tok)
	if _, err := c.Next([]byte(`{"status":"` + tok + `","scope":"x ` + tok + ` y"}`)); err != nil {
		t.Fatalf("Next error: %v", err)
	}
	if strings.Contains(c.Err().Error(), tok) {
		t.Errorf("error leaks token: %q", c.Err().Error())
	}
}
