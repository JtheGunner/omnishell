package engine

import (
	"fmt"
	"sort"
	"strings"

	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/JtheGunner/omnishell/internal/graph"
	"github.com/JtheGunner/omnishell/internal/lockfile"
	"github.com/JtheGunner/omnishell/internal/module"
	"github.com/JtheGunner/omnishell/internal/pkgmgr"
	"github.com/JtheGunner/omnishell/internal/platform"
)

// ModuleAction is what apply will do with a module.
type ModuleAction string

const (
	ActionInstall   ModuleAction = "install"
	ActionUpdate    ModuleAction = "update"
	ActionUnchanged ModuleAction = "unchanged"
	ActionRemove    ModuleAction = "remove"
	ActionSkip      ModuleAction = "skip"
)

// PackagePlan is one package the plan may install.
type PackagePlan struct {
	Name             string
	Manager          string
	AlreadyInstalled bool
}

// ModulePlan is the planned outcome for one module.
type ModulePlan struct {
	ID              string
	Action          ModuleAction
	Reason          string
	Shells          []string
	Manifest        module.Manifest
	Options         map[string]any
	OptionsHash     string
	MissingPackages []PackagePlan
	UsesFallback    bool
	UsesCheckHook   bool
	DegradedReason  string
}

// Plan is the full computed plan.
type Plan struct {
	Order            []string
	Modules          map[string]ModulePlan
	ManagedShells    []string
	PackageManager   string
	ManagerAvailable bool
	HasChanges       bool
	// UnknownModules are ids enabled in the config that no registered module
	// provides, sorted. Apply warns for each; Doctor emits a notice.
	UnknownModules []string
}

func managedShells(cfg config.Config, info platform.Info) []string {
	present := map[string]bool{}
	for _, s := range info.Shells {
		if s.Present {
			present[s.Name] = true
		}
	}

	want := cfg.Omnishell.Shells
	if len(want) == 0 {
		for _, s := range info.Shells {
			if s.Present {
				want = append(want, s.Name)
			}
		}
	}
	wantSet := map[string]bool{}
	for _, s := range want {
		wantSet[s] = true
	}
	var out []string
	for _, s := range platform.SupportedShells { // fixed [zsh, bash] order
		// Only manage a shell that actually exists on this host, matching what
		// `omnishell init` does — otherwise apply would create e.g. ~/.bashrc
		// on a machine with no bash.
		if wantSet[s] && present[s] {
			out = append(out, s)
		}
	}
	return out
}

func contains(ss []string, v string) bool {
	for _, s := range ss {
		if s == v {
			return true
		}
	}
	return false
}

// ComputePlan builds the plan without touching the system.
func ComputePlan(e Engine, cfg config.Config, lock lockfile.Lock, noPackages bool) (Plan, error) {
	shells := managedShells(cfg, e.Platform)
	mgrName := ""
	if e.ManagerOK {
		mgrName = e.Manager.Name()
	}
	p := Plan{
		Modules:          map[string]ModulePlan{},
		ManagedShells:    shells,
		PackageManager:   mgrName,
		ManagerAvailable: e.ManagerOK,
	}

	active := map[string]module.Manifest{}
	var unknown, inactive []string
	for id, mc := range cfg.Modules {
		if !mc.Enabled {
			continue
		}
		mod, ok := e.Registry.Get(id)
		if !ok {
			// No registered module provides this id. Apply warns to stderr and
			// Doctor emits an unknown-module notice; planning just skips it.
			unknown = append(unknown, id)
			continue
		}
		if !contains(mod.Manifest.Platforms, string(e.Platform.OS)) {
			// Enabled and known, but not for this OS: surface it as a skip
			// rather than pretending it does not exist.
			inactive = append(inactive, id)
			continue
		}
		active[id] = mod.Manifest
	}
	sort.Strings(unknown)
	p.UnknownModules = unknown
	for _, id := range inactive {
		p.Modules[id] = ModulePlan{
			ID:     id,
			Action: ActionSkip,
			Reason: "not supported on " + string(e.Platform.OS),
		}
	}

	// Validate options; abort as ConfigError on failure. Iterate sorted so the
	// surfaced error is deterministic when several modules have invalid options.
	optsByID := map[string]map[string]any{}
	activeIDs := make([]string, 0, len(active))
	for id := range active {
		activeIDs = append(activeIDs, id)
	}
	sort.Strings(activeIDs)
	for _, id := range activeIDs {
		norm, err := module.ValidateOptions(active[id].Options, cfg.Modules[id].Options)
		if err != nil {
			return Plan{}, ConfigError{Err: err}
		}
		optsByID[id] = norm
	}

	order, err := graph.Order(active)
	if err != nil {
		return Plan{}, ConfigError{Err: err}
	}
	p.Order = order

	for _, id := range order {
		mf := active[id]
		mod, _ := e.Registry.Get(id)
		norm := optsByID[id]
		hash := module.OptionsHash(norm)

		var shellsForModule []string
		var tmplErr string
		for _, sh := range shells {
			if !contains(mf.Shells, sh) {
				continue
			}
			_, has, terr := mod.Template(sh)
			if terr != nil {
				// A real read error (not "no such file") — don't silently
				// treat it as "this module has no snippet".
				tmplErr = "template read failed for " + sh + ": " + terr.Error()
				break
			}
			if has {
				shellsForModule = append(shellsForModule, sh)
			}
		}

		mp := ModulePlan{
			ID: id, Manifest: mf, Options: norm, OptionsHash: hash,
			Shells: shellsForModule,
		}
		switch {
		case tmplErr != "":
			mp.DegradedReason = tmplErr
		case len(shellsForModule) == 0:
			mp.DegradedReason = "no snippet for any managed shell"
		}

		if !noPackages {
			planPackages(&mp, e, mod, shells)
		}

		// A planned-degraded module emits no sections and rebuildLock records
		// ShellsRendered=nil for it, so its expected rendered-shell set is
		// empty — comparing against the non-empty planned shell list would flip
		// it to ActionUpdate on every run and pin HasChanges true forever.
		expectedShells := shellsForModule
		if mp.DegradedReason != "" {
			expectedShells = nil
		}

		prev, inLock := lock.Modules[id]
		switch {
		case !inLock:
			mp.Action = ActionInstall
		case prev.OptionsHash != hash || prev.ModuleVersion != mf.Module.Version ||
			!equalStringSet(prev.ShellsRendered, expectedShells) || len(mp.MissingPackages) > 0:
			mp.Action = ActionUpdate
		default:
			mp.Action = ActionUnchanged
		}
		p.Modules[id] = mp
	}

	// Modules present in the lock but no longer active → removal.
	for id, st := range lock.Modules {
		if _, stillActive := p.Modules[id]; stillActive {
			continue
		}
		if st.Status == "disabled" {
			continue
		}
		p.Modules[id] = ModulePlan{ID: id, Action: ActionRemove, Reason: "no longer enabled"}
	}

	for _, mp := range p.Modules {
		if mp.Action == ActionInstall || mp.Action == ActionUpdate || mp.Action == ActionRemove {
			p.HasChanges = true
			break
		}
	}
	return p, nil
}

func planPackages(mp *ModulePlan, e Engine, mod module.Module, shells []string) {
	mf := mp.Manifest
	if !e.ManagerOK {
		if len(mf.Packages.Brew)+len(mf.Packages.Apt)+len(mf.Packages.Dnf)+
			len(mf.Packages.Pacman)+len(mf.Packages.Zypper)+len(mf.Packages.Apk) > 0 {
			mp.DegradedReason = "no package manager detected"
		}
		return
	}
	pkgs := mf.Packages.ForManager(e.Manager.Name())
	if len(pkgs) > 0 {
		for _, name := range pkgs {
			installed, _ := e.Manager.IsInstalled(name)
			if !installed {
				mp.MissingPackages = append(mp.MissingPackages, PackagePlan{Name: name, Manager: e.Manager.Name()})
			}
		}
		return
	}
	if len(mf.Packages.Fallback) > 0 {
		mp.UsesFallback = true
		ok, _ := pkgmgr.FallbackSatisfied(mf.Packages.Fallback[0], pkgmgr.FallbackContext{
			VendorDir: e.Platform.ConfigDir + "/vendor",
			Platform:  string(e.Platform.OS),
		})
		if !ok {
			mp.MissingPackages = append(mp.MissingPackages, PackagePlan{Name: mf.Packages.Fallback[0].Repo, Manager: "git"})
		}
		return
	}
	if mod.HasHook("check") {
		mp.UsesCheckHook = true
	}
}

func equalStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[string]bool{}
	for _, s := range a {
		seen[s] = true
	}
	for _, s := range b {
		if !seen[s] {
			return false
		}
	}
	return true
}

// RenderPlan formats a plan for the user.
func RenderPlan(p Plan) string {
	var b strings.Builder
	mgr := p.PackageManager
	if !p.ManagerAvailable {
		mgr = "none detected"
	}
	_, _ = fmt.Fprintf(&b, "Plan (package manager: %s, shells: %s)\n\n", mgr, strings.Join(p.ManagedShells, ", "))

	var nInstall, nUpdate, nRemove, nUnchanged int
	line := func(mp ModulePlan) {
		if mp.DegradedReason != "" {
			_, _ = fmt.Fprintf(&b, "  %-8s %-20s %s\n", "degraded", mp.ID, mp.DegradedReason)
		}
		switch mp.Action {
		case ActionInstall, ActionUpdate:
			extra := ""
			if len(mp.Shells) > 0 {
				extra = "snippet: " + strings.Join(mp.Shells, ", ")
			}
			if len(mp.MissingPackages) > 0 {
				names := make([]string, len(mp.MissingPackages))
				for i, pp := range mp.MissingPackages {
					names[i] = pp.Name + " (" + pp.Manager + ")"
				}
				extra += "   packages: " + strings.Join(names, ", ")
			}
			_, _ = fmt.Fprintf(&b, "  %-8s %-20s %s\n", string(mp.Action), mp.ID, strings.TrimSpace(extra))
		case ActionRemove:
			_, _ = fmt.Fprintf(&b, "  %-8s %-20s %s\n", "remove", mp.ID, mp.Reason)
		case ActionSkip:
			_, _ = fmt.Fprintf(&b, "  %-8s %-20s %s\n", "skip", mp.ID, mp.Reason)
		}
	}

	for _, id := range p.Order {
		mp := p.Modules[id]
		switch mp.Action {
		case ActionInstall:
			nInstall++
		case ActionUpdate:
			nUpdate++
		case ActionUnchanged:
			nUnchanged++
		}
		if mp.Action != ActionUnchanged {
			line(mp)
		}
	}

	var removals []string
	for id, mp := range p.Modules {
		if mp.Action == ActionRemove {
			removals = append(removals, id)
		}
	}
	sort.Strings(removals)
	for _, id := range removals {
		nRemove++
		line(p.Modules[id])
	}

	var skips []string
	for id, mp := range p.Modules {
		if mp.Action == ActionSkip {
			skips = append(skips, id)
		}
	}
	sort.Strings(skips)
	for _, id := range skips {
		line(p.Modules[id])
	}

	_, _ = fmt.Fprintf(&b, "\n%d to install, %d to update, %d to remove, %d unchanged\n",
		nInstall, nUpdate, nRemove, nUnchanged)
	if len(skips) > 0 {
		_, _ = fmt.Fprintf(&b, "%d skipped (not supported on this platform)\n", len(skips))
	}
	return b.String()
}
