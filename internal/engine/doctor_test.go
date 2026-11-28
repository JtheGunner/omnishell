package engine_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
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
	if err := os.WriteFile(initBash, append(b, []byte("\nrm -rf /\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".bashrc"), []byte("# nothing here\n"), 0o644); err != nil {
		t.Fatal(err)
	}

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

// zshCompletionsDoctorSetup enables the zsh-completions fixture, applies it and
// creates the module's vendor directory with the given mode. It returns the
// engine, config, the config and lock paths, and that directory.
func zshCompletionsDoctorSetup(t *testing.T, vendorMode os.FileMode) (e engine.Engine, cfg config.Config, cfgPath, lockPath, dir string) {
	t.Helper()
	home := t.TempDir()
	var out bytes.Buffer
	mgr := &pkgmgr.MockManager{NameV: "apt", DetectV: true, Installed: map[string]bool{}}
	e = applyEngine(t, home, mgr, &out)
	cfgPath = filepath.Join(home, ".config", "omnishell", "config.toml")
	lockPath = filepath.Join(home, ".config", "omnishell", "state.lock.json")
	writeConfig(t, cfgPath, "[omnishell]\nversion=1\nshells=[\"bash\"]\n[modules.zsh-completions]\nenabled=true\n")
	cfg, _ = config.Load(cfgPath)
	if _, err := e.Apply(cfg, cfgPath, lockPath, engine.ApplyOptions{Yes: true}); err != nil {
		t.Fatal(err)
	}
	dir = filepath.Join(home, ".config", "omnishell", "vendor", "zsh-completions", "src")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, vendorMode); err != nil {
		t.Fatal(err)
	}
	return e, cfg, cfgPath, lockPath, dir
}

func findCode(rep engine.DoctorReport, code string) (engine.Finding, bool) {
	for _, f := range rep.Findings {
		if f.Code == code {
			return f, true
		}
	}
	return engine.Finding{}, false
}

func TestDoctorWarnsAboutWritableCompletionDir(t *testing.T) {
	e, cfg, cfgPath, lockPath, dir := zshCompletionsDoctorSetup(t, 0o775)
	rep, err := e.Doctor(cfg, cfgPath, lockPath)
	if err != nil {
		t.Fatal(err)
	}
	f, ok := findCode(rep, "completion-dir-writable:"+dir)
	if !ok {
		t.Fatalf("no completion-dir-writable finding for %s: %+v", dir, rep.Findings)
	}
	if f.Severity != engine.SeverityNotice {
		t.Fatalf("severity = %q, want notice (a warning, not drift)", f.Severity)
	}
	if want := "chmod go-w '" + dir + "'"; !strings.Contains(f.Message, want) {
		t.Fatalf("message %q lacks the hint %q", f.Message, want)
	}
	if rep.HasDrift() {
		t.Fatalf("a writable completion directory must not count as drift: %+v", rep.Findings)
	}
	// Doctor is read-only: it must not have touched the permissions.
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o775 {
		t.Fatalf("doctor changed the mode to %v", info.Mode().Perm())
	}
}

func TestDoctorAcceptsPrivateCompletionDir(t *testing.T) {
	e, cfg, cfgPath, lockPath, dir := zshCompletionsDoctorSetup(t, 0o755)
	rep, err := e.Doctor(cfg, cfgPath, lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := findCode(rep, "completion-dir-writable:"+dir); ok {
		t.Fatalf("0755 directory was reported: %+v", rep.Findings)
	}
}

func TestDoctorIgnoresCompletionDirWhenModuleDisabled(t *testing.T) {
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
	dir := filepath.Join(home, ".config", "omnishell", "vendor", "zsh-completions", "src")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	rep, err := e.Doctor(cfg, cfgPath, lockPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range rep.Findings {
		if strings.HasPrefix(f.Code, "completion-dir-writable:") {
			t.Fatalf("reported without the module enabled: %+v", f)
		}
	}
}
