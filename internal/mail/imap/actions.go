package imap

import (
	"context"
	"errors"
	"fmt"

	goimap "github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"

	"github.com/dkouvdis/dk-email/internal/mail"
)

// SetFlag adds or removes \Seen or \Flagged on uids with UID STORE, which
// is safe to repeat if do retries after a dropped connection.
func (c *Client) SetFlag(ctx context.Context, folder string, uids []mail.UID, f mail.Flag, on bool) error {
	if len(uids) == 0 {
		return nil
	}
	flag, err := imapFlag(f)
	if err != nil {
		return fmt.Errorf("imap: SetFlag: %w", err)
	}
	op := goimap.StoreFlagsDel
	if on {
		op = goimap.StoreFlagsAdd
	}
	return c.do(ctx, "SetFlag", func(conn *imapclient.Client) error {
		if err := c.openLocked(conn, folder, readWrite, true); err != nil {
			return err
		}
		store := &goimap.StoreFlags{Op: op, Silent: true, Flags: []goimap.Flag{flag}}
		if err := conn.Store(uidSet(uids), store, nil).Close(); err != nil {
			return fmt.Errorf("store %v on %q: %w", f, folder, err)
		}
		return nil
	})
}

// Move moves uids from one folder to another, never expunging anything but
// those UIDs. Only UIDs still present and not flagged \Deleted are moved,
// so a repeat (do's retry after a dropped connection) skips what's done.
// With the MOVE capability it's a UID MOVE. Otherwise it copies them, flags
// them \Deleted and, with UIDPLUS, runs UID EXPUNGE on just the given UIDs;
// without UIDPLUS they stay flagged \Deleted (hidden from Summaries)
// because a plain EXPUNGE would also remove other mail marked \Deleted.
func (c *Client) Move(ctx context.Context, from string, uids []mail.UID, to string) error {
	if len(uids) == 0 || from == to {
		return nil
	}
	return c.do(ctx, "Move", func(conn *imapclient.Client) error {
		if err := c.openLocked(conn, from, readWrite, true); err != nil {
			return err
		}
		criteria := &goimap.SearchCriteria{
			UID:     []goimap.UIDSet{uidSet(uids)},
			NotFlag: []goimap.Flag{goimap.FlagDeleted},
		}
		data, err := conn.UIDSearch(criteria, nil).Wait()
		if err != nil {
			return fmt.Errorf("search %q: %w", from, err)
		}
		pending := data.AllUIDs()
		switch {
		case conn.Caps().Has(goimap.CapMove):
			if len(pending) > 0 {
				_, err = conn.Move(goimap.UIDSetNum(pending...), to).Wait()
			}
		default:
			err = copyDeleteExpunge(conn, pending, uids, to)
		}
		if err != nil {
			return destError(from, to, err)
		}
		return nil
	})
}

// copyDeleteExpunge moves pending without the MOVE capability, then
// expunges all of uids, which includes any an earlier attempt flagged.
func copyDeleteExpunge(conn *imapclient.Client, pending []goimap.UID, uids []mail.UID, to string) error {
	if len(pending) > 0 {
		set := goimap.UIDSetNum(pending...)
		if _, err := conn.Copy(set, to).Wait(); err != nil {
			return err
		}
		store := &goimap.StoreFlags{Op: goimap.StoreFlagsAdd, Silent: true, Flags: []goimap.Flag{goimap.FlagDeleted}}
		if err := conn.Store(set, store, nil).Close(); err != nil {
			return fmt.Errorf("flag as deleted: %w", err)
		}
	}
	if !conn.Caps().Has(goimap.CapUIDPlus) {
		return nil
	}
	if err := conn.UIDExpunge(uidSet(uids)).Close(); err != nil {
		return fmt.Errorf("expunge: %w", err)
	}
	return nil
}

// destError names the folders, mapping a missing destination to
// mail.ErrFolderNotFound while keeping the server error for do.
func destError(from, to string, err error) error {
	var serverErr *goimap.Error
	if errors.As(err, &serverErr) && (serverErr.Code == goimap.ResponseCodeTryCreate || serverErr.Code == goimap.ResponseCodeNonExistent) {
		return fmt.Errorf("move from %q to %q: %w: %w", from, to, mail.ErrFolderNotFound, err)
	}
	return fmt.Errorf("move from %q to %q: %w", from, to, err)
}

func imapFlag(f mail.Flag) (goimap.Flag, error) {
	switch f {
	case mail.FlagSeen:
		return goimap.FlagSeen, nil
	case mail.FlagFlagged:
		return goimap.FlagFlagged, nil
	}
	return "", fmt.Errorf("unsupported flag %v", f)
}

func uidSet(uids []mail.UID) goimap.UIDSet {
	out := make([]goimap.UID, len(uids))
	for i, u := range uids {
		out[i] = goimap.UID(u)
	}
	return goimap.UIDSetNum(out...)
}
