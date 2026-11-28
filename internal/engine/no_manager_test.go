package engine_test

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/JtheGunner/omnishell/internal/engine"
	"github.com/JtheGunner/omnishell/internal/lockfile"
	"github.com/JtheGunner/omnishell/internal/pkgmgr"
)

func noManagerEngine(t *testing.T) engine.Engine {
	t.Helper()
	e := testEngine(t, nil)
	e.Manager = nil
	e.ManagerOK = false
	return e
}

func planNoManager(t *testing.T, e engine.Engine, id string) engine.ModulePlan {
	t.Helper()
	cfg := config.Config{
		Omnishell: config.OmnishellSection{Version: 1, Shells: []string{"bash"}},
		Modules:   map[string]config.ModuleConfig{id: {Enabled: true}},
	}
	p, err := engine.ComputePlan(e, cfg, lockfile.Lock{Modules: map[string]lockfile.ModuleState{}}, false)
	if err != nil {
		t.Fatal(err)
	}
	return p.Modules[id]
}

// A module with no package entries and a fallback does not need a package
// manager, so the plan must queue the fallback instead of dropping the module
// silently.
func TestComputePlanPlansFallbackOnlyModuleWithoutManager(t *testing.T) {
	mp := planNoManager(t, noManagerEngine(t), "cargo-built")
	if mp.DegradedReason != "" {
		t.Fatalf("DegradedReason = %q, want none", mp.DegradedReason)
	}
	if !mp.UsesFallback || mp.Fallback.Type != "git" {
		t.Fatalf("fallback not selected: %+v", mp)
	}
	if len(mp.MissingPackages) != 1 || mp.MissingPackages[0].Manager != "git" {
		t.Fatalf("MissingPackages = %+v, want one git entry", mp.MissingPackages)
	}
	if !mp.PackagesPlanned {
		t.Fatal("PackagesPlanned = false: the lockfile would drop the recorded fallback ref")
	}
	if mp.Action != engine.ActionInstall {
		t.Fatalf("Action = %v, want install", mp.Action)
	}
}

// A module that lists manager packages stays degraded without a manager: the
// fallback only stands in for packages the manager cannot provide.
func TestComputePlanDegradesPackageModuleWithoutManager(t *testing.T) {
	mp := planNoManager(t, noManagerEngine(t), "fzf")
	if mp.DegradedReason != "no package manager detected" {
		t.Fatalf("DegradedReason = %q, want %q", mp.DegradedReason, "no package manager detected")
	}
	if mp.UsesFallback || len(mp.MissingPackages) != 0 {
		t.Fatalf("fzf must not plan anything without a manager: %+v", mp)
	}
}

// A module with neither packages nor a fallback has nothing to install and
// stays a plain snippet module without a manager.
func TestComputePlanLeavesSnippetOnlyModuleAloneWithoutManager(t *testing.T) {
	mp := planNoManager(t, noManagerEngine(t), "history")
	if mp.DegradedReason != "" || mp.UsesFallback || len(mp.MissingPackages) != 0 {
		t.Fatalf("snippet-only module changed: %+v", mp)
	}
}

func noManagerApply(t *testing.T, id string, runner pkgmgr.Runner) (engine.Result, error) {
	t.Helper()
	home := t.TempDir()
	var out bytes.Buffer
	e := applyEngine(t, home, nil, &out)
	e.Manager = nil
	e.ManagerOK = false
	e.Runner = runner
	cfgPath := filepath.Join(home, ".config", "omnishell", "config.toml")
	lockPath := filepath.Join(home, ".config", "omnishell", "state.lock.json")
	writeConfig(t, cfgPath, "[omnishell]\nversion=1\nshells=[\"bash\"]\n[modules."+id+"]\nenabled=true\n")
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	return e.Apply(cfg, cfgPath, lockPath, engine.ApplyOptions{Yes: true})
}

func TestApplyInstallsFallbackOnlyModuleWithoutManager(t *testing.T) {
	runner := &pkgmgr.MockRunner{Responses: map[string]pkgmgr.MockResponse{
		"cargo --version": {Out: []byte("cargo 1.90.0 (abc 2026-01-01)\n")},
		"cmake --version": {Out: []byte("cmake version 3.30.0\n")},
	}}
	res, err := noManagerApply(t, "cargo-built", runner)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got := moduleResult(t, res, "cargo-built").Status; got != "applied" {
		t.Fatalf("status = %q, want applied", got)
	}
	cloned := false
	for _, c := range runner.Calls {
		if strings.Contains(c, " clone ") {
			cloned = true
		}
	}
	if !cloned {
		t.Fatalf("fallback not attempted; runner calls = %v", runner.Calls)
	}
}

// Without a manager a missing build prerequisite must degrade the module and
// fail the run, never end silently.
func TestApplyDegradesFallbackOnlyModuleWithoutManagerWhenPrerequisiteMissing(t *testing.T) {
	runner := &pkgmgr.MockRunner{Responses: map[string]pkgmgr.MockResponse{
		"cargo --version": {Out: []byte("cargo 1.90.0 (abc 2026-01-01)\n")},
		"cmake --version": {Err: errors.New("exec: \"cmake\": executable file not found in $PATH")},
	}}
	res, err := noManagerApply(t, "cargo-built", runner)
	if !errors.Is(err, engine.ErrDegraded) {
		t.Fatalf("err = %v, want ErrDegraded", err)
	}
	mr := moduleResult(t, res, "cargo-built")
	if mr.Status != "degraded" || !strings.Contains(mr.Note, "cmake (not found)") {
		t.Fatalf("result = %+v, want degraded naming cmake", mr)
	}
}
