package modedit

import (
	"errors"
	"sort"

	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/JtheGunner/omnishell/internal/module"
)

// Status is a module's state in config.toml.
type Status string

const (
	StatusEnabled  Status = "enabled"
	StatusDisabled Status = "disabled"
	// StatusUnknown means there is no config.toml yet, so nothing is enabled or
	// disabled.
	StatusUnknown Status = "—"
)

// PackageState reports whether a module's packages are installed.
type PackageState string

const (
	// PackagesOK: every package for the detected manager is installed.
	PackagesOK PackageState = "ok"
	// PackagesMissing: at least one is not installed (or could not be checked).
	PackagesMissing PackageState = "missing"
	// PackagesNA: the module needs no package, or none could be checked
	// (no package manager detected, or fallback-only).
	PackagesNA PackageState = "n/a"
)

// Origin says where a module comes from.
type Origin string

const (
	OriginBuiltin Origin = "builtin"
	OriginUser    Origin = "user"
	// OriginUserOverride is a user module that replaces a built-in of the same id.
	OriginUserOverride Origin = "user*"
)

// ModuleView is everything a front end shows about one module.
type ModuleView struct {
	ID          string
	Name        string
	Description string
	Homepage    string
	Status      Status
	Packages    PackageState
	Platforms   []string
	Shells      []string
	Origin      Origin
	OptionCount int
}

// Views returns one ModuleView per registry module, sorted by ID. A missing
// config.toml is not an error: every Status is then StatusUnknown. Any other
// config error is returned and no views are.
func (ed Editor) Views() ([]ModuleView, error) {
	cfg, cfgMissing, err := ed.loadConfigAllowMissing()
	if err != nil {
		return nil, err
	}

	overrides := map[string]bool{}
	for _, id := range ed.Engine.Registry.Overrides() {
		overrides[id] = true
	}

	views := make([]ModuleView, 0)
	for _, m := range ed.Engine.Registry.All() {
		mf := m.Manifest
		id := mf.Module.ID

		status := StatusUnknown
		if !cfgMissing {
			status = StatusDisabled
			if mc, ok := cfg.Modules[id]; ok && mc.Enabled {
				status = StatusEnabled
			}
		}

		origin := OriginBuiltin
		if m.Source == module.SourceUser {
			origin = OriginUser
		}
		if overrides[id] {
			origin = OriginUserOverride
		}

		views = append(views, ModuleView{
			ID:          id,
			Name:        mf.Module.Name,
			Description: mf.Module.Description,
			Homepage:    mf.Module.Homepage,
			Status:      status,
			Packages:    ed.packageState(mf),
			Platforms:   mf.Platforms,
			Shells:      mf.Shells,
			Origin:      origin,
			OptionCount: len(mf.Options),
		})
	}
	sort.Slice(views, func(i, j int) bool { return views[i].ID < views[j].ID })
	return views, nil
}

// loadConfigAllowMissing loads config.toml, treating a missing file as "no
// config" rather than an error.
func (ed Editor) loadConfigAllowMissing() (config.Config, bool, error) {
	cfg, err := config.Load(ed.CfgPath)
	if err == nil {
		return cfg, false, nil
	}
	if errors.Is(err, config.ErrNotFound) {
		return config.Default(), true, nil
	}
	return config.Config{}, false, err
}

// packageState reports ok/missing/n/a for a module's packages under the
// detected manager. With no manager detected, or when the module declares no
// packages for that manager, it is n/a; fallback-only modules also report n/a
// here (fallback satisfaction needs the apply-time vendor context).
func (ed Editor) packageState(mf module.Manifest) PackageState {
	e := ed.Engine
	var pkgs []string
	if e.ManagerOK {
		pkgs = mf.Packages.ForManager(e.Manager.Name())
	}
	if !e.ManagerOK || len(pkgs) == 0 {
		return PackagesNA
	}
	for _, p := range pkgs {
		installed, err := e.Manager.IsInstalled(p)
		if err != nil || !installed {
			return PackagesMissing
		}
	}
	return PackagesOK
}
