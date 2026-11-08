package main

import (
	"fmt"
	"regexp"
	"runtime/debug"
	"strings"
)

// Set at build time by GoReleaser via -ldflags "-X main.version=... -X main.commit=... -X main.date=...".
var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

type buildVersion struct {
	version, commit, date string
}

func defaultVersion() buildVersion {
	return buildVersion{"dev", "unknown", "unknown"}
}

func (b buildVersion) String() string {
	return fmt.Sprintf("dk-email %s (%s, %s)", strings.TrimPrefix(b.version, "v"), b.commit, b.date)
}

// Matches Go pseudo-versions such as v0.0.0-20261108191108-2b38c82e1234
// and v0.1.1-0.20261108191108-2b38c82e1234.
var pseudoVersion = regexp.MustCompile(`-(?:.*\.)?\d{14}-[0-9a-f]{12}$`)

// resolveVersion fills in values not injected via ldflags from the Go build
// info, so `go install …@vX.Y.Z` and plain `go build` still report something useful.
func resolveVersion(injected buildVersion, info *debug.BuildInfo, ok bool) buildVersion {
	if !ok || info == nil {
		return injected
	}
	out := injected
	if out.version == "dev" && isReleaseVersion(info.Main.Version) {
		out.version = info.Main.Version
	}

	var revision, vcsTime string
	var modified bool
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			revision = s.Value
		case "vcs.time":
			vcsTime = s.Value
		case "vcs.modified":
			modified = s.Value == "true"
		}
	}
	if out.commit == "unknown" && revision != "" {
		out.commit = revision[:min(7, len(revision))]
		if modified {
			out.commit += "-dirty"
		}
	}
	if out.date == "unknown" && len(vcsTime) >= len("2006-01-02") {
		out.date = vcsTime[:len("2006-01-02")]
	}
	return out
}

func isReleaseVersion(v string) bool {
	return strings.HasPrefix(v, "v") && !strings.Contains(v, "+") && !pseudoVersion.MatchString(v)
}

func currentVersion() buildVersion {
	info, ok := debug.ReadBuildInfo()
	return resolveVersion(buildVersion{version, commit, date}, info, ok)
}
