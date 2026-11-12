package imap

import (
	"context"
	"errors"
	"testing"
	"time"

	goimap "github.com/emersion/go-imap/v2"

	"github.com/dkouvdis/dk-email/internal/mail"
)

func TestRoleFor_MapsNamesAndSpecialUseAttributes(t *testing.T) {
	tests := []struct {
		name  string
		mbox  string
		attrs []goimap.MailboxAttr
		want  mail.Role
	}{
		{"INBOX by name", "INBOX", nil, mail.RoleInbox},
		{"inbox is case-insensitive", "Inbox", nil, mail.RoleInbox},
		{"sent", "Sent Mail", []goimap.MailboxAttr{"\\Sent"}, mail.RoleSent},
		{"drafts", "Drafts", []goimap.MailboxAttr{"\\Drafts"}, mail.RoleDrafts},
		{"trash", "[Gmail]/Trash", []goimap.MailboxAttr{"\\Trash"}, mail.RoleTrash},
		{"junk", "[Gmail]/Spam", []goimap.MailboxAttr{"\\Junk"}, mail.RoleJunk},
		{"archive", "Archive", []goimap.MailboxAttr{"\\Archive"}, mail.RoleArchive},
		{"all", "[Gmail]/All Mail", []goimap.MailboxAttr{"\\All"}, mail.RoleAll},
		{"attributes are case-insensitive", "Sent", []goimap.MailboxAttr{"\\sent"}, mail.RoleSent},
		{"role found among other attributes", "Sent", []goimap.MailboxAttr{"\\HasNoChildren", "\\Sent"}, mail.RoleSent},
		{"flagged is not a role", "[Gmail]/Starred", []goimap.MailboxAttr{"\\Flagged"}, mail.RoleNone},
		{"important is not a role", "[Gmail]/Important", []goimap.MailboxAttr{"\\Important"}, mail.RoleNone},
		{"plain folder", "Receipts", []goimap.MailboxAttr{"\\HasNoChildren"}, mail.RoleNone},
		{"no attributes", "Receipts", nil, mail.RoleNone},
		{"name alone does not imply a role", "Sent", nil, mail.RoleNone},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := roleFor(tt.mbox, tt.attrs)

			if got != tt.want {
				t.Errorf("roleFor(%q, %v) = %v, want %v", tt.mbox, tt.attrs, got, tt.want)
			}
		})
	}
}

func TestIsSelectable(t *testing.T) {
	tests := []struct {
		name  string
		attrs []goimap.MailboxAttr
		want  bool
	}{
		{"no attributes", nil, true},
		{"ordinary attributes", []goimap.MailboxAttr{"\\HasChildren", "\\Sent"}, true},
		{"noselect", []goimap.MailboxAttr{"\\Noselect"}, false},
		{"noselect in other case", []goimap.MailboxAttr{"\\NoSelect"}, false},
		{"nonexistent", []goimap.MailboxAttr{"\\NonExistent"}, false},
		{"noselect among others", []goimap.MailboxAttr{"\\HasChildren", "\\Noselect"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isSelectable(tt.attrs); got != tt.want {
				t.Errorf("isSelectable(%v) = %v, want %v", tt.attrs, got, tt.want)
			}
		})
	}
}

func TestListOptions_UsesOnlyWhatTheServerAdvertises(t *testing.T) {
	tests := []struct {
		name        string
		caps        goimap.CapSet
		wantNil     bool
		wantSpecial bool
		wantStatus  bool
	}{
		{"no capabilities", nil, true, false, false},
		{"plain IMAP4rev1", goimap.CapSet{goimap.CapIMAP4rev1: {}}, true, false, false},
		{"LIST-EXTENDED only", goimap.CapSet{goimap.CapIMAP4rev1: {}, goimap.CapListExtended: {}}, false, false, false},
		{"LIST-EXTENDED and SPECIAL-USE", goimap.CapSet{goimap.CapListExtended: {}, goimap.CapSpecialUse: {}}, false, true, false},
		{"Gmail-like", goimap.CapSet{goimap.CapListExtended: {}, goimap.CapListStatus: {}, goimap.CapSpecialUse: {}}, false, true, true},
		{"LIST-STATUS without LIST-EXTENDED", goimap.CapSet{goimap.CapListStatus: {}}, true, false, false},
		{"IMAP4rev2 implies LIST-EXTENDED and LIST-STATUS", goimap.CapSet{goimap.CapIMAP4rev2: {}}, false, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := listOptions(tt.caps)

			if tt.wantNil {
				if got != nil {
					t.Fatalf("listOptions() = %+v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatal("listOptions() = nil, want options")
			}
			if got.ReturnSpecialUse != tt.wantSpecial {
				t.Errorf("ReturnSpecialUse = %v, want %v", got.ReturnSpecialUse, tt.wantSpecial)
			}
			if gotStatus := got.ReturnStatus != nil; gotStatus != tt.wantStatus {
				t.Errorf("ReturnStatus set = %v, want %v", gotStatus, tt.wantStatus)
			}
		})
	}
}

func TestNewFolder_FillsFieldsAndToleratesMissingData(t *testing.T) {
	messages, unseen := uint32(7), uint32(3)
	tests := []struct {
		name string
		data goimap.ListData
		want mail.Folder
	}{
		{
			"full",
			goimap.ListData{
				Mailbox: "[Gmail]/Trash", Delim: '/', Attrs: []goimap.MailboxAttr{"\\Trash"},
				Status: &goimap.StatusData{NumMessages: &messages, NumUnseen: &unseen},
			},
			mail.Folder{Name: "[Gmail]/Trash", Delimiter: "/", Role: mail.RoleTrash, Unread: 3, Total: 7},
		},
		{
			"no delimiter and no status",
			goimap.ListData{Mailbox: "INBOX"},
			mail.Folder{Name: "INBOX", Role: mail.RoleInbox},
		},
		{
			"status without counts",
			goimap.ListData{Mailbox: "Receipts", Delim: '.', Status: &goimap.StatusData{}},
			mail.Folder{Name: "Receipts", Delimiter: "."},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := newFolder(&tt.data)

			if got != tt.want {
				t.Errorf("newFolder() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestSentFolder(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		srv := startServer(t, modeImplicit, capsListStatus)
		c := srv.newClient(t, modeImplicit)

		name, ok, err := c.SentFolder(timeoutCtx(t, 10*time.Second))

		if err != nil || !ok || name != "Sent" {
			t.Errorf("SentFolder() = %q, %v, %v; want %q, true, nil", name, ok, err, "Sent")
		}
	})
	t.Run("none", func(t *testing.T) {
		srv := startServer(t, modeImplicit, capsListStatus)
		srv.noSentRole.Store(true)
		c := srv.newClient(t, modeImplicit)

		name, ok, err := c.SentFolder(timeoutCtx(t, 10*time.Second))

		if err != nil || ok || name != "" {
			t.Errorf("SentFolder() = %q, %v, %v; want empty, false, nil", name, ok, err)
		}
	})
	t.Run("error", func(t *testing.T) {
		srv := startServer(t, modeImplicit, capsListStatus)
		c := srv.newClient(t, modeImplicit)
		ctx, cancel := context.WithCancel(bg)
		cancel()

		if _, ok, err := c.SentFolder(ctx); ok || !errors.Is(err, context.Canceled) {
			t.Errorf("SentFolder() = _, %v, %v; want false, context.Canceled", ok, err)
		}
	})
}

func TestDraftsFolder(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		srv := startServer(t, modeImplicit, capsListStatus)
		srv.addDrafts(t)
		c := srv.newClient(t, modeImplicit)

		name, ok, err := c.DraftsFolder(timeoutCtx(t, 10*time.Second))

		if err != nil || !ok || name != "Drafts" {
			t.Errorf("DraftsFolder() = %q, %v, %v; want %q, true, nil", name, ok, err, "Drafts")
		}
	})
	t.Run("none", func(t *testing.T) {
		srv := startServer(t, modeImplicit, capsListStatus)
		c := srv.newClient(t, modeImplicit)

		name, ok, err := c.DraftsFolder(timeoutCtx(t, 10*time.Second))

		if err != nil || ok || name != "" {
			t.Errorf("DraftsFolder() = %q, %v, %v; want empty, false, nil", name, ok, err)
		}
	})
}
