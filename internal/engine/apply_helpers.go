package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/JtheGunner/omnishell/internal/atomicfile"
	"github.com/JtheGunner/omnishell/internal/backup"
	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/JtheGunner/omnishell/internal/initfile"
	"github.com/JtheGunner/omnishell/internal/lockfile"
	"github.com/JtheGunner/omnishell/internal/module"
	"github.com/JtheGunner/omnishell/internal/pkgmgr"
	"github.com/JtheGunner/omnishell/internal/rcfile"
	"github.com/JtheGunner/omnishell/internal/render"
)

// initPath returns the tool-managed init file path for a shell.
func (e Engine) initPath(shell string) string {
	return filepath.Join(e.Platform.ConfigDir, "init."+shell)
}

// rcPath returns the rc file path for a managed shell, or "" if none is known.
func (e Engine) rcPath(shell string) string {
	for _, s := range e.Platform.Shells {
		if s.Name == shell {
			return s.RCPath
		}
	}
	return ""
}

// vendorDir is where git-fallback packages are cloned.
func (e Engine) vendorDir() string {
	return filepath.Join(e.Platform.ConfigDir, "vendor")
}

// HomeRelative rewrites a path under home to start with "$HOME"; other paths are
// returned verbatim. Exported so the CLI (`omnishell init`) and the engine
// (`apply`'s ensureRC + the lock) emit byte-identical rc source lines — any
// divergence makes the rc marker block churn on every apply.
func HomeRelative(path, home string) string {
	if home == "" {
		return path
	}
	if path == home {
		return "$HOME"
	}
	if strings.HasPrefix(path, home+string(filepath.Separator)) {
		return "$HOME" + path[len(home):]
	}
	return path
}

// homeRelative is the Engine-bound convenience wrapper around HomeRelative.
func (e Engine) homeRelative(path string) string {
	return HomeRelative(path, e.Platform.HomeDir)
}

// renderContext is the data/helper set for a module template.
func (e Engine) renderContext(mp ModulePlan, shell string, active map[string]bool) render.Context {
	return render.Context{
		Options:   mp.Options,
		Platform:  string(e.Platform.OS),
		Shell:     shell,
		VendorDir: e.vendorDir(),
		ConfigDir: e.Platform.ConfigDir,
		Bin:       map[string]string{},
		Active:    active,
	}
}

// activeSet is the set of non-degraded, non-removed module ids.
func activeSet(plan Plan, degraded map[string]string) map[string]bool {
	out := map[string]bool{}
	for _, id := range plan.Order {
		mp, ok := plan.Modules[id]
		if !ok || mp.Action == ActionRemove {
			continue
		}
		if degraded[id] != "" {
			continue
		}
		out[id] = true
	}
	return out
}

// plannedDegraded returns id -> DegradedReason for every module the plan alone
// marks degraded while it still has a snippet to emit for some managed shell
// (the len(Shells)==0 "no snippet anywhere" case is handled by the skip path).
// Apply, Doctor and initOrRCDrift all seed their `degraded` map from this so the
// three of them compute initfile.ContentHash over the identical section set —
// otherwise the hash Apply stores and the hash the other two recompute can
// never agree, and a stably-degraded module rewrites init.<shell> forever.
func plannedDegraded(plan Plan) map[string]string {
	out := map[string]string{}
	for _, id := range plan.Order {
		mp := plan.Modules[id]
		if mp.DegradedReason != "" && len(mp.Shells) > 0 {
			out[id] = mp.DegradedReason
		}
	}
	return out
}

// initOrRCDrift reports whether, for any managed shell, the would-be init file
// hash differs from the lock, the on-disk init file exists but was hand-edited,
// or the rc file lacks the marker block. Used only for the idempotent no-op
// short-circuit: any of these means apply has real work to do (including a
// --force repair of an out-of-band edit that left the marker sections intact),
// so it must not bail out early.
func (e Engine) initOrRCDrift(plan Plan, lock lockfile.Lock) bool {
	degraded := plannedDegraded(plan)
	rendered := e.renderAll(plan, degraded)
	for _, shell := range plan.ManagedShells {
		sections := e.buildSections(plan, rendered, degraded, shell)
		if initfile.ContentHash(sections) != lock.InitFiles[shell].ContentHash {
			return true
		}
		if data, err := os.ReadFile(e.initPath(shell)); err == nil &&
			e.initFileHandEdited(shell, string(data), sections) {
			return true
		}
		rcPath := e.rcPath(shell)
		if rcPath == "" {
			continue
		}
		data, err := os.ReadFile(rcPath)
		if err != nil || !rcfile.BlockPresent(string(data)) {
			return true
		}
	}
	return false
}

// installPackages walks plan.Order and satisfies MissingPackages, marking a
// module degraded when a package cannot be installed.
func (e Engine) installPackages(plan Plan, degraded map[string]string,
	vendorPaths map[string][]string, installedNow map[string]map[string]bool) {
	for _, id := range plan.Order {
		mp, ok := plan.Modules[id]
		if !ok || mp.Action == ActionRemove || len(mp.MissingPackages) == 0 {
			continue
		}

		var real []string
		var fallback bool
		for _, pp := range mp.MissingPackages {
			if pp.Manager == "git" {
				fallback = true
				continue
			}
			real = append(real, pp.Name)
		}

		if len(real) > 0 {
			if !e.ManagerOK {
				degraded[id] = "no package manager to install " + strings.Join(real, ", ")
				continue
			}
			_, _ = fmt.Fprintf(e.Stdout, "installing %s via %s\n", strings.Join(real, ", "), e.Manager.Name())
			if e.Manager.NeedsSudo() {
				_, _ = fmt.Fprintln(e.Stdout, "(sudo may prompt for your password)")
			}
			if err := e.Manager.Install(real); err != nil {
				degraded[id] = "package install failed: " + err.Error()
				continue
			}
			for _, name := range real {
				if ok, _ := e.Manager.IsInstalled(name); ok {
					mark(installedNow, id, name)
				} else {
					degraded[id] = "package still missing after install: " + name
				}
			}
		}

		if fallback && mp.UsesFallback && len(mp.Manifest.Packages.Fallback) > 0 {
			dest, err := pkgmgr.InstallGitFallback(mp.Manifest.Packages.Fallback[0], pkgmgr.FallbackContext{
				VendorDir: e.vendorDir(),
				Platform:  string(e.Platform.OS),
			}, e.Runner)
			if err != nil {
				degraded[id] = "fallback install failed: " + err.Error()
				continue
			}
			vendorPaths[id] = append(vendorPaths[id], dest)
		}
	}
}

func mark(m map[string]map[string]bool, id, key string) {
	if m[id] == nil {
		m[id] = map[string]bool{}
	}
	m[id][key] = true
}

// hookEnv is the environment contract passed to check/install/remove hooks.
func (e Engine) hookEnv(mp ModulePlan, shell string) map[string]string {
	env := map[string]string{
		"OMNISHELL_VENDOR_DIR": e.vendorDir(),
		"OMNISHELL_CONFIG_DIR": e.Platform.ConfigDir,
		"OMNISHELL_PLATFORM":   string(e.Platform.OS),
		"OMNISHELL_SHELL":      shell,
	}
	for k, v := range mp.Options {
		env["OMNISHELL_OPT_"+strings.ToUpper(k)] = fmt.Sprintf("%v", v)
	}
	return env
}

// runHookScript materialises hooks/<name>.sh to a 0o700 temp file and runs it via
// e.Runner with the hook env vars. The temp file is always removed.
func (e Engine) runHookScript(mod module.Module, name string, env map[string]string) error {
	body, err := mod.ReadHook(name)
	if err != nil {
		return fmt.Errorf("read %s hook: %w", name, err)
	}
	f, err := os.CreateTemp("", "omnishell-"+name+"-*.sh")
	if err != nil {
		return fmt.Errorf("materialise %s hook: %w", name, err)
	}
	tmp := f.Name()
	defer func() { _ = os.Remove(tmp) }() // best-effort cleanup of the materialised hook script
	if _, err := f.Write(body); err != nil {
		_ = f.Close()
		return fmt.Errorf("write %s hook: %w", name, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close %s hook: %w", name, err)
	}
	if err := os.Chmod(tmp, 0o700); err != nil {
		return fmt.Errorf("chmod %s hook: %w", name, err)
	}

	// pkgmgr.Runner carries no env parameter; expose the contract via the
	// process environment for the duration of the call.
	restore := setEnv(env)
	defer restore()

	if _, err := e.Runner.Run(tmp); err != nil {
		return err
	}
	return nil
}

func setEnv(env map[string]string) func() {
	prev := map[string]*string{}
	for k, v := range env {
		if old, ok := os.LookupEnv(k); ok {
			s := old
			prev[k] = &s
		} else {
			prev[k] = nil
		}
		_ = os.Setenv(k, v)
	}
	return func() {
		for k, old := range prev {
			if old == nil {
				_ = os.Unsetenv(k)
			} else {
				_ = os.Setenv(k, *old)
			}
		}
	}
}

// runCheckHooks runs check.sh (and install.sh on failure) for modules that use a
// check hook, degrading the module when the check ultimately fails.
func (e Engine) runCheckHooks(plan Plan, degraded map[string]string) {
	for _, id := range plan.Order {
		mp, ok := plan.Modules[id]
		if !ok || mp.Action == ActionRemove || !mp.UsesCheckHook || degraded[id] != "" {
			continue
		}
		mod, ok := e.Registry.Get(id)
		if !ok {
			continue
		}
		env := e.hookEnv(mp, "")
		if e.runHookScript(mod, "check", env) == nil {
			continue
		}
		if mod.HasHook("install") {
			_ = e.runHookScript(mod, "install", env)
			if e.runHookScript(mod, "check", env) == nil {
				continue
			}
		}
		degraded[id] = "check hook failed"
	}
}

// renderAll renders every (module, shell) pair once. A render error sets
// degraded[id] and omits every key for that id.
func (e Engine) renderAll(plan Plan, degraded map[string]string) map[[2]string]string {
	active := activeSet(plan, degraded)
	out := map[[2]string]string{}
	for _, id := range plan.Order {
		mp, ok := plan.Modules[id]
		if !ok || mp.Action == ActionRemove || degraded[id] != "" {
			continue
		}
		mod, ok := e.Registry.Get(id)
		if !ok {
			continue
		}
		failed := false
		for _, shell := range mp.Shells {
			body, has, err := mod.Template(shell)
			if err != nil {
				failed = true
				degraded[id] = "template read failed: " + err.Error()
				break
			}
			if !has {
				continue
			}
			snippet, rerr := render.Render(body, e.renderContext(mp, shell, active))
			if rerr != nil {
				failed = true
				degraded[id] = "render failed: " + rerr.Error()
				break
			}
			out[[2]string{id, shell}] = snippet
		}
		if failed {
			for _, shell := range mp.Shells {
				delete(out, [2]string{id, shell})
			}
		}
	}
	return out
}

// buildSections assembles the ordered sections for one shell from the rendered
// snippets, excluding degraded and removed modules.
func (e Engine) buildSections(plan Plan, rendered map[[2]string]string,
	degraded map[string]string, shell string) []initfile.Section {
	var sections []initfile.Section
	for _, id := range plan.Order {
		if degraded[id] != "" {
			continue
		}
		mp, ok := plan.Modules[id]
		if !ok || mp.Action == ActionRemove {
			continue
		}
		if !contains(mp.Shells, shell) {
			continue
		}
		body, ok := rendered[[2]string{id, shell}]
		if !ok {
			continue
		}
		sections = append(sections, initfile.Section{
			ID:      id,
			Version: mp.Manifest.Module.Version,
			Body:    body,
		})
	}
	return sections
}

// initFileHandEdited reports whether the on-disk init file was edited by hand.
// It fires when initfile.DetectHandEdit flags in-marker tampering, or when the
// file no longer matches the exact form Build would produce for its own declared
// sections (which catches edits outside the marker blocks). A normal config
// change leaves a pristine file that still matches, so it never trips the guard.
func (e Engine) initFileHandEdited(shell, existing string, newSections []initfile.Section) bool {
	if strings.TrimSpace(existing) == "" {
		return false // an empty / whitespace-only file was never a real init file — safe to overwrite
	}
	if edited, _ := initfile.DetectHandEdit(existing, newSections); edited {
		return true
	}
	return !initfile.MatchesGeneratedForm(shell, existing)
}

// ensureRC guarantees the marker block in a shell's rc file, backing it up first
// and only rewriting when the block actually changed.
func (e Engine) ensureRC(shell, initPath string, bk backup.Session, lock *lockfile.Lock) error {
	rcPath := e.rcPath(shell)
	if rcPath == "" {
		return nil
	}
	var content string
	perm := os.FileMode(0o644)
	if fi, err := os.Stat(rcPath); err == nil {
		perm = fi.Mode().Perm()
	}
	if b, err := os.ReadFile(rcPath); err == nil {
		content = string(b)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read rc file %s: %w", rcPath, err)
	}
	if _, err := bk.Save(rcPath); err != nil {
		return err
	}
	updated, changed := rcfile.EnsureBlock(content, shell, e.homeRelative(initPath))
	if changed {
		if err := atomicfile.WriteFile(rcPath, []byte(updated), perm); err != nil {
			return fmt.Errorf("write rc file %s: %w", rcPath, err)
		}
	}
	if lock.RCFiles == nil {
		lock.RCFiles = map[string]lockfile.RCState{}
	}
	lock.RCFiles[shell] = lockfile.RCState{
		Path:         e.homeRelative(rcPath),
		BlockPresent: rcfile.BlockPresent(updated),
	}
	return nil
}

// rebuildLock returns a NEW Lock: previous metadata carried, ActionRemove
// modules dropped (only when they were really applied before), and every other
// module upserted from the plan + install results.
func (e Engine) rebuildLock(_ config.Config, plan Plan, prev lockfile.Lock,
	degraded map[string]string, vendorPaths map[string][]string,
	installedNow map[string]map[string]bool) lockfile.Lock {

	nl := lockfile.Lock{
		Schema:           lockfile.SchemaVersion,
		OmnishellVersion: prev.OmnishellVersion,
		LastApply:        prev.LastApply,
		Platform:         prev.Platform,
		PackageManager:   prev.PackageManager,
		Modules:          map[string]lockfile.ModuleState{},
		InitFiles:        map[string]lockfile.FileState{},
		RCFiles:          map[string]lockfile.RCState{},
	}
	for k, v := range prev.InitFiles {
		nl.InitFiles[k] = v
	}
	for k, v := range prev.RCFiles {
		nl.RCFiles[k] = v
	}

	removed := map[string]bool{}
	for id, mp := range plan.Modules {
		if mp.Action != ActionRemove {
			continue
		}
		p := prev.Modules[id]
		if len(p.ShellsRendered) > 0 && p.Status != "disabled" {
			removed[id] = true
		}
	}

	for _, id := range plan.Order {
		mp := plan.Modules[id]
		if mp.Action == ActionRemove {
			continue
		}
		prevMod := prev.Modules[id]

		status := "ok"
		if degraded[id] != "" {
			status = "degraded"
		}

		var shellsRendered []string
		if degraded[id] == "" {
			shellsRendered = append(shellsRendered, mp.Shells...)
		}

		vps := vendorPaths[id]
		if vps == nil {
			vps = prevMod.VendorPaths
		}

		nl.Modules[id] = lockfile.ModuleState{
			ModuleVersion:  mp.Manifest.Module.Version,
			Enabled:        true,
			OptionsHash:    mp.OptionsHash,
			ShellsRendered: shellsRendered,
			Packages:       mergePackages(mp, plan, prevMod, installedNow[id]),
			VendorPaths:    vps,
			Status:         status,
		}
	}

	// Carry forward modules the plan does not touch (disabled entries, and
	// "removals" that were never really applied).
	for id, st := range prev.Modules {
		if _, active := nl.Modules[id]; active {
			continue
		}
		if removed[id] {
			continue
		}
		nl.Modules[id] = st
	}

	return nl
}

// mergePackages combines the manifest's package list for the active manager with
// any previously-recorded packages, preserving installed_by_omnishell truth.
func mergePackages(mp ModulePlan, plan Plan, prevMod lockfile.ModuleState,
	installedNow map[string]bool) []lockfile.PackageState {

	prevInstalled := map[string]bool{}
	for _, p := range prevMod.Packages {
		prevInstalled[p.Name] = p.InstalledByOmnishell
	}

	var out []lockfile.PackageState
	seen := map[string]bool{}
	add := func(name, mgr string) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		out = append(out, lockfile.PackageState{
			Name:                 name,
			Manager:              mgr,
			InstalledByOmnishell: prevInstalled[name] || (installedNow != nil && installedNow[name]),
		})
	}

	if plan.ManagerAvailable {
		for _, name := range mp.Manifest.Packages.ForManager(plan.PackageManager) {
			add(name, plan.PackageManager)
		}
	}
	for _, pp := range mp.MissingPackages {
		add(pp.Name, pp.Manager)
	}
	for _, p := range prevMod.Packages {
		add(p.Name, p.Manager)
	}
	return out
}

// summarise turns the plan + degraded set into per-module results.
func summarise(plan Plan, degraded map[string]string) []ModuleResult {
	var out []ModuleResult
	for _, id := range plan.Order {
		mp := plan.Modules[id]
		mr := ModuleResult{ID: id, Action: mp.Action}
		switch {
		case degraded[id] != "":
			mr.Status = "degraded"
			mr.Note = degraded[id]
		case mp.Action == ActionRemove:
			mr.Status = "removed"
		case mp.Action == ActionUnchanged:
			mr.Status = "unchanged"
		case mp.Action == ActionSkip:
			mr.Status = "skipped"
		default:
			mr.Status = "applied"
		}
		out = append(out, mr)
	}

	var removals, skips []string
	for id, mp := range plan.Modules {
		if contains(plan.Order, id) {
			continue
		}
		switch mp.Action {
		case ActionRemove:
			removals = append(removals, id)
		case ActionSkip:
			skips = append(skips, id)
		}
	}
	sort.Strings(removals)
	for _, id := range removals {
		out = append(out, ModuleResult{ID: id, Action: ActionRemove, Status: "removed"})
	}
	sort.Strings(skips)
	for _, id := range skips {
		out = append(out, ModuleResult{ID: id, Action: ActionSkip, Status: "skipped", Note: plan.Modules[id].Reason})
	}
	return out
}
