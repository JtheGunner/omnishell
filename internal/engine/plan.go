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
	// Update marks a git fallback whose clone already exists but was built from
	// another ref than the manifest pins; From is the recorded ref ("" when
	// unknown), To the pinned one.
	Update   bool
	From, To string
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
	// Fallback is the fallback entry the plan selected; the zero value when the
	// module uses none.
	Fallback module.Fallback
	// PackagesPlanned is true once package planning ran to completion for the
	// module. It is false under --no-packages and when the planner gave up
	// early (no package manager, planned degradation); in those cases
	// UsesFallback says nothing about the module, so the lockfile must keep the
	// refs it already recorded.
	PackagesPlanned bool
	// UnavailablePackages are the manager packages the repositories cannot
	// provide; non-empty only when the plan switched to the module's fallback.
	UnavailablePackages []string
	UsesCheckHook       bool
	DegradedReason      string
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

// ManagedShells returns the shells omnishell will manage for cfg on this
// host, in the fixed [zsh, bash] order: cfg.Omnishell.Shells if set,
// otherwise every shell auto-detected as present, always narrowed to shells
// actually present on the host (matching what `omnishell init` does).
func ManagedShells(cfg config.Config, info platform.Info) []string {
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
	shells := ManagedShells(cfg, e.Platform)
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

		// A module with no compatible managed shell can never render its
		// snippet, so installing its packages would only leave software on
		// the system with nothing sourcing it — skip package planning too.
		prev, inLock := lock.Modules[id]
		if !noPackages && mp.DegradedReason == "" {
			planPackages(&mp, e, mod, shells, prev)
		}

		// A planned-degraded module emits no sections and rebuildLock records
		// ShellsRendered=nil for it, so its expected rendered-shell set is
		// empty — comparing against the non-empty planned shell list would flip
		// it to ActionUpdate on every run and pin HasChanges true forever.
		expectedShells := shellsForModule
		if mp.DegradedReason != "" {
			expectedShells = nil
		}

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

func planPackages(mp *ModulePlan, e Engine, mod module.Module, shells []string, prev lockfile.ModuleState) {
	mf := mp.Manifest
	if !e.ManagerOK {
		if len(mf.Packages.Brew)+len(mf.Packages.Apt)+len(mf.Packages.Dnf)+
			len(mf.Packages.Pacman)+len(mf.Packages.Zypper)+len(mf.Packages.Apk) > 0 {
			mp.DegradedReason = "no package manager detected"
		}
		return
	}
	mp.PackagesPlanned = true
	pkgs := mf.Packages.ForManager(e.Manager.Name())
	if len(pkgs) > 0 {
		var missing []PackagePlan
		var unavailable []string
		for _, name := range pkgs {
			installed, _ := e.Manager.IsInstalled(name)
			if installed {
				continue
			}
			missing = append(missing, PackagePlan{Name: name, Manager: e.Manager.Name()})
			// Only probe when there is a fallback to switch to. A failed probe
			// means "unknown": keep the package path so a real error surfaces.
			if len(mf.Packages.Fallback) == 0 {
				continue
			}
			if ok, err := e.Manager.Available(name); err == nil && !ok {
				unavailable = append(unavailable, name)
			}
		}
		if len(unavailable) > 0 {
			mp.UnavailablePackages = unavailable
			planFallback(mp, e, prev)
			return
		}
		mp.MissingPackages = append(mp.MissingPackages, missing...)
		return
	}
	if len(mf.Packages.Fallback) > 0 {
		planFallback(mp, e, prev)
		return
	}
	if mod.HasHook("check") {
		mp.UsesCheckHook = true
	}
}

const (
	fallbackKindGit     = "git"
	fallbackKindRelease = "release"
)

// selectFallback returns the first fallback entry usable on this host: a
// release entry needs an asset for the host's OS and architecture, a git entry
// is always usable.
func selectFallback(fbs []module.Fallback, info platform.Info) (module.Fallback, bool) {
	for _, fb := range fbs {
		if fb.Type == "release" {
			if _, ok := fb.AssetFor(string(info.OS), info.Arch); !ok {
				continue
			}
		}
		return fb, true
	}
	return module.Fallback{}, false
}

func (e Engine) releaseContext() pkgmgr.ReleaseContext {
	return pkgmgr.ReleaseContext{VendorDir: e.vendorDir(), OS: string(e.Platform.OS), Arch: e.Platform.Arch}
}

// planFallback selects the module's fallback. A git fallback with a missing
// clone is queued as a fresh install; an existing clone is queued as an update
// when the manifest pins a ref and the clone was not recorded as built from it.
// A release fallback is planned by planRelease. The lockfile supplies the
// recorded ref and kind, so planning never probes anything but the filesystem.
func planFallback(mp *ModulePlan, e Engine, prev lockfile.ModuleState) {
	fb, ok := selectFallback(mp.Manifest.Packages.Fallback, e.Platform)
	if !ok {
		mp.DegradedReason = fmt.Sprintf("no fallback is available for %s/%s", e.Platform.OS, e.Platform.Arch)
		return
	}
	mp.UsesFallback = true
	mp.Fallback = fb
	if fb.Type == "release" {
		planRelease(mp, e, fb, prev)
		return
	}
	satisfied, _ := pkgmgr.FallbackSatisfied(fb, pkgmgr.FallbackContext{
		VendorDir: e.Platform.ConfigDir + "/vendor",
		Platform:  string(e.Platform.OS),
	})
	switch {
	case !satisfied:
		mp.MissingPackages = append(mp.MissingPackages, PackagePlan{Name: fb.Repo, Manager: "git"})
	case fb.Ref != "" && prev.FallbackRef != fb.Ref && prev.FallbackSkippedRef != fb.Ref:
		mp.MissingPackages = append(mp.MissingPackages, PackagePlan{
			Name: fb.Repo, Manager: "git", Update: true, From: prev.FallbackRef, To: fb.Ref,
		})
	}
}

// recordedChecksumHolds reports whether the checksum recorded for an installed
// release binary is the one the manifest pins for this host. An empty record (a
// Cargo build adopted as the release binary) holds, since there is nothing to
// compare; a different one means the pin moved after the install, so the
// binary is installed again from the pinned asset.
func recordedChecksumHolds(fb module.Fallback, e Engine, prev lockfile.ModuleState) bool {
	if prev.FallbackSHA256 == "" {
		return true
	}
	asset, ok := fb.AssetFor(string(e.Platform.OS), e.Platform.Arch)
	return ok && asset.SHA256 == prev.FallbackSHA256
}

// planRelease queues the release binary when it is missing or was installed
// from another ref or another kind of fallback. The recorded kind of a
// lockfile entry that has a ref but no kind is git.
func planRelease(mp *ModulePlan, e Engine, fb module.Fallback, prev lockfile.ModuleState) {
	recordedKind := prev.FallbackKind
	if recordedKind == "" && prev.FallbackRef != "" {
		recordedKind = fallbackKindGit
	}
	pp := PackagePlan{Name: fb.Bin, Manager: "release", To: fb.Ref}
	switch {
	case !pkgmgr.ReleaseInstalled(fb, e.releaseContext()):
		mp.MissingPackages = append(mp.MissingPackages, pp)
	case recordedKind == fallbackKindRelease && prev.FallbackRef == fb.Ref && recordedChecksumHolds(fb, e, prev):
		// Settled: this ref is installed from the asset the manifest pins.
	default:
		pp.Update, pp.From = true, prev.FallbackRef
		mp.MissingPackages = append(mp.MissingPackages, pp)
	}
}

// describePackage names a planned package for the plan output.
func describePackage(pp PackagePlan) string {
	if pp.Manager == "release" {
		if !pp.Update {
			return fmt.Sprintf("%s (release binary %s)", pp.Name, pp.To)
		}
		from := pp.From
		if from == "" {
			from = "unrecorded"
		}
		return fmt.Sprintf("%s (release update %s → %s)", pp.Name, from, pp.To)
	}
	if pp.Update {
		from := pp.From
		if from == "" {
			from = "unrecorded"
		}
		return fmt.Sprintf("%s (git update %s → %s, rebuild)", pp.Name, from, pp.To)
	}
	return pp.Name + " (" + pp.Manager + ")"
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
					names[i] = describePackage(pp)
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
