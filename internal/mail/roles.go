package mail

import (
	"errors"
	"fmt"
	"slices"
)

var (
	// ErrNoArchive means the account has nowhere to archive to, so archiving
	// is disabled for it.
	ErrNoArchive = errors.New("no archive folder")
	// ErrNoTrash means the account has no \Trash folder, so delete is
	// disabled for it rather than falling back to a permanent expunge.
	ErrNoTrash = errors.New("no trash folder")
)

// ArchiveFolder picks where archived mail goes: the configured override if
// it exists, else the \Archive folder, else \All (Gmail's All Mail).
func ArchiveFolder(override string, folders []Folder) (string, error) {
	if override != "" {
		if slices.ContainsFunc(folders, func(f Folder) bool { return f.Name == override }) {
			return override, nil
		}
		return "", fmt.Errorf("%w: archive_folder %q doesn't exist on the server", ErrNoArchive, override)
	}
	for _, role := range []Role{RoleArchive, RoleAll} {
		if i := slices.IndexFunc(folders, func(f Folder) bool { return f.Role == role }); i >= 0 {
			return folders[i].Name, nil
		}
	}
	return "", fmt.Errorf("%w: the server marks none as Archive or All Mail; set archive_folder in config.toml", ErrNoArchive)
}

// TrashFolder picks where deleted mail goes: the folder with the \Trash role.
func TrashFolder(folders []Folder) (string, error) {
	if i := slices.IndexFunc(folders, func(f Folder) bool { return f.Role == RoleTrash }); i >= 0 {
		return folders[i].Name, nil
	}
	return "", fmt.Errorf("%w: the server marks none as Trash", ErrNoTrash)
}
