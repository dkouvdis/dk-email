package mail

import (
	"os/exec"
	"strings"
	"testing"
)

// The UI depends only on this package, so it must stay free of IMAP, UI and
// any other third-party imports.
func TestMailPackage_ImportsOnlyStandardLibrary(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", "-f", "{{if not .Standard}}{{.ImportPath}}{{end}}", ".").Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}

	for _, dep := range strings.Fields(string(out)) {
		if dep != "github.com/dkouvdis/dk-email/internal/mail" {
			t.Errorf("mail imports non-standard package %q", dep)
		}
	}
}

func TestAddress_String(t *testing.T) {
	tests := []struct {
		addr Address
		want string
	}{
		{Address{Name: "Jane Doe", Email: "jane@example.com"}, "Jane Doe <jane@example.com>"},
		{Address{Email: "jane@example.com"}, "jane@example.com"},
		{Address{Name: "Jane Doe"}, "Jane Doe"},
	}
	for _, tt := range tests {
		if got := tt.addr.String(); got != tt.want {
			t.Errorf("%+v.String() = %q, want %q", tt.addr, got, tt.want)
		}
	}
}

func TestAddress_DisplayName(t *testing.T) {
	tests := []struct {
		addr Address
		want string
	}{
		{Address{Name: "Jane Doe", Email: "jane@example.com"}, "Jane Doe"},
		{Address{Name: "  ", Email: "jane@example.com"}, "jane@example.com"},
		{Address{}, "(unknown sender)"},
	}
	for _, tt := range tests {
		if got := tt.addr.DisplayName(); got != tt.want {
			t.Errorf("%+v.DisplayName() = %q, want %q", tt.addr, got, tt.want)
		}
	}
}

func TestRole_String(t *testing.T) {
	tests := map[Role]string{
		RoleNone: "", RoleInbox: "inbox", RoleDrafts: "drafts", RoleSent: "sent",
		RoleArchive: "archive", RoleAll: "all", RoleTrash: "trash", RoleJunk: "junk",
		Role(99): "role(99)",
	}
	for role, want := range tests {
		if got := role.String(); got != want {
			t.Errorf("Role(%d).String() = %q, want %q", role, got, want)
		}
	}
}

func TestFlag_String(t *testing.T) {
	tests := map[Flag]string{FlagSeen: "seen", FlagFlagged: "flagged", FlagAnswered: "answered", FlagDraft: "draft", Flag(99): "flag(99)"}
	for flag, want := range tests {
		if got := flag.String(); got != want {
			t.Errorf("Flag(%d).String() = %q, want %q", flag, got, want)
		}
	}
}
