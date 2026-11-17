// Package search parses Gmail-style search input into a mail.SearchQuery.
package search

import (
	"strings"
	"time"

	"github.com/dkouvdis/dk-email/internal/mail"
)

const dateLayout = "2006-01-02"

var fieldKinds = map[string]mail.SearchKind{
	"from":    mail.SearchFrom,
	"to":      mail.SearchTo,
	"cc":      mail.SearchCc,
	"subject": mail.SearchSubject,
	"body":    mail.SearchBody,
}

var flagKinds = map[string]mail.SearchKind{
	"unread":  mail.SearchUnread,
	"read":    mail.SearchRead,
	"starred": mail.SearchStarred,
	"flagged": mail.SearchStarred,
}

// Parse splits input into search terms. It never fails: anything it does not
// understand (unknown keys, bad dates, empty values) becomes plain text.
func Parse(input string) mail.SearchQuery {
	q := mail.SearchQuery{Raw: strings.TrimSpace(input)}
	for _, tok := range tokenize(q.Raw) {
		if term, ok := parseToken(tok); ok {
			q.Terms = append(q.Terms, term)
		}
	}
	return q
}

// token is one whitespace-separated piece of input. A phrase began with a
// quote, so it is literal text and never an operator.
type token struct {
	text   string
	not    bool
	phrase bool
}

// tokenize splits on whitespace outside double quotes and strips the quotes
// and one leading unquoted "-". An unclosed quote runs to the end of the input.
func tokenize(s string) []token {
	var toks []token
	var cur strings.Builder
	var tok token
	inQuote, has, n := false, false, 0
	flush := func() {
		if has {
			tok.text = cur.String()
			toks = append(toks, tok)
		}
		cur.Reset()
		tok, inQuote, has, n = token{}, false, false, 0
	}
	for _, r := range s {
		switch {
		case r == '-' && n == 0 && !inQuote:
			tok.not = true
			has = true
			n++
			continue
		case r == '"':
			if n == 0 || (n == 1 && tok.not) {
				tok.phrase = true
			}
			inQuote = !inQuote
		case !inQuote && (r == ' ' || r == '\t' || r == '\n' || r == '\r'):
			flush()
			continue
		default:
			cur.WriteRune(r)
		}
		has = true
		n++
	}
	flush()
	return toks
}

func parseToken(tok token) (mail.SearchTerm, bool) {
	if tok.text == "" {
		return mail.SearchTerm{}, false
	}
	text := mail.SearchTerm{Kind: mail.SearchText, Value: tok.text, Not: tok.not}
	key, val, found := strings.Cut(tok.text, ":")
	if tok.phrase || !found || val == "" {
		return text, true
	}
	not := tok.not
	key = strings.ToLower(key)
	if kind, ok := fieldKinds[key]; ok {
		return mail.SearchTerm{Kind: kind, Value: val, Not: not}, true
	}
	switch key {
	case "is":
		if kind, ok := flagKinds[strings.ToLower(val)]; ok {
			return mail.SearchTerm{Kind: kind, Not: not}, true
		}
	case "since", "after", "before":
		if d, err := time.Parse(dateLayout, val); err == nil {
			kind := mail.SearchSince
			if key == "before" {
				kind = mail.SearchBefore
			}
			return mail.SearchTerm{Kind: kind, Date: d, Not: not}, true
		}
	}
	return text, true
}
