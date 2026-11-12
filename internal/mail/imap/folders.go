package imap

import (
	"context"
	"fmt"
	"strings"

	goimap "github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"

	"github.com/dkouvdis/dk-email/internal/mail"
)

// Folders lists the account's selectable folders, in server order.
func (c *Client) Folders(ctx context.Context) ([]mail.Folder, error) {
	var folders []mail.Folder
	err := c.do(ctx, "Folders", func(conn *imapclient.Client) error {
		var err error
		folders, err = listFolders(conn)
		return err
	})
	if err != nil {
		return nil, err
	}
	return folders, nil
}

// SentFolder returns the name of the folder with mail.RoleSent; ok is false
// when the account has none.
func (c *Client) SentFolder(ctx context.Context) (name string, ok bool, err error) {
	err = c.do(ctx, "SentFolder", func(conn *imapclient.Client) error {
		folders, err := listFolders(conn)
		if err != nil {
			return err
		}
		for _, f := range folders {
			if f.Role == mail.RoleSent {
				name, ok = f.Name, true
				return nil
			}
		}
		name, ok = "", false
		return nil
	})
	if err != nil {
		return "", false, err
	}
	return name, ok, nil
}

var countOptions = goimap.StatusOptions{NumMessages: true, NumUnseen: true}

func listFolders(conn *imapclient.Client) ([]mail.Folder, error) {
	list, err := conn.List("", "*", listOptions(conn.Caps())).Collect()
	if err != nil {
		return nil, fmt.Errorf("list mailboxes: %w", err)
	}

	var selectable []*goimap.ListData
	for _, data := range list {
		if isSelectable(data.Attrs) {
			selectable = append(selectable, data)
		}
	}

	// Servers without LIST-STATUS, or ones that skip a mailbox in it, need a
	// STATUS each. The commands are pipelined and matched by mailbox name.
	pending := make(map[*goimap.ListData]*imapclient.StatusCommand)
	for _, data := range selectable {
		if data.Status == nil {
			pending[data] = conn.Status(data.Mailbox, &countOptions)
		}
	}
	for data, cmd := range pending {
		status, err := cmd.Wait()
		if err != nil {
			return nil, fmt.Errorf("status of %q: %w", data.Mailbox, err)
		}
		data.Status = status
	}

	folders := make([]mail.Folder, 0, len(selectable))
	for _, data := range selectable {
		folders = append(folders, newFolder(data))
	}
	return folders, nil
}

// listOptions asks only for what the server advertises: RETURN options need
// LIST-EXTENDED, and without it the attributes come from a plain LIST.
func listOptions(caps goimap.CapSet) *goimap.ListOptions {
	if !caps.Has(goimap.CapListExtended) {
		return nil
	}
	opts := &goimap.ListOptions{ReturnSpecialUse: caps.Has(goimap.CapSpecialUse)}
	if caps.Has(goimap.CapListStatus) {
		opts.ReturnStatus = &countOptions
	}
	return opts
}

func newFolder(data *goimap.ListData) mail.Folder {
	f := mail.Folder{
		Name: data.Mailbox,
		Role: roleFor(data.Mailbox, data.Attrs),
	}
	if data.Delim != 0 {
		f.Delimiter = string(data.Delim)
	}
	if s := data.Status; s != nil {
		if s.NumMessages != nil {
			f.Total = int(*s.NumMessages)
		}
		if s.NumUnseen != nil {
			f.Unread = int(*s.NumUnseen)
		}
	}
	return f
}

func isSelectable(attrs []goimap.MailboxAttr) bool {
	for _, a := range attrs {
		if hasAttr(a, goimap.MailboxAttrNoSelect) || hasAttr(a, goimap.MailboxAttrNonExistent) {
			return false
		}
	}
	return true
}

func hasAttr(a, want goimap.MailboxAttr) bool {
	return strings.EqualFold(string(a), string(want))
}

// roleFor maps a mailbox to its mail.Role. Only the special-use attributes
// that the UI treats specially are mapped; Gmail's \Flagged and \Important
// stay RoleNone.
func roleFor(name string, attrs []goimap.MailboxAttr) mail.Role {
	if strings.EqualFold(name, "INBOX") {
		return mail.RoleInbox
	}
	roles := []struct {
		attr goimap.MailboxAttr
		role mail.Role
	}{
		{goimap.MailboxAttrSent, mail.RoleSent},
		{goimap.MailboxAttrDrafts, mail.RoleDrafts},
		{goimap.MailboxAttrTrash, mail.RoleTrash},
		{goimap.MailboxAttrJunk, mail.RoleJunk},
		{goimap.MailboxAttrArchive, mail.RoleArchive},
		{goimap.MailboxAttrAll, mail.RoleAll},
	}
	for _, a := range attrs {
		for _, r := range roles {
			if hasAttr(a, r.attr) {
				return r.role
			}
		}
	}
	return mail.RoleNone
}
