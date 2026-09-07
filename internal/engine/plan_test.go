package engine_test

import (
	"errors"
	"testing"
	"time"

	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/JtheGunner/omnishell/internal/engine"
	"github.com/JtheGunner/omnishell/internal/graph"
	"github.com/JtheGunner/omnishell/internal/lockfile"
	"github.com/JtheGunner/omnishell/internal/module"
	"github.com/JtheGunner/omnishell/internal/pkgmgr"
	"github.com/JtheGunner/omnishell/internal/platform"
)

// testRegistry builds a Registry from an in-repo fixture dir.
// Fixtures live in internal/engine/testdata/modules/{completion,fzf,history}.
func testEngine(t *testing.T, mgr *pkgmgr.MockManager) engine.Engine {
	t.Helper()
	reg, err := module.LoadRegistry(nil, "testdata/modules")
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	return engine.Engine{
		Platform: platform.Info{
			OS:        platform.Linux,
			HomeDir:   t.TempDir(),
			ConfigDir: t.TempDir(),
			Shells: []platform.ShellInfo{
				{Name: "zsh", RCPath: "/x/.zshrc", Present: false},
				{Name: "bash", RCPath: "/x/.bashrc", Present: true},
			},
		},
		Registry:  reg,
		Manager:   mgr,
		ManagerOK: true,
		Runner:    &pkgmgr.MockRunner{},
		Now:       func() time.Time { return time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC) },
	}
}

func TestComputePlanFreshInstall(t *testing.T) {
	mgr := &pkgmgr.MockManager{NameV: "apt", DetectV: true, Installed: map[string]bool{}}
	e := testEngine(t, mgr)
	cfg := config.Config{
		Omnishell: config.OmnishellSection{Version: 1, Shells: []string{"bash"}},
		Modules: map[string]config.ModuleConfig{
			"completion": {Enabled: true},
			"fzf":        {Enabled: true, Options: map[string]any{"ctrl_r": true}},
		},
	}
	p, err := engine.ComputePlan(e, cfg, lockfile.Lock{Modules: map[string]lockfile.ModuleState{}}, false)
	if err != nil {
		t.Fatalf("ComputePlan: %v", err)
	}
	if len(p.Order) != 2 || p.Order[0] != "completion" || p.Order[1] != "fzf" {
		t.Fatalf("order = %v", p.Order)
	}
	if p.Modules["fzf"].Action != engine.ActionInstall {
		t.Fatalf("fzf action = %v", p.Modules["fzf"].Action)
	}
	if len(p.Modules["fzf"].MissingPackages) != 1 || p.Modules["fzf"].MissingPackages[0].Name != "fzf" {
		t.Fatalf("fzf missing packages = %+v", p.Modules["fzf"].MissingPackages)
	}
	if !p.HasChanges {
		t.Fatal("HasChanges should be true for a fresh install")
	}
}

// TestComputePlanSkipsPackagesForShellMismatchedModule covers a bug found by
// a live end-to-end run: a zsh-only module enabled on a bash-only host
// (managed shells = ["bash"]) was still planned to install its packages even
// though it can never render — leaving software on the system (and, for a
// package like zsh-syntax-highlighting, even a whole extra shell pulled in
// as a dependency) with nothing sourcing it. A module with no compatible
// managed shell must plan no packages at all.
func TestComputePlanSkipsPackagesForShellMismatchedModule(t *testing.T) {
	mgr := &pkgmgr.MockManager{NameV: "apt", DetectV: true, Installed: map[string]bool{}}
	e := testEngine(t, mgr)
	cfg := config.Config{
		Omnishell: config.OmnishellSection{Version: 1, Shells: []string{"bash"}},
		Modules:   map[string]config.ModuleConfig{"zshonly": {Enabled: true}},
	}
	p, err := engine.ComputePlan(e, cfg, lockfile.Lock{Modules: map[string]lockfile.ModuleState{}}, false)
	if err != nil {
		t.Fatalf("ComputePlan: %v", err)
	}
	mp := p.Modules["zshonly"]
	if mp.DegradedReason != "no snippet for any managed shell" {
		t.Fatalf("DegradedReason = %q, want the shell-mismatch reason", mp.DegradedReason)
	}
	if len(mp.MissingPackages) != 0 {
		t.Fatalf("MissingPackages = %+v, want none — packages must not be planned for a module that can't render", mp.MissingPackages)
	}
}

func TestComputePlanUnchangedWhenLockMatches(t *testing.T) {
	mgr := &pkgmgr.MockManager{NameV: "apt", DetectV: true, Installed: map[string]bool{"fzf": true}}
	e := testEngine(t, mgr)
	cfg := config.Config{
		Omnishell: config.OmnishellSection{Version: 1, Shells: []string{"bash"}},
		Modules:   map[string]config.ModuleConfig{"fzf": {Enabled: true, Options: map[string]any{"ctrl_r": true}}},
	}
	// Precompute the options hash the plan will expect.
	norm, _ := module.ValidateOptions(mustManifest(t, e, "fzf").Options, map[string]any{"ctrl_r": true})
	lock := lockfile.Lock{Modules: map[string]lockfile.ModuleState{
		"fzf": {ModuleVersion: mustManifest(t, e, "fzf").Module.Version, Enabled: true,
			OptionsHash: module.OptionsHash(norm), ShellsRendered: []string{"bash"}, Status: "ok"},
	}}
	p, err := engine.ComputePlan(e, cfg, lock, false)
	if err != nil {
		t.Fatal(err)
	}
	if p.Modules["fzf"].Action != engine.ActionUnchanged {
		t.Fatalf("fzf action = %v, want unchanged", p.Modules["fzf"].Action)
	}
	if p.HasChanges {
		t.Fatal("HasChanges should be false")
	}
}

func TestComputePlanRemovesDisabledModuleInLock(t *testing.T) {
	mgr := &pkgmgr.MockManager{NameV: "apt", DetectV: true, Installed: map[string]bool{}}
	e := testEngine(t, mgr)
	cfg := config.Config{Omnishell: config.OmnishellSection{Version: 1, Shells: []string{"bash"}}, Modules: map[string]config.ModuleConfig{}}
	lock := lockfile.Lock{Modules: map[string]lockfile.ModuleState{
		"completion": {ModuleVersion: "1.0.0", Enabled: true, ShellsRendered: []string{"bash"}, Status: "ok"},
	}}
	p, err := engine.ComputePlan(e, cfg, lock, false)
	if err != nil {
		t.Fatal(err)
	}
	if p.Modules["completion"].Action != engine.ActionRemove {
		t.Fatalf("completion action = %v, want remove", p.Modules["completion"].Action)
	}
	if !p.HasChanges {
		t.Fatal("HasChanges should be true (removal)")
	}
}

func TestComputePlanCollectsUnknownModules(t *testing.T) {
	mgr := &pkgmgr.MockManager{NameV: "apt", DetectV: true, Installed: map[string]bool{}}
	e := testEngine(t, mgr)
	cfg := config.Config{
		Omnishell: config.OmnishellSection{Version: 1, Shells: []string{"bash"}},
		Modules: map[string]config.ModuleConfig{
			"completion":    {Enabled: true},
			"nope":          {Enabled: true},
			"alsomissing":   {Enabled: true},
			"disabledghost": {Enabled: false},
		},
	}
	p, err := engine.ComputePlan(e, cfg, lockfile.Lock{Modules: map[string]lockfile.ModuleState{}}, false)
	if err != nil {
		t.Fatal(err)
	}
	got := p.UnknownModules
	if len(got) != 2 || got[0] != "alsomissing" || got[1] != "nope" {
		t.Fatalf("UnknownModules = %v, want [alsomissing nope] (sorted, disabled excluded)", got)
	}
}

func TestComputePlanSkipsModuleNotSupportedOnPlatform(t *testing.T) {
	mgr := &pkgmgr.MockManager{NameV: "apt", DetectV: true, Installed: map[string]bool{}}
	e := testEngine(t, mgr) // platform.Linux
	// completion's fixture manifest lists platforms = [macos, linux]; force a
	// macos-only view by swapping the platform on a copy.
	e.Platform.OS = platform.OS("plan9")
	cfg := config.Config{
		Omnishell: config.OmnishellSection{Version: 1, Shells: []string{"bash"}},
		Modules:   map[string]config.ModuleConfig{"completion": {Enabled: true}},
	}
	p, err := engine.ComputePlan(e, cfg, lockfile.Lock{Modules: map[string]lockfile.ModuleState{}}, false)
	if err != nil {
		t.Fatal(err)
	}
	mp, ok := p.Modules["completion"]
	if !ok || mp.Action != engine.ActionSkip {
		t.Fatalf("completion plan = %+v, want ActionSkip", mp)
	}
	if contains(p.Order, "completion") {
		t.Fatal("a skipped module must not be in the render order")
	}
	if p.HasChanges {
		t.Fatal("a skip is not a change")
	}
}

func contains(ss []string, v string) bool {
	for _, s := range ss {
		if s == v {
			return true
		}
	}
	return false
}

func TestComputePlanOptionValidationError(t *testing.T) {
	mgr := &pkgmgr.MockManager{NameV: "apt", DetectV: true}
	e := testEngine(t, mgr)
	cfg := config.Config{
		Omnishell: config.OmnishellSection{Version: 1, Shells: []string{"bash"}},
		Modules:   map[string]config.ModuleConfig{"fzf": {Enabled: true, Options: map[string]any{"bogus": 1}}},
	}
	_, err := engine.ComputePlan(e, cfg, lockfile.Lock{Modules: map[string]lockfile.ModuleState{}}, false)
	var ce engine.ConfigError
	if err == nil || !asConfigError(err, &ce) {
		t.Fatalf("err = %v, want engine.ConfigError", err)
	}
}

func TestComputePlanConflictingModules(t *testing.T) {
	mgr := &pkgmgr.MockManager{NameV: "apt", DetectV: true, Installed: map[string]bool{}}
	e := testEngine(t, mgr)
	cfg := config.Config{
		Omnishell: config.OmnishellSection{Version: 1, Shells: []string{"bash"}},
		Modules: map[string]config.ModuleConfig{
			"fzf":        {Enabled: true, Options: map[string]any{"ctrl_r": true}},
			"conflictor": {Enabled: true},
		},
	}
	_, err := engine.ComputePlan(e, cfg, lockfile.Lock{Modules: map[string]lockfile.ModuleState{}}, false)
	var ce engine.ConfigError
	if err == nil || !asConfigError(err, &ce) {
		t.Fatalf("err = %v, want engine.ConfigError", err)
	}
	var cfe graph.ConflictError
	if !errors.As(err, &cfe) || cfe.Module != "conflictor" || cfe.Conflicts != "fzf" {
		t.Fatalf("err = %v, want wrapped graph.ConflictError{conflictor, fzf}", err)
	}
}
