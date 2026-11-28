package modedit

import (
	"fmt"
	"sort"
	"strings"

	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/JtheGunner/omnishell/internal/engine"
	"github.com/JtheGunner/omnishell/internal/module"
	"github.com/JtheGunner/omnishell/internal/render"
)

// CheatsheetOptions selects what Cheatsheet renders.
type CheatsheetOptions struct {
	// IDs limits the output to these modules, each of which must be active.
	// Empty means every active module.
	IDs []string
	// Shell is the requested shell ("zsh" or "bash"); empty picks one.
	Shell string
	// EnvShell is the base name of $SHELL. It is used only when Shell is empty
	// and it names a managed shell.
	EnvShell string
}

// CheatsheetEntry is one module's rendered cheatsheet.
type CheatsheetEntry struct {
	ID, Name, Description string
	// PackagesMissing is true when the module's packages are not all installed.
	PackagesMissing bool
	// Body is the rendered cheatsheet.tmpl, not yet cleaned for display.
	Body string
}

// CheatsheetFailure is a module whose cheatsheet could not be read or rendered.
type CheatsheetFailure struct {
	ID  string
	Err error
}

// Cheatsheet is the result of Editor.Cheatsheet.
type Cheatsheet struct {
	// Shell is the shell the templates were rendered for.
	Shell   string
	Entries []CheatsheetEntry
	// NoCheatsheet lists selected modules that ship no cheatsheet.tmpl.
	NoCheatsheet []string
	Failed       []CheatsheetFailure
}

// Cheatsheet renders the cheatsheet of every active module (or of opts.IDs)
// for one shell, using the options from config.toml. It writes nothing: no
// config, lockfile or init file. A module is active when it is enabled and
// supports this OS and the chosen shell. A module whose template fails to
// render lands in Failed and does not stop the others.
func (ed Editor) Cheatsheet(opts CheatsheetOptions) (Cheatsheet, error) {
	cfg, cfgMissing, err := ed.loadConfigAllowMissing()
	if err != nil {
		return Cheatsheet{}, err
	}
	if cfgMissing {
		return Cheatsheet{}, config.Error{
			Path: ed.CfgPath,
			Msg:  "no config file yet; run `omnishell init` first",
		}
	}

	shell, err := ed.cheatsheetShell(cfg, opts)
	if err != nil {
		return Cheatsheet{}, err
	}

	osName := string(ed.Engine.Platform.OS)
	supported := map[string]bool{} // enabled and supported on this OS
	active := map[string]bool{}    // ... and runnable in the chosen shell
	for id, mc := range cfg.Modules {
		m, ok := ed.Engine.Registry.Get(id)
		if !ok || !mc.Enabled || !contains(m.Manifest.Platforms, osName) {
			continue
		}
		supported[id] = true
		if contains(m.Manifest.Shells, shell) {
			active[id] = true
		}
	}

	ids, err := ed.cheatsheetSelection(opts.IDs, active, shell)
	if err != nil {
		return Cheatsheet{}, err
	}

	result := Cheatsheet{Shell: shell}
	for _, id := range ids {
		mod, _ := ed.Engine.Registry.Get(id)
		mf := mod.Manifest

		body, found, err := mod.Cheatsheet()
		if err != nil {
			result.Failed = append(result.Failed, CheatsheetFailure{ID: id, Err: fmt.Errorf("read cheatsheet: %w", err)})
			continue
		}
		if !found {
			result.NoCheatsheet = append(result.NoCheatsheet, id)
			continue
		}

		options, err := module.ValidateOptions(mf.Options, cfg.Modules[id].Options)
		if err != nil {
			return Cheatsheet{}, engine.ConfigError{Err: fmt.Errorf("module %q: %w", id, err)}
		}

		rendered, err := render.Render(body, render.Context{
			Options:   options,
			Platform:  osName,
			Shell:     shell,
			VendorDir: ed.Engine.VendorDir(),
			ConfigDir: ed.Engine.Platform.ConfigDir,
			Bin:       map[string]string{},
			Active:    supported,
		})
		if err != nil {
			result.Failed = append(result.Failed, CheatsheetFailure{ID: id, Err: err})
			continue
		}

		result.Entries = append(result.Entries, CheatsheetEntry{
			ID:              id,
			Name:            mf.Module.Name,
			Description:     mf.Module.Description,
			PackagesMissing: ed.packageState(mf) == PackagesMissing,
			Body:            rendered,
		})
	}
	return result, nil
}

// cheatsheetShell picks the shell to render for: the requested one, else
// $SHELL's when managed, else the first managed shell.
func (ed Editor) cheatsheetShell(cfg config.Config, opts CheatsheetOptions) (string, error) {
	managed := engine.ManagedShells(cfg, ed.Engine.Platform)
	if len(managed) == 0 {
		return "", config.Error{Path: ed.CfgPath, Msg: "no managed shell found; install zsh or bash first"}
	}
	switch {
	case opts.Shell != "":
		if !contains(managed, opts.Shell) {
			return "", config.Error{Path: ed.CfgPath, Msg: fmt.Sprintf(
				"shell %q is not managed (managed shells: %s)", opts.Shell, strings.Join(managed, ", "))}
		}
		return opts.Shell, nil
	case contains(managed, opts.EnvShell):
		return opts.EnvShell, nil
	default:
		return managed[0], nil
	}
}

// cheatsheetSelection returns the sorted, de-duplicated module ids to render:
// the requested ones (each must be active) or every active module.
func (ed Editor) cheatsheetSelection(requested []string, active map[string]bool, shell string) ([]string, error) {
	seen := map[string]bool{}
	var ids []string
	if len(requested) == 0 {
		for id := range active {
			ids = append(ids, id)
		}
	}
	for _, id := range requested {
		if !active[id] {
			return nil, config.Error{Path: ed.CfgPath, Msg: fmt.Sprintf(
				"module %q is not active for %s (it must exist, be enabled, and support this OS and shell)", id, shell)}
		}
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids, nil
}
