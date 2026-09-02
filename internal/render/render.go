// Package render executes a module's shell snippet template.
package render

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"text/template"
)

// Context is the data and helper set available to a module template.
type Context struct {
	Options   map[string]any
	Platform  string
	Shell     string
	VendorDir string
	ConfigDir string
	Bin       map[string]string
	Active    map[string]bool
}

// Render executes tmpl against ctx.
func Render(tmpl string, ctx Context) (string, error) {
	funcs := template.FuncMap{
		"shellquote": shellquote,
		"pathjoin":   func(parts ...string) string { return filepath.Join(parts...) },
		"has":        func(id string) bool { return ctx.Active[id] },
	}
	t, err := template.New("snippet").Option("missingkey=error").Funcs(funcs).Parse(tmpl)
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, ctx); err != nil {
		return "", fmt.Errorf("render template: %w", err)
	}
	return normalise(buf.String()), nil
}

func shellquote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func normalise(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
	}
	out := strings.Join(lines, "\n")
	out = strings.Trim(out, "\n")
	if out == "" {
		return ""
	}
	return out + "\n"
}
