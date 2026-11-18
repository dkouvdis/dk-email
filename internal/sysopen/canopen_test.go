package sysopen

import (
	"errors"
	"testing"
)

func TestCanOpen(t *testing.T) {
	found := func(string) (string, error) { return "/bin/x", nil }
	missing := func(string) (string, error) { return "", errors.New("not found") }
	tests := []struct {
		name     string
		goos     string
		env      map[string]string
		lookPath func(string) (string, error)
		want     bool
	}{
		{"darwin with open", "darwin", nil, found, true},
		{"darwin without open", "darwin", nil, missing, false},
		{"darwin over ssh connection", "darwin", map[string]string{"SSH_CONNECTION": "1 2 3 4"}, found, false},
		{"darwin over ssh tty", "darwin", map[string]string{"SSH_TTY": "/dev/pts/0"}, found, false},
		{"windows", "windows", nil, missing, true},
		{"windows over ssh", "windows", map[string]string{"SSH_TTY": "x"}, found, false},
		{"linux with display", "linux", map[string]string{"DISPLAY": ":0"}, found, true},
		{"linux with wayland", "linux", map[string]string{"WAYLAND_DISPLAY": "wayland-0"}, found, true},
		{"linux headless", "linux", nil, found, false},
		{"linux without xdg-open", "linux", map[string]string{"DISPLAY": ":0"}, missing, false},
		{"linux over ssh with display", "linux", map[string]string{"DISPLAY": ":0", "SSH_CONNECTION": "x"}, found, false},
		{"freebsd with display", "freebsd", map[string]string{"DISPLAY": ":0"}, found, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			getenv := func(k string) string { return tt.env[k] }
			if got := canOpen(tt.goos, getenv, tt.lookPath); got != tt.want {
				t.Errorf("canOpen = %v, want %v", got, tt.want)
			}
		})
	}
}
