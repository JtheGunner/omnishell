package engine_test

import (
	"testing"
	"time"

	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/JtheGunner/omnishell/internal/engine"
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
