package engine_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JtheGunner/omnishell/internal/backup"
	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/JtheGunner/omnishell/internal/engine"
	"github.com/JtheGunner/omnishell/internal/lockfile"
	"github.com/JtheGunner/omnishell/internal/module"
	"github.com/JtheGunner/omnishell/internal/pkgmgr"
	"github.com/JtheGunner/omnishell/internal/platform"
)

func applyEngine(t *testing.T, home string, mgr *pkgmgr.MockManager, out *bytes.Buffer) engine.Engine {
	t.Helper()
	reg, err := module.LoadRegistry(nil, "testdata/modules")
	if err != nil {
		t.Fatal(err)
	}
	return engine.Engine{
		Platform: platform.Info{
			OS:        platform.Linux,
			HomeDir:   home,
			ConfigDir: filepath.Join(home, ".config", "omnishell"),
			Shells: []platform.ShellInfo{
				{Name: "zsh", RCPath: filepath.Join(home, ".zshrc"), Present: false},
				{Name: "bash", RCPath: filepath.Join(home, ".bashrc"), Present: true},
			},
		},
		Registry:  reg,
		Manager:   mgr,
		ManagerOK: true,
		Runner:    &pkgmgr.MockRunner{},
		Now:       func() time.Time { return time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC) },
		Stdout:    out,
		Stderr:    out,
		Prompt:    func(string) bool { return true },
	}
}

func TestApplyFreshInstallWritesInitAndRC(t *testing.T) {
	home := t.TempDir()
	var out bytes.Buffer
	mgr := &pkgmgr.MockManager{NameV: "apt", DetectV: true, Installed: map[string]bool{}}
	e := applyEngine(t, home, mgr, &out)

	cfgPath := filepath.Join(home, ".config", "omnishell", "config.toml")
	lockPath := filepath.Join(home, ".config", "omnishell", "state.lock.json")
	writeConfig(t, cfgPath, `
[omnishell]
version = 1
shells = ["bash"]
[modules.completion]
enabled = true
[modules.fzf]
enabled = true
[modules.fzf.options]
ctrl_r = true
`)
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	res, err := e.Apply(cfg, cfgPath, lockPath, engine.ApplyOptions{Yes: true})
	if err != nil {
		t.Fatalf("Apply: %v\noutput:\n%s", err, out.String())
	}
	if !res.Changed {
		t.Fatal("res.Changed = false")
	}

	initBash := filepath.Join(home, ".config", "omnishell", "init.bash")
	body, err := os.ReadFile(initBash)
	if err != nil {
		t.Fatalf("init.bash not written: %v", err)
	}
	if !strings.Contains(string(body), "# >>> omnishell:completion") ||
		!strings.Contains(string(body), "source fzf-key-bindings.bash") {
		t.Fatalf("init.bash content wrong:\n%s", body)
	}

	rc, _ := os.ReadFile(filepath.Join(home, ".bashrc"))
	if !strings.Contains(string(rc), `[ -f "$HOME/.config/omnishell/init.bash" ] && source`) {
		t.Fatalf(".bashrc missing source block:\n%s", rc)
	}
	if len(mgr.InstallCalls) != 1 || mgr.InstallCalls[0][0] != "fzf" {
		t.Fatalf("fzf not installed: %+v", mgr.InstallCalls)
	}

	lock, exists, err := lockfile.Load(lockPath)
	if err != nil || !exists {
		t.Fatalf("lock: exists=%v err=%v", exists, err)
	}
	if lock.Modules["fzf"].Status != "ok" || lock.Modules["fzf"].OptionsHash == "" {
		t.Fatalf("lock fzf state wrong: %+v", lock.Modules["fzf"])
	}
	if !lock.Modules["fzf"].Packages[0].InstalledByOmnishell {
		t.Fatal("fzf package not marked installed_by_omnishell")
	}
}

func TestApplyIsIdempotent(t *testing.T) {
	home := t.TempDir()
	var out bytes.Buffer
	mgr := &pkgmgr.MockManager{NameV: "apt", DetectV: true, Installed: map[string]bool{}}
	e := applyEngine(t, home, mgr, &out)
	cfgPath := filepath.Join(home, ".config", "omnishell", "config.toml")
	lockPath := filepath.Join(home, ".config", "omnishell", "state.lock.json")
	writeConfig(t, cfgPath, "[omnishell]\nversion=1\nshells=[\"bash\"]\n[modules.completion]\nenabled=true\n")
	cfg, _ := config.Load(cfgPath)

	if _, err := e.Apply(cfg, cfgPath, lockPath, engine.ApplyOptions{Yes: true}); err != nil {
		t.Fatal(err)
	}
	info1, _ := os.Stat(filepath.Join(home, ".config", "omnishell", "init.bash"))

	res, err := e.Apply(cfg, cfgPath, lockPath, engine.ApplyOptions{Yes: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("second apply reported a change")
	}
	info2, _ := os.Stat(filepath.Join(home, ".config", "omnishell", "init.bash"))
	if !info1.ModTime().Equal(info2.ModTime()) {
		t.Fatal("init.bash was rewritten on a no-op apply")
	}
	backups, _ := os.ReadDir(filepath.Join(home, ".config", "omnishell", "backups"))
	if len(backups) != 1 {
		t.Fatalf("no-op apply created a backup; backups=%d", len(backups))
	}
}

// TestApplyStablyDegradedIsIdempotent covers the C2 regression: a module that
// is permanently degraded by the plan (fzf needs a package, no package manager
// exists) must not make apply rewrite init.<shell> or cut a new backup on every
// run, and doctor must settle on module-degraded alone — no initfile-stale, no
// pending-apply.
func TestApplyStablyDegradedIsIdempotent(t *testing.T) {
	home := t.TempDir()
	var out bytes.Buffer
	e := applyEngine(t, home, &pkgmgr.MockManager{NameV: "apt", DetectV: true}, &out)
	e.ManagerOK = false
	e.Manager = nil

	cfgPath := filepath.Join(home, ".config", "omnishell", "config.toml")
	lockPath := filepath.Join(home, ".config", "omnishell", "state.lock.json")
	writeConfig(t, cfgPath, "[omnishell]\nversion=1\nshells=[\"bash\"]\n[modules.completion]\nenabled=true\n[modules.fzf]\nenabled=true\n")
	cfg, _ := config.Load(cfgPath)

	res, err := e.Apply(cfg, cfgPath, lockPath, engine.ApplyOptions{Yes: true})
	if !errors.Is(err, engine.ErrDegraded) {
		t.Fatalf("first apply err = %v, want ErrDegraded", err)
	}
	if !res.Changed {
		t.Fatal("first apply made no change")
	}
	initBash := filepath.Join(home, ".config", "omnishell", "init.bash")
	body, rerr := os.ReadFile(initBash)
	if rerr != nil {
		t.Fatalf("init.bash: %v", rerr)
	}
	if strings.Contains(string(body), "omnishell:fzf") {
		t.Fatalf("degraded fzf leaked into init.bash:\n%s", body)
	}

	info1, _ := os.Stat(initBash)
	backups1, _ := os.ReadDir(filepath.Join(home, ".config", "omnishell", "backups"))

	res2, err2 := e.Apply(cfg, cfgPath, lockPath, engine.ApplyOptions{Yes: true})
	if !errors.Is(err2, engine.ErrDegraded) {
		t.Fatalf("second apply err = %v, want ErrDegraded", err2)
	}
	if res2.Changed {
		t.Fatal("second apply reported a change on a stably-degraded config")
	}
	info2, _ := os.Stat(initBash)
	if !info1.ModTime().Equal(info2.ModTime()) {
		t.Fatal("second apply rewrote init.bash")
	}
	backups2, _ := os.ReadDir(filepath.Join(home, ".config", "omnishell", "backups"))
	if len(backups2) != len(backups1) {
		t.Fatalf("second apply created a new backup dir (%d -> %d)", len(backups1), len(backups2))
	}

	rep, derr := e.Doctor(cfg, cfgPath, lockPath)
	if derr != nil {
		t.Fatal(derr)
	}
	codes := map[string]bool{}
	for _, f := range rep.Findings {
		codes[f.Code] = true
	}
	if !codes["module-degraded:fzf"] {
		t.Fatalf("doctor missing module-degraded:fzf: %+v", rep.Findings)
	}
	if codes["initfile-stale:bash"] || codes["pending-apply:fzf"] {
		t.Fatalf("doctor emitted redundant findings for a stably-degraded module: %+v", rep.Findings)
	}
}

func TestApplyDryRunWritesNothing(t *testing.T) {
	home := t.TempDir()
	var out bytes.Buffer
	e := applyEngine(t, home, &pkgmgr.MockManager{NameV: "apt", DetectV: true}, &out)
	cfgPath := filepath.Join(home, ".config", "omnishell", "config.toml")
	lockPath := filepath.Join(home, ".config", "omnishell", "state.lock.json")
	writeConfig(t, cfgPath, "[omnishell]\nversion=1\nshells=[\"bash\"]\n[modules.completion]\nenabled=true\n")
	cfg, _ := config.Load(cfgPath)

	res, err := e.Apply(cfg, cfgPath, lockPath, engine.ApplyOptions{DryRun: true, Yes: true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.DryRun || !strings.Contains(res.PlanText, "install") {
		t.Fatalf("dry-run result wrong: %+v", res)
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "omnishell", "init.bash")); !os.IsNotExist(err) {
		t.Fatal("dry-run wrote init.bash")
	}
}

func TestApplyConfigChangeNeedsNoForce(t *testing.T) {
	home := t.TempDir()
	var out bytes.Buffer
	e := applyEngine(t, home, &pkgmgr.MockManager{NameV: "apt", DetectV: true}, &out)
	cfgPath := filepath.Join(home, ".config", "omnishell", "config.toml")
	lockPath := filepath.Join(home, ".config", "omnishell", "state.lock.json")
	writeConfig(t, cfgPath, "[omnishell]\nversion=1\nshells=[\"bash\"]\n[modules.completion]\nenabled=true\n")
	cfg, _ := config.Load(cfgPath)
	if _, err := e.Apply(cfg, cfgPath, lockPath, engine.ApplyOptions{Yes: true}); err != nil {
		t.Fatal(err)
	}

	// A plain config change (no tampering of init.bash) must apply without --force.
	writeConfig(t, cfgPath, "[omnishell]\nversion=1\nshells=[\"bash\"]\n[modules.completion]\nenabled=true\n[modules.history]\nenabled=true\n")
	cfg2, _ := config.Load(cfgPath)

	res, err := e.Apply(cfg2, cfgPath, lockPath, engine.ApplyOptions{Yes: true})
	if err != nil {
		t.Fatalf("plain config change required force: %v", err)
	}
	if !res.Changed {
		t.Fatal("config change made no change")
	}
	body, _ := os.ReadFile(filepath.Join(home, ".config", "omnishell", "init.bash"))
	if !strings.Contains(string(body), "omnishell:history") {
		t.Fatalf("init.bash missing history section:\n%s", body)
	}
}

func TestApplyOverEmptyInitFileNeedsNoForce(t *testing.T) {
	home := t.TempDir()
	var out bytes.Buffer
	e := applyEngine(t, home, &pkgmgr.MockManager{NameV: "apt", DetectV: true}, &out)
	cfgPath := filepath.Join(home, ".config", "omnishell", "config.toml")
	lockPath := filepath.Join(home, ".config", "omnishell", "state.lock.json")
	writeConfig(t, cfgPath, "[omnishell]\nversion=1\nshells=[\"bash\"]\n[modules.completion]\nenabled=true\n")
	cfg, _ := config.Load(cfgPath)

	// Simulate a leftover zero-byte init file (e.g. an interrupted write or a
	// stray touch) before the first ever apply. It must not trip the guard.
	initBash := filepath.Join(home, ".config", "omnishell", "init.bash")
	writeConfig(t, initBash, "")

	res, err := e.Apply(cfg, cfgPath, lockPath, engine.ApplyOptions{Yes: true})
	if err != nil {
		t.Fatalf("Apply over empty init file required force: %v\noutput:\n%s", err, out.String())
	}
	if !res.Changed {
		t.Fatal("res.Changed = false")
	}

	body, rerr := os.ReadFile(initBash)
	if rerr != nil {
		t.Fatalf("init.bash not written: %v", rerr)
	}
	if !strings.Contains(string(body), "GENERATED BY omnishell") {
		t.Fatalf("init.bash missing omnishell header:\n%s", body)
	}
	if !strings.Contains(string(body), "# >>> omnishell:completion") {
		t.Fatalf("init.bash missing completion section:\n%s", body)
	}
}

func TestApplyHandEditGuard(t *testing.T) {
	home := t.TempDir()
	var out bytes.Buffer
	e := applyEngine(t, home, &pkgmgr.MockManager{NameV: "apt", DetectV: true}, &out)
	cfgPath := filepath.Join(home, ".config", "omnishell", "config.toml")
	lockPath := filepath.Join(home, ".config", "omnishell", "state.lock.json")
	writeConfig(t, cfgPath, "[omnishell]\nversion=1\nshells=[\"bash\"]\n[modules.completion]\nenabled=true\n")
	cfg, _ := config.Load(cfgPath)
	if _, err := e.Apply(cfg, cfgPath, lockPath, engine.ApplyOptions{Yes: true}); err != nil {
		t.Fatal(err)
	}

	initBash := filepath.Join(home, ".config", "omnishell", "init.bash")
	orig, _ := os.ReadFile(initBash)
	if err := os.WriteFile(initBash, append(orig, []byte("\n# tampered\n")...), 0o644); err != nil {
		t.Fatal(err)
	}

	// Force a change so Apply wants to write.
	writeConfig(t, cfgPath, "[omnishell]\nversion=1\nshells=[\"bash\"]\n[modules.completion]\nenabled=true\n[modules.history]\nenabled=true\n")
	cfg2, _ := config.Load(cfgPath)

	_, err := e.Apply(cfg2, cfgPath, lockPath, engine.ApplyOptions{Yes: true})
	if err == nil || !strings.Contains(err.Error(), "force") {
		t.Fatalf("err = %v, want hand-edit guard error", err)
	}

	res, err := e.Apply(cfg2, cfgPath, lockPath, engine.ApplyOptions{Yes: true, Force: true})
	if err != nil {
		t.Fatalf("Apply --force: %v", err)
	}
	if !res.Changed {
		t.Fatal("force apply made no change")
	}
}

// TestApplyHandEditGuardRunsBeforeInstall proves the hand-edit guard aborts the
// run before installPackages, so a hand-edited init file never triggers a sudo
// prompt or a package install (I1).
func TestApplyHandEditGuardRunsBeforeInstall(t *testing.T) {
	home := t.TempDir()
	var out bytes.Buffer
	mgr := &pkgmgr.MockManager{NameV: "apt", DetectV: true, Installed: map[string]bool{}}
	e := applyEngine(t, home, mgr, &out)
	cfgPath := filepath.Join(home, ".config", "omnishell", "config.toml")
	lockPath := filepath.Join(home, ".config", "omnishell", "state.lock.json")
	writeConfig(t, cfgPath, "[omnishell]\nversion=1\nshells=[\"bash\"]\n[modules.completion]\nenabled=true\n")
	cfg, _ := config.Load(cfgPath)
	if _, err := e.Apply(cfg, cfgPath, lockPath, engine.ApplyOptions{Yes: true}); err != nil {
		t.Fatal(err)
	}

	initBash := filepath.Join(home, ".config", "omnishell", "init.bash")
	orig, _ := os.ReadFile(initBash)
	if err := os.WriteFile(initBash, append(orig, []byte("\n# tampered\n")...), 0o644); err != nil {
		t.Fatal(err)
	}

	// Now enable fzf, which needs a package the mock manager has not installed.
	writeConfig(t, cfgPath, "[omnishell]\nversion=1\nshells=[\"bash\"]\n[modules.completion]\nenabled=true\n[modules.fzf]\nenabled=true\n")
	cfg2, _ := config.Load(cfgPath)

	mgr.InstallCalls = nil
	_, err := e.Apply(cfg2, cfgPath, lockPath, engine.ApplyOptions{Yes: true})
	if err == nil || !strings.Contains(err.Error(), "force") {
		t.Fatalf("err = %v, want hand-edit guard error", err)
	}
	if len(mgr.InstallCalls) != 0 {
		t.Fatalf("guard fired but packages were still installed: %+v", mgr.InstallCalls)
	}
}

// engineWithShells is applyEngine with explicit control over which shells are
// Present — needed to simulate a shell disappearing between two Apply runs
// (e.g. its binary was removed from the host, the way zsh gets pulled in as a
// package dependency and later purged).
func engineWithShells(t *testing.T, home string, mgr *pkgmgr.MockManager, out *bytes.Buffer, zshPresent, bashPresent bool) engine.Engine {
	t.Helper()
	reg, err := module.LoadRegistry(nil, "testdata/modules")
	if err != nil {
		t.Fatal(err)
	}
	return engine.Engine{
		Platform: platform.Info{
			OS:        platform.Linux,
			HomeDir:   home,
			ConfigDir: filepath.Join(home, ".config", "omnishell"),
			Shells: []platform.ShellInfo{
				{Name: "zsh", RCPath: filepath.Join(home, ".zshrc"), Present: zshPresent},
				{Name: "bash", RCPath: filepath.Join(home, ".bashrc"), Present: bashPresent},
			},
		},
		Registry:  reg,
		Manager:   mgr,
		ManagerOK: true,
		Runner:    &pkgmgr.MockRunner{},
		Now:       func() time.Time { return time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC) },
		Stdout:    out,
		Stderr:    out,
		Prompt:    func(string) bool { return true },
	}
}

// TestApplyCleansUpShellThatDisappeared covers a bug found by a live
// end-to-end test: a shell that WAS managed (its init file written, its rc
// file hooked) but later disappears from the host (e.g. zsh got pulled in as
// an apt dependency of a module's package and was later removed) left its
// init file and rc marker block behind forever — apply/doctor only ever
// looked at the currently managed shells, so nothing noticed or cleaned up
// the orphan. A later apply must remove both, and leave the still-managed
// shell untouched.
func TestApplyCleansUpShellThatDisappeared(t *testing.T) {
	home := t.TempDir()
	var out bytes.Buffer
	mgr := &pkgmgr.MockManager{NameV: "apt", DetectV: true, Installed: map[string]bool{}}
	cfgPath := filepath.Join(home, ".config", "omnishell", "config.toml")
	lockPath := filepath.Join(home, ".config", "omnishell", "state.lock.json")
	writeConfig(t, cfgPath, "[omnishell]\nversion=1\n[modules.completion]\nenabled=true\n")
	cfg, _ := config.Load(cfgPath)

	// First apply: both shells present.
	e1 := engineWithShells(t, home, mgr, &out, true, true)
	if _, err := e1.Apply(cfg, cfgPath, lockPath, engine.ApplyOptions{Yes: true}); err != nil {
		t.Fatalf("first Apply: %v\n%s", err, out.String())
	}
	initZsh := filepath.Join(home, ".config", "omnishell", "init.zsh")
	initBash := filepath.Join(home, ".config", "omnishell", "init.bash")
	if _, err := os.Stat(initZsh); err != nil {
		t.Fatalf("init.zsh not written: %v", err)
	}

	// zsh disappears (its binary was removed from the host) — Doctor must
	// flag it even before another apply runs.
	e2 := engineWithShells(t, home, mgr, &out, false, true)
	rep, err := e2.Doctor(cfg, cfgPath, lockPath)
	if err != nil {
		t.Fatal(err)
	}
	foundStale := false
	for _, f := range rep.Findings {
		if f.Code == "stale-shell:zsh" {
			foundStale = true
		}
	}
	if !foundStale {
		t.Fatalf("Doctor did not report stale-shell:zsh: %+v", rep.Findings)
	}

	// Second apply: zsh no longer present. It must clean up init.zsh and the
	// .zshrc marker block, and leave bash's files untouched.
	out.Reset()
	res, err := e2.Apply(cfg, cfgPath, lockPath, engine.ApplyOptions{Yes: true})
	if err != nil {
		t.Fatalf("second Apply: %v\n%s", err, out.String())
	}
	if !res.Changed {
		t.Fatal("cleanup apply reported Changed = false")
	}
	if _, err := os.Stat(initZsh); !os.IsNotExist(err) {
		t.Fatalf("init.zsh should have been removed, stat err = %v", err)
	}
	zshrc, err := os.ReadFile(filepath.Join(home, ".zshrc"))
	if err != nil {
		t.Fatalf("read .zshrc: %v", err)
	}
	if strings.Contains(string(zshrc), "omnishell") {
		t.Fatalf(".zshrc still has the omnishell block:\n%s", zshrc)
	}
	if _, err := os.Stat(initBash); err != nil {
		t.Fatalf("init.bash should be untouched: %v", err)
	}
	bashrc, err := os.ReadFile(filepath.Join(home, ".bashrc"))
	if err != nil {
		t.Fatalf("read .bashrc: %v", err)
	}
	if !strings.Contains(string(bashrc), "omnishell") {
		t.Fatalf(".bashrc lost its omnishell block:\n%s", bashrc)
	}

	newLock, _, err := lockfile.Load(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := newLock.InitFiles["zsh"]; ok {
		t.Fatalf("lock still records init file for zsh: %+v", newLock.InitFiles)
	}
	if _, ok := newLock.RCFiles["zsh"]; ok {
		t.Fatalf("lock still records rc file for zsh: %+v", newLock.RCFiles)
	}

	// Doctor is clean again after the cleanup.
	rep2, err := e2.Doctor(cfg, cfgPath, lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if rep2.HasDrift() {
		t.Fatalf("doctor still reports drift after cleanup: %+v", rep2.Findings)
	}
}

func TestApplyWritesManifestAndBacksUpLockfile(t *testing.T) {
	home := t.TempDir()
	var out bytes.Buffer
	mgr := &pkgmgr.MockManager{NameV: "apt", DetectV: true, Installed: map[string]bool{}}
	e := applyEngine(t, home, mgr, &out)

	cfgPath := filepath.Join(home, ".config", "omnishell", "config.toml")
	lockPath := filepath.Join(home, ".config", "omnishell", "state.lock.json")
	writeConfig(t, cfgPath, `
[omnishell]
version = 1
shells = ["bash"]
[modules.completion]
enabled = true
`)
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	res, err := e.Apply(cfg, cfgPath, lockPath, engine.ApplyOptions{Yes: true})
	if err != nil {
		t.Fatalf("Apply: %v\n%s", err, out.String())
	}
	if res.BackupDir == "" {
		t.Fatal("res.BackupDir empty")
	}

	m, err := backup.ReadManifest(res.BackupDir)
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	if m.Kind != "apply" {
		t.Fatalf("manifest kind = %q, want %q", m.Kind, "apply")
	}
	foundLock := false
	for _, f := range m.Files {
		if f.OriginalPath == lockPath {
			foundLock = true
			if f.ExistedBefore {
				t.Fatalf("first-ever apply's lockfile entry has ExistedBefore=true, want false")
			}
		}
	}
	if !foundLock {
		t.Fatalf("manifest does not record %s: %+v", lockPath, m.Files)
	}
}
