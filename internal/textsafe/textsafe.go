// Package textsafe neutralises untrusted repository text before it is printed.
// It is pure and imported by report, links and cli, so the three outputs
// cannot drift apart.
package textsafe

import (
	"fmt"
	"strings"
	"unicode"
)

const ellipsis = "..."

// Escape makes s safe for a terminal while keeping it readable: invalid UTF-8
// becomes U+FFFD, control characters below U+0100 become \xNN, other control
// and format characters (and U+2028/U+2029) become \uNNNN. The input is cut
// to limit characters (before escaping) and "..." is appended when cut.
func Escape(s string, limit int) string {
	s = strings.ToValidUTF8(s, "�")
	var b strings.Builder
	count := 0
	for _, r := range s {
		if count == limit {
			b.WriteString(ellipsis)
			break
		}
		count++
		switch {
		case r < 0x100 && unicode.IsControl(r):
			fmt.Fprintf(&b, "\\x%02x", r)
		case unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == ' ' || r == ' ':
			fmt.Fprintf(&b, "\\u%04x", r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Mask is the lossy variant for single-line listings: control and other
// non-printable characters (except the space) become "?", and the text is cut
// to limit characters followed by "...". Invalid UTF-8 bytes are ranged as
// U+FFFD, which is printable and kept.
func Mask(s string, limit int) string {
	var b strings.Builder
	count := 0
	for _, r := range s {
		if count == limit {
			b.WriteString(ellipsis)
			break
		}
		if unicode.IsControl(r) || !unicode.IsPrint(r) && r != ' ' {
			r = '?'
		}
		b.WriteRune(r)
		count++
	}
	return b.String()
}
