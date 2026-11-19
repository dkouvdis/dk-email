// Package mail defines provider-agnostic mail types and the Mailbox interface
// that is the only boundary between the UI and the network. It must import
// nothing outside the standard library.
//
// Sending (reply/compose) is the separate Sender interface, so it never
// changes Mailbox.
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
	// ErrUnsupported means the server lacks a capability the call needs.
	ErrUnsupported = errors.New("not supported by the server")
	// ErrFolderChanged means the folder's UIDs were reset on the server
	// (IMAP UIDVALIDITY changed), so UIDs from earlier calls are invalid and
	// the caller must reload from the newest message.
	ErrFolderChanged = errors.New("folder changed on server; reload it")
	// ErrAfterDelivery means the message was delivered but a later step
	// (saving to Sent, marking the original answered) failed. Callers must
	// treat the message as sent and never retry.
	ErrAfterDelivery = errors.New("message sent, but a follow-up step failed")
	// ErrDraftSaved means SaveDraft stored the new draft but a follow-up
	// step (removing the copy it replaces) failed. Callers must treat the
	// save as done.
	ErrDraftSaved = errors.New("draft saved, but a follow-up step failed")
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
	FlagSeen     Flag = iota + 1 // read
	FlagFlagged                  // starred
	FlagAnswered                 // set by sending after a reply; the UI doesn't toggle it
	FlagDraft                    // set on messages saved to Drafts
)

func (f Flag) String() string {
	switch f {
	case FlagSeen:
		return "seen"
	case FlagFlagged:
		return "flagged"
	case FlagAnswered:
		return "answered"
	case FlagDraft:
		return "draft"
	}
	return fmt.Sprintf("flag(%d)", int(f))
}

// SearchKind is what a SearchTerm matches.
type SearchKind int

const (
	SearchText SearchKind = iota // anywhere in headers or body
	SearchFrom
	SearchTo
	SearchCc
	SearchSubject
	SearchBody
	SearchUnread
	SearchRead
	SearchStarred
	SearchSince  // on or after Date
	SearchBefore // before Date
)

// SearchTerm is one condition of a SearchQuery. Value is set for the text
// kinds, Date for SearchSince and SearchBefore. Not negates the term.
type SearchTerm struct {
	Kind  SearchKind
	Value string
	Date  time.Time
	Not   bool
}

// SearchQuery is a folder search: every term must match. Raw is the query as
// the user typed it.
type SearchQuery struct {
	Raw   string
	Terms []SearchTerm
}

type Folder struct {
	Name      string // full server path, e.g. "[Gmail]/All Mail"
	Delimiter string // hierarchy separator, e.g. "/"
	Role      Role
	Unread    int
	Total     int
	// UIDNext and UIDValidity are the folder's IMAP UIDNEXT and UIDVALIDITY,
	// 0 when the server didn't report them. A rise in UIDNext means new mail.
	UIDNext     UID
	UIDValidity uint32
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
	ReplyTo      []Address // the Reply-To header, if any
	Body         string    // display text; HTML-only mail is converted to text
	BodyFromHTML bool
	Attachments  []Attachment
}

type Attachment struct {
	Name        string
	ContentType string
	Size        int64
}

// MessageRef points at a stored message.
type MessageRef struct {
	Folder string
	UID    UID
}

// File is an attachment of an outgoing message, with its content.
type File struct {
	Name        string
	ContentType string // e.g. "application/pdf"; "" means application/octet-stream
	Data        []byte
}

// Draft is an outgoing message as the user wrote it. Addresses are already
// parsed; Body is plain text.
type Draft struct {
	From        Address
	To, Cc, Bcc []Address
	Subject     string
	Body        string
	Attachments []File // sent with the message, in this order
	InReplyTo   string // Message-ID being replied to, or ""
	References  []string
	ReplyTo     *MessageRef // marked \Answered after delivery
	DraftOf     *MessageRef // the saved draft this one replaces; removed once it is saved again or sent
}

// Sender delivers drafts for one account. Send returns an error wrapping
// ErrAfterDelivery when the message was delivered but saving to Sent or
// marking the original answered failed; the caller must not retry then.
type Sender interface {
	Send(ctx context.Context, d Draft) error
}

// Drafter saves drafts to the account's Drafts folder and reopens them.
// SaveDraft returns an error wrapping ErrDraftSaved when the draft was saved
// but the copy named by d.DraftOf could not be removed.
type Drafter interface {
	SaveDraft(ctx context.Context, d Draft) error
	// OpenDraft reads a saved draft; the result's DraftOf is ref.
	OpenDraft(ctx context.Context, ref MessageRef) (Draft, error)
}

// Mailbox is one account's mail store. Implementations must be safe for
// concurrent use and must honour context cancellation.
type Mailbox interface {
	// Folders lists all folders with their role and unread/total counts.
	Folders(ctx context.Context) ([]Folder, error)
	// Summaries returns up to limit messages with UID below before (0 = newest), newest first.
	// Paging (before != 0) fails with ErrFolderChanged if the folder's UIDs were reset.
	Summaries(ctx context.Context, folder string, before UID, limit int) ([]Summary, error)
	// Search returns up to limit messages in folder matching every term of q with UID below before
	// (0 = newest), newest first, plus how many messages match below before (all matches when before is 0).
	Search(ctx context.Context, folder string, q SearchQuery, before UID, limit int) ([]Summary, int, error)
	// Message fetches and parses a full message without marking it seen.
	Message(ctx context.Context, folder string, uid UID) (Message, error)
	// Raw returns a message's exact stored bytes (RFC 5322) without marking it read.
	Raw(ctx context.Context, folder string, uid UID) ([]byte, error)
	SetFlag(ctx context.Context, folder string, uids []UID, f Flag, on bool) error
	Move(ctx context.Context, from string, uids []UID, to string) error
	Close() error
}

// Login is what one connection authenticates with.
type Login struct {
	// Password is sent with LOGIN or AUTH PLAIN when Token is empty.
	Password string
	// Token is an OAuth 2.0 access token, sent with XOAUTH2.
	Token string
}

// Credentials yields the Login for one connection attempt. Transports call
// it on every dial, reconnect, send and verify, so OAuth tokens stay fresh.
type Credentials func(ctx context.Context) (Login, error)

// StaticPassword returns Credentials that always yield password.
func StaticPassword(password string) Credentials {
	return func(context.Context) (Login, error) { return Login{Password: password}, nil }
}

// Watcher waits for new mail so it can be fetched without polling. It is
// separate from Mailbox because it holds a connection of its own.
type Watcher interface {
	// Wait blocks until folder gains messages after the previous Wait (or,
	// for the first, after it starts watching), ctx ends, or the watch
	// fails. It returns ErrUnsupported if the server can't push. Only one
	// Wait may run at a time.
	Wait(ctx context.Context, folder string) error
	Close() error
}
