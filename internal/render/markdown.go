package render

import (
	"regexp"
	"strings"

	"charm.land/glamour/v2"
	gansi "charm.land/glamour/v2/ansi"
	"github.com/charmbracelet/x/ansi"
)

// Markdown renders md, the body of a message converted from HTML, as styled
// terminal text no wider than width. md must already be sanitized: the
// only escape sequences in the result are the ones the style adds.
//
// Links show only their text, wrapped in an OSC 8 hyperlink so the terminal
// can open it (cmd/ctrl+click); bare URLs show the URL itself.
func Markdown(md string, width int, style gansi.StyleConfig) (string, error) {
	md = strings.ReplaceAll(md, hrefMark, "")
	style.Link.Format = "{{" + hrefFunc + " .text}}"
	r, err := glamour.NewTermRenderer(
		glamour.WithStyles(style),
		glamour.WithWordWrap(width),
		// Single newlines come from <br>; tidy trims the markdown hard-break spaces.
		glamour.WithPreservedNewLines(),
	)
	if err != nil {
		return "", err
	}
	out, err := r.Render(md)
	if err != nil {
		return "", err
	}
	lines := strings.Split(hideHrefs(out), "\n")
	for i, l := range lines {
		if strings.TrimSpace(ansi.Strip(l)) == "" {
			lines[i] = ""
		}
	}
	out = blankRuns.ReplaceAllString(strings.Join(lines, "\n"), "\n\n")
	// Glamour doesn't break words longer than the line, such as URLs.
	return Wrap(strings.Trim(out, "\n"), width), nil
}

// Glamour renders a link as its text then " " and the URL, and a bare URL as
// the URL alone, each part in the same OSC 8 hyperlink. It has no option to
// hide the URL part, so the Link style's Format calls hrefFunc to replace
// the visible URL with hrefMark (keeping word wrap close), and hideHrefs
// then drops the part after link text or puts the URL back for bare URLs.
const (
	hrefFunc = "dkEmailHrefMark"
	hrefMark = "\uE000" // private use; stripped from input
)

func init() {
	gansi.TemplateFuncMap[hrefFunc] = func(token string) string {
		end := strings.IndexByte(token, '\a')
		if !strings.HasPrefix(token, "\x1b]8;") || end < 0 {
			return token
		}
		return token[:end+1] + hrefMark + ansi.ResetHyperlink()
	}
}

// hyperlink matches one OSC 8 hyperlink: params, URL, then the visible part.
var hyperlink = regexp.MustCompile(`(?s)\x1b\]8;([^;\a]*);([^\a]*)\a(.*?)\x1b\]8;;\a`)

func hideHrefs(s string) string {
	var b strings.Builder
	last, prevEnd, prevOpen := 0, -1, ""
	for _, m := range hyperlink.FindAllStringSubmatchIndex(s, -1) {
		open, url, text := s[m[2]:m[5]], s[m[4]:m[5]], s[m[6]:m[7]]
		if ansi.Strip(text) != hrefMark {
			prevEnd, prevOpen = m[1], open
			continue
		}
		gap := ansi.Strip(s[max(prevEnd, 0):m[0]])
		switch {
		case prevEnd < 0 || open != prevOpen || strings.TrimSpace(gap) != "":
			// A bare URL: show it.
			b.WriteString(s[last:m[6]])
			b.WriteString(strings.Replace(text, hrefMark, url, 1))
			last = m[7]
		case !strings.Contains(gap, "\n"):
			b.WriteString(s[last:prevEnd]) // drop the separating space too
			last = m[1]
		default:
			b.WriteString(s[last:m[0]])
			last = m[1]
		}
		prevEnd, prevOpen = -1, ""
	}
	b.WriteString(s[last:])
	return b.String()
}
