package imap

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	goimap "github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	gomail "github.com/emersion/go-message/mail"
	"github.com/emersion/go-message/textproto"

	"github.com/dkouvdis/dk-email/internal/mail"
	"github.com/dkouvdis/dk-email/internal/render"
)

// Summaries returns up to limit messages with UIDs below before (0 means
// the newest), newest first. The folder is opened read-only and headers are
// fetched with PEEK, so no flags change. When paging (before != 0) after the
// folder's UIDVALIDITY changed, it returns mail.ErrFolderChanged.
func (c *Client) Summaries(ctx context.Context, folder string, before mail.UID, limit int) ([]mail.Summary, error) {
	if limit <= 0 || before == 1 {
		return nil, nil
	}
	var out []mail.Summary
	err := c.do(ctx, "Summaries", func(conn *imapclient.Client) error {
		if err := c.openLocked(conn, folder, readOnly, before != 0); err != nil {
			return err
		}
		uids, _, err := newestUIDs(conn, &goimap.SearchCriteria{}, before, limit)
		if err != nil {
			return fmt.Errorf("search %q: %w", folder, err)
		}
		out, err = fetchSummaries(conn, uids)
		if err != nil {
			return fmt.Errorf("fetch from %q: %w", folder, err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

const (
	readOnly  = true
	readWrite = false
)

// openLocked selects folder (EXAMINE when readOnly) and records its
// UIDVALIDITY. When the caller passes UIDs it got earlier (checkUIDs), a
// changed UIDVALIDITY is an error because those UIDs are stale.
func (c *Client) openLocked(conn *imapclient.Client, folder string, readOnly, checkUIDs bool) error {
	data, err := conn.Select(folder, &goimap.SelectOptions{ReadOnly: readOnly}).Wait()
	if err != nil {
		var serverErr *goimap.Error
		if errors.As(err, &serverErr) && serverErr.Code == goimap.ResponseCodeNonExistent {
			return fmt.Errorf("open %q: %w: %w", folder, mail.ErrFolderNotFound, err)
		}
		return fmt.Errorf("open %q: %w", folder, err)
	}

	prev, known := c.uidValidity[folder]
	if c.uidValidity == nil {
		c.uidValidity = make(map[string]uint32)
	}
	c.uidValidity[folder] = data.UIDValidity
	if checkUIDs && known && prev != data.UIDValidity {
		return finalError{fmt.Errorf("%q: %w", folder, mail.ErrFolderChanged)}
	}
	return nil
}

// newestUIDs returns the highest limit UIDs below before that match
// criteria, in ascending order, plus how many UIDs matched below before.
// Messages flagged \Deleted are skipped: they're on their way out, e.g.
// moved without UIDPLUS to expunge them. criteria is modified.
func newestUIDs(conn *imapclient.Client, criteria *goimap.SearchCriteria, before mail.UID, limit int) (uids []goimap.UID, total int, err error) {
	extra := goimap.SearchCriteria{NotFlag: []goimap.Flag{goimap.FlagDeleted}}
	if before != 0 {
		var set goimap.UIDSet
		set.AddRange(1, goimap.UID(before-1))
		extra.UID = []goimap.UIDSet{set}
	}
	criteria.And(&extra)
	data, err := conn.UIDSearch(criteria, nil).Wait()
	if err != nil {
		return nil, 0, err
	}
	uids = data.AllUIDs()
	slices.Sort(uids)
	total = len(uids)
	if len(uids) > limit {
		uids = uids[len(uids)-limit:]
	}
	return uids, total, nil
}

var referencesSection = &goimap.FetchItemBodySection{
	Specifier:    goimap.PartSpecifierHeader,
	HeaderFields: []string{"References"},
	Peek:         true,
}

func fetchSummaries(conn *imapclient.Client, uids []goimap.UID) ([]mail.Summary, error) {
	if len(uids) == 0 {
		return nil, nil
	}
	opts := &goimap.FetchOptions{
		UID:          true,
		Flags:        true,
		Envelope:     true,
		InternalDate: true,
		BodySection:  []*goimap.FetchItemBodySection{referencesSection},
	}
	msgs, err := conn.Fetch(goimap.UIDSetNum(uids...), opts).Collect()
	if err != nil {
		return nil, err
	}
	out := make([]mail.Summary, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, newSummary(m))
	}
	slices.SortFunc(out, func(a, b mail.Summary) int { return cmp.Compare(b.UID, a.UID) })
	return out, nil
}

func newSummary(m *imapclient.FetchMessageBuffer) mail.Summary {
	s := mail.Summary{
		UID:        mail.UID(m.UID),
		Date:       m.InternalDate,
		References: parseReferences(m.FindBodySection(referencesSection)),
	}
	s.Seen, s.Flagged = readFlags(m.Flags)
	if env := m.Envelope; env != nil {
		if len(env.From) > 0 {
			s.From = newAddress(env.From[0])
		}
		for _, a := range env.To {
			s.To = append(s.To, newAddress(a))
		}
		s.Subject = render.SanitizeLine(env.Subject)
		if !env.Date.IsZero() {
			s.Date = env.Date
		}
		s.MessageID = env.MessageID
		s.InReplyTo = strings.Join(env.InReplyTo, " ")
	}
	return s
}

// readFlags reports the flags the UI shows: \Seen and \Flagged (starred).
func readFlags(flags []goimap.Flag) (seen, flagged bool) {
	for _, f := range flags {
		switch {
		case strings.EqualFold(string(f), string(goimap.FlagSeen)):
			seen = true
		case strings.EqualFold(string(f), string(goimap.FlagFlagged)):
			flagged = true
		}
	}
	return seen, flagged
}

// newAddress sanitizes as well, since list rows print names directly.
func newAddress(a goimap.Address) mail.Address {
	return mail.Address{Name: render.SanitizeLine(a.Name), Email: render.SanitizeLine(a.Addr())}
}

// parseReferences reads message ids from a fetched References header. A
// malformed header falls back to whatever <...> ids it contains.
func parseReferences(raw []byte) []string {
	if len(raw) == 0 {
		return nil
	}
	h, err := textproto.ReadHeader(bufio.NewReader(bytes.NewReader(raw)))
	if err != nil {
		return nil
	}
	var mh gomail.Header
	mh.Set("References", h.Get("References"))
	if ids, err := mh.MsgIDList("References"); err == nil {
		return ids
	}
	var ids []string
	v := h.Get("References")
	for {
		start := strings.IndexByte(v, '<')
		if start < 0 {
			return ids
		}
		end := strings.IndexByte(v[start:], '>')
		if end < 0 {
			return ids
		}
		if id := strings.TrimSpace(v[start+1 : start+end]); id != "" {
			ids = append(ids, id)
		}
		v = v[start+end+1:]
	}
}
