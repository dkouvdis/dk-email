package thread

import "strings"

// normalizeSubject strips leading reply/forward markers and [list] tags,
// collapses whitespace and lowercases the result so it can be compared.
// isReply reports whether a Re:/Fwd: style marker was removed.
func normalizeSubject(s string) (norm string, isReply bool) {
	s = strings.Join(strings.Fields(s), " ")
	for {
		s = strings.TrimLeft(s, " ")
		if rest, ok := stripListTag(s); ok {
			s = rest
			continue
		}
		if rest, ok := stripReplyPrefix(s); ok {
			s = rest
			isReply = true
			continue
		}
		break
	}
	return strings.ToLower(s), isReply
}

// stripListTag removes a leading "[list-name]" tag.
func stripListTag(s string) (string, bool) {
	if !strings.HasPrefix(s, "[") {
		return s, false
	}
	end := strings.IndexByte(s, ']')
	if end <= 1 {
		return s, false
	}
	// Only strip when something follows; "[urgent]" alone is the subject.
	rest := strings.TrimLeft(s[end+1:], " ")
	if rest == "" {
		return s, false
	}
	return rest, true
}

var replyPrefixes = []string{"re", "fwd", "fw"}

// stripReplyPrefix removes a leading "Re:", "Fwd:", "Fw:" or a counted form
// such as "Re[2]:", case-insensitively.
func stripReplyPrefix(s string) (string, bool) {
	for _, p := range replyPrefixes {
		if len(s) < len(p) || !strings.EqualFold(s[:len(p)], p) {
			continue
		}
		rest := s[len(p):]
		if strings.HasPrefix(rest, "[") {
			end := strings.IndexByte(rest, ']')
			if end < 0 || !allDigits(rest[1:end]) {
				continue
			}
			rest = rest[end+1:]
		}
		rest = strings.TrimLeft(rest, " ")
		if strings.HasPrefix(rest, ":") {
			return rest[1:], true
		}
	}
	return s, false
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
