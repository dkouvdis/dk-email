package imap

import (
	"bytes"
	"context"
	"fmt"

	goimap "github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"

	"github.com/dkouvdis/dk-email/internal/mail"
	"github.com/dkouvdis/dk-email/internal/render"
)

// wholeMessage is BODY.PEEK[], so fetching never sets \Seen.
var wholeMessage = &goimap.FetchItemBodySection{Peek: true}

// Message fetches and parses one message. Like Summaries it opens the folder
// read-only, and it returns mail.ErrFolderChanged if the folder's UIDs were
// reset since they were listed.
func (c *Client) Message(ctx context.Context, folder string, uid mail.UID) (mail.Message, error) {
	var out mail.Message
	err := c.do(ctx, "Message", func(conn *imapclient.Client) error {
		if err := c.examineLocked(conn, folder, true); err != nil {
			return err
		}
		opts := &goimap.FetchOptions{
			UID:          true,
			Flags:        true,
			InternalDate: true,
			BodySection:  []*goimap.FetchItemBodySection{wholeMessage},
		}
		msgs, err := conn.Fetch(goimap.UIDSetNum(goimap.UID(uid)), opts).Collect()
		if err != nil {
			return fmt.Errorf("fetch UID %d from %q: %w", uid, folder, err)
		}
		if len(msgs) == 0 {
			return finalError{fmt.Errorf("UID %d in %q: %w", uid, folder, mail.ErrMessageNotFound)}
		}
		m := msgs[0]
		parsed, err := render.Parse(bytes.NewReader(m.FindBodySection(wholeMessage)))
		if err != nil {
			return finalError{fmt.Errorf("UID %d in %q: %w", uid, folder, err)}
		}
		parsed.UID = uid
		if parsed.Date.IsZero() {
			parsed.Date = m.InternalDate
		}
		parsed.Seen, parsed.Flagged = readFlags(m.Flags)
		out = parsed
		return nil
	})
	if err != nil {
		return mail.Message{}, err
	}
	return out, nil
}
