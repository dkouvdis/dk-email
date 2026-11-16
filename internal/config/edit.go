package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"reflect"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
)

// ReplaceAccount rewrites the index-th [[account]] block (the position Load
// returns it at) with update applied to the account as written in the file:
// no defaults, tls_cert as typed. Everything outside the block is kept byte
// for byte; comments inside it are lost. oldName must match the account
// found at index, otherwise the file changed since it was loaded and nothing
// is written. The result is reparsed and every other account must be
// unchanged, so a bad edit leaves the file untouched.
func ReplaceAccount(path string, index int, oldName string, update func(Account) Account) error {
	return editAccount(path, "edit", index, oldName, func(raw Account, eol string) (string, bool) {
		return strings.ReplaceAll(accountBlock(update(raw)), "\n", eol), false
	})
}

// RemoveAccount deletes the index-th [[account]] block, with the blank lines
// that follow it. The checks and the write are the same as ReplaceAccount's.
func RemoveAccount(path string, index int, oldName string) error {
	return editAccount(path, "remove", index, oldName, func(Account, string) (string, bool) {
		return "", true
	})
}

var (
	// tableHeader matches a [table] or [[array]] header line.
	tableHeader = regexp.MustCompile(`^\[\[?\s*[A-Za-z0-9_."' -]+\s*\]\]?\s*(#.*)?$`)
	accountHdr  = regexp.MustCompile(`^\[\[\s*account\s*\]\]\s*(#.*)?$`)
)

// editAccount finds the account block and splices in what edit returns for
// it. When remove is true the blank lines after the block are dropped too.
func editAccount(path, verb string, index int, oldName string, edit func(raw Account, eol string) (block string, remove bool)) error {
	target, err := resolveSymlink(path)
	if err != nil {
		return fmt.Errorf("%s account: %w", verb, err)
	}
	original, err := os.ReadFile(target)
	if err != nil {
		return fmt.Errorf("%s account: %w", verb, err)
	}
	stale := fmt.Errorf("config changed on disk: account %q not found", oldName)

	var raw struct {
		Accounts []Account `toml:"account"`
	}
	if _, err := toml.Decode(string(original), &raw); err != nil {
		return fmt.Errorf("%s account: parse %s: %w", verb, path, err)
	}
	if index < 0 || index >= len(raw.Accounts) || raw.Accounts[index].Name != oldName {
		return stale
	}
	before, err := parse(path, original)
	if err != nil {
		return fmt.Errorf("%s account %q: %w", verb, oldName, err)
	}

	eol := "\n"
	if bytes.Contains(original, []byte("\r\n")) {
		eol = "\r\n"
	}
	lines := strings.SplitAfter(string(original), "\n")
	start, end := accountSpan(lines, index)
	if start < 0 {
		return stale
	}
	block, remove := edit(raw.Accounts[index], eol)
	rest := lines[end:]
	prefix := lines[:start]
	if remove {
		for len(rest) > 0 && strings.TrimSpace(rest[0]) == "" {
			rest = rest[1:]
		}
		if len(rest) == 0 {
			// Nothing follows: don't leave the blank gap that preceded it.
			for len(prefix) > 0 && strings.TrimSpace(prefix[len(prefix)-1]) == "" {
				prefix = prefix[:len(prefix)-1]
			}
		}
	}
	updated := strings.Join(prefix, "") + block + strings.Join(rest, "")

	after, err := parse(path, []byte(updated))
	if err != nil {
		return fmt.Errorf("%s account %q: %w", verb, oldName, err)
	}
	want := append([]Account(nil), before.Accounts...)
	if remove {
		want = append(want[:index], want[index+1:]...)
	} else if index < len(after.Accounts) {
		want[index] = after.Accounts[index]
	}
	if len(want) != len(after.Accounts) || len(want) > 0 && !reflect.DeepEqual(want, after.Accounts) {
		return errors.New(verb + " account: other accounts would change, nothing written")
	}

	mode := fs.FileMode(0o600)
	if fi, err := os.Stat(target); err == nil {
		mode = fi.Mode().Perm()
	}
	if err := writeAtomic(target, []byte(updated), mode); err != nil {
		return fmt.Errorf("%s account %q: %w", verb, oldName, err)
	}
	return nil
}

// accountSpan returns the line range [start, end) of the index-th [[account]]
// block: from its header to the last non-blank, non-comment line before the
// next table header. Blank and comment lines right above that header stay
// outside, with it. It returns -1, -1 if there is no such block.
func accountSpan(lines []string, index int) (int, int) {
	n := -1
	start := -1
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if !tableHeader.MatchString(t) {
			continue
		}
		if start >= 0 {
			return start, blockEnd(lines, start, i)
		}
		if accountHdr.MatchString(t) {
			if n++; n == index {
				start = i
			}
		}
	}
	if start < 0 {
		return -1, -1
	}
	return start, blockEnd(lines, start, len(lines))
}

// blockEnd trims trailing blank and comment lines off lines[start:limit].
func blockEnd(lines []string, start, limit int) int {
	end := limit
	for end > start+1 {
		t := strings.TrimSpace(lines[end-1])
		if t != "" && !strings.HasPrefix(t, "#") {
			break
		}
		end--
	}
	return end
}
