package config

import (
	"fmt"
	"strconv"
)

// Colors overrides the UI's colour roles. An empty value keeps the built-in
// default, which follows the terminal's own 16-colour palette.
type Colors struct {
	Accent   Color `toml:"accent"`
	OnAccent Color `toml:"on_accent"`
	Muted    Color `toml:"muted"`
	Border   Color `toml:"border"`
	Unread   Color `toml:"unread"`
	Star     Color `toml:"star"`
	Error    Color `toml:"error"`
	Hint     Color `toml:"hint"`
	Key      Color `toml:"key"`
}

// Color is an ANSI colour number (0-255) or a hex colour ("#rgb" or
// "#rrggbb"). TOML may give the number bare (5) or quoted ("5").
type Color string

func (c *Color) UnmarshalTOML(v any) error {
	switch v := v.(type) {
	case string:
		*c = Color(v)
	case int64:
		*c = Color(strconv.FormatInt(v, 10))
	default:
		return fmt.Errorf("colour must be a number or a string, got %v", v)
	}
	return nil
}

func (c Color) valid() bool {
	s := string(c)
	if n, err := strconv.Atoi(s); err == nil {
		return n >= 0 && n <= 255 && s == strconv.Itoa(n)
	}
	if len(s) != 4 && len(s) != 7 || s[0] != '#' {
		return false
	}
	_, err := strconv.ParseUint(s[1:], 16, 32)
	return err == nil
}

func (c Colors) validate() []error {
	var errs []error
	for _, r := range []struct {
		key string
		c   Color
	}{
		{"accent", c.Accent}, {"on_accent", c.OnAccent}, {"muted", c.Muted},
		{"border", c.Border}, {"unread", c.Unread}, {"star", c.Star},
		{"error", c.Error}, {"hint", c.Hint}, {"key", c.Key},
	} {
		if r.c != "" && !r.c.valid() {
			errs = append(errs, fmt.Errorf(`colors.%s must be an ANSI colour number 0-255 or a hex colour like "#ff00ff", got %q`, r.key, r.c))
		}
	}
	return errs
}
