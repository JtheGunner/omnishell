package engine_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/JtheGunner/omnishell/internal/engine"
	"github.com/JtheGunner/omnishell/internal/pkgmgr"
)

func TestUninstallRemovesRCBlockAndInitFiles(t *testing.T) {
	home := t.TempDir()
	var out bytes.Buffer
	mgr := &pkgmgr.MockManager{NameV: "apt", DetectV: true, Installed: map[string]bool{}}
	e := applyEngine(t, home, mgr, &out)
	cfgPath := filepath.Join(home, ".config", "omnishell", "config.toml")
	lockPath := filepath.Join(home, ".config", "omnishell", "state.lock.json")
	rc := filepath.Join(home, ".bashrc")
	os.WriteFile(rc, []byte("export X=1\n"), 0o644)
	writeConfig(t, cfgPath, "[omnishell]\nversion=1\nshells=[\"bash\"]\n[modules.completion]\nenabled=true\n")
	cfg, _ := config.Load(cfgPath)
	if _, err := e.Apply(cfg, cfgPath, lockPath, engine.ApplyOptions{Yes: true}); err != nil {
		t.Fatal(err)
	}

	res, err := e.Uninstall(lockPath, engine.UninstallOptions{Yes: true})
	if err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if !res.Changed {
		t.Fatal("Uninstall made no change")
	}
	got, _ := os.ReadFile(rc)
	if strings.Contains(string(got), "omnishell") {
		t.Fatalf(".bashrc still has omnishell block:\n%s", got)
	}
	if string(got) != "export X=1\n" {
		t.Fatalf(".bashrc not restored cleanly:\n%q", got)
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "omnishell", "init.bash")); !os.IsNotExist(err) {
		t.Fatal("init.bash still present after uninstall")
	}
}

func TestUninstallPurgeRemovesConfigDir(t *testing.T) {
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
	if _, err := e.Uninstall(lockPath, engine.UninstallOptions{Yes: true, PurgeConfigDir: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "omnishell")); !os.IsNotExist(err) {
		t.Fatal("config dir still present after --purge")
	}
}

func TestUninstallAbortedByPrompt(t *testing.T) {
	home := t.TempDir()
	var out bytes.Buffer
	mgr := &pkgmgr.MockManager{NameV: "apt", DetectV: true, Installed: map[string]bool{}}
	e := applyEngine(t, home, mgr, &out)
	cfgPath := filepath.Join(home, ".config", "omnishell", "config.toml")
	lockPath := filepath.Join(home, ".config", "omnishell", "state.lock.json")
	rc := filepath.Join(home, ".bashrc")
	os.WriteFile(rc, []byte("export X=1\n"), 0o644)
	writeConfig(t, cfgPath, "[omnishell]\nversion=1\nshells=[\"bash\"]\n[modules.completion]\nenabled=true\n")
	cfg, _ := config.Load(cfgPath)
	if _, err := e.Apply(cfg, cfgPath, lockPath, engine.ApplyOptions{Yes: true}); err != nil {
		t.Fatal(err)
	}

	// Deny the confirmation prompt.
	e.Prompt = func(string) bool { return false }

	res, err := e.Uninstall(lockPath, engine.UninstallOptions{})
	if !errors.Is(err, engine.ErrAborted) {
		t.Fatalf("want ErrAborted, got %v", err)
	}
	if res.Changed {
		t.Fatal("aborted Uninstall reported Changed")
	}
	got, _ := os.ReadFile(rc)
	if !strings.Contains(string(got), "# >>> omnishell >>>") {
		t.Fatalf(".bashrc marker block was removed despite abort:\n%s", got)
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "omnishell", "init.bash")); err != nil {
		t.Fatalf("init.bash missing after aborted uninstall: %v", err)
	}
}

func TestUninstallNothingToDo(t *testing.T) {
	home := t.TempDir()
	var out bytes.Buffer
	mgr := &pkgmgr.MockManager{NameV: "apt", DetectV: true, Installed: map[string]bool{}}
	e := applyEngine(t, home, mgr, &out)
	lockPath := filepath.Join(home, ".config", "omnishell", "state.lock.json")

	res, err := e.Uninstall(lockPath, engine.UninstallOptions{Yes: true})
	if err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if res.Changed {
		t.Fatal("Uninstall on a clean system reported Changed")
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "omnishell")); !os.IsNotExist(err) {
		t.Fatal("Uninstall created the config dir on a clean system")
	}
}
