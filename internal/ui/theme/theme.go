// Package theme holds every colour the UI draws with. The defaults use the
// terminal's 16-colour palette, so dk-email follows the terminal theme,
// light or dark. config.Colors overrides single roles.
package theme

import (
	"image/color"

	"charm.land/lipgloss/v2"

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
	}
}
