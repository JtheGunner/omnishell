package tui

import (
	"slices"
	"strings"
	"unicode"

	"github.com/JtheGunner/omnishell/internal/modedit"
)

// Module text comes from manifests that users write or copy from strangers. A
// raw escape sequence in a description could retitle the window, clear the
// screen or recolour everything after it, and a bidi override could make one
// module's text read as another's. Everything shown is therefore cleaned once,
// when the model is built.

// sanitize replaces control characters (ESC, BEL, CR, newline, tab, C1) and
// bidi override characters with a space, so nothing in s can act as a
// terminal command or reorder the text around it.
func sanitize(s string) string {
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

// sanitizeViews returns a cleaned copy of views; the caller's slice and the
// slices inside it are left alone.
func sanitizeViews(views []modedit.ModuleView) []modedit.ModuleView {
	out := make([]modedit.ModuleView, len(views))
	for i, v := range views {
		v.ID = sanitize(v.ID)
		v.Name = sanitize(v.Name)
		v.Description = sanitize(v.Description)
		v.Homepage = sanitize(v.Homepage)
		v.Platforms = sanitizeAll(v.Platforms)
		v.Shells = sanitizeAll(v.Shells)
		out[i] = v
	}
	return out
}

func sanitizeAll(in []string) []string {
	out := slices.Clone(in)
	for i, s := range out {
		out[i] = sanitize(s)
	}
	return out
}
