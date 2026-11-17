package imap

import (
	"fmt"
	"slices"
	"testing"
	"time"

	goimap "github.com/emersion/go-imap/v2"

	"github.com/dkouvdis/dk-email/internal/mail"
	"github.com/dkouvdis/dk-email/internal/search"
)

func day(d int) time.Time { return time.Date(2026, 11, d, 12, 0, 0, 0, time.UTC) }

func seedSearch(t *testing.T, srv *testServer) {
	t.Helper()
	srv.createMailbox(t, "Find")
	srv.appendRaw(t, "Find", `Message-ID: <s1@example.com>
From: Alice <alice@example.com>
To: bob@example.com
Cc: carol@example.com
Subject: Lunch plans

pizza at noon
`, day(1), goimap.FlagSeen)
	srv.appendRaw(t, "Find", `Message-ID: <s2@example.com>
From: Dave <dave@example.com>
To: alice@example.com
Subject: Invoice

please pay the invoice
`, day(3), goimap.FlagFlagged)
	srv.appendRaw(t, "Find", `Message-ID: <s3@example.com>
From: Erin <erin@example.com>
To: bob@example.com
Subject: Re: Lunch plans

sounds good
`, day(5))
	srv.appendRaw(t, "Find", `Message-ID: <s4@example.com>
From: Frank <frank@example.com>
To: carol@example.com
Subject: Quarterly report

lunch is on me
`, day(7), goimap.FlagSeen, goimap.FlagFlagged)
	srv.appendRaw(t, "Find", `Message-ID: <s5@example.com>
From: Alice <alice@example.com>
To: bob@example.com
Subject: Gone lunch

deleted already
`, day(8), goimap.FlagDeleted)
	srv.appendRaw(t, "Find", `Message-ID: <s6@example.com>
From: Grace <grace@example.com>
To: bob@example.com
Subject: Newsletter

weekly digest
`, day(10))
}

func subjects(sums []mail.Summary) []string {
	var out []string
	for _, s := range sums {
		out = append(out, s.Subject)
	}
	return out
}

func TestSearch_MatchesTerms(t *testing.T) {
	srv := startServer(t, modeImplicit, capsListStatus)
	seedSearch(t, srv)
	c := srv.newClient(t, modeImplicit)
	ctx := timeoutCtx(t, 10*time.Second)

	tests := []struct {
		name  string
		query string
		want  []string
	}{
		{"bare text matches headers and body", "lunch", []string{"Quarterly report", "Re: Lunch plans", "Lunch plans"}},
		{"from", "from:alice", []string{"Lunch plans"}},
		{"to", "to:alice", []string{"Invoice"}},
		{"cc", "cc:carol", []string{"Lunch plans"}},
		{"subject", "subject:lunch", []string{"Re: Lunch plans", "Lunch plans"}},
		{"body", "body:lunch", []string{"Quarterly report"}},
		{"unread", "is:unread", []string{"Newsletter", "Re: Lunch plans", "Invoice"}},
		{"read", "is:read", []string{"Quarterly report", "Lunch plans"}},
		{"starred", "is:starred", []string{"Quarterly report", "Invoice"}},
		{"since", "since:2026-11-05", []string{"Newsletter", "Quarterly report", "Re: Lunch plans"}},
		{"before", "before:2026-11-05", []string{"Invoice", "Lunch plans"}},
		{"negated field", "subject:lunch -from:erin", []string{"Lunch plans"}},
		{"negated flag", "-is:unread", []string{"Quarterly report", "Lunch plans"}},
		{"two terms", "lunch is:unread", []string{"Re: Lunch plans"}},
		{"no matches", "zzzznothing", nil},
		{"deleted never matches", "deleted", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, total, err := c.Search(ctx, "Find", search.Parse(tt.query), 0, 50)

			if err != nil {
				t.Fatalf("Search(%q) error = %v", tt.query, err)
			}
			if !slices.Equal(subjects(got), tt.want) {
				t.Errorf("Search(%q) subjects = %q, want %q", tt.query, subjects(got), tt.want)
			}
			if total != len(tt.want) {
				t.Errorf("Search(%q) total = %d, want %d", tt.query, total, len(tt.want))
			}
			if len(tt.want) == 0 && got != nil {
				t.Errorf("Search(%q) = %v, want nil", tt.query, got)
			}
		})
	}
}

func TestSearch_ReturnsSummaryFields(t *testing.T) {
	srv := startServer(t, modeImplicit, capsListStatus)
	seedSearch(t, srv)
	c := srv.newClient(t, modeImplicit)

	got, _, err := c.Search(timeoutCtx(t, 10*time.Second), "Find", search.Parse("from:alice"), 0, 10)

	if err != nil || len(got) != 1 {
		t.Fatalf("Search() = %v, %v", got, err)
	}
	assertSummary(t, got[0], mail.Summary{
		UID:       got[0].UID,
		From:      mail.Address{Name: "Alice", Email: "alice@example.com"},
		To:        []mail.Address{{Email: "bob@example.com"}},
		Subject:   "Lunch plans",
		Date:      day(1),
		Seen:      true,
		MessageID: "s1@example.com",
	})
}

func TestSearch_PagesNewestFirstWithoutGapsOrDuplicates(t *testing.T) {
	srv := startServer(t, modeImplicit, capsListStatus)
	srv.seedBulk(t, "Bulk", 450)
	c := srv.newClient(t, modeImplicit)
	ctx := timeoutCtx(t, 20*time.Second)
	q := search.Parse("subject:message")

	var all []mail.Summary
	var pages []int
	var firstTotal int
	before := mail.UID(0)
	for {
		page, total, err := c.Search(ctx, "Bulk", q, before, 200)
		if err != nil {
			t.Fatalf("Search(before=%d) error = %v", before, err)
		}
		if len(page) == 0 {
			break
		}
		if len(pages) == 0 {
			firstTotal = total
		}
		pages = append(pages, len(page))
		all = append(all, page...)
		before = page[len(page)-1].UID
	}

	if !slices.Equal(pages, []int{200, 200, 50}) {
		t.Errorf("page sizes = %v, want [200 200 50]", pages)
	}
	if firstTotal != 450 {
		t.Errorf("first page total = %d, want 450", firstTotal)
	}
	if len(all) != 450 {
		t.Fatalf("got %d summaries, want 450", len(all))
	}
	for i, s := range all {
		if want := fmt.Sprintf("message %03d", 450-i); s.Subject != want {
			t.Fatalf("summary %d subject = %q, want %q (newest first, no gaps)", i, s.Subject, want)
		}
	}
}

func TestSearch_TotalCountsOnlyBelowBefore(t *testing.T) {
	srv := startServer(t, modeImplicit, capsListStatus)
	srv.seedBulk(t, "Bulk", 10)
	c := srv.newClient(t, modeImplicit)

	got, total, err := c.Search(timeoutCtx(t, 10*time.Second), "Bulk", search.Parse("subject:message"), 6, 2)

	if err != nil || len(got) != 2 || total != 5 {
		t.Errorf("Search(before=6, limit=2) = %d results, total %d, %v; want 2, 5", len(got), total, err)
	}
}

func TestSearch_EmptyArguments(t *testing.T) {
	srv := startServer(t, modeImplicit, capsListStatus)
	c := srv.newClient(t, modeImplicit)
	ctx := timeoutCtx(t, 10*time.Second)

	for name, args := range map[string]struct {
		before mail.UID
		limit  int
	}{"zero limit": {0, 0}, "nothing below UID 1": {1, 10}} {
		got, total, err := c.Search(ctx, "INBOX", search.Parse("x"), args.before, args.limit)
		if got != nil || total != 0 || err != nil {
			t.Errorf("%s: Search() = %v, %d, %v; want nil, 0, nil", name, got, total, err)
		}
	}
}

func TestSearch_DoesNotChangeFlags(t *testing.T) {
	srv := startServer(t, modeImplicit, capsListStatus)
	c := srv.newClient(t, modeImplicit)

	got, total, err := c.Search(timeoutCtx(t, 10*time.Second), "INBOX", search.Parse("is:unread"), 0, 10)

	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(got) != 2 || total != 2 {
		t.Errorf("got %d unread, total %d, want 2, 2", len(got), total)
	}
	status, err := srv.user.Status("INBOX", &goimap.StatusOptions{NumUnseen: true})
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if *status.NumUnseen != 2 {
		t.Errorf("server unseen count = %d after Search, want 2 (no implicit \\Seen)", *status.NumUnseen)
	}
}

func TestSearchCriteria_MapsTerms(t *testing.T) {
	d := time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)
	hdr := func(k string) goimap.SearchCriteria {
		return goimap.SearchCriteria{Header: []goimap.SearchCriteriaHeaderField{{Key: k, Value: "x"}}}
	}
	tests := []struct {
		name string
		term mail.SearchTerm
		want goimap.SearchCriteria
	}{
		{"text", mail.SearchTerm{Kind: mail.SearchText, Value: "x"}, goimap.SearchCriteria{Text: []string{"x"}}},
		{"body", mail.SearchTerm{Kind: mail.SearchBody, Value: "x"}, goimap.SearchCriteria{Body: []string{"x"}}},
		{"from", mail.SearchTerm{Kind: mail.SearchFrom, Value: "x"}, hdr("From")},
		{"to", mail.SearchTerm{Kind: mail.SearchTo, Value: "x"}, hdr("To")},
		{"cc", mail.SearchTerm{Kind: mail.SearchCc, Value: "x"}, hdr("Cc")},
		{"subject", mail.SearchTerm{Kind: mail.SearchSubject, Value: "x"}, hdr("Subject")},
		{"unread", mail.SearchTerm{Kind: mail.SearchUnread}, goimap.SearchCriteria{NotFlag: []goimap.Flag{goimap.FlagSeen}}},
		{"read", mail.SearchTerm{Kind: mail.SearchRead}, goimap.SearchCriteria{Flag: []goimap.Flag{goimap.FlagSeen}}},
		{"starred", mail.SearchTerm{Kind: mail.SearchStarred}, goimap.SearchCriteria{Flag: []goimap.Flag{goimap.FlagFlagged}}},
		{"since", mail.SearchTerm{Kind: mail.SearchSince, Date: d}, goimap.SearchCriteria{Since: d}},
		{"before", mail.SearchTerm{Kind: mail.SearchBefore, Date: d}, goimap.SearchCriteria{Before: d}},
		{"negated", mail.SearchTerm{Kind: mail.SearchFrom, Value: "x", Not: true}, goimap.SearchCriteria{Not: []goimap.SearchCriteria{hdr("From")}}},
		{"negated flag", mail.SearchTerm{Kind: mail.SearchUnread, Not: true}, goimap.SearchCriteria{Not: []goimap.SearchCriteria{
			{NotFlag: []goimap.Flag{goimap.FlagSeen}},
		}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := searchCriteria(mail.SearchQuery{Terms: []mail.SearchTerm{tt.term}})

			if fmt.Sprintf("%+v", *got) != fmt.Sprintf("%+v", tt.want) {
				t.Errorf("searchCriteria = %+v, want %+v", *got, tt.want)
			}
		})
	}
}

func TestSearchCriteria_SkipsUnknownKinds(t *testing.T) {
	got := searchCriteria(mail.SearchQuery{Terms: []mail.SearchTerm{{Kind: mail.SearchKind(99), Value: "x"}}})

	if fmt.Sprintf("%+v", *got) != fmt.Sprintf("%+v", goimap.SearchCriteria{}) {
		t.Errorf("searchCriteria = %+v, want empty", *got)
	}
}
