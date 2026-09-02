// Package module parses module manifests, validates option schemas, exposes
// module folders, and builds the registry of built-in + user modules.
package module

import (
	"fmt"
	"regexp"

	"github.com/BurntSushi/toml"
)

// ManifestSchemaVersion is the manifest schema version this build understands.
const ManifestSchemaVersion = 1

var idRe = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// ModuleMeta is the [module] table.
type ModuleMeta struct {
	ID          string `toml:"id"`
	Name        string `toml:"name"`
	Description string `toml:"description"`
	Version     string `toml:"version"`
	Schema      int    `toml:"schema"`
}

// Fallback is one [[packages.fallback]] entry.
type Fallback struct {
	Type string `toml:"type"`
	Repo string `toml:"repo"`
	Dest string `toml:"dest"`
	Run  string `toml:"run"`
}

// Packages is the [packages] table.
type Packages struct {
	Brew     []string   `toml:"brew"`
	Apt      []string   `toml:"apt"`
	Dnf      []string   `toml:"dnf"`
	Pacman   []string   `toml:"pacman"`
	Zypper   []string   `toml:"zypper"`
	Apk      []string   `toml:"apk"`
	Fallback []Fallback `toml:"fallback"`
}

// ForManager returns the package list for a manager name ("brew", "apt", ...).
func (p Packages) ForManager(name string) []string {
	switch name {
	case "brew":
		return p.Brew
	case "apt":
		return p.Apt
	case "dnf":
		return p.Dnf
	case "pacman":
		return p.Pacman
	case "zypper":
		return p.Zypper
	case "apk":
		return p.Apk
	default:
		return nil
	}
}

// OptionSchema describes one [options.<key>] entry.
type OptionSchema struct {
	Type    string   `toml:"type"`
	Default any      `toml:"default"`
	Help    string   `toml:"help"`
	Values  []string `toml:"values"`
}

// Manifest is a parsed manifest.toml.
type Manifest struct {
	Module    ModuleMeta              `toml:"module"`
	Platforms []string               `toml:"platforms"`
	Shells    []string               `toml:"shells"`
	Requires  []string               `toml:"requires"`
	After     []string               `toml:"after"`
	Packages  Packages               `toml:"packages"`
	Options   map[string]OptionSchema `toml:"options"`
}

// ManifestError is a structured manifest validation error.
type ManifestError struct {
	ID    string
	Field string
	Msg   string
}

func (e ManifestError) Error() string {
	return fmt.Sprintf("module %q: %s: %s", e.ID, e.Field, e.Msg)
}

// ParseManifest strictly decodes manifest TOML.
func ParseManifest(data []byte) (Manifest, error) {
	var m Manifest
	md, err := toml.Decode(string(data), &m)
	if err != nil {
		return Manifest{}, fmt.Errorf("parse manifest: %w", err)
	}
	if u := md.Undecoded(); len(u) > 0 {
		return Manifest{}, fmt.Errorf("parse manifest: unknown key %s", u[0].String())
	}
	if m.Options == nil {
		m.Options = map[string]OptionSchema{}
	}
	return m, nil
}

var validOptionTypes = map[string]bool{
	"bool": true, "string": true, "int": true,
	"enum": true, "list<string>": true, "list<enum>": true,
}

func subsetOf(vals []string, allowed map[string]bool) (string, bool) {
	for _, v := range vals {
		if !allowed[v] {
			return v, false
		}
	}
	return "", true
}

// ValidateManifest enforces the manifest rules from the spec.
func ValidateManifest(m Manifest) error {
	id := m.Module.ID
	e := func(field, msg string) error { return ManifestError{ID: id, Field: field, Msg: msg} }

	if !idRe.MatchString(id) {
		return e("module.id", "must match ^[a-z][a-z0-9-]*$")
	}
	if m.Module.Version == "" {
		return e("module.version", "must not be empty")
	}
	if m.Module.Schema != ManifestSchemaVersion {
		return e("module.schema", fmt.Sprintf("must be %d", ManifestSchemaVersion))
	}
	if len(m.Platforms) == 0 {
		return e("platforms", "must list at least one of macos, linux")
	}
	if bad, ok := subsetOf(m.Platforms, map[string]bool{"macos": true, "linux": true}); !ok {
		return e("platforms", "unknown platform "+bad)
	}
	if len(m.Shells) == 0 {
		return e("shells", "must list at least one of zsh, bash")
	}
	if bad, ok := subsetOf(m.Shells, map[string]bool{"zsh": true, "bash": true}); !ok {
		return e("shells", "unknown shell "+bad)
	}
	for key, opt := range m.Options {
		if !validOptionTypes[opt.Type] {
			return e("options."+key+".type", "unknown type "+opt.Type)
		}
		if (opt.Type == "enum" || opt.Type == "list<enum>") && len(opt.Values) == 0 {
			return e("options."+key+".values", "enum types require a non-empty values list")
		}
		if err := checkDefault(opt); err != nil {
			return e("options."+key+".default", err.Error())
		}
	}
	return nil
}

func checkDefault(opt OptionSchema) error {
	if opt.Default == nil {
		return nil
	}
	switch opt.Type {
	case "bool":
		if _, ok := opt.Default.(bool); !ok {
			return fmt.Errorf("must be a boolean")
		}
	case "int":
		switch opt.Default.(type) {
		case int, int64:
		default:
			return fmt.Errorf("must be an integer")
		}
	case "string":
		if _, ok := opt.Default.(string); !ok {
			return fmt.Errorf("must be a string")
		}
	case "enum":
		s, ok := opt.Default.(string)
		if !ok {
			return fmt.Errorf("must be a string in values")
		}
		if _, in := subsetOf([]string{s}, toSet(opt.Values)); !in {
			return fmt.Errorf("%q is not in values", s)
		}
	case "list<string>", "list<enum>":
		if _, ok := toStringSlice(opt.Default); !ok {
			return fmt.Errorf("must be a list")
		}
	}
	return nil
}

func toSet(vals []string) map[string]bool {
	s := make(map[string]bool, len(vals))
	for _, v := range vals {
		s[v] = true
	}
	return s
}

func toStringSlice(v any) ([]string, bool) {
	switch x := v.(type) {
	case []string:
		return x, true
	case []any:
		out := make([]string, 0, len(x))
		for _, e := range x {
			s, ok := e.(string)
			if !ok {
				return nil, false
			}
			out = append(out, s)
		}
		return out, true
	default:
		return nil, false
	}
}
