package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// AppendAccount adds acct to the config at path as a new [[account]] block
// of text at the end of the file, so comments and formatting are kept
// exactly; the file is never re-encoded. The result is parsed and
// validated before anything is written, so a rejected account (e.g. a
// duplicate name) leaves the file untouched. The write is atomic, keeps
// the file's mode (0600 for a new file) and follows a symlinked path.
func AppendAccount(path string, acct Account) error {
	target, err := resolveSymlink(path)
	if err != nil {
		return fmt.Errorf("add account: %w", err)
	}
	original, err := os.ReadFile(target)
	mode := fs.FileMode(0o600)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return fmt.Errorf("add account: %w", err)
	default:
		if fi, err := os.Stat(target); err == nil {
			mode = fi.Mode().Perm()
		}
	}

	updated := appendBlock(original, accountBlock(acct))
	if _, err := parse(path, updated); err != nil {
		return fmt.Errorf("add account %q: %w", acct.Name, err)
	}
	if err := writeAtomic(target, updated, mode); err != nil {
		return fmt.Errorf("add account %q: %w", acct.Name, err)
	}
	return nil
}

// resolveSymlink returns the file a symlinked path points at, so writing
// updates it instead of replacing the link. Other paths come back as is.
func resolveSymlink(path string) (string, error) {
	fi, err := os.Lstat(path)
	if err != nil || fi.Mode()&fs.ModeSymlink == 0 {
		return path, nil
	}
	return filepath.EvalSymlinks(path)
}

// appendBlock adds block after original, matching its line endings (CRLF if
// it uses them), ending an unterminated last line and leaving one blank
// line before the block.
func appendBlock(original []byte, block string) []byte {
	eol := "\n"
	if bytes.Contains(original, []byte("\r\n")) {
		eol = "\r\n"
	}
	var b bytes.Buffer
	b.Write(original)
	if len(bytes.TrimSpace(original)) > 0 {
		if !bytes.HasSuffix(original, []byte("\n")) {
			b.WriteString(eol)
		}
		b.WriteString(eol)
	}
	b.WriteString(strings.ReplaceAll(block, "\n", eol))
	return b.Bytes()
}

// accountBlock renders acct as TOML, writing optional keys only when set.
func accountBlock(a Account) string {
	var b strings.Builder
	b.WriteString("[[account]]\n")
	str := func(k, v string) {
		if v != "" {
			fmt.Fprintf(&b, "%s = %s\n", k, quote(v))
		}
	}
	str("name", a.Name)
	str("email", a.Email)
	if a.Username != a.Email {
		str("username", a.Username)
	}
	str("imap_host", a.IMAPHost)
	if a.IMAPPort != 0 {
		fmt.Fprintf(&b, "imap_port = %d\n", a.IMAPPort)
	}
	str("tls", a.TLS)
	str("password_cmd", a.PasswordCmd)
	str("archive_folder", a.ArchiveFolder)
	str("tls_cert", a.TLSCert)
	return b.String()
}

// quote returns s as a TOML basic string.
func quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"':
			b.WriteString(`\"`)
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\t':
			b.WriteString(`\t`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\u%04X`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// writeAtomic replaces path with data via a temp file in the same folder,
// so a crash never leaves a half-written config.
func writeAtomic(path string, data []byte, mode fs.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			tmp.Close()
			os.Remove(name)
		}
	}()
	if err := tmp.Chmod(mode); err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	ok = true
	return nil
}
