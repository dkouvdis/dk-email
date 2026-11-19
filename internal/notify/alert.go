package notify

import (
	"strconv"
	"strings"
	"unicode"
)

// Count is one account's new mail in an announcement.
type Count struct {
	Account string
	New     int
}

// Text is the announcement for counts, in order: "dk-email: 3 new in Work"
// or "dk-email: 3 new in Work, 1 in Personal", or "" when there is no new mail.
func Text(counts []Count) string {
	s := Summary(counts)
	if s == "" {
		return ""
	}
	return "dk-email: " + s
}

// Summary is Text without the "dk-email: " prefix. It never names a sender or
// subject. Control characters in account names are dropped, since names come
// from config and the text goes into an escape sequence.
func Summary(counts []Count) string {
	var parts []string
	for _, c := range counts {
		if c.New <= 0 {
			continue
		}
		name := strings.Map(func(r rune) rune {
			if unicode.IsControl(r) {
				return -1
			}
			return r
		}, c.Account)
		lead := ""
		if len(parts) == 0 {
			lead = "new "
		}
		parts = append(parts, strconv.Itoa(c.New)+" "+lead+"in "+name)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, ", ")
}

// Sequence is what to write to the terminal: an OSC 9 desktop notification
// carrying text when desktop is set, then a bell when bell is set. Under tmux
// the OSC 9 is wrapped for passthrough (tmux handles the bell itself).
func Sequence(text string, bell, desktop, tmux bool) string {
	var out string
	if desktop {
		// text always starts with "dk-email:", so it can't be mistaken for OSC 9;4 progress.
		osc := "\x1b]9;" + text + "\x07"
		if tmux {
			osc = "\x1bPtmux;" + strings.ReplaceAll(osc, "\x1b", "\x1b\x1b") + "\x1b\\"
		}
		out = osc
	}
	if bell {
		out += "\a"
	}
	return out
}
