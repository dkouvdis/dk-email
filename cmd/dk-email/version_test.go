package main

import (
	"runtime/debug"
	"testing"
)

func TestFormatVersion(t *testing.T) {
	tests := []struct {
		name string
		info buildVersion
		want string
	}{
		{"untagged defaults", buildVersion{"dev", "unknown", "unknown"}, "dk-email dev (unknown, unknown)"},
		{"release injected by ldflags", buildVersion{"0.1.0-beta.1", "abc1234", "2026-11-20"}, "dk-email 0.1.0-beta.1 (abc1234, 2026-11-20)"},
		{"leading v is stripped", buildVersion{"v0.1.0", "abc1234", "2026-11-20"}, "dk-email 0.1.0 (abc1234, 2026-11-20)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.info.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveVersion_LdflagsWin(t *testing.T) {
	injected := buildVersion{"0.1.0", "abc1234", "2026-11-20"}
	info := &debug.BuildInfo{Main: debug.Module{Version: "v9.9.9"}}

	got := resolveVersion(injected, info, true)

	if got != injected {
		t.Errorf("resolveVersion() = %+v, want injected %+v", got, injected)
	}
}

func TestResolveVersion_GoInstallTaggedRelease(t *testing.T) {
	info := &debug.BuildInfo{Main: debug.Module{Version: "v0.2.0"}}

	got := resolveVersion(defaultVersion(), info, true)

	if got.version != "v0.2.0" {
		t.Errorf("version = %q, want %q", got.version, "v0.2.0")
	}
}

func TestResolveVersion_IgnoresNonReleaseModuleVersions(t *testing.T) {
	for _, v := range []string{
		"(devel)",
		"",
		"v0.0.0-20261108191108-2b38c82e1234",
		"v0.1.1-0.20261108191108-2b38c82e1234",
		"v0.1.0+dirty",
	} {
		t.Run(v, func(t *testing.T) {
			info := &debug.BuildInfo{Main: debug.Module{Version: v}}

			got := resolveVersion(defaultVersion(), info, true)

			if got.version != "dev" {
				t.Errorf("version = %q, want %q", got.version, "dev")
			}
		})
	}
}

func TestResolveVersion_VCSFallback(t *testing.T) {
	info := &debug.BuildInfo{Settings: []debug.BuildSetting{
		{Key: "vcs.revision", Value: "2b38c82e1234567890abcdef"},
		{Key: "vcs.time", Value: "2026-11-08T19:11:08Z"},
		{Key: "vcs.modified", Value: "true"},
	}}

	got := resolveVersion(defaultVersion(), info, true)

	want := buildVersion{"dev", "2b38c82-dirty", "2026-11-08"}
	if got != want {
		t.Errorf("resolveVersion() = %+v, want %+v", got, want)
	}
}

func TestResolveVersion_NoBuildInfo(t *testing.T) {
	got := resolveVersion(defaultVersion(), nil, false)

	if got != defaultVersion() {
		t.Errorf("resolveVersion() = %+v, want defaults", got)
	}
}

func TestFormatVersion_GoReleaserDate(t *testing.T) {
	got := buildVersion{"v0.1.0-beta.1", "abc1234", "2026-11-20T10:15:00Z"}.String()

	if want := "dk-email 0.1.0-beta.1 (abc1234, 2026-11-20)"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}
