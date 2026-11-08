package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRun_VersionFlag(t *testing.T) {
	for _, flag := range []string{"--version", "-version"} {
		t.Run(flag, func(t *testing.T) {
			var stdout, stderr bytes.Buffer

			err := run([]string{flag}, &stdout, &stderr)

			if err != nil {
				t.Fatalf("run() error = %v", err)
			}
			if !strings.HasPrefix(stdout.String(), "dk-email ") {
				t.Errorf("stdout = %q, want it to start with %q", stdout.String(), "dk-email ")
			}
		})
	}
}

func TestRun_AcceptsConfigFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer

	err := run([]string{"--config", "/tmp/dk-email-test.toml"}, &stdout, &stderr)

	if err != nil {
		t.Fatalf("run() error = %v", err)
	}
}

func TestRun_UnknownFlagFails(t *testing.T) {
	var stdout, stderr bytes.Buffer

	err := run([]string{"--nope"}, &stdout, &stderr)

	if err == nil {
		t.Fatal("run() error = nil, want error for unknown flag")
	}
}
