package render

import (
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

const tabSpaces = "    "

// Sanitize makes untrusted mail text safe to print in a terminal. It drops
// invalid UTF-8, ANSI escape sequences (CSI, OSC, …), other control
// characters except newlines, and bidi overrides that can disguise text. It
// normalizes line endings to \n and expands tabs.
func Sanitize(s string) string {
	s = strings.ToValidUTF8(s, "")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = strings.ReplaceAll(s, "\t", tabSpaces)
	s = ansi.Strip(s)
	return strings.Map(func(r rune) rune {
		if r == '\n' {
			return r
		}
		if unicode.IsControl(r) || isBidiControl(r) {
			return -1
		}
		return r
	}, s)
}

// SanitizeLine is Sanitize for single-line fields such as subjects and
// names: line breaks become spaces and surrounding space is trimmed.
func SanitizeLine(s string) string {
	s = strings.ReplaceAll(Sanitize(s), "\n", " ")
	return strings.TrimSpace(s)
}

// isBidiControl reports the embedding, override and isolate controls used
// in "right-to-left override" spoofing.
func isBidiControl(r rune) bool {
	return (r >= '\u202a' && r <= '\u202e') || (r >= '\u2066' && r <= '\u2069')
}
