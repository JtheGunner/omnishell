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

func zesRegistry(t *testing.T) module.Registry {
	t.Helper()
	reg, err := module.LoadRegistry(modules.FS(), "")
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

func zesModule(t *testing.T) module.Module {
	t.Helper()
	m, ok := zesRegistry(t).Get("zsh-edit-select")
	if !ok {
		t.Fatal("module zsh-edit-select not embedded")
	}
	return m
}

func TestZshEditSelectGolden(t *testing.T) {
	assertGoldenNamed(t, "zsh-edit-select", "zsh", renderModule(t, "zsh-edit-select", "zsh", nil))
}

// The cheatsheet names Cmd on macOS and Ctrl elsewhere, and only macOS needs
// the Accessibility permission.
func TestZshEditSelectCheatsheetPerPlatform(t *testing.T) {
	body, has, err := zesModule(t).Cheatsheet()
	if err != nil || !has {
		t.Fatalf("cheatsheet: has=%v err=%v", has, err)
	}
	for platform, want := range map[string]struct{ key, ax bool }{
		"macos": {key: true, ax: true},
		"linux": {key: false, ax: false},
	} {
		out, err := render.Render(body, render.Context{
			Options: map[string]any{}, Platform: platform, Shell: "zsh",
			VendorDir: "/home/j/.config/omnishell/vendor",
			ConfigDir: "/home/j/.config/omnishell",
			Bin:       map[string]string{}, Active: map[string]bool{},
		})
		if err != nil {
			t.Fatalf("%s: %v", platform, err)
		}
		if got := strings.Contains(out, "`Cmd+C`"); got != want.key {
			t.Errorf("%s: mentions Cmd+C = %v, want %v\n%s", platform, got, want.key, out)
		}
		if got := strings.Contains(out, "`Ctrl+C`"); got == want.key {
			t.Errorf("%s: mentions Ctrl+C = %v, want %v\n%s", platform, got, !want.key, out)
		}
		if got := strings.Contains(out, "setup-ax"); got != want.ax {
			t.Errorf("%s: mentions setup-ax = %v, want %v", platform, got, want.ax)
		}
		if !strings.Contains(out, "manual install") {
			t.Errorf("%s: the cheatsheet must warn about a manual install:\n%s", platform, out)
		}
	}
}

func TestZshEditSelectManifest(t *testing.T) {
	m := zesModule(t)
	if got := m.Manifest.Shells; len(got) != 1 || got[0] != "zsh" {
		t.Errorf("shells = %v, want [zsh]", got)
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
	if fb.Type != "git" || fb.Ref != "v0.7.01" || fb.Repo != "https://github.com/Michael-Matta1/zsh-edit-select.git" {
		t.Errorf("fallback = %+v, want the pinned git clone", fb)
	}
	if got := strings.Join(m.Manifest.After, ","); got != "completion,autosuggestions,syntax-highlighting" {
		t.Errorf("after = %q", got)
	}
}

func TestZshEditSelectLoadsAfterAutosuggestionsAndHighlighting(t *testing.T) {
	reg := zesRegistry(t)
	active := map[string]module.Manifest{}
	for _, id := range []string{"zsh-edit-select", "completion", "autosuggestions", "syntax-highlighting", "you-should-use", "fzf-tab", "fzf"} {
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
	for _, dep := range []string{"completion", "autosuggestions", "syntax-highlighting"} {
		if pos["zsh-edit-select"] < pos[dep] {
			t.Errorf("zsh-edit-select loads before %s: %v", dep, order)
		}
	}
}

// The build is the only platform-dependent part: macOS builds the agent from
// the pinned clone, every other platform renders an empty command and so skips
// the build.
func TestZshEditSelectBuildRunsOnMacOSOnly(t *testing.T) {
	fb := zesModule(t).Manifest.Packages.Fallback[0]
	for platform, wantBuild := range map[string]bool{"macos": true, "linux": false} {
		vendor := filepath.Join(t.TempDir(), "vendor")
		runner := &pkgmgr.MockRunner{}
		ctx := pkgmgr.FallbackContext{VendorDir: vendor, Platform: platform}
		dest, err := pkgmgr.InstallGitFallback(fb, ctx, runner)
		if err != nil {
			t.Fatalf("%s: %v", platform, err)
		}
		if want := filepath.Join(vendor, "zsh-edit-select"); dest != want {
			t.Errorf("%s: dest = %q, want %q", platform, dest, want)
		}
		build := "make -C " + filepath.Join(vendor, "zsh-edit-select", "impl-macos", "backends", "macos")
		var cloned, built bool
		for _, c := range runner.Calls {
			if strings.Contains(c, " clone ") && strings.Contains(c, "--branch v0.7.01") {
				cloned = true
			}
			if c == build {
				built = true
			}
			if strings.HasPrefix(c, "-C ") {
				t.Errorf("%s: a build with an empty command was run: %q", platform, c)
			}
		}
		if !cloned {
			t.Errorf("%s: pinned clone not run: %v", platform, runner.Calls)
		}
		if built != wantBuild {
			t.Errorf("%s: built = %v, want %v (calls %v)", platform, built, wantBuild, runner.Calls)
		}
	}
}

// A failed macOS build must fail the install, so apply reports the module as
// degraded instead of leaving a plugin without its agent.
func TestZshEditSelectBuildFailureFailsInstall(t *testing.T) {
	fb := zesModule(t).Manifest.Packages.Fallback[0]
	vendor := filepath.Join(t.TempDir(), "vendor")
	build := "make -C " + filepath.Join(vendor, "zsh-edit-select", "impl-macos", "backends", "macos")
	runner := &pkgmgr.MockRunner{Responses: map[string]pkgmgr.MockResponse{
		build: {Err: os.ErrNotExist},
	}}
	_, err := pkgmgr.InstallGitFallback(fb, pkgmgr.FallbackContext{VendorDir: vendor, Platform: "macos"}, runner)
	if err == nil || !strings.Contains(err.Error(), "fallback run") {
		t.Fatalf("err = %v, want a fallback run error", err)
	}
}

// With no package manager the module is still planned: it has no package
// entries, only the fallback (see the engine's planPackages).
func TestZshEditSelectPlansFallbackWithoutManager(t *testing.T) {
	e := engine.Engine{
		Platform: platform.Info{
			OS: platform.MacOS, HomeDir: t.TempDir(), ConfigDir: t.TempDir(),
			Shells: []platform.ShellInfo{{Name: "zsh", RCPath: "/x/.zshrc", Present: true}},
		},
		Registry:  zesRegistry(t),
		ManagerOK: false,
		Runner:    &pkgmgr.MockRunner{},
		Now:       func() time.Time { return time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC) },
	}
	cfg := config.Config{
		Omnishell: config.OmnishellSection{Version: 1, Shells: []string{"zsh"}},
		Modules:   map[string]config.ModuleConfig{"zsh-edit-select": {Enabled: true}},
	}
	p, err := engine.ComputePlan(e, cfg, lockfile.Lock{Modules: map[string]lockfile.ModuleState{}}, false)
	if err != nil {
		t.Fatal(err)
	}
	mp := p.Modules["zsh-edit-select"]
	if mp.DegradedReason != "" || !mp.UsesFallback || len(mp.MissingPackages) != 1 || mp.MissingPackages[0].Manager != "git" {
		t.Fatalf("plan = %+v, want the git fallback planned without a manager", mp)
	}
}

func TestZshEditSelectSnippetInRealZsh(t *testing.T) {
	zsh, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("zsh not installed")
	}
	vendor := filepath.Join(t.TempDir(), "vendor dir")
	pluginDir := filepath.Join(vendor, "zsh-edit-select")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	stub := "echo stub-sourced\n"
	if err := os.WriteFile(filepath.Join(pluginDir, "zsh-edit-select.plugin.zsh"), []byte(stub), 0o644); err != nil {
		t.Fatal(err)
	}
	snippet := func(vendorDir string) string {
		body, has, err := zesModule(t).Template("zsh")
		if err != nil || !has {
			t.Fatalf("zsh template: has=%v err=%v", has, err)
		}
		out, err := renderSnippet(body, vendorDir)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}

	out, err := exec.Command(zsh, "-f", "-c", snippet(vendor)).CombinedOutput()
	if err != nil {
		t.Fatalf("zsh: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "stub-sourced") {
		t.Errorf("snippet must source the plugin from a path with a space, got %q", out)
	}

	missing := snippet(filepath.Join(t.TempDir(), "none"))
	out, err = exec.Command(zsh, "-f", "-c", missing).CombinedOutput()
	if err != nil {
		t.Fatalf("missing plugin: %v\n%s", err, out)
	}
	if strings.TrimSpace(string(out)) != "" {
		t.Errorf("a missing plugin must be silent, got %q", out)
	}

	if out, err := exec.Command(zsh, "-n", "-c", snippet(vendor)).CombinedOutput(); err != nil {
		t.Errorf("zsh -n: %v\n%s", err, out)
	}
}

func renderSnippet(body, vendorDir string) (string, error) {
	return render.Render(body, render.Context{
		Options: map[string]any{}, Platform: "macos", Shell: "zsh",
		VendorDir: vendorDir, ConfigDir: filepath.Dir(vendorDir),
		Bin: map[string]string{}, Active: map[string]bool{},
	})
}
