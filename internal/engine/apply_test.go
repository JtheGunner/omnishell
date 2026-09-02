package engine_test

import (
	"bytes"
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

func writeConfig(t *testing.T, path, body string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
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
	os.WriteFile(initBash, append(orig, []byte("\n# tampered\n")...), 0o644)

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
