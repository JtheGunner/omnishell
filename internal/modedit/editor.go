// Package modedit holds the rules for changing which modules are enabled and
// how they are configured, plus the per-module view the `list` command and the
// TUI show. Both front ends call it, so "same rules as enable/set" is a
// property of the code, not a convention.
//
// modedit only ever writes config.toml, and only through the comment-preserving
// atomic edits in internal/config.
package modedit

import (
	"fmt"
	"sort"
	"strings"

	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/JtheGunner/omnishell/internal/engine"
	"github.com/JtheGunner/omnishell/internal/module"
)

// Editor edits the config.toml at CfgPath, validating against the modules in
// Engine.Registry and the shells Engine.Platform reports.
type Editor struct {
	Engine  engine.Engine
	CfgPath string
}

// Enable sets modules.<id>.enabled = true. It refuses a module that none of the
// currently managed shells can run.
func (ed Editor) Enable(id string) error { return ed.setEnabled(id, true) }

// Disable sets modules.<id>.enabled = false. It does not check shell
// compatibility, so an incompatible module can always be switched off.
func (ed Editor) Disable(id string) error { return ed.setEnabled(id, false) }

// SetOption sets modules.<id>.options.<key> from the raw string form a user
// types. It requires an existing config.toml, validates the module and key,
// and parses raw against the option's schema before anything is written, so a
// rejected value leaves the file untouched. It never changes enablement.
func (ed Editor) SetOption(id, key, raw string) error {
	if _, err := config.Load(ed.CfgPath); err != nil {
		return err
	}
	mod, ok := ed.Engine.Registry.Get(id)
	if !ok {
		return config.Error{Path: ed.CfgPath, Msg: fmt.Sprintf("unknown module %q", id)}
	}
	schema, ok := mod.Manifest.Options[key]
	if !ok {
		return config.Error{
			Path: ed.CfgPath,
			Msg: fmt.Sprintf("unknown option %q for module %q (valid: %s)",
				key, id, strings.Join(optionKeys(mod.Manifest.Options), ", ")),
		}
	}
	typed, err := schema.ParseValue(raw)
	if err != nil {
		return config.Error{
			Path: ed.CfgPath,
			Msg:  fmt.Sprintf("invalid value for %s.%s: %v", id, key, err),
		}
	}
	return config.SetOption(ed.CfgPath, id, key, typed)
}

// setEnabled requires an existing config.toml (a missing one wraps
// config.ErrNotFound), validates the module id, and flips modules.<id>.enabled.
func (ed Editor) setEnabled(id string, enabled bool) error {
	cfg, err := config.Load(ed.CfgPath)
	if err != nil {
		return err
	}
	mod, ok := ed.Engine.Registry.Get(id)
	if !ok {
		return config.Error{Path: ed.CfgPath, Msg: fmt.Sprintf("unknown module %q", id)}
	}
	if enabled {
		if err := ed.checkShellCompatible(cfg, mod); err != nil {
			return err
		}
	}
	return config.SetEnabled(ed.CfgPath, id, enabled)
}

// checkShellCompatible refuses to enable a module that none of the shells
// omnishell currently manages on this host could ever run — e.g. a zsh-only
// module (autosuggestions, syntax-highlighting) on a bash-only system.
// Enabling it anyway would leave it permanently degraded ("no snippet for
// any managed shell") with no way to notice besides `doctor`/`list`.
func (ed Editor) checkShellCompatible(cfg config.Config, mod module.Module) error {
	managed := engine.ManagedShells(cfg, ed.Engine.Platform)
	for _, sh := range managed {
		if contains(mod.Manifest.Shells, sh) {
			return nil
		}
	}
	return config.Error{Path: ed.CfgPath, Msg: fmt.Sprintf(
		"module %q only supports %s, but none of your managed shells (%s) do — install one of those shells first",
		mod.Manifest.Module.ID, strings.Join(mod.Manifest.Shells, ", "), joinOrNone(managed),
	)}
}

func contains(ss []string, v string) bool {
	for _, s := range ss {
		if s == v {
			return true
		}
	}
	return false
}

func joinOrNone(ss []string) string {
	if len(ss) == 0 {
		return "none"
	}
	return strings.Join(ss, ", ")
}

// optionKeys returns the option keys of a manifest, sorted, for error messages.
func optionKeys(opts map[string]module.OptionSchema) []string {
	keys := make([]string, 0, len(opts))
	for k := range opts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
