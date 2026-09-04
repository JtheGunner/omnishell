package engine

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/JtheGunner/omnishell/internal/initfile"
	"github.com/JtheGunner/omnishell/internal/lockfile"
	"github.com/JtheGunner/omnishell/internal/rcfile"
)

// Severity values a Finding may carry.
const (
	SeverityDrift  = "drift"
	SeverityNotice = "notice"
)

// Finding is a single issue Doctor detected.
type Finding struct {
	Severity string
	Code     string
	Message  string
}

// DoctorReport is the complete set of findings from one Doctor run.
type DoctorReport struct {
	Findings []Finding
}

// HasDrift reports whether any finding has drift severity.
func (r DoctorReport) HasDrift() bool {
	for _, f := range r.Findings {
		if f.Severity == SeverityDrift {
			return true
		}
	}
	return false
}

// Doctor inspects the on-disk state against cfg and the lock and reports drift.
// It is strictly read-only: no file is written and neither cfg nor the lock is
// mutated. A ConfigError from planning is returned to the caller (the CLI maps it
// to exit code 2) rather than recorded as a finding.
func (e Engine) Doctor(cfg config.Config, cfgPath, lockPath string) (DoctorReport, error) {
	lock, lockExists, err := lockfile.Load(lockPath)
	if err != nil {
		return DoctorReport{}, err
	}

	var rep DoctorReport
	add := func(severity, code, message string) {
		rep.Findings = append(rep.Findings, Finding{Severity: severity, Code: code, Message: message})
	}

	// 1. Lock file missing but the config enables at least one module.
	enabledIDs := enabledModuleIDs(cfg)
	if !lockExists && len(enabledIDs) > 0 {
		add(SeverityDrift, "never-applied",
			"no lock file found; omnishell apply has never run for this configuration")
	}

	// 2. Compute the plan. A ConfigError propagates unchanged.
	plan, err := ComputePlan(e, cfg, lock, false)
	if err != nil {
		return DoctorReport{}, err
	}

	// Seed the same planner-degraded set Apply uses, so the sections we hash
	// below exclude exactly what Apply excluded — otherwise a stably-degraded
	// module makes Doctor cry "initfile-stale" forever.
	degraded := plannedDegraded(plan)
	rendered := e.renderAll(plan, degraded)

	// 3. Per managed shell: init file missing / hand-edited / stale.
	for _, shell := range plan.ManagedShells {
		initPath := e.initPath(shell)
		sections := e.buildSections(plan, rendered, degraded, shell)

		data, rerr := os.ReadFile(initPath)
		if rerr != nil {
			add(SeverityDrift, "initfile-missing:"+shell,
				fmt.Sprintf("init file %s is missing; run omnishell apply", initPath))
			continue
		}
		content := string(data)
		if e.initFileHandEdited(shell, content, sections) {
			add(SeverityDrift, "initfile-edited:"+shell,
				fmt.Sprintf("init file %s has been edited by hand", initPath))
			continue
		}
		if initfile.ContentHash(sections) != lock.InitFiles[shell].ContentHash {
			add(SeverityDrift, "initfile-stale:"+shell,
				fmt.Sprintf("init file %s no longer matches the last apply; run omnishell apply", initPath))
		}
	}

	// 3b. Shells the lock still has files for but that are no longer
	// managed (e.g. the shell binary was removed from the host) — their init
	// file and rc marker block are now orphaned; apply cleans them up.
	for _, shell := range staleShells(lock, plan.ManagedShells) {
		add(SeverityDrift, "stale-shell:"+shell,
			fmt.Sprintf("shell %q is no longer managed but still has omnishell files; run omnishell apply to clean up", shell))
	}

	// 4. Per managed shell with a real rc path: marker block absent.
	for _, shell := range plan.ManagedShells {
		rcPath := e.rcPath(shell)
		if rcPath == "" {
			continue
		}
		data, rerr := os.ReadFile(rcPath)
		if rerr != nil || !rcfile.BlockPresent(string(data)) {
			add(SeverityDrift, "rc-block-missing:"+shell,
				fmt.Sprintf("rc file %s does not source the omnishell init file", rcPath))
		}
	}

	// 5. Per enabled module: missing packages / degraded.
	for _, id := range enabledIDs {
		mp, ok := plan.Modules[id]
		if !ok {
			continue
		}
		if len(mp.MissingPackages) > 0 {
			names := make([]string, len(mp.MissingPackages))
			for i, pp := range mp.MissingPackages {
				names[i] = pp.Name
			}
			add(SeverityDrift, "packages-missing:"+id,
				fmt.Sprintf("module %q is missing packages: %s", id, strings.Join(names, ", ")))
		}
		if mp.DegradedReason != "" {
			add(SeverityDrift, "module-degraded:"+id,
				fmt.Sprintf("module %q is degraded: %s", id, mp.DegradedReason))
		}
	}

	// 6. Orphan lock entries: a module recorded in the lock but gone from config.
	orphanIDs := make([]string, 0, len(lock.Modules))
	for id := range lock.Modules {
		if _, inCfg := cfg.Modules[id]; !inCfg {
			orphanIDs = append(orphanIDs, id)
		}
	}
	sort.Strings(orphanIDs)
	for _, id := range orphanIDs {
		add(SeverityDrift, "orphan-lock-entry:"+id,
			fmt.Sprintf("lock file records module %q which is no longer in the config", id))
	}

	// 7. Registry overrides: a user module shadows a built-in. Notice, not drift.
	for _, id := range e.Registry.Overrides() {
		add(SeverityNotice, "module-override:"+id,
			fmt.Sprintf("module %q shadows a built-in module", id))
	}

	// 7b. Enabled ids in the config that no registered module provides. Notice.
	for _, id := range plan.UnknownModules {
		add(SeverityNotice, "unknown-module:"+id,
			fmt.Sprintf("config enables module %q but no module provides it", id))
	}

	// 7c. User module dirs that were skipped because they could not be loaded.
	for _, m := range e.Registry.Malformed() {
		add(SeverityNotice, "malformed-module:"+m, "user module dir skipped: "+m)
	}

	// 8. Per enabled module: config changed since the last apply.
	for _, id := range enabledIDs {
		mp, ok := plan.Modules[id]
		if !ok {
			continue
		}
		// A stably-degraded module is intentionally excluded from the init
		// files; module-degraded:<id> is the real signal, so don't also nag
		// pending-apply for the same module.
		if mp.Action == ActionUpdate && degraded[id] == "" {
			add(SeverityDrift, "pending-apply:"+id,
				fmt.Sprintf("module %q has configuration changes not yet applied", id))
		}
	}

	return rep, nil
}

// enabledModuleIDs returns the sorted ids of every enabled module in cfg.
func enabledModuleIDs(cfg config.Config) []string {
	ids := make([]string, 0, len(cfg.Modules))
	for id, mc := range cfg.Modules {
		if mc.Enabled {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}
