package tui

import (
	"slices"

	"github.com/JtheGunner/omnishell/internal/modedit"
)

// Module text comes from manifests that users write or copy from strangers. A
// raw escape sequence in a description could retitle the window, clear the
// screen or recolour everything after it, and a bidi override could make one
// module's text read as another's. Everything shown is therefore cleaned once,
// when the model is built.

// sanitize is modedit.Clean: it replaces control characters and bidi override
// characters with a space, so nothing in s can act as a terminal command or
// reorder the text around it.
func sanitize(s string) string { return modedit.Clean(s) }

// sanitizeViews returns a cleaned copy of views; the caller's slice and the
// slices inside it are left alone.
func sanitizeViews(views []modedit.ModuleView) []modedit.ModuleView {
	out := make([]modedit.ModuleView, len(views))
	for i, v := range views {
		v.ID = sanitize(v.ID)
		v.Name = sanitize(v.Name)
		v.Description = sanitize(v.Description)
		v.Homepage = sanitize(v.Homepage)
		v.Unavailable = sanitize(v.Unavailable)
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
