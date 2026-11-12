package popup

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"charm.land/bubbles/v2/key"
)

// HintSep separates hints, in the hint bar and in a popup's own prompt.
const HintSep = " | "

// HintItem formats one hint, description first: "Next field: tab".
func HintItem(b key.Binding) string {
	d := b.Help().Desc
	r, n := utf8.DecodeRuneInString(d)
	return string(unicode.ToUpper(r)) + d[n:] + ": " + b.Help().Key
}

// HintLine joins hints, e.g. "Save: s | Discard: d".
func HintLine(bs []key.Binding) string {
	items := make([]string, len(bs))
	for i, b := range bs {
		items[i] = HintItem(b)
	}
	return strings.Join(items, HintSep)
}
