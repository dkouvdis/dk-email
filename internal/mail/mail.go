// Package mail defines provider-agnostic mail types and the Mailbox interface
// that is the only boundary between the UI and the network. It must import
// nothing outside the standard library.
//
// Sending (reply/compose) will be a separate Sender interface, so adding it
// never changes Mailbox.
package mail

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrFolderNotFound  = errors.New("folder not found")
	ErrMessageNotFound = errors.New("message not found")
	ErrClosed          = errors.New("mailbox closed")
)

// UID identifies a message within one folder (IMAP UID semantics).
type UID uint32

// Role is a folder's special use, independent of its server-specific name.
type Role int

const (
	RoleNone Role = iota
	RoleInbox
	RoleDrafts
	RoleSent
	RoleArchive
	RoleAll // e.g. Gmail's All Mail
	RoleTrash
	RoleJunk
)

func (r Role) String() string {
	switch r {
	case RoleNone:
		return ""
	case RoleInbox:
		return "inbox"
	case RoleDrafts:
		return "drafts"
	case RoleSent:
		return "sent"
	case RoleArchive:
		return "archive"
	case RoleAll:
		return "all"
	case RoleTrash:
		return "trash"
	case RoleJunk:
		return "junk"
	}
	return fmt.Sprintf("role(%d)", int(r))
}

// Flag is a message flag the UI can toggle.
type Flag int

const (
	FlagSeen    Flag = iota + 1 // read
	FlagFlagged                 // starred
)

func (f Flag) String() string {
	switch f {
	case FlagSeen:
		return "seen"
	case FlagFlagged:
		return "flagged"
	}
	return fmt.Sprintf("flag(%d)", int(f))
}

type Folder struct {
	Name      string // full server path, e.g. "[Gmail]/All Mail"
	Delimiter string // hierarchy separator, e.g. "/"
	Role      Role
	Unread    int
	Total     int
}

type Address struct {
	Name  string
	Email string
}

func (a Address) String() string {
	switch {
	case a.Name != "" && a.Email != "":
		return fmt.Sprintf("%s <%s>", a.Name, a.Email)
	case a.Email != "":
		return a.Email
	}
	return a.Name
}

// DisplayName is what list rows show for a sender.
func (a Address) DisplayName() string {
	if name := strings.TrimSpace(a.Name); name != "" {
		return name
	}
	if a.Email != "" {
		return a.Email
	}
	return "(unknown sender)"
}

// Summary is everything the thread list and threading need, without the body.
type Summary struct {
	UID     UID
	From    Address
	To      []Address
	Subject string
	Date    time.Time
	Seen    bool
	Flagged bool

	MessageID  string
	InReplyTo  string
	References []string
}

// Message is a fully fetched message, ready to display.
type Message struct {
	Summary
	Cc           []Address
	Body         string // display text; HTML-only mail is converted to text
	BodyFromHTML bool
	Attachments  []Attachment
}

type Attachment struct {
	Name        string
	ContentType string
	Size        int64
}

// Mailbox is one account's mail store. Implementations must be safe for
// concurrent use and must honour context cancellation.
type Mailbox interface {
	// Folders lists all folders with their role and unread/total counts.
	Folders(ctx context.Context) ([]Folder, error)
	// Summaries returns up to limit messages with UID below before (0 = newest), newest first.
	Summaries(ctx context.Context, folder string, before UID, limit int) ([]Summary, error)
	// Message fetches and parses a full message without marking it seen.
	Message(ctx context.Context, folder string, uid UID) (Message, error)
	SetFlag(ctx context.Context, folder string, uids []UID, f Flag, on bool) error
	Move(ctx context.Context, from string, uids []UID, to string) error
	Close() error
}
