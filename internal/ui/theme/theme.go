// Package theme holds every colour the UI draws with. The defaults use the
// terminal's 16-colour palette, so dk-email follows the terminal theme,
// light or dark. config.Colors overrides single roles.
package theme

import (
	"image/color"
	"strings"

	gansi "charm.land/glamour/v2/ansi"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/dkouvdis/dk-email/internal/config"
)

// Theme is the styles for each colour role. Components derive their own
// styles from these and never pick a colour themselves.
type Theme struct {
	// Accent marks focus: focused borders, the open folder, scroll thumbs.
	Accent lipgloss.Style
	// Cursor is the selected row: on_accent text on the accent colour.
	Cursor lipgloss.Style
	// Muted is secondary text. By default it's the terminal's faint style.
	Muted  lipgloss.Style
	Unread lipgloss.Style
	Star   lipgloss.Style
	Error  lipgloss.Style
	Hint   lipgloss.Style
	Key    lipgloss.Style

	AccentColor, BorderColor color.Color

	// Markdown styles message bodies converted from HTML. Like the rest of
	// the theme it sets no body colour, so text follows the terminal.
	Markdown gansi.StyleConfig
}

// defaults are ANSI 0-15 numbers, which the terminal maps to its own theme.
var defaults = config.Colors{
	Accent:   "5", // magenta
	OnAccent: "0", // black
	Border:   "8", // bright black
	Unread:   "4", // blue
	Star:     "3", // yellow
	Error:    "1", // red
	Hint:     "6", // cyan
	Key:      "5", // magenta
}

// Default is the theme with no overrides.
func Default() Theme { return New(config.Colors{}) }

// New applies the overrides in c on top of the defaults. c must have passed
// config validation.
func New(c config.Colors) Theme {
	pick := func(override, def config.Color) color.Color {
		if override != "" {
			return lipgloss.Color(string(override))
		}
		return lipgloss.Color(string(def))
	}
	fg := func(override, def config.Color) lipgloss.Style {
		return lipgloss.NewStyle().Foreground(pick(override, def))
	}

	accent := pick(c.Accent, defaults.Accent)
	muted := lipgloss.NewStyle().Faint(true)
	if c.Muted != "" {
		muted = fg(c.Muted, "")
	}
	str := func(override, def config.Color) *string {
		if override != "" {
			return new(string(override))
		}
		return new(string(def))
	}
	return Theme{
		Accent:      lipgloss.NewStyle().Foreground(accent),
		Cursor:      lipgloss.NewStyle().Foreground(pick(c.OnAccent, defaults.OnAccent)).Background(accent),
		Muted:       muted,
		Unread:      fg(c.Unread, defaults.Unread),
		Star:        fg(c.Star, defaults.Star),
		Error:       fg(c.Error, defaults.Error),
		Hint:        fg(c.Hint, defaults.Hint),
		Key:         fg(c.Key, defaults.Key),
		AccentColor: accent,
		BorderColor: pick(c.Border, defaults.Border),
		Markdown:    markdown(str(c.Accent, defaults.Accent), str(c.Hint, defaults.Hint), str(c.Border, defaults.Border), c.Muted),
	}
}

// Dim redraws block in the Muted style only, dropping every other style
// (bold, colours, the selected row), so it recedes behind what has focus:
// the screen behind a popup, or the sidebar's inactive accounts.
func (t Theme) Dim(block string) string {
	lines := strings.Split(ansi.Strip(block), "\n")
	for i, l := range lines {
		lines[i] = t.Muted.Render(l)
	}
	return strings.Join(lines, "\n")
}

func markdown(accent, hint, border *string, muted config.Color) gansi.StyleConfig {
	t := new(true)
	faint := gansi.StylePrimitive{Faint: t}
	if muted != "" {
		faint = gansi.StylePrimitive{Color: new(string(muted))}
	}
	heading := gansi.StyleBlock{StylePrimitive: gansi.StylePrimitive{Color: accent, Bold: t}}
	// Links show only their text; bare URLs (Link) look the same as link text.
	link := gansi.StylePrimitive{Color: hint, Underline: t}
	return gansi.StyleConfig{
		Document:       gansi.StyleBlock{Margin: new(uint(0))},
		Paragraph:      gansi.StyleBlock{},
		BlockQuote:     gansi.StyleBlock{StylePrimitive: faint, Indent: new(uint(1)), IndentToken: new("│ ")},
		List:           gansi.StyleList{LevelIndent: 2},
		Heading:        gansi.StyleBlock{StylePrimitive: gansi.StylePrimitive{Color: accent, Bold: t, BlockSuffix: "\n"}},
		H1:             heading,
		H2:             heading,
		H3:             heading,
		H4:             heading,
		H5:             heading,
		H6:             heading,
		Strong:         gansi.StylePrimitive{Bold: t},
		Emph:           gansi.StylePrimitive{Italic: t},
		Strikethrough:  gansi.StylePrimitive{CrossedOut: t},
		HorizontalRule: gansi.StylePrimitive{Color: border, Format: "\n────────\n"},
		Item:           gansi.StylePrimitive{BlockPrefix: "• "},
		Enumeration:    gansi.StylePrimitive{BlockPrefix: ". "},
		Link:           link,
		LinkText:       link,
		ImageText:      gansi.StylePrimitive{Format: "{{.text}}"},
		Code:           gansi.StyleBlock{StylePrimitive: gansi.StylePrimitive{Color: hint}},
		CodeBlock:      gansi.StyleCodeBlock{StyleBlock: gansi.StyleBlock{StylePrimitive: faint, Margin: new(uint(0))}},
		Table:          gansi.StyleTable{},
	}
}
