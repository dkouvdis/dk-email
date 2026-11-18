package ui

import (
	"net/url"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/dkouvdis/dk-email/internal/mail"
)

// droppedMsg is the result of reading every file a paste dropped.
type droppedMsg struct{ files []mail.File }

// dropFallbackMsg returns a paste that was not a readable drop, so it can be
// inserted as the text it is.
type dropFallbackMsg struct{ paste tea.PasteMsg }

// readDropped runs in a tea.Cmd and reads every dropped path. If any of them
// cannot be read the paste was text after all, and comes back as such.
func readDropped(paths []string, paste tea.PasteMsg) tea.Cmd {
	return func() tea.Msg {
		files := make([]mail.File, 0, len(paths))
		for _, p := range paths {
			f, err := readFile(p)
			if err != nil {
				return dropFallbackMsg{paste: paste}
			}
			files = append(files, f)
		}
		return droppedMsg{files: files}
	}
}

// parseDropped returns the paths in text when text is nothing but one or more
// file references as terminals paste them on a drop: shell-escaped or quoted
// paths, or file:// URLs, separated by whitespace. Anything else is not a drop.
func parseDropped(text, goos string) ([]string, bool) {
	tokens, ok := dropTokens(strings.TrimSpace(text), goos == "windows")
	if !ok || len(tokens) == 0 {
		return nil, false
	}
	paths := make([]string, 0, len(tokens))
	for _, t := range tokens {
		p, ok := dropPath(t, goos == "windows")
		if !ok {
			return nil, false
		}
		paths = append(paths, p)
	}
	return paths, true
}

// dropTokens splits text the way a shell would: quotes group, a backslash
// escapes (except on Windows, where it is a path separator).
func dropTokens(text string, windows bool) ([]string, bool) {
	var tokens []string
	var cur strings.Builder
	in := false // a token is under way, even if empty ('')
	flush := func() {
		if in {
			tokens = append(tokens, cur.String())
			cur.Reset()
			in = false
		}
	}
	rs := []rune(text)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch {
		case r == ' ' || r == '\t' || r == '\r' || r == '\n':
			flush()
		case r == '\'':
			in = true
			j := i + 1
			for j < len(rs) && rs[j] != '\'' {
				cur.WriteRune(rs[j])
				j++
			}
			if j == len(rs) {
				return nil, false
			}
			i = j
		case r == '"':
			in = true
			j := i + 1
			for j < len(rs) && rs[j] != '"' {
				if rs[j] == '\\' && !windows && j+1 < len(rs) && (rs[j+1] == '"' || rs[j+1] == '\\') {
					j++
				}
				cur.WriteRune(rs[j])
				j++
			}
			if j == len(rs) {
				return nil, false
			}
			i = j
		case r == '\\' && !windows:
			in = true
			if i+1 < len(rs) {
				i++
				cur.WriteRune(rs[i])
			}
		default:
			in = true
			cur.WriteRune(r)
		}
	}
	flush()
	return tokens, true
}

// dropPath is the file path token names, if it names one.
func dropPath(token string, windows bool) (string, bool) {
	switch {
	case strings.HasPrefix(token, "file://"):
		u, err := url.Parse(token)
		if err != nil || (u.Host != "" && u.Host != "localhost") || u.Path == "" {
			return "", false
		}
		p := u.Path
		if windows && len(p) >= 3 && p[0] == '/' && p[2] == ':' {
			p = p[1:]
		}
		return p, true
	case strings.HasPrefix(token, "/"), strings.HasPrefix(token, "~/"):
		return token, true
	case windows && (isDrivePath(token) || strings.HasPrefix(token, `\\`)):
		return token, true
	}
	return "", false
}

// isDrivePath reports whether p starts with X:\ or X:/.
func isDrivePath(p string) bool {
	if len(p) < 3 || p[1] != ':' || (p[2] != '\\' && p[2] != '/') {
		return false
	}
	c := p[0] | 0x20
	return c >= 'a' && c <= 'z'
}
