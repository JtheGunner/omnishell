package config

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// errInvalidUTF8 is returned for a value TOML cannot represent: a TOML file is
// UTF-8 text, so stray bytes have no spelling that reads back as the same value.
var errInvalidUTF8 = errors.New("value is not valid UTF-8")

// quoteTOML renders s as a TOML basic string.
//
// It is not strconv.Quote: that writes Go literals, and Go's escapes are not
// TOML's. Go spells BEL as \a, VT as \v and other control characters as \xHH;
// TOML 1.0 has none of these, so a file holding them no longer loads (or, for
// \xHH, only loads with a parser that also knows TOML 1.1). Everything that is
// valid in both languages is written exactly as strconv.Quote writes it, so
// existing files and diffs do not change; the rest becomes \u00XX.
func quoteTOML(s string) (string, error) {
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('"')
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			return "", errInvalidUTF8
		}
		i += size

		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\t':
			b.WriteString(`\t`)
		case '\n':
			b.WriteString(`\n`)
		case '\f':
			b.WriteString(`\f`)
		case '\r':
			b.WriteString(`\r`)
		default:
			switch {
			case r < 0x20 || r == 0x7f:
				// Control characters TOML forbids unescaped.
				fmt.Fprintf(&b, `\u%04x`, r)
			case !strconv.IsPrint(r):
				// Invisible or unassigned characters stay visible in the file.
				if r <= 0xffff {
					fmt.Fprintf(&b, `\u%04x`, r)
				} else {
					fmt.Fprintf(&b, `\U%08x`, r)
				}
			default:
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String(), nil
}
