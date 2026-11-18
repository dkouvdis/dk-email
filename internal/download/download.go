// Package download saves attachments to the user's Downloads folder without
// overwriting anything.
package download

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	maxName     = 200 // bytes, so the "(n)" suffix still fits any filesystem
	maxExt      = 16  // longer "extensions" are part of the stem
	maxVariants = 999
)

// Dir returns the user's Downloads folder.
func Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return dirFor(runtime.GOOS, os.Getenv, home, os.ReadFile), nil
}

// dirFor picks the Downloads folder; the arguments are injected for tests.
func dirFor(goos string, getenv func(string) string, home string, readFile func(string) ([]byte, error)) string {
	fallback := filepath.Join(home, "Downloads")
	switch goos {
	case "darwin", "windows":
		return fallback
	}
	if d := getenv("XDG_DOWNLOAD_DIR"); d != "" && filepath.IsAbs(d) {
		return d
	}
	cfg := getenv("XDG_CONFIG_HOME")
	if cfg == "" || !filepath.IsAbs(cfg) {
		cfg = filepath.Join(home, ".config")
	}
	data, err := readFile(filepath.Join(cfg, "user-dirs.dirs"))
	if err != nil {
		return fallback
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		rest, ok := strings.CutPrefix(line, "XDG_DOWNLOAD_DIR=")
		if !ok { // also skips comments
			continue
		}
		rest = strings.Trim(strings.TrimSpace(rest), `"'`)
		if r, ok := strings.CutPrefix(rest, "$HOME"); ok && (r == "" || r[0] == '/') {
			rest = home + r
		}
		if filepath.IsAbs(rest) {
			return rest
		}
	}
	return fallback
}

// Save writes data into dir under a safe version of name and returns the path.
// A file already there with the same bytes is reused; anything else keeps its
// place and the new file gets "name (1).ext", "name (2).ext", and so on.
func Save(dir, name string, data []byte) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	name = clean(name)
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 0; i <= maxVariants; i++ {
		cand := name
		if i > 0 {
			cand = fmt.Sprintf("%s (%d)%s", stem, i, ext)
		}
		path := filepath.Join(dir, cand)
		if same(path, data) {
			return path, nil
		}
		if _, err := os.Lstat(path); err == nil {
			continue // something else is there
		}
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, fs.ErrExist) {
			continue // lost a race; take the next name
		}
		if err != nil {
			return "", err
		}
		_, werr := f.Write(data)
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		if werr != nil {
			_ = os.Remove(path)
			return "", werr
		}
		return path, nil
	}
	return "", fmt.Errorf("download: too many files named %q in %s", name, dir)
}

// same reports whether path is a regular file holding exactly data.
func same(path string, data []byte) bool {
	fi, err := os.Lstat(path)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() != int64(len(data)) {
		return false
	}
	got, err := os.ReadFile(path)
	return err == nil && bytes.Equal(got, data)
}

// clean turns an attachment's claimed name into a plain, visible file name.
func clean(name string) string {
	name = strings.ReplaceAll(name, `\`, "/")
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	name = strings.Map(func(r rune) rune {
		switch {
		case unicode.IsControl(r), unicode.Is(unicode.Cf, r):
			return -1 // Cf: invisible, e.g. U+202E reversing how the name reads
		case strings.ContainsRune(`<>:"|?*`, r):
			return '_'
		}
		return r
	}, name)
	name = strings.Trim(name, " .") // no hidden files, no trailing dots
	if len(name) > maxName {
		ext := filepath.Ext(name)
		if len(ext) > maxExt {
			ext = ""
		}
		stem := name[:maxName-len(ext)]
		for len(stem) > 0 && !utf8.ValidString(stem) {
			stem = stem[:len(stem)-1]
		}
		name = stem + ext
	}
	if name == "" || name == "(unnamed)" {
		return "attachment"
	}
	return name
}

// runnableExts are the extensions that can run code when opened.
var runnableExts = map[string]bool{
	".app": true, ".applescript": true, ".appimage": true, ".bash": true,
	".bat": true, ".bin": true, ".cmd": true, ".com": true,
	".command": true, ".cpl": true, ".csh": true, ".deb": true,
	".desktop": true, ".exe": true, ".fileloc": true, ".fish": true,
	".hta": true, ".inetloc": true, ".jar": true, ".js": true,
	".jse": true, ".ksh": true, ".lnk": true, ".mpkg": true,
	".msi": true, ".msp": true, ".pif": true, ".pkg": true,
	".pl": true, ".ps1": true, ".psm1": true, ".py": true,
	".rb": true, ".reg": true, ".rpm": true, ".run": true,
	".scpt": true, ".scptd": true, ".scr": true, ".sh": true,
	".terminal": true, ".tool": true, ".url": true, ".vbe": true,
	".vbs": true, ".webloc": true, ".workflow": true, ".wsf": true,
	".wsh": true, ".zsh": true,
}

// Runnable reports whether opening the file named name could run code, judged
// by its final extension (so "invoice.pdf.exe" counts).
func Runnable(name string) bool {
	return runnableExts[strings.ToLower(filepath.Ext(name))]
}
