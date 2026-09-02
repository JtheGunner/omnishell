package engine

import (
	"fmt"
	"os"
	"strings"

	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/JtheGunner/omnishell/internal/lockfile"
	"github.com/JtheGunner/omnishell/internal/pkgmgr"
)

// Remove disables moduleID in the config on disk, then re-applies so its init
// section and lock entry are dropped (Apply already treats a lock-present-but-
// inactive module as ActionRemove). When purge is set it additionally, using the
// PRE-Apply lock entry: uninstalls every package it installed for the module,
// runs the module's remove.sh hook, and deletes vendored paths recorded in the
// lock. It returns the Result from Apply with moduleID amended to Status
// "removed" (Note mentions "purged" when purge is set).
//
// Nothing on disk is mutated when opts.DryRun is set: the config disable is
// skipped and Apply only renders the plan.
func (e Engine) Remove(cfg config.Config, cfgPath, lockPath, moduleID string, purge bool, opts ApplyOptions) (Result, error) {
	preLock, _, err := lockfile.Load(lockPath)
	if err != nil {
		return Result{}, err
	}

	_, inRegistry := e.Registry.Get(moduleID)
	_, inLock := preLock.Modules[moduleID]
	if !inRegistry && !inLock {
		return Result{}, fmt.Errorf("unknown module %q", moduleID)
	}

	// Capture the pre-Apply lock entry now; Apply rewrites the lock.
	preEntry := preLock.Modules[moduleID]

	if opts.DryRun {
		res, aerr := e.Apply(cfg, cfgPath, lockPath, opts)
		res.Modules = amendRemoved(res.Modules, moduleID, purge)
		return res, aerr
	}

	if err := config.SetEnabled(cfgPath, moduleID, false); err != nil {
		return Result{}, fmt.Errorf("disable %q in config: %w", moduleID, err)
	}

	reloaded, err := config.Load(cfgPath)
	if err != nil {
		return Result{}, err
	}

	res, err := e.Apply(reloaded, cfgPath, lockPath, opts)
	if err != nil {
		return res, err
	}

	if purge {
		if err := e.purgeModule(moduleID, preEntry); err != nil {
			return res, err
		}
	}

	res.Modules = amendRemoved(res.Modules, moduleID, purge)
	return res, nil
}

// purgeModule uninstalls omnishell-installed packages, runs the remove.sh hook,
// and deletes vendored paths for moduleID, using the pre-Apply lock entry.
func (e Engine) purgeModule(moduleID string, entry lockfile.ModuleState) error {
	if err := e.uninstallPackages(moduleID, entry); err != nil {
		return err
	}
	if err := e.runRemoveHook(moduleID); err != nil {
		return err
	}
	for _, vp := range entry.VendorPaths {
		if vp == "" {
			continue
		}
		fmt.Fprintf(e.Stdout, "removing vendored path %s\n", vp)
		if err := os.RemoveAll(vp); err != nil {
			return fmt.Errorf("remove vendored path %s: %w", vp, err)
		}
	}
	return nil
}

// uninstallPackages groups the module's installed_by_omnishell packages by their
// recorded manager and runs one uninstall command per manager via e.Runner,
// announcing the command (and the sudo prompt) first.
func (e Engine) uninstallPackages(moduleID string, entry lockfile.ModuleState) error {
	byManager := map[string][]string{}
	var order []string
	for _, p := range entry.Packages {
		if !p.InstalledByOmnishell || p.Manager == "" || p.Manager == "git" {
			continue
		}
		if _, seen := byManager[p.Manager]; !seen {
			order = append(order, p.Manager)
		}
		byManager[p.Manager] = append(byManager[p.Manager], p.Name)
	}

	for _, mgr := range order {
		names := byManager[mgr]
		argv := pkgmgr.UninstallArgv(mgr, names)
		if len(argv) == 0 {
			fmt.Fprintf(e.Stdout, "cannot uninstall %s: unknown package manager %q\n",
				strings.Join(names, ", "), mgr)
			continue
		}
		fmt.Fprintf(e.Stdout, "uninstalling %s via %s (%s)\n",
			strings.Join(names, ", "), mgr, strings.Join(argv, " "))
		if argv[0] == "sudo" {
			fmt.Fprintln(e.Stdout, "(sudo may prompt for your password)")
		}
		if _, err := e.Runner.Run(argv[0], argv[1:]...); err != nil {
			return fmt.Errorf("uninstall %s: %s: %w", moduleID, strings.Join(argv, " "), err)
		}
	}
	return nil
}

// runRemoveHook runs hooks/remove.sh for moduleID when the module is in the
// registry and declares one, reusing the check/install hook contract.
func (e Engine) runRemoveHook(moduleID string) error {
	mod, ok := e.Registry.Get(moduleID)
	if !ok || !mod.HasHook("remove") {
		return nil
	}
	fmt.Fprintf(e.Stdout, "running remove hook for %s\n", moduleID)
	env := e.hookEnv(ModulePlan{ID: moduleID}, "")
	if err := e.runHookScript(mod, "remove", env); err != nil {
		return fmt.Errorf("remove hook for %s: %w", moduleID, err)
	}
	return nil
}

// amendRemoved returns a NEW slice with moduleID's entry forced to Status
// "removed" (and a purge Note when purge), appending one if it is absent.
func amendRemoved(mods []ModuleResult, moduleID string, purge bool) []ModuleResult {
	note := ""
	if purge {
		note = "purged packages, remove hook, and vendored files"
	}
	out := make([]ModuleResult, 0, len(mods)+1)
	found := false
	for _, m := range mods {
		if m.ID == moduleID {
			found = true
			m.Status = "removed"
			if purge {
				m.Note = note
			}
		}
		out = append(out, m)
	}
	if !found {
		out = append(out, ModuleResult{
			ID:     moduleID,
			Action: ActionRemove,
			Status: "removed",
			Note:   note,
		})
	}
	return out
}
