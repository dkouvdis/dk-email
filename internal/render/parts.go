package render

import (
	"bytes"
	"io"
	"mime"
	"strings"
)

// Part is one attachment of a message with its decoded content.
type Part struct {
	Filename string // as Parse reports it: decoded, sanitised, "(unnamed)" if none
	// ContentType is the media type, e.g. "application/pdf". Parse lists the
	// same type. Text parts also carry a charset: "utf-8" because their Data
	// is converted to UTF-8 (or was already ASCII/UTF-8, or declared none),
	// except when the declared charset is unknown and so cannot be
	// converted; then Data keeps the original bytes and ContentType the
	// original charset parameter, e.g. "text/plain; charset=x-unknown".
	ContentType string
	Data        []byte // the part's body with its transfer encoding (and, for text with a known charset, that charset) removed
}

// Parts returns the attachments of the raw message in the order Parse lists
// them, applying the same attachment rules: each Part matches the Attachment
// at the same index in Parse's result, and len(Data) is its Size. Like Parse,
// only unreadable top-level headers are an error; a truncated part keeps the
// bytes read before the damage.
func Parts(raw []byte) ([]Part, error) {
	e, rootUnknownCharset, err := readEntity(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	var parts []Part
	walkLeaves(e, rootUnknownCharset, func(l leaf) {
		if !l.attachment {
			return
		}
		data, _ := io.ReadAll(l.body)
		ct := l.mediaType
		if strings.HasPrefix(ct, "text/") {
			charset := "utf-8"
			if l.charsetKept && l.charset != "" {
				charset = l.charset
			}
			ct = mime.FormatMediaType(ct, map[string]string{"charset": charset})
		}
		parts = append(parts, Part{Filename: l.name, ContentType: ct, Data: data})
	})
	return parts, nil
}
