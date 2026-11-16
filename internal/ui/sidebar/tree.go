package sidebar

import (
	"slices"
	"strings"

	"github.com/dkouvdis/dk-email/internal/mail"
)

// Entry is one row of an account's folder tree: a folder, or a parent that
// exists only in other folders' paths (Folder false), shown but not
// selectable.
type Entry struct {
	Name   string // full server path
	Label  string // shown in the tree: the last part of the path, or "Inbox"
	Path   string // shown while filtering: the path with "/" between parts
	Depth  int
	Folder bool
	Unread int // 0 for Trash, whose count isn't shown
}

type node struct {
	entry Entry
	role  mail.Role
	kids  []*node
}

// Tree arranges folders by path. Special folders (Inbox, Drafts, Sent,
// Archive, All Mail, Trash, Junk) stay at the top level in that order, so
// Gmail's [Gmail]/… folders don't nest; other folders follow, nested under
// their parents and sorted by name at each level. A folder nested under a
// special folder, such as INBOX/Later, stays under it.
func Tree(folders []mail.Folder) []Entry {
	byName := make(map[string]mail.Folder, len(folders))
	for _, f := range folders {
		byName[f.Name] = f
	}
	nodes := map[string]*node{}
	var roots []*node

	var add func(name, delim string) *node
	add = func(name, delim string) *node {
		if n, ok := nodes[name]; ok {
			return n
		}
		f, isFolder := byName[name]
		parent, last := splitPath(name, delim)
		n := &node{entry: Entry{Name: name, Label: last, Path: displayPath(name, delim), Folder: isFolder, Unread: f.Unread}}
		nodes[name] = n
		if isFolder && f.Role != mail.RoleNone {
			n.role = f.Role
			n.entry.Label = Label(f)
			n.entry.Path = n.entry.Label
		}
		if n.role == mail.RoleTrash {
			n.entry.Unread = 0
		}
		if n.role != mail.RoleNone || parent == "" {
			roots = append(roots, n)
		} else {
			p := add(parent, delim)
			p.kids = append(p.kids, n)
		}
		return n
	}
	for _, f := range folders {
		add(f.Name, f.Delimiter)
	}

	var out []Entry
	var walk func(ns []*node, depth int)
	walk = func(ns []*node, depth int) {
		slices.SortStableFunc(ns, compareNodes)
		for _, n := range ns {
			e := n.entry
			e.Depth = depth
			out = append(out, e)
			walk(n.kids, depth+1)
		}
	}
	walk(roots, 0)
	return out
}

// compareNodes puts special folders first, in role order, then the rest by
// name, ignoring case.
func compareNodes(a, b *node) int {
	return compareFolders(
		mail.Folder{Name: a.entry.Label, Role: a.role},
		mail.Folder{Name: b.entry.Label, Role: b.role},
	)
}

// splitPath returns the parent path and the last part of name, or "" and
// name when it has no parent.
func splitPath(name, delim string) (parent, last string) {
	if delim == "" {
		return "", name
	}
	i := strings.LastIndex(name, delim)
	if i <= 0 || i+len(delim) >= len(name) {
		return "", name
	}
	return name[:i], name[i+len(delim):]
}

func displayPath(name, delim string) string {
	if delim == "" || delim == "/" {
		return name
	}
	return strings.ReplaceAll(name, delim, "/")
}
