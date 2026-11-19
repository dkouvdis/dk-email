package imap

import (
	"context"
	"fmt"

	goimap "github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"

	"github.com/dkouvdis/dk-email/internal/mail"
)

// Search returns up to limit messages matching every term of q with UIDs
// below before (0 means the newest), newest first, plus how many messages
// match below before. Like Summaries, the folder is opened read-only and
// paging fails with mail.ErrFolderChanged after a UIDVALIDITY change.
func (c *Client) Search(ctx context.Context, folder string, q mail.SearchQuery, before mail.UID, limit int) ([]mail.Summary, int, error) {
	if limit <= 0 || before == 1 {
		return nil, 0, nil
	}
	var out []mail.Summary
	var total int
	err := c.do(ctx, "Search", func(conn *imapclient.Client) error {
		if err := c.openLocked(conn, folder, readOnly, before != 0); err != nil {
			return err
		}
		uids, n, err := newestUIDs(conn, searchCriteria(q), before, limit)
		if err != nil {
			return fmt.Errorf("search %q: %w", folder, err)
		}
		total = n
		out, err = fetchSummaries(conn, uids)
		if err != nil {
			return fmt.Errorf("fetch from %q: %w", folder, err)
		}
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

// UnreadFrom counts the unread messages in folder whose UID is uid or higher.
// It opens the folder read-only like Search and fails with
// mail.ErrFolderChanged after a UIDVALIDITY change. Messages flagged \Seen or
// \Deleted are not counted.
func (c *Client) UnreadFrom(ctx context.Context, folder string, uid mail.UID) (int, error) {
	var n int
	err := c.do(ctx, "UnreadFrom", func(conn *imapclient.Client) error {
		if err := c.openLocked(conn, folder, readOnly, true); err != nil {
			return err
		}
		var set goimap.UIDSet
		set.AddRange(goimap.UID(uid), 0) // uid:*
		criteria := &goimap.SearchCriteria{
			UID:     []goimap.UIDSet{set},
			NotFlag: []goimap.Flag{goimap.FlagDeleted, goimap.FlagSeen},
		}
		data, err := conn.UIDSearch(criteria, nil).Wait()
		if err != nil {
			return fmt.Errorf("count %q: %w", folder, err)
		}
		// uid:* always matches the last message, even below uid.
		n = 0
		for _, u := range data.AllUIDs() {
			if mail.UID(u) >= uid {
				n++
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return n, nil
}

// searchCriteria compiles q into IMAP SEARCH keys, ANDing every term.
// Terms of unknown kind are skipped.
func searchCriteria(q mail.SearchQuery) *goimap.SearchCriteria {
	criteria := &goimap.SearchCriteria{}
	header := func(key, value string) goimap.SearchCriteria {
		return goimap.SearchCriteria{Header: []goimap.SearchCriteriaHeaderField{{Key: key, Value: value}}}
	}
	for _, t := range q.Terms {
		var c goimap.SearchCriteria
		switch t.Kind {
		case mail.SearchText:
			c.Text = []string{t.Value}
		case mail.SearchBody:
			c.Body = []string{t.Value}
		case mail.SearchFrom:
			c = header("From", t.Value)
		case mail.SearchTo:
			c = header("To", t.Value)
		case mail.SearchCc:
			c = header("Cc", t.Value)
		case mail.SearchSubject:
			c = header("Subject", t.Value)
		case mail.SearchUnread:
			c.NotFlag = []goimap.Flag{goimap.FlagSeen}
		case mail.SearchRead:
			c.Flag = []goimap.Flag{goimap.FlagSeen}
		case mail.SearchStarred:
			c.Flag = []goimap.Flag{goimap.FlagFlagged}
		case mail.SearchSince:
			c.Since = t.Date
		case mail.SearchBefore:
			c.Before = t.Date
		default:
			continue
		}
		if t.Not {
			c = goimap.SearchCriteria{Not: []goimap.SearchCriteria{c}}
		}
		criteria.And(&c)
	}
	return criteria
}
