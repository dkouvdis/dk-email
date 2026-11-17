package search

import (
	"reflect"
	"testing"
	"time"

	"github.com/dkouvdis/dk-email/internal/mail"
)

func day(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func TestParse(t *testing.T) {
	text := func(v string) mail.SearchTerm { return mail.SearchTerm{Kind: mail.SearchText, Value: v} }
	notText := func(v string) mail.SearchTerm {
		return mail.SearchTerm{Kind: mail.SearchText, Value: v, Not: true}
	}
	tests := []struct {
		name  string
		input string
		raw   string
		terms []mail.SearchTerm
	}{
		{"empty", "", "", nil},
		{"blank", "  \t ", "", nil},
		{"bare words", "hello world", "hello world", []mail.SearchTerm{text("hello"), text("world")}},
		{"phrase", `"hello world"`, `"hello world"`, []mail.SearchTerm{text("hello world")}},
		{"from", "from:bob", "from:bob", []mail.SearchTerm{{Kind: mail.SearchFrom, Value: "bob"}}},
		{"to", "to:bob", "to:bob", []mail.SearchTerm{{Kind: mail.SearchTo, Value: "bob"}}},
		{"cc", "cc:bob", "cc:bob", []mail.SearchTerm{{Kind: mail.SearchCc, Value: "bob"}}},
		{"subject", "subject:report", "subject:report", []mail.SearchTerm{{Kind: mail.SearchSubject, Value: "report"}}},
		{"body", "body:invoice", "body:invoice", []mail.SearchTerm{{Kind: mail.SearchBody, Value: "invoice"}}},
		{"case-insensitive key", "FROM:bob Subject:x", "FROM:bob Subject:x", []mail.SearchTerm{
			{Kind: mail.SearchFrom, Value: "bob"}, {Kind: mail.SearchSubject, Value: "x"}}},
		{"quoted field value", `from:"Jane Doe"`, `from:"Jane Doe"`, []mail.SearchTerm{{Kind: mail.SearchFrom, Value: "Jane Doe"}}},
		{"is unread", "is:unread", "is:unread", []mail.SearchTerm{{Kind: mail.SearchUnread}}},
		{"is read", "is:read", "is:read", []mail.SearchTerm{{Kind: mail.SearchRead}}},
		{"is starred", "is:starred", "is:starred", []mail.SearchTerm{{Kind: mail.SearchStarred}}},
		{"is flagged", "IS:Flagged", "IS:Flagged", []mail.SearchTerm{{Kind: mail.SearchStarred}}},
		{"since", "since:2026-03-04", "since:2026-03-04", []mail.SearchTerm{{Kind: mail.SearchSince, Date: day(2026, 3, 4)}}},
		{"after", "after:2026-03-04", "after:2026-03-04", []mail.SearchTerm{{Kind: mail.SearchSince, Date: day(2026, 3, 4)}}},
		{"since slashes", "since:2026/03/04", "since:2026/03/04", []mail.SearchTerm{{Kind: mail.SearchSince, Date: day(2026, 3, 4)}}},
		{"before slashes", "before:2026/03/04", "before:2026/03/04", []mail.SearchTerm{{Kind: mail.SearchBefore, Date: day(2026, 3, 4)}}},
		{"before", "before:2026-03-04", "before:2026-03-04", []mail.SearchTerm{{Kind: mail.SearchBefore, Date: day(2026, 3, 4)}}},
		{"negated word", "-spam", "-spam", []mail.SearchTerm{notText("spam")}},
		{"negated phrase", `-"a b"`, `-"a b"`, []mail.SearchTerm{notText("a b")}},
		{"negated prefix", "-from:bob", "-from:bob", []mail.SearchTerm{{Kind: mail.SearchFrom, Value: "bob", Not: true}}},
		{"negated is", "-is:unread", "-is:unread", []mail.SearchTerm{{Kind: mail.SearchUnread, Not: true}}},
		{"lone dash", "a - b", "a - b", []mail.SearchTerm{text("a"), text("b")}},
		{"unknown key", "foo:bar", "foo:bar", []mail.SearchTerm{text("foo:bar")}},
		{"url", "http://x", "http://x", []mail.SearchTerm{text("http://x")}},
		{"negated unknown key", "-foo:bar", "-foo:bar", []mail.SearchTerm{notText("foo:bar")}},
		{"empty value", "from:", "from:", []mail.SearchTerm{text("from:")}},
		{"unknown is", "is:weird", "is:weird", []mail.SearchTerm{text("is:weird")}},
		{"malformed date word", "before:yesterday", "before:yesterday", []mail.SearchTerm{text("before:yesterday")}},
		{"malformed date month", "since:2026-13-01", "since:2026-13-01", []mail.SearchTerm{text("since:2026-13-01")}},
		{"quoted field is literal", `"from:bob"`, `"from:bob"`, []mail.SearchTerm{text("from:bob")}},
		{"quoted dash is literal", `"-x"`, `"-x"`, []mail.SearchTerm{text("-x")}},
		{"negated unclosed field", `-subject:"weekly rep`, `-subject:"weekly rep`, []mail.SearchTerm{{Kind: mail.SearchSubject, Value: "weekly rep", Not: true}}},
		{"unclosed phrase", `"a b`, `"a b`, []mail.SearchTerm{text("a b")}},
		{"unclosed quote", `"hello world`, `"hello world`, []mail.SearchTerm{text("hello world")}},
		{"unclosed quote in field", `subject:"weekly rep`, `subject:"weekly rep`, []mail.SearchTerm{{Kind: mail.SearchSubject, Value: "weekly rep"}}},
		{"raw trimmed", "  from:bob  ", "from:bob", []mail.SearchTerm{{Kind: mail.SearchFrom, Value: "bob"}}},
		{"term order", `is:unread "a b" -to:x since:2026-01-02 word`, `is:unread "a b" -to:x since:2026-01-02 word`, []mail.SearchTerm{
			{Kind: mail.SearchUnread},
			text("a b"),
			{Kind: mail.SearchTo, Value: "x", Not: true},
			{Kind: mail.SearchSince, Date: day(2026, 1, 2)},
			text("word"),
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Parse(tt.input)
			want := mail.SearchQuery{Raw: tt.raw, Terms: tt.terms}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("Parse(%q) = %+v, want %+v", tt.input, got, want)
			}
		})
	}
}
