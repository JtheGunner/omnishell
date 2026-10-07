package modedit

import (
	"strings"
	"unicode"
)

// Clean replaces control characters (ESC, BEL, CR, newline, tab, C1) and bidi
// override characters with a space, so text that comes from a module manifest
// can neither act as a terminal command nor reorder the text around it. Every
// front end that prints manifest text to a terminal runs it through Clean.
func Clean(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || isBidiControl(r) {
			return ' '
		}
		return r
	}, s)
}

func isBidiControl(r rune) bool {
	return (r >= 0x202A && r <= 0x202E) || (r >= 0x2066 && r <= 0x2069)
}
