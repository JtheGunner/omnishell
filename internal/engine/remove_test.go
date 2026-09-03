package engine_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/JtheGunner/omnishell/internal/engine"
	"github.com/JtheGunner/omnishell/internal/lockfile"
	"github.com/JtheGunner/omnishell/internal/module"
	"github.com/JtheGunner/omnishell/internal/pkgmgr"
	"github.com/JtheGunner/omnishell/internal/platform"
)

func TestRemoveDropsSectionAndDisablesInConfig(t *testing.T) {
	home := t.TempDir()
	var out bytes.Buffer
	mgr := &pkgmgr.MockManager{NameV: "apt", DetectV: true, Installed: map[string]bool{}}
	e := applyEngine(t, home, mgr, &out)
	cfgPath := filepath.Join(home, ".config", "omnishell", "config.toml")
	lockPath := filepath.Join(home, ".config", "omnishell", "state.lock.json")
	writeConfig(t, cfgPath, "[omnishell]\nversion=1\nshells=[\"bash\"]\n[modules.completion]\nenabled=true\n[modules.fzf]\nenabled=true\n[modules.fzf.options]\nctrl_r=true\n")
	cfg, _ := config.Load(cfgPath)
	if _, err := e.Apply(cfg, cfgPath, lockPath, engine.ApplyOptions{Yes: true}); err != nil {
		t.Fatal(err)
	}

	cfg2, _ := config.Load(cfgPath)
	res, err := e.Remove(cfg2, cfgPath, lockPath, "fzf", false, engine.ApplyOptions{Yes: true})
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if !res.Changed {
		t.Fatal("Remove made no change")
	}
	body, _ := os.ReadFile(filepath.Join(home, ".config", "omnishell", "init.bash"))
	if strings.Contains(string(body), "omnishell:fzf") {
		t.Fatalf("fzf section still present:\n%s", body)
	}
	after, _ := config.Load(cfgPath)
	if after.Modules["fzf"].Enabled {
		t.Fatal("fzf still enabled in config after Remove")
	}
	lock, _, _ := lockfile.Load(lockPath)
	if _, ok := lock.Modules["fzf"]; ok {
		t.Fatal("fzf still in lockfile after Remove")
	}

	var removed bool
	for _, m := range res.Modules {
		if m.ID == "fzf" {
			removed = m.Status == "removed"
		}
	}
	if !removed {
		t.Fatalf("res.Modules did not mark fzf removed: %+v", res.Modules)
	}
}

func TestRemovePurgeUninstallsOmnishellPackages(t *testing.T) {
	home := t.TempDir()
	var out bytes.Buffer
	r := &pkgmgr.MockRunner{}
	mgr := &pkgmgr.MockManager{NameV: "apt", DetectV: true, Installed: map[string]bool{}, SudoV: true}
	e := applyEngine(t, home, mgr, &out)
	e.Runner = r
	cfgPath := filepath.Join(home, ".config", "omnishell", "config.toml")
	lockPath := filepath.Join(home, ".config", "omnishell", "state.lock.json")
	writeConfig(t, cfgPath, "[omnishell]\nversion=1\nshells=[\"bash\"]\n[modules.fzf]\nenabled=true\n[modules.fzf.options]\nctrl_r=true\n")
	cfg, _ := config.Load(cfgPath)
	if _, err := e.Apply(cfg, cfgPath, lockPath, engine.ApplyOptions{Yes: true}); err != nil {
		t.Fatal(err)
	}

	cfg2, _ := config.Load(cfgPath)
	if _, err := e.Remove(cfg2, cfgPath, lockPath, "fzf", true, engine.ApplyOptions{Yes: true}); err != nil {
		t.Fatalf("Remove --purge: %v", err)
	}
	joined := strings.Join(r.Calls, "\n")
	if !strings.Contains(joined, "sudo apt-get remove -y -- fzf") {
		t.Fatalf("purge did not uninstall fzf; calls:\n%s", joined)
	}
	if !strings.Contains(out.String(), "sudo may prompt") {
		t.Fatalf("purge did not announce the sudo prompt:\n%s", out.String())
	}
}

func TestRemoveDryRunLeavesConfigUntouched(t *testing.T) {
	home := t.TempDir()
	var out bytes.Buffer
	mgr := &pkgmgr.MockManager{NameV: "apt", DetectV: true, Installed: map[string]bool{}}
	e := applyEngine(t, home, mgr, &out)
	cfgPath := filepath.Join(home, ".config", "omnishell", "config.toml")
	lockPath := filepath.Join(home, ".config", "omnishell", "state.lock.json")
	writeConfig(t, cfgPath, "[omnishell]\nversion=1\nshells=[\"bash\"]\n[modules.completion]\nenabled=true\n[modules.fzf]\nenabled=true\n[modules.fzf.options]\nctrl_r=true\n")
	cfg, _ := config.Load(cfgPath)
	if _, err := e.Apply(cfg, cfgPath, lockPath, engine.ApplyOptions{Yes: true}); err != nil {
		t.Fatal(err)
	}
	initPath := filepath.Join(home, ".config", "omnishell", "init.bash")
	cfgBefore, _ := os.ReadFile(cfgPath)
	lockBefore, _ := os.ReadFile(lockPath)
	initBefore, _ := os.ReadFile(initPath)

	cfg2, _ := config.Load(cfgPath)
	res, err := e.Remove(cfg2, cfgPath, lockPath, "fzf", false, engine.ApplyOptions{DryRun: true, Yes: true})
	if err != nil {
		t.Fatalf("Remove --dry-run: %v", err)
	}
	if !res.DryRun {
		t.Fatal("res.DryRun = false")
	}
	if res.Changed {
		t.Fatal("dry-run reported Changed")
	}
	if !strings.Contains(res.PlanText, "remove") || !strings.Contains(res.PlanText, "fzf") {
		t.Fatalf("dry-run plan does not show fzf removal:\n%s", res.PlanText)
	}
	if cfgAfter, _ := os.ReadFile(cfgPath); string(cfgAfter) != string(cfgBefore) {
		t.Fatalf("dry-run mutated config.toml:\n%s", cfgAfter)
	}
	if after, _ := config.Load(cfgPath); !after.Modules["fzf"].Enabled {
		t.Fatal("dry-run disabled fzf in config")
	}
	if lockAfter, _ := os.ReadFile(lockPath); string(lockAfter) != string(lockBefore) {
		t.Fatal("dry-run rewrote the lockfile")
	}
	if initAfter, _ := os.ReadFile(initPath); string(initAfter) != string(initBefore) {
		t.Fatal("dry-run rewrote init.bash")
	}
}

func TestRemovePurgeCompletesDespiteUnrelatedDegraded(t *testing.T) {
	home := t.TempDir()
	modulesDir := filepath.Join(home, "modules-src")
	if err := os.CopyFS(modulesDir, os.DirFS("testdata/modules")); err != nil {
		t.Fatal(err)
	}
	// A third module that always degrades: its bash.tmpl references a field
	// that does not exist on the render context, so rendering fails.
	brokeDir := filepath.Join(modulesDir, "broke")
	if err := os.MkdirAll(brokeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeConfig(t, filepath.Join(brokeDir, "manifest.toml"),
		"platforms = [\"linux\"]\nshells = [\"bash\"]\nrequires = []\nafter = []\n\n[module]\nid = \"broke\"\nname = \"Broke\"\ndescription = \"always degrades\"\nversion = \"1.0.0\"\nschema = 1\n")
	writeConfig(t, filepath.Join(brokeDir, "bash.tmpl"), "{{ .DoesNotExist }}\n")

	var out bytes.Buffer
	r := &pkgmgr.MockRunner{}
	mgr := &pkgmgr.MockManager{NameV: "apt", DetectV: true, Installed: map[string]bool{}, SudoV: true}
	e := removeHookEngine(t, home, modulesDir, mgr, r, &out)

	cfgPath := filepath.Join(home, ".config", "omnishell", "config.toml")
	lockPath := filepath.Join(home, ".config", "omnishell", "state.lock.json")
	writeConfig(t, cfgPath, "[omnishell]\nversion=1\nshells=[\"bash\"]\n[modules.completion]\nenabled=true\n[modules.fzf]\nenabled=true\n[modules.fzf.options]\nctrl_r=true\n[modules.broke]\nenabled=true\n")
	cfg, _ := config.Load(cfgPath)
	if _, err := e.Apply(cfg, cfgPath, lockPath, engine.ApplyOptions{Yes: true}); !errors.Is(err, engine.ErrDegraded) {
		t.Fatalf("Apply: want ErrDegraded, got %v", err)
	}

	cfg2, _ := config.Load(cfgPath)
	res, err := e.Remove(cfg2, cfgPath, lockPath, "fzf", true, engine.ApplyOptions{Yes: true})
	if !errors.Is(err, engine.ErrDegraded) {
		t.Fatalf("Remove: want an error wrapping ErrDegraded, got %v", err)
	}
	if joined := strings.Join(r.Calls, "\n"); !strings.Contains(joined, "sudo apt-get remove -y -- fzf") {
		t.Fatalf("purge did not uninstall fzf despite the unrelated ErrDegraded; calls:\n%s", joined)
	}
	lock, _, _ := lockfile.Load(lockPath)
	if _, ok := lock.Modules["fzf"]; ok {
		t.Fatal("fzf still in lockfile after Remove")
	}
	var fzfRemoved bool
	for _, m := range res.Modules {
		if m.ID == "fzf" && m.Status == "removed" {
			fzfRemoved = true
		}
	}
	if !fzfRemoved {
		t.Fatalf("res.Modules did not mark fzf removed: %+v", res.Modules)
	}
}

func TestRemoveUnknownModule(t *testing.T) {
	home := t.TempDir()
	var out bytes.Buffer
	e := applyEngine(t, home, &pkgmgr.MockManager{NameV: "apt", DetectV: true}, &out)
	cfgPath := filepath.Join(home, ".config", "omnishell", "config.toml")
	lockPath := filepath.Join(home, ".config", "omnishell", "state.lock.json")
	writeConfig(t, cfgPath, "[omnishell]\nversion=1\nshells=[\"bash\"]\n[modules.completion]\nenabled=true\n")
	cfg, _ := config.Load(cfgPath)
	_, err := e.Remove(cfg, cfgPath, lockPath, "nope", false, engine.ApplyOptions{Yes: true})
	if err == nil || !strings.Contains(err.Error(), `unknown module "nope"`) {
		t.Fatalf("err = %v, want unknown module error", err)
	}
}

// removeHookEngine builds an Engine whose registry is loaded from modulesDir so a
// test can supply a module carrying a hooks/remove.sh script.
func removeHookEngine(t *testing.T, home, modulesDir string, mgr *pkgmgr.MockManager, r pkgmgr.Runner, out *bytes.Buffer) engine.Engine {
	t.Helper()
	reg, err := module.LoadRegistry(nil, modulesDir)
	if err != nil {
		t.Fatal(err)
	}
	return engine.Engine{
		Platform: platform.Info{
			OS:        platform.Linux,
			HomeDir:   home,
			ConfigDir: filepath.Join(home, ".config", "omnishell"),
			Shells: []platform.ShellInfo{
				{Name: "bash", RCPath: filepath.Join(home, ".bashrc"), Present: true},
			},
		},
		Registry:  reg,
		Manager:   mgr,
		ManagerOK: true,
		Runner:    r,
		Now:       func() time.Time { return time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC) },
		Stdout:    out,
		Stderr:    out,
		Prompt:    func(string) bool { return true },
	}
}

func TestRemovePurgeRunsRemoveHook(t *testing.T) {
	home := t.TempDir()
	modulesDir := filepath.Join(home, "modules-src")
	modDir := filepath.Join(modulesDir, "hookmod", "hooks")
	if err := os.MkdirAll(modDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeConfig(t, filepath.Join(modulesDir, "hookmod", "manifest.toml"),
		"platforms = [\"linux\"]\nshells = [\"bash\"]\nrequires = []\nafter = []\n\n[module]\nid = \"hookmod\"\nname = \"Hook Module\"\ndescription = \"exercises the remove hook\"\nversion = \"1.0.0\"\nschema = 1\n")
	writeConfig(t, filepath.Join(modulesDir, "hookmod", "bash.tmpl"), "echo hookmod\n")
	// The hook records one of its OMNISHELL_* env vars to a sentinel file so the
	// test can prove both that the script ran AND that setEnv wired the contract.
	writeConfig(t, filepath.Join(modDir, "remove.sh"),
		"#!/bin/sh\nprintf '%s' \"$OMNISHELL_CONFIG_DIR\" > \"$OMNISHELL_CONFIG_DIR/hook-ran\"\n")

	// MockRunner only records calls; it never executes the script. Use a real
	// ExecRunner here (only) so runHookScript/setEnv are genuinely exercised.
	var buf bytes.Buffer
	mgr := &pkgmgr.MockManager{NameV: "apt", DetectV: true, Installed: map[string]bool{}}
	e := removeHookEngine(t, home, modulesDir, mgr, pkgmgr.ExecRunner{Stdout: &buf, Stderr: &buf}, &buf)

	cfgPath := filepath.Join(home, ".config", "omnishell", "config.toml")
	lockPath := filepath.Join(home, ".config", "omnishell", "state.lock.json")
	writeConfig(t, cfgPath, "[omnishell]\nversion=1\nshells=[\"bash\"]\n[modules.hookmod]\nenabled=true\n")
	cfg, _ := config.Load(cfgPath)
	if _, err := e.Apply(cfg, cfgPath, lockPath, engine.ApplyOptions{Yes: true}); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	cfg2, _ := config.Load(cfgPath)
	if _, err := e.Remove(cfg2, cfgPath, lockPath, "hookmod", true, engine.ApplyOptions{Yes: true}); err != nil {
		t.Fatalf("Remove --purge: %v", err)
	}

	configDir := filepath.Join(home, ".config", "omnishell")
	got, err := os.ReadFile(filepath.Join(configDir, "hook-ran"))
	if err != nil {
		t.Fatalf("remove hook did not run (no sentinel): %v", err)
	}
	if string(got) != configDir {
		t.Fatalf("hook saw OMNISHELL_CONFIG_DIR=%q, want %q", got, configDir)
	}
}
