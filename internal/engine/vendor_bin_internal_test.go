package engine

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/JtheGunner/omnishell/internal/initfile"
	"github.com/JtheGunner/omnishell/internal/lockfile"
	"github.com/JtheGunner/omnishell/internal/module"
	"github.com/JtheGunner/omnishell/internal/pkgmgr"
	"github.com/JtheGunner/omnishell/internal/platform"
)

func vendorBinEngine(t *testing.T, home string) Engine {
	t.Helper()
	reg, err := module.LoadRegistry(nil, "testdata/modules")
	if err != nil {
		t.Fatal(err)
	}
	return Engine{
		Platform: platform.Info{
			OS:        platform.Linux,
			HomeDir:   home,
			ConfigDir: filepath.Join(home, ".config", "omnishell"),
			Shells:    []platform.ShellInfo{{Name: "bash", RCPath: filepath.Join(home, ".bashrc"), Present: true}},
		},
		Registry:  reg,
		Manager:   &pkgmgr.MockManager{NameV: "apt", DetectV: true, Installed: map[string]bool{}},
		ManagerOK: true,
		Runner:    &pkgmgr.MockRunner{},
		Now:       func() time.Time { return time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC) },
		Stdout:    &bytes.Buffer{},
		Stderr:    &bytes.Buffer{},
	}
}

func completionPlan(t *testing.T, e Engine) Plan {
	t.Helper()
	m, ok := e.Registry.Get("completion")
	if !ok {
		t.Fatal("completion module missing from testdata")
	}
	return Plan{
		Order:         []string{"completion"},
		ManagedShells: []string{"bash"},
		Modules: map[string]ModulePlan{
			"completion": {ID: "completion", Action: ActionUnchanged, Shells: []string{"bash"}, Manifest: m.Manifest},
		},
	}
}

func TestBuildSectionsPutsVendorBinFirst(t *testing.T) {
	e := vendorBinEngine(t, t.TempDir())
	plan := completionPlan(t, e)
	rendered := e.renderAll(plan, map[string]string{})

	sections := e.buildSections(plan, rendered, map[string]string{}, "bash")
	if len(sections) != 2 {
		t.Fatalf("got %d sections, want the vendor-bin section and completion: %+v", len(sections), sections)
	}
	if sections[0].ID != vendorBinSectionID || sections[1].ID != "completion" {
		t.Fatalf("section order = %q, %q; want %q first", sections[0].ID, sections[1].ID, vendorBinSectionID)
	}
	want := filepath.Join(e.Platform.ConfigDir, "vendor", "bin")
	if !strings.Contains(sections[0].Body, "'"+want+"'") {
		t.Fatalf("vendor-bin section does not name %s:\n%s", want, sections[0].Body)
	}
}

func TestBuildSectionsWithoutModulesStaysEmpty(t *testing.T) {
	e := vendorBinEngine(t, t.TempDir())
	plan := completionPlan(t, e)
	rendered := e.renderAll(plan, map[string]string{})

	// zsh is not a shell of the planned module, so there is nothing to emit.
	if sections := e.buildSections(plan, rendered, map[string]string{}, "zsh"); len(sections) != 0 {
		t.Fatalf("got sections for a shell with no module snippet: %+v", sections)
	}
	degraded := map[string]string{"completion": "broken"}
	if sections := e.buildSections(plan, rendered, degraded, "bash"); len(sections) != 0 {
		t.Fatalf("got sections although every module is degraded: %+v", sections)
	}
}

// runs the vendor-bin section in a real bash and returns what the script prints
func runVendorBinSection(t *testing.T, e Engine, pathEnv, script string) string {
	t.Helper()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	body := e.vendorBinSection().Body
	cmd := exec.Command(bash, "--noprofile", "--norc", "-c", body+body+script)
	cmd.Env = []string{"PATH=" + pathEnv, "HOME=" + e.Platform.HomeDir}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bash: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

func writeTool(t *testing.T, dir, name string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestVendorBinSectionInShell(t *testing.T) {
	const basePath = "/usr/bin:/bin"

	t.Run("makes a vendored binary resolvable", func(t *testing.T) {
		// a quote and a space in the path must survive the shell quoting
		e := vendorBinEngine(t, filepath.Join(t.TempDir(), "it's a home"))
		vendorBin := filepath.Join(e.vendorDir(), "bin")
		tool := writeTool(t, vendorBin, "omnishell-test-tool")

		got := runVendorBinSection(t, e, basePath, `command -v omnishell-test-tool; printf '%s\n' "$PATH"`)
		lines := strings.Split(got, "\n")
		if len(lines) != 2 || lines[0] != tool {
			t.Fatalf("command -v = %q, want %q", got, tool)
		}
		// the section ran twice: the directory is appended once, after the rest
		if want := basePath + ":" + vendorBin; lines[1] != want {
			t.Fatalf("PATH = %q, want %q", lines[1], want)
		}
	})

	t.Run("a copy earlier on PATH wins", func(t *testing.T) {
		e := vendorBinEngine(t, t.TempDir())
		writeTool(t, filepath.Join(e.vendorDir(), "bin"), "omnishell-test-tool")
		systemDir := t.TempDir()
		system := writeTool(t, systemDir, "omnishell-test-tool")

		got := runVendorBinSection(t, e, systemDir+":"+basePath, `command -v omnishell-test-tool`)
		if got != system {
			t.Fatalf("command -v = %q, want the system copy %q", got, system)
		}
	})

	t.Run("leaves PATH alone without a vendor bin directory", func(t *testing.T) {
		e := vendorBinEngine(t, t.TempDir())
		got := runVendorBinSection(t, e, basePath, `printf '%s|%s\n' "$PATH" "${_omnishell_vendor_bin-unset}"`)
		if want := basePath + "|unset"; got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})
}

// An init file written before the vendor-bin section existed is a pristine
// generated file: the next apply must add the section, not report a hand edit.
func TestApplyAddsVendorBinSectionToOlderInitFile(t *testing.T) {
	home := t.TempDir()
	e := vendorBinEngine(t, home)
	cfgPath := filepath.Join(e.Platform.ConfigDir, "config.toml")
	lockPath := filepath.Join(e.Platform.ConfigDir, "state.lock.json")
	if err := os.MkdirAll(e.Platform.ConfigDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgBody := "[omnishell]\nversion = 1\nshells = [\"bash\"]\n[modules.completion]\nenabled = true\n"
	if err := os.WriteFile(cfgPath, []byte(cfgBody), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Apply(cfg, cfgPath, lockPath, ApplyOptions{Yes: true}); err != nil {
		t.Fatalf("first apply: %v", err)
	}

	// rewrite the init file and the lock as the previous version left them
	lock, _, err := lockfile.Load(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := ComputePlan(e, cfg, lock, false)
	if err != nil {
		t.Fatal(err)
	}
	degraded := plannedDegraded(plan)
	older := e.buildSections(plan, e.renderAll(plan, degraded), degraded, "bash")[1:]
	initPath := e.initPath("bash")
	if err := os.WriteFile(initPath, []byte(initfile.Build("bash", older, e.now())), 0o644); err != nil {
		t.Fatal(err)
	}
	state := lock.InitFiles["bash"]
	state.ContentHash = initfile.ContentHash(older)
	lock.InitFiles["bash"] = state
	if err := lock.Write(lockPath); err != nil {
		t.Fatal(err)
	}

	res, err := e.Apply(cfg, cfgPath, lockPath, ApplyOptions{Yes: true})
	if err != nil {
		t.Fatalf("apply over an older init file: %v", err)
	}
	if !res.Changed || res.NothingToDo {
		t.Fatalf("apply did not rewrite the older init file: %+v", res)
	}
	body, err := os.ReadFile(initPath)
	if err != nil {
		t.Fatal(err)
	}
	vendorAt := strings.Index(string(body), "# >>> omnishell:"+vendorBinSectionID+" ")
	moduleAt := strings.Index(string(body), "# >>> omnishell:completion ")
	if vendorAt < 0 || moduleAt < 0 || vendorAt > moduleAt {
		t.Fatalf("vendor-bin section missing or not first:\n%s", body)
	}

	// and the result is stable: nothing left to do
	res, err = e.Apply(cfg, cfgPath, lockPath, ApplyOptions{Yes: true})
	if err != nil || !res.NothingToDo {
		t.Fatalf("second apply: NothingToDo=%v err=%v", res.NothingToDo, err)
	}
}
