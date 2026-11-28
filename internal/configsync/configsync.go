// Package configsync brings an existing config.toml up to date with the
// installed modules: it lists the options a module gained since the file was
// written, the options and modules that no longer exist, and applies that as a
// line-based edit that keeps every comment, value and the order of the file.
//
// New options are added as commented-out lines holding the default, so the
// behaviour of the config never changes and the user still sees what is
// possible. Orphaned options and module tables are removed. Whatever cannot be
// edited safely (inline tables, dotted keys, multi-line strings) is left alone
// and reported back.
package configsync

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/JtheGunner/omnishell/internal/module"
)

// Kind classifies a Finding.
type Kind string

const (
	// MissingOption is an option the module defines that config.toml does not mention.
	MissingOption Kind = "missing-option"
	// OrphanOption is an option in config.toml the module no longer defines.
	OrphanOption Kind = "orphan-option"
	// OrphanModule is a [modules.<id>] table for a module that no longer exists.
	OrphanModule Kind = "orphan-module"
)

// Finding is one difference between config.toml and the installed modules.
type Finding struct {
	Kind   Kind
	Module string
	// Option is empty for OrphanModule.
	Option string
	// Default and Help describe a MissingOption: the default as a TOML literal
	// and the manifest's help text.
	Default string
	Help    string
}

// Report is the result of Plan.
type Report struct {
	Findings []Finding
}

// Lookup returns the option schema of a module, or false if no module has that id.
type Lookup func(id string) (map[string]module.OptionSchema, bool)

// Plan compares the config file content with the modules Lookup knows. skip
// names modules to leave alone even though Lookup does not know them (a user
// module whose manifest is currently broken must not look like a removed one).
// Findings are ordered by module id, orphaned options before missing ones.
func Plan(path string, data []byte, lookup Lookup, skip func(id string) bool) (Report, error) {
	cfg, err := config.Parse(path, data)
	if err != nil {
		return Report{}, err
	}
	lines := splitLines(data)

	ids := make([]string, 0, len(cfg.Modules))
	for id := range cfg.Modules {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	var rep Report
	for _, id := range ids {
		if skip(id) {
			continue
		}
		schema, ok := lookup(id)
		if !ok {
			rep.Findings = append(rep.Findings, Finding{Kind: OrphanModule, Module: id})
			continue
		}
		set := cfg.Modules[id].Options

		for _, key := range sortedKeys(set) {
			if _, known := schema[key]; !known {
				rep.Findings = append(rep.Findings, Finding{Kind: OrphanOption, Module: id, Option: key})
			}
		}

		defaults, derr := module.ValidateOptions(schema, nil)
		if derr != nil {
			return Report{}, config.Error{Path: path, Msg: fmt.Sprintf("module %q: %v", id, derr)}
		}
		for _, key := range sortedKeys(schema) {
			if _, present := set[key]; present || hasCommentedKey(lines, id, key) {
				continue
			}
			lit, lerr := config.FormatValue(defaults[key])
			if lerr != nil {
				return Report{}, config.Error{Path: path, Msg: fmt.Sprintf("module %q option %q: %v", id, key, lerr)}
			}
			rep.Findings = append(rep.Findings, Finding{
				Kind: MissingOption, Module: id, Option: key,
				Default: lit, Help: oneLine(schema[key].Help),
			})
		}
	}
	return rep, nil
}

// Apply returns the file content with the findings of Plan applied, plus the
// findings that are still open afterwards because they could not be edited
// safely. An up-to-date file is returned byte for byte. The result is checked
// to still parse before it is returned.
func Apply(path string, data []byte, lookup Lookup, skip func(id string) bool) ([]byte, []Finding, error) {
	rep, err := Plan(path, data, lookup, skip)
	if err != nil {
		return nil, nil, err
	}
	if len(rep.Findings) == 0 {
		return data, nil, nil
	}

	lines := splitLines(data)
	missing := map[string][]Finding{}
	var missingIDs []string
	for _, f := range rep.Findings {
		switch f.Kind {
		case OrphanModule:
			lines = removeModule(lines, f.Module)
		case OrphanOption:
			lines = removeOption(lines, f.Module, f.Option)
		case MissingOption:
			if _, seen := missing[f.Module]; !seen {
				missingIDs = append(missingIDs, f.Module)
			}
			missing[f.Module] = append(missing[f.Module], f)
		}
	}
	for _, id := range missingIDs {
		candidate := addOptions(lines, id, missing[id])
		if _, perr := config.Parse(path, joinLines(candidate)); perr != nil {
			continue // keep the file as it was; Plan below reports the option as still open
		}
		lines = candidate
	}

	out := joinLines(lines)
	if _, perr := config.Parse(path, out); perr != nil {
		return nil, nil, fmt.Errorf("config sync would leave an invalid file, nothing written: %w", perr)
	}
	rest, err := Plan(path, out, lookup, skip)
	if err != nil {
		return nil, nil, err
	}
	return out, rest.Findings, nil
}

// --- line editing ---------------------------------------------------------

func splitLines(data []byte) []string {
	s := strings.TrimSuffix(string(data), "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func joinLines(lines []string) []byte {
	if len(lines) == 0 {
		return nil
	}
	return []byte(strings.Join(lines, "\n") + "\n")
}

func isBlank(line string) bool { return strings.TrimSpace(line) == "" }

func isComment(line string) bool { return strings.HasPrefix(strings.TrimSpace(line), "#") }

func isHeader(line string) bool { return strings.HasPrefix(strings.TrimSpace(line), "[") }

// idPattern matches a module id written bare or quoted in a table header.
func idPattern(id string) string {
	q := regexp.QuoteMeta(id)
	return `(?:` + q + `|"` + q + `")`
}

func tableRe(id, sub string) *regexp.Regexp {
	p := `^\s*\[\s*modules\s*\.\s*` + idPattern(id)
	if sub != "" {
		p += `\s*\.\s*` + sub
	}
	return regexp.MustCompile(p + `\s*\]\s*(?:#.*)?$`)
}

// moduleTablesRe matches the header of [modules.<id>] and any sub-table of it.
func moduleTablesRe(id string) *regexp.Regexp {
	return regexp.MustCompile(`^\s*\[\s*modules\s*\.\s*` + idPattern(id) + `\s*(?:\.[^\]]*)?\]\s*(?:#.*)?$`)
}

// findTable returns the index of the header line matching re and the index one
// past the end of its body (the next header or the end of the file).
func findTable(lines []string, re *regexp.Regexp) (start, end int, ok bool) {
	for i, l := range lines {
		if !re.MatchString(l) {
			continue
		}
		end = len(lines)
		for j := i + 1; j < len(lines); j++ {
			if isHeader(lines[j]) {
				end = j
				break
			}
		}
		return i, end, true
	}
	return 0, 0, false
}

// lastContent returns the index one past the last non-blank line in
// lines[from:to], and at least from.
func lastContent(lines []string, from, to int) int {
	pos := from
	for i := from; i < to; i++ {
		if !isBlank(lines[i]) {
			pos = i + 1
		}
	}
	return pos
}

func hasCommentedKey(lines []string, id, key string) bool {
	start, end, ok := findTable(lines, tableRe(id, "options"))
	if !ok {
		return false
	}
	re := regexp.MustCompile(`^\s*#\s*` + regexp.QuoteMeta(key) + `\s*=`)
	for _, l := range lines[start+1 : end] {
		if re.MatchString(l) {
			return true
		}
	}
	return false
}

func removeModule(lines []string, id string) []string {
	re := moduleTablesRe(id)
	for {
		start, end, ok := findTable(lines, re)
		if !ok {
			return lines
		}
		end = lastContent(lines, start, end)
		if start > 0 && isBlank(lines[start-1]) {
			start--
		}
		next := make([]string, 0, len(lines))
		next = append(next, lines[:start]...)
		lines = append(next, lines[end:]...)
	}
}

func removeOption(lines []string, id, key string) []string {
	start, end, ok := findTable(lines, tableRe(id, "options"))
	if !ok {
		return lines
	}
	for i := start + 1; i < end; i++ {
		if isBlank(lines[i]) || isComment(lines[i]) || assignedKey(lines[i]) != key {
			continue
		}
		last := i
		depth := bracketDepth(lines[i])
		if strings.Contains(lines[i], `"""`) || strings.Contains(lines[i], `'''`) {
			return lines // multi-line strings are not edited
		}
		for depth > 0 && last+1 < end {
			last++
			depth += bracketDepth(lines[last])
		}
		if depth != 0 {
			return lines
		}
		next := make([]string, 0, len(lines))
		next = append(next, lines[:i]...)
		return append(next, lines[last+1:]...)
	}
	return lines
}

// assignedKey returns the key a "key = value" line assigns, without quotes.
func assignedKey(line string) string {
	name, _, found := strings.Cut(strings.TrimSpace(line), "=")
	if !found {
		return ""
	}
	return strings.Trim(strings.TrimSpace(name), `"'`)
}

// bracketDepth is the net number of arrays and inline tables a line opens,
// ignoring brackets inside strings and comments.
func bracketDepth(line string) int {
	depth := 0
	var quote rune
	escaped := false
	for _, r := range line {
		switch {
		case quote != 0:
			switch {
			case escaped:
				escaped = false
			case r == '\\' && quote == '"':
				escaped = true
			case r == quote:
				quote = 0
			}
		case r == '"' || r == '\'':
			quote = r
		case r == '#':
			return depth
		case r == '[' || r == '{':
			depth++
		case r == ']' || r == '}':
			depth--
		}
	}
	return depth
}

func addOptions(lines []string, id string, items []Finding) []string {
	comments := make([]string, 0, len(items))
	for _, f := range items {
		c := "# " + f.Option + " = " + f.Default
		if f.Help != "" {
			c += "   # " + f.Help
		}
		comments = append(comments, c)
	}

	var pos int
	var block []string
	if start, end, ok := findTable(lines, tableRe(id, "options")); ok {
		pos, block = lastContent(lines, start+1, end), comments
	} else if start, end, ok := findTable(lines, tableRe(id, "")); ok {
		pos = lastContent(lines, start+1, end)
		block = append([]string{"", "[modules." + id + ".options]"}, comments...)
	} else {
		pos = len(lines)
		block = append([]string{"[modules." + id + ".options]"}, comments...)
		if pos > 0 && !isBlank(lines[pos-1]) {
			block = append([]string{""}, block...)
		}
	}

	out := make([]string, 0, len(lines)+len(block))
	out = append(out, lines[:pos]...)
	out = append(out, block...)
	return append(out, lines[pos:]...)
}

// --- helpers --------------------------------------------------------------

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
