package engine_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/JtheGunner/omnishell/internal/engine"
	"github.com/JtheGunner/omnishell/internal/pkgmgr"
)

func TestDoctorCleanAfterApply(t *testing.T) {
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
	rep, err := e.Doctor(cfg, cfgPath, lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if rep.HasDrift() {
		t.Fatalf("clean system reported drift: %+v", rep.Findings)
	}
}

func TestDoctorDetectsHandEditAndMissingRCBlock(t *testing.T) {
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
	// tamper init file, wipe .bashrc
	initBash := filepath.Join(home, ".config", "omnishell", "init.bash")
	b, _ := os.ReadFile(initBash)
	os.WriteFile(initBash, append(b, []byte("\nrm -rf /\n")...), 0o644)
	os.WriteFile(filepath.Join(home, ".bashrc"), []byte("# nothing here\n"), 0o644)

	rep, err := e.Doctor(cfg, cfgPath, lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.HasDrift() {
		t.Fatal("expected drift")
	}
	codes := map[string]bool{}
	for _, f := range rep.Findings {
		codes[f.Code] = true
	}
	if !codes["initfile-edited:bash"] || !codes["rc-block-missing:bash"] {
		t.Fatalf("missing expected findings: %+v", rep.Findings)
	}
}

func TestDoctorNeverApplied(t *testing.T) {
	home := t.TempDir()
	var out bytes.Buffer
	e := applyEngine(t, home, &pkgmgr.MockManager{NameV: "apt", DetectV: true}, &out)
	cfgPath := filepath.Join(home, ".config", "omnishell", "config.toml")
	lockPath := filepath.Join(home, ".config", "omnishell", "state.lock.json")
	writeConfig(t, cfgPath, "[omnishell]\nversion=1\nshells=[\"bash\"]\n[modules.completion]\nenabled=true\n")
	cfg, _ := config.Load(cfgPath)
	rep, err := e.Doctor(cfg, cfgPath, lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.HasDrift() || rep.Findings[0].Code != "never-applied" {
		t.Fatalf("want never-applied drift, got %+v", rep.Findings)
	}
}
