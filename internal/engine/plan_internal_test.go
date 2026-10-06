package engine

import (
	"io"
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
		{"an update already declined for this ref is not planned again", true, "v2", lockfile.ModuleState{FallbackSkippedRef: "v2"}, 0, false},
		{"a newer ref after a declined one is planned", true, "v3", lockfile.ModuleState{FallbackSkippedRef: "v2"}, 1, true},
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

func releaseFallbackEntry(ref string) module.Fallback {
	return module.Fallback{
		Type: "release", Repo: "https://example.com/x", Ref: ref, Bin: "x",
		Assets: []module.Asset{{OS: "linux", Arch: "amd64", URL: "https://example.com/{{.Ref}}/x", SHA256: strings.Repeat("a", 64)}},
	}
}

func gitFallbackEntry(ref string) module.Fallback {
	return module.Fallback{Type: "git", Repo: "https://example.com/x.git", Dest: "{{.VendorDir}}/x", Ref: ref}
}

func modulePlanWith(fbs ...module.Fallback) ModulePlan {
	return ModulePlan{Manifest: module.Manifest{Packages: module.Packages{Fallback: fbs}}}
}

// engineWithBinary builds an engine whose vendor/bin/x exists when present.
func engineWithBinary(t *testing.T, present bool, arch string) Engine {
	t.Helper()
	configDir := t.TempDir()
	if present {
		bin := filepath.Join(configDir, "vendor", "bin", "x")
		if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(bin, []byte("x"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return Engine{Platform: platform.Info{OS: platform.Linux, Arch: arch, ConfigDir: configDir}}
}

func TestSelectFallback(t *testing.T) {
	amd64 := platform.Info{OS: platform.Linux, Arch: "amd64"}
	riscv := platform.Info{OS: platform.Linux, Arch: "riscv64"}
	cases := []struct {
		name     string
		fbs      []module.Fallback
		info     platform.Info
		wantType string
		wantOK   bool
	}{
		{"release comes first and matches", []module.Fallback{releaseFallbackEntry("v1"), gitFallbackEntry("v1")}, amd64, "release", true},
		{"unsupported architecture falls through to git", []module.Fallback{releaseFallbackEntry("v1"), gitFallbackEntry("v1")}, riscv, "git", true},
		{"a git entry listed first wins", []module.Fallback{gitFallbackEntry("v1"), releaseFallbackEntry("v1")}, amd64, "git", true},
		{"release only and no matching asset", []module.Fallback{releaseFallbackEntry("v1")}, riscv, "", false},
		{"no fallbacks", nil, amd64, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fb, ok := selectFallback(tc.fbs, tc.info)
			if ok != tc.wantOK || fb.Type != tc.wantType {
				t.Fatalf("selectFallback = %q, %v; want %q, %v", fb.Type, ok, tc.wantType, tc.wantOK)
			}
		})
	}
}

func TestPlanRelease(t *testing.T) {
	cases := []struct {
		name       string
		present    bool
		prev       lockfile.ModuleState
		wantMissed int
		wantUpdate bool
		wantFrom   string
	}{
		{"fresh install", false, lockfile.ModuleState{}, 1, false, ""},
		{"settled", true, lockfile.ModuleState{FallbackKind: "release", FallbackRef: "v2"}, 0, false, ""},
		{"recorded but the binary is gone", false, lockfile.ModuleState{FallbackKind: "release", FallbackRef: "v2"}, 1, false, ""},
		{"older release is replaced", true, lockfile.ModuleState{FallbackKind: "release", FallbackRef: "v1"}, 1, true, "v1"},
		{"a git build is replaced", true, lockfile.ModuleState{FallbackKind: "git", FallbackRef: "v1"}, 1, true, "v1"},
		{"a legacy lockfile entry counts as git", true, lockfile.ModuleState{FallbackRef: "v1"}, 1, true, "v1"},
		{"a binary nobody recorded is replaced", true, lockfile.ModuleState{}, 1, true, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mp := modulePlanWith(releaseFallbackEntry("v2"), gitFallbackEntry("v2"))
			planFallback(&mp, engineWithBinary(t, tc.present, "amd64"), tc.prev)
			if !mp.UsesFallback || mp.Fallback.Type != "release" {
				t.Fatalf("UsesFallback=%v Fallback=%+v, want the release entry", mp.UsesFallback, mp.Fallback)
			}
			if len(mp.MissingPackages) != tc.wantMissed {
				t.Fatalf("MissingPackages = %+v, want %d", mp.MissingPackages, tc.wantMissed)
			}
			if tc.wantMissed == 0 {
				return
			}
			pp := mp.MissingPackages[0]
			if pp.Manager != "release" || pp.Name != "x" || pp.To != "v2" || pp.Update != tc.wantUpdate || pp.From != tc.wantFrom {
				t.Fatalf("entry = %+v", pp)
			}
		})
	}
}

func TestPlanFallbackFallsThroughToGitOnAnUnsupportedArchitecture(t *testing.T) {
	mp := modulePlanWith(releaseFallbackEntry("v2"), gitFallbackEntry("v2"))
	planFallback(&mp, engineWithBinary(t, false, "riscv64"), lockfile.ModuleState{})
	if mp.Fallback.Type != "git" || len(mp.MissingPackages) != 1 || mp.MissingPackages[0].Manager != "git" {
		t.Fatalf("Fallback=%+v Missing=%+v, want the git entry queued", mp.Fallback, mp.MissingPackages)
	}
}

func TestPlanFallbackDegradesWhenNothingIsUsable(t *testing.T) {
	mp := modulePlanWith(releaseFallbackEntry("v2"))
	planFallback(&mp, engineWithBinary(t, false, "riscv64"), lockfile.ModuleState{})
	if mp.UsesFallback || mp.DegradedReason == "" || len(mp.MissingPackages) != 0 {
		t.Fatalf("UsesFallback=%v Reason=%q Missing=%+v", mp.UsesFallback, mp.DegradedReason, mp.MissingPackages)
	}
	if !strings.Contains(mp.DegradedReason, "linux/riscv64") {
		t.Fatalf("reason %q does not name the platform", mp.DegradedReason)
	}
}

type neverDownloader struct{ calls int }

func (n *neverDownloader) Download(string, io.Writer, int64) error {
	n.calls++
	return nil
}

func TestPlanReleaseNeverRunsCommandsOrDownloads(t *testing.T) {
	runner := &pkgmgr.MockRunner{}
	dl := &neverDownloader{}
	e := engineWithBinary(t, true, "amd64")
	e.Runner, e.Downloader = runner, dl
	mp := modulePlanWith(releaseFallbackEntry("v2"), gitFallbackEntry("v2"))
	planFallback(&mp, e, lockfile.ModuleState{FallbackKind: "release", FallbackRef: "v1"})
	if len(runner.Calls) != 0 || dl.calls != 0 {
		t.Fatalf("planning must be side-effect-free; commands=%v downloads=%d", runner.Calls, dl.calls)
	}
}

func TestDescribeReleasePackages(t *testing.T) {
	if got := describePackage(PackagePlan{Name: "x", Manager: "release", To: "v2"}); got != "x (release binary v2)" {
		t.Fatalf("fresh = %q", got)
	}
	if got := describePackage(PackagePlan{Name: "x", Manager: "release", Update: true, From: "v1", To: "v2"}); got != "x (release update v1 → v2)" {
		t.Fatalf("update = %q", got)
	}
	if got := describePackage(PackagePlan{Name: "x", Manager: "release", Update: true, To: "v2"}); got != "x (release update unrecorded → v2)" {
		t.Fatalf("unrecorded = %q", got)
	}
}
