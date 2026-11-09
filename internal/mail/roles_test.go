package mail

import (
	"errors"
	"strings"
	"testing"
)

func TestArchiveFolder(t *testing.T) {
	archive := Folder{Name: "Archive", Role: RoleArchive}
	all := Folder{Name: "[Gmail]/All Mail", Role: RoleAll}
	custom := Folder{Name: "Old"}
	inbox := Folder{Name: "INBOX", Role: RoleInbox}

	tests := []struct {
		name     string
		override string
		folders  []Folder
		want     string
		wantErr  string
	}{
		{"override wins over roles", "Old", []Folder{inbox, archive, all, custom}, "Old", ""},
		{"archive role", "", []Folder{inbox, all, archive}, "Archive", ""},
		{"all mail when no archive", "", []Folder{inbox, all}, "[Gmail]/All Mail", ""},
		{"none", "", []Folder{inbox, custom}, "", "no archive folder"},
		{"no folders", "", nil, "", "no archive folder"},
		{"missing override", "Gone", []Folder{inbox, archive}, "", `archive_folder "Gone" doesn't exist`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ArchiveFolder(tt.override, tt.folders)

			if got != tt.want {
				t.Errorf("ArchiveFolder() = %q, want %q", got, tt.want)
			}
			switch {
			case tt.wantErr == "" && err != nil:
				t.Errorf("error = %v", err)
			case tt.wantErr != "" && (!errors.Is(err, ErrNoArchive) || !strings.Contains(err.Error(), tt.wantErr)):
				t.Errorf("error = %v, want ErrNoArchive containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestTrashFolder(t *testing.T) {
	trash := Folder{Name: "[Gmail]/Bin", Role: RoleTrash}

	if got, err := TrashFolder([]Folder{{Name: "INBOX", Role: RoleInbox}, trash}); got != "[Gmail]/Bin" || err != nil {
		t.Errorf("TrashFolder() = %q, %v", got, err)
	}
	// A folder merely named Trash isn't trusted without the \Trash role.
	if got, err := TrashFolder([]Folder{{Name: "Trash"}}); got != "" || !errors.Is(err, ErrNoTrash) {
		t.Errorf("TrashFolder() without a role = %q, %v; want ErrNoTrash", got, err)
	}
}
