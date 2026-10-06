package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JtheGunner/omnishell/internal/lockfile"
	"github.com/JtheGunner/omnishell/internal/module"
	"github.com/JtheGunner/omnishell/internal/pkgmgr"
	"github.com/JtheGunner/omnishell/internal/platform"
)

func fallbackModulePlan(ref string) ModulePlan {
	return ModulePlan{Manifest: module.Manifest{Packages: module.Packages{
		Fallback: []module.Fallback{{
			Type: "git", Repo: "https://example.com/x.git", Dest: "{{.VendorDir}}/x", Ref: ref,
		}},
	}}}
}

func engineWithClone(t *testing.T, populated bool) Engine {
	t.Helper()
	configDir := t.TempDir()
	if populated {
		dir := filepath.Join(configDir, "vendor", "x")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "file"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return Engine{Platform: platform.Info{OS: platform.Linux, ConfigDir: configDir}}
}

func TestPlanFallback(t *testing.T) {
	cases := []struct {
		name       string
		populated  bool
		ref        string
		prev       lockfile.ModuleState
		wantMissed int
		wantUpdate bool
	}{
		{"fresh clone", false, "v2", lockfile.ModuleState{}, 1, false},
		{"pinned and recorded", true, "v2", lockfile.ModuleState{FallbackRef: "v2"}, 0, false},
		{"pinned, recorded ref differs", true, "v2", lockfile.ModuleState{FallbackRef: "v1"}, 1, true},
		{"pinned, nothing recorded", true, "v2", lockfile.ModuleState{}, 1, true},
		{"no ref keeps a populated clone", true, "", lockfile.ModuleState{}, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mp := fallbackModulePlan(tc.ref)
			planFallback(&mp, engineWithClone(t, tc.populated), tc.prev)
			if !mp.UsesFallback {
				t.Fatal("UsesFallback = false, want true")
			}
			if len(mp.MissingPackages) != tc.wantMissed {
				t.Fatalf("MissingPackages = %+v, want %d entries", mp.MissingPackages, tc.wantMissed)
			}
			if tc.wantMissed == 0 {
				return
			}
			pp := mp.MissingPackages[0]
			if pp.Manager != "git" || pp.Update != tc.wantUpdate {
				t.Fatalf("entry = %+v, want a git entry with Update=%v", pp, tc.wantUpdate)
			}
			if tc.wantUpdate && (pp.From != tc.prev.FallbackRef || pp.To != tc.ref) {
				t.Fatalf("From/To = %q/%q, want %q/%q", pp.From, pp.To, tc.prev.FallbackRef, tc.ref)
			}
		})
	}
}

func TestPlanFallbackNeverRunsCommands(t *testing.T) {
	runner := &pkgmgr.MockRunner{}
	e := engineWithClone(t, true)
	e.Runner = runner
	mp := fallbackModulePlan("v2")
	planFallback(&mp, e, lockfile.ModuleState{FallbackRef: "v1"})
	if len(runner.Calls) != 0 {
		t.Fatalf("planning must stay side-effect-free, but ran: %v", runner.Calls)
	}
}

func TestRenderPlanNamesAFallbackUpdate(t *testing.T) {
	plan := func(from string) Plan {
		return Plan{
			Order:            []string{"x"},
			ManagedShells:    []string{"bash"},
			PackageManager:   "apt",
			ManagerAvailable: true,
			Modules: map[string]ModulePlan{"x": {
				ID: "x", Action: ActionUpdate, Shells: []string{"bash"},
				MissingPackages: []PackagePlan{{Name: "https://example.com/x.git", Manager: "git", Update: true, From: from, To: "v2"}},
			}},
		}
	}
	if got := RenderPlan(plan("v1")); !strings.Contains(got, "git update v1 → v2, rebuild") {
		t.Fatalf("plan text lacks the update:\n%s", got)
	}
	if got := RenderPlan(plan("")); !strings.Contains(got, "git update unrecorded → v2, rebuild") {
		t.Fatalf("plan text lacks the unrecorded wording:\n%s", got)
	}
}
