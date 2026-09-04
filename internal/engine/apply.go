package engine

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/JtheGunner/omnishell/internal/atomicfile"
	"github.com/JtheGunner/omnishell/internal/backup"
	"github.com/JtheGunner/omnishell/internal/buildinfo"
	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/JtheGunner/omnishell/internal/initfile"
	"github.com/JtheGunner/omnishell/internal/lockfile"
	"github.com/JtheGunner/omnishell/internal/rcfile"
)

// Sentinel errors the CLI maps to specific exit codes.
var (
	// ErrAborted is returned (not printed) when Prompt answers no.
	ErrAborted = errors.New("aborted by user")
	// ErrHandEdited is wrapped when an init file was hand-edited and --force was not given.
	ErrHandEdited = errors.New("init file has been edited by hand")
	// ErrDegraded is returned when at least one module ended up degraded.
	ErrDegraded = errors.New("one or more modules are degraded")
)

// ApplyOptions are the flags of `omnishell apply`.
type ApplyOptions struct {
	DryRun, Yes, NoPackages, Force bool
}

// ModuleResult is the per-module outcome of an apply.
type ModuleResult struct {
	ID     string
	Action ModuleAction
	Status string // applied | degraded | skipped | removed | unchanged
	Note   string
}

// Result is the outcome of Apply.
type Result struct {
	DryRun           bool
	PlanText         string
	Modules          []ModuleResult
	InitFilesWritten []string
	BackupDir        string
	Changed          bool
}

// Apply brings the system to the desired state described by cfg. It loads the
// lock from lockPath itself and writes it back on success. Nothing is written to
// disk before the hand-edit guard passes; every rc/init write is preceded by a
// backup and performed atomically.
func (e Engine) Apply(cfg config.Config, cfgPath, lockPath string, opts ApplyOptions) (Result, error) {
	lock, _, err := lockfile.Load(lockPath)
	if err != nil {
		return Result{}, err
	}

	plan, err := ComputePlan(e, cfg, lock, opts.NoPackages)
	if err != nil {
		return Result{}, err // a ConfigError propagates unchanged
	}

	for _, id := range plan.UnknownModules {
		_, _ = fmt.Fprintf(e.Stderr, "warning: unknown module %q in config (ignored)\n", id)
	}

	res := Result{PlanText: RenderPlan(plan)}

	if opts.DryRun {
		res.DryRun = true
		return res, nil
	}

	// Idempotent no-op: no planned changes and the on-disk init/rc files still
	// match the lock. A stably-degraded module is not "work to do" — it must not
	// force a rewrite — but the exit code still has to reflect it, so report the
	// degradation without touching disk.
	if !plan.HasChanges && !e.initOrRCDrift(plan, lock) {
		if pd := plannedDegraded(plan); len(pd) > 0 {
			res.Modules = summarise(plan, pd)
			return res, ErrDegraded
		}
		return res, nil
	}

	if !opts.Yes && e.Prompt != nil {
		_, _ = fmt.Fprintln(e.Stdout, res.PlanText)
		if !e.Prompt("Proceed?") {
			return res, ErrAborted
		}
	}

	vendorPaths := map[string][]string{}
	installedNow := map[string]map[string]bool{}

	// Carry a planner-detected degradation into the apply pass when the module
	// still has a snippet to emit but cannot be satisfied — today that means
	// "needs packages but no package manager was detected". Without this,
	// ComputePlan's DegradedReason was computed and then dropped, so apply wrote
	// a snippet that doctor would immediately flag as degraded, and the exit
	// code did not reflect the problem. The same helper seeds Doctor and
	// initOrRCDrift so all three hash the identical section set. The
	// len(Shells)==0 case ("no snippet for any managed shell") is left to the
	// existing skip path.
	degraded := plannedDegraded(plan)

	// Hand-edit guard — BEFORE installPackages (which may sudo-install, git
	// clone into vendor/, and run hooks/install.sh) and BEFORE runCheckHooks.
	// A hand-edited init file must abort the run without prompting for sudo or
	// mutating the system. The guard only needs the planned sections; package-
	// and hook-driven degradations merely drop sections, which the guard
	// tolerates (it fires on in-marker tampering or edits outside the blocks).
	guardDegraded := plannedDegraded(plan)
	guardRendered := e.renderAll(plan, guardDegraded)
	for _, shell := range plan.ManagedShells {
		initPath := e.initPath(shell)
		existing, rerr := os.ReadFile(initPath)
		if rerr != nil {
			continue
		}
		guardSections := e.buildSections(plan, guardRendered, guardDegraded, shell)
		if e.initFileHandEdited(shell, string(existing), guardSections) && !opts.Force {
			return res, fmt.Errorf("%w: %s (run with --force to overwrite)", ErrHandEdited, initPath)
		}
	}

	if !opts.NoPackages {
		e.installPackages(plan, degraded, vendorPaths, installedNow)
	}
	e.runCheckHooks(plan, degraded)

	rendered := e.renderAll(plan, degraded)

	sectionsByShell := map[string][]initfile.Section{}
	for _, shell := range plan.ManagedShells {
		sectionsByShell[shell] = e.buildSections(plan, rendered, degraded, shell)
	}

	// Write phase — only now touch disk.
	bk, err := backup.NewSession(e.Platform.ConfigDir, e.now())
	if err != nil {
		return res, fmt.Errorf("create backup session: %w", err)
	}
	res.BackupDir = bk.Dir
	if err := os.MkdirAll(bk.Dir, 0o755); err != nil {
		return res, fmt.Errorf("create backup dir %s: %w", bk.Dir, err)
	}

	newLock := e.rebuildLock(cfg, plan, lock, degraded, vendorPaths, installedNow)

	// A shell that dropped out of ManagedShells since the last apply (its
	// binary was removed from the host) leaves an orphaned init file and rc
	// marker block behind — nothing else ever looks at a shell outside
	// ManagedShells to notice or clean it up. Do that here, once, before
	// writing the still-managed shells below.
	for _, shell := range staleShells(lock, plan.ManagedShells) {
		if rcPath := e.rcPath(shell); rcPath != "" {
			if data, rerr := os.ReadFile(rcPath); rerr == nil && rcfile.BlockPresent(string(data)) {
				if _, err := bk.Save(rcPath); err != nil {
					return res, err
				}
				updated, _ := rcfile.RemoveBlock(string(data)) // 2nd value is `changed`, not an error; always true after BlockPresent
				if err := atomicfile.WriteFile(rcPath, []byte(updated), 0o644); err != nil {
					return res, fmt.Errorf("write rc file %s: %w", rcPath, err)
				}
				res.Changed = true
			}
		}
		initPath := e.initPath(shell)
		if _, serr := os.Stat(initPath); serr == nil {
			if _, err := bk.Save(initPath); err != nil {
				return res, err
			}
			if err := os.Remove(initPath); err != nil {
				return res, fmt.Errorf("remove init file %s: %w", initPath, err)
			}
			res.Changed = true
		}
		res.Modules = append(res.Modules, ModuleResult{ID: "shell:" + shell, Status: "removed", Note: "shell no longer present"})
	}

	for _, shell := range plan.ManagedShells {
		initPath := e.initPath(shell)
		sections := sectionsByShell[shell]
		if _, err := bk.Save(initPath); err != nil {
			return res, err
		}
		content := initfile.Build(shell, sections, e.now())
		if err := atomicfile.WriteFile(initPath, []byte(content), 0o644); err != nil {
			return res, fmt.Errorf("write init file %s: %w", initPath, err)
		}
		res.InitFilesWritten = append(res.InitFilesWritten, initPath)
		newLock.InitFiles[shell] = lockfile.FileState{
			Path:        e.homeRelative(initPath),
			ContentHash: initfile.ContentHash(sections),
		}
		if err := e.ensureRC(shell, initPath, bk, &newLock); err != nil {
			return res, err
		}
	}

	newLock.Schema = lockfile.SchemaVersion
	newLock.OmnishellVersion = buildinfo.Version
	newLock.LastApply = e.now().UTC().Format(time.RFC3339)
	newLock.Platform = string(e.Platform.OS)
	if plan.ManagerAvailable {
		newLock.PackageManager = plan.PackageManager
	}
	if err := newLock.Write(lockPath); err != nil {
		return res, fmt.Errorf("write lockfile %s: %w", lockPath, err)
	}

	res.Changed = true
	res.Modules = summarise(plan, degraded)
	for _, m := range res.Modules {
		if m.Status == "degraded" {
			return res, ErrDegraded
		}
	}
	return res, nil
}
