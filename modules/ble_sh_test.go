package modules_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/JtheGunner/omnishell/internal/engine"
	"github.com/JtheGunner/omnishell/internal/graph"
	"github.com/JtheGunner/omnishell/internal/lockfile"
	"github.com/JtheGunner/omnishell/internal/module"
	"github.com/JtheGunner/omnishell/internal/pkgmgr"
	"github.com/JtheGunner/omnishell/internal/platform"
	"github.com/JtheGunner/omnishell/internal/render"
	"github.com/JtheGunner/omnishell/modules"
)

func bleShRegistry(t *testing.T) module.Registry {
	t.Helper()
	reg, err := module.LoadRegistry(modules.FS(), "")
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

func bleShModule(t *testing.T) module.Module {
	t.Helper()
	m, ok := bleShRegistry(t).Get("ble-sh")
	if !ok {
		t.Fatal("module ble-sh not embedded")
	}
	return m
}

func renderBleShWithVendor(t *testing.T, vendorDir string) string {
	t.Helper()
	body, has, err := bleShModule(t).Template("bash")
	if err != nil || !has {
		t.Fatalf("bash template: has=%v err=%v", has, err)
	}
	out, err := render.Render(body, render.Context{
		Options: map[string]any{}, Platform: "linux", Shell: "bash",
		VendorDir: vendorDir, ConfigDir: filepath.Dir(vendorDir),
		Bin: map[string]string{}, Active: map[string]bool{},
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestBleShGolden(t *testing.T) {
	assertGoldenNamed(t, "ble-sh", "bash", renderModule(t, "ble-sh", "bash", nil))
}

func TestBleShManifest(t *testing.T) {
	m := bleShModule(t)
	if got := m.Manifest.Shells; len(got) != 1 || got[0] != "bash" {
		t.Errorf("shells = %v, want [bash]", got)
	}
	if _, has, err := m.Template("zsh"); err != nil || has {
		t.Errorf("zsh template: has=%v err=%v, want none", has, err)
	}
	for _, mgr := range []string{"brew", "apt", "dnf", "pacman", "zypper", "apk"} {
		if got := m.Manifest.Packages.ForManager(mgr); len(got) != 0 {
			t.Errorf("%s packages = %v, want none", mgr, got)
		}
	}
	fbs := m.Manifest.Packages.Fallback
	if len(fbs) != 1 {
		t.Fatalf("fallbacks = %+v, want exactly one", fbs)
	}
	fb := fbs[0]
	if fb.Type != "git" || fb.Ref != "v0.4.0-devel3" || fb.Repo != "https://github.com/akinomyoga/ble.sh.git" {
		t.Errorf("fallback = %+v, want pinned git clone of ble.sh", fb)
	}
	if got := strings.Join(fb.Run, " "); got != "make -C {{.VendorDir}}/ble.sh" {
		t.Errorf("run = %q", got)
	}
	if got := strings.Join(fb.Requires, ","); got != "make,gawk,git" {
		t.Errorf("requires = %q, want make,gawk,git", got)
	}
	if got := strings.Join(m.Manifest.After, ","); got != "completion,history" {
		t.Errorf("after = %q, want completion,history", got)
	}
	if got := strings.Join(m.Manifest.Conflicts, ","); got != "fzf,atuin" {
		t.Errorf("conflicts = %q, want fzf,atuin", got)
	}
}

func TestBleShConflicts(t *testing.T) {
	reg := bleShRegistry(t)
	for _, other := range []string{"fzf", "atuin"} {
		active := map[string]module.Manifest{}
		for _, id := range []string{"ble-sh", other} {
			m, ok := reg.Get(id)
			if !ok {
				t.Fatalf("module %s missing", id)
			}
			active[id] = m.Manifest
		}
		if _, err := graph.Order(active); err == nil {
			t.Errorf("ble-sh together with %s must be a conflict error", other)
		}
	}
}

func TestBleShLoadsAfterCompletionAndHistory(t *testing.T) {
	reg := bleShRegistry(t)
	active := map[string]module.Manifest{}
	for _, id := range []string{"ble-sh", "completion", "history"} {
		m, ok := reg.Get(id)
		if !ok {
			t.Fatalf("module %s missing", id)
		}
		active[id] = m.Manifest
	}
	order, err := graph.Order(active)
	if err != nil {
		t.Fatal(err)
	}
	pos := map[string]int{}
	for i, id := range order {
		pos[id] = i
	}
	for _, dep := range []string{"completion", "history"} {
		if pos["ble-sh"] < pos[dep] {
			t.Errorf("ble-sh loads before %s: %v", dep, order)
		}
	}
}

// A module without package entries but with a fallback must plan the pinned
// clone as a fresh install.
func TestBleShPlansFallbackWithoutPackages(t *testing.T) {
	mgr := &pkgmgr.MockManager{NameV: "apt", DetectV: true, Installed: map[string]bool{}}
	e := engine.Engine{
		Platform: platform.Info{
			OS: platform.Linux, HomeDir: t.TempDir(), ConfigDir: t.TempDir(),
			Shells: []platform.ShellInfo{{Name: "bash", RCPath: "/x/.bashrc", Present: true}},
		},
		Registry:  bleShRegistry(t),
		Manager:   mgr,
		ManagerOK: true,
		Runner:    &pkgmgr.MockRunner{},
		Now:       func() time.Time { return time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC) },
	}
	cfg := config.Config{
		Omnishell: config.OmnishellSection{Version: 1, Shells: []string{"bash"}},
		Modules:   map[string]config.ModuleConfig{"ble-sh": {Enabled: true}},
	}
	p, err := engine.ComputePlan(e, cfg, lockfile.Lock{Modules: map[string]lockfile.ModuleState{}}, false)
	if err != nil {
		t.Fatal(err)
	}
	mp := p.Modules["ble-sh"]
	if !mp.UsesFallback || len(mp.MissingPackages) != 1 || mp.MissingPackages[0].Manager != "git" {
		t.Fatalf("plan = %+v, want the git fallback planned", mp)
	}
	if len(mgr.InstallCalls) != 0 {
		t.Fatalf("planning must not install: %+v", mgr.InstallCalls)
	}
}

func TestBleShSnippetInRealBash(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not installed")
	}
	vendor := filepath.Join(t.TempDir(), "vendor dir")
	stubDir := filepath.Join(vendor, "ble.sh", "out")
	if err := os.MkdirAll(stubDir, 0o755); err != nil {
		t.Fatal(err)
	}
	stub := "echo \"stub-sourced args=$*\"\n"
	if err := os.WriteFile(filepath.Join(stubDir, "ble.sh"), []byte(stub), 0o644); err != nil {
		t.Fatal(err)
	}
	snippet := renderBleShWithVendor(t, vendor)

	run := func(args ...string) string {
		t.Helper()
		out, err := exec.Command(bash, append(args, snippet)...).CombinedOutput()
		if err != nil {
			t.Fatalf("bash %v: %v\n%s", args, err, out)
		}
		return string(out)
	}

	if got := run("--norc", "--noprofile", "-i", "-c"); !strings.Contains(got, "stub-sourced args=--attach=prompt") {
		t.Errorf("interactive shell must source ble.sh with --attach=prompt, got %q", got)
	}
	if got := run("--norc", "--noprofile", "-c"); strings.Contains(got, "stub-sourced") {
		t.Errorf("non-interactive shell must not source ble.sh, got %q", got)
	}

	missing := renderBleShWithVendor(t, filepath.Join(t.TempDir(), "none"))
	// Stdout only: an interactive bash without a terminal warns on stderr.
	out, err := exec.Command(bash, "--norc", "--noprofile", "-i", "-c", missing).Output()
	if err != nil {
		t.Fatalf("missing file: %v\n%s", err, out)
	}
	if strings.TrimSpace(string(out)) != "" {
		t.Errorf("a missing ble.sh must be silent, got %q", out)
	}
}

func TestBleShSnippetParses(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not installed")
	}
	snippet := renderModule(t, "ble-sh", "bash", nil)
	if out, err := exec.Command(bash, "-n", "-c", snippet).CombinedOutput(); err != nil {
		t.Errorf("bash -n: %v\n%s", err, out)
	}
}
