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
	"github.com/JtheGunner/omnishell/internal/lockfile"
	"github.com/JtheGunner/omnishell/internal/module"
	"github.com/JtheGunner/omnishell/internal/pkgmgr"
)

// writeMultiModule writes a user module "multitool" with two manager packages,
// alpha and beta, and a release fallback that stands in for alpha only.
func writeMultiModule(t *testing.T, dir string) {
	t.Helper()
	modDir := filepath.Join(dir, "multitool")
	if err := os.MkdirAll(modDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `platforms = ["macos", "linux"]
shells    = ["bash"]
requires  = []
after     = []

[module]
id          = "multitool"
name        = "multitool"
description = "Fixture: two packages, a release fallback for one of them"
version     = "1.0.0"
schema      = 1

[packages]
apt = ["alpha", "beta"]

[[packages.fallback]]
type     = "release"
repo     = "https://example.com/alpha"
ref      = "v1.0.0"
bin      = "alpha"
replaces = ["alpha"]

[[packages.fallback.assets]]
os     = "linux"
arch   = "amd64"
url    = "https://example.com/alpha/{{.Ref}}/alpha-linux-amd64"
sha256 = "` + relSHA(relPayload) + `"
`
	if err := os.WriteFile(filepath.Join(modDir, "manifest.toml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modDir, "bash.tmpl"), []byte("echo multitool\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

type multiSandbox struct {
	e                 engine.Engine
	home              string
	cfgPath, lockPath string
	out               *bytes.Buffer
	mgr               *pkgmgr.MockManager
	dl                *relDownloader
}

// newMultiSandbox builds an amd64 apt host with the multitool module enabled;
// unavailable names the packages the repositories cannot provide.
func newMultiSandbox(t *testing.T, unavailable ...string) multiSandbox {
	t.Helper()
	home := t.TempDir()
	out := &bytes.Buffer{}
	mgr := &pkgmgr.MockManager{NameV: "apt", DetectV: true, Installed: map[string]bool{}, Unavailable: map[string]bool{}}
	for _, name := range unavailable {
		mgr.Unavailable[name] = true
	}
	e := applyEngine(t, home, mgr, out)
	modDir := t.TempDir()
	writeMultiModule(t, modDir)
	reg, err := module.LoadRegistry(nil, modDir)
	if err != nil {
		t.Fatal(err)
	}
	dl := &relDownloader{body: []byte(relPayload)}
	e.Registry, e.Downloader = reg, dl
	e.Platform.Arch = "amd64"
	cfgPath := filepath.Join(home, ".config", "omnishell", "config.toml")
	writeConfig(t, cfgPath, "[omnishell]\nversion=1\nshells=[\"bash\"]\n[modules.multitool]\nenabled=true\n")
	return multiSandbox{e: e, home: home, cfgPath: cfgPath,
		lockPath: filepath.Join(home, ".config", "omnishell", "state.lock.json"), out: out, mgr: mgr, dl: dl}
}

func (s multiSandbox) plan(t *testing.T) engine.ModulePlan {
	t.Helper()
	cfg, err := config.Load(s.cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	lock, _, err := lockfile.Load(s.lockPath)
	if err != nil {
		t.Fatal(err)
	}
	p, err := engine.ComputePlan(s.e, cfg, lock, false)
	if err != nil {
		t.Fatal(err)
	}
	return p.Modules["multitool"]
}

func (s multiSandbox) apply(t *testing.T) (engine.Result, error) {
	t.Helper()
	cfg, err := config.Load(s.cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	return s.e.Apply(cfg, s.cfgPath, s.lockPath, engine.ApplyOptions{Yes: true})
}

func (s multiSandbox) binPath() string {
	return filepath.Join(s.home, ".config", "omnishell", "vendor", "bin", "alpha")
}

// managers lists the planned packages as "manager:name".
func managers(mp engine.ModulePlan) []string {
	var out []string
	for _, pp := range mp.MissingPackages {
		out = append(out, pp.Manager+":"+pp.Name)
	}
	return out
}

func TestComputePlanPartialFallback(t *testing.T) {
	cases := []struct {
		name            string
		unavailable     []string
		wantFallback    bool
		wantUnavailable string
		wantPackages    string
	}{
		{"the replaced package is unavailable", []string{"alpha"}, true, "alpha", "release:alpha apt:beta"},
		{"everything is available", nil, false, "", "apt:alpha apt:beta"},
		{"only a package the fallback does not replace is unavailable", []string{"beta"}, false, "", "apt:alpha apt:beta"},
		{"both are unavailable", []string{"alpha", "beta"}, true, "alpha", "release:alpha apt:beta"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newMultiSandbox(t, tc.unavailable...)
			mp := s.plan(t)
			if mp.UsesFallback != tc.wantFallback {
				t.Fatalf("UsesFallback = %v, want %v", mp.UsesFallback, tc.wantFallback)
			}
			if got := strings.Join(mp.UnavailablePackages, " "); got != tc.wantUnavailable {
				t.Fatalf("UnavailablePackages = %q, want %q", got, tc.wantUnavailable)
			}
			if got := strings.Join(managers(mp), " "); got != tc.wantPackages {
				t.Fatalf("MissingPackages = %q, want %q", got, tc.wantPackages)
			}
			if mp.DegradedReason != "" {
				t.Fatalf("planning degraded the module: %s", mp.DegradedReason)
			}
			if len(s.mgr.InstallCalls) != 0 || len(s.dl.calls) != 0 {
				t.Fatalf("planning must not install: %v %v", s.mgr.InstallCalls, s.dl.calls)
			}
		})
	}
}

func TestComputePlanPartialFallbackSkipsAnInstalledPackage(t *testing.T) {
	s := newMultiSandbox(t, "alpha")
	s.mgr.Installed["beta"] = true
	mp := s.plan(t)
	if got := strings.Join(managers(mp), " "); got != "release:alpha" {
		t.Fatalf("MissingPackages = %q, want only the release binary", got)
	}
}

func TestApplyPartialFallbackInstallsBoth(t *testing.T) {
	s := newMultiSandbox(t, "alpha")
	res, err := s.apply(t)
	if err != nil {
		t.Fatalf("Apply: %v\n%s", err, s.out.String())
	}
	if len(s.mgr.InstallCalls) != 1 || strings.Join(s.mgr.InstallCalls[0], " ") != "beta" {
		t.Fatalf("manager installs = %v, want [[beta]]", s.mgr.InstallCalls)
	}
	if got, rerr := os.ReadFile(s.binPath()); rerr != nil || string(got) != relPayload {
		t.Fatalf("release binary = %q (err %v)", got, rerr)
	}
	if !strings.Contains(s.out.String(), "alpha: not available via apt, installing the release binary (ref v1.0.0)") {
		t.Fatalf("output lacks the fallback notice:\n%s", s.out.String())
	}
	for _, m := range res.Modules {
		if m.ID == "multitool" && m.Status != "applied" {
			t.Fatalf("module status = %q (%s), want applied", m.Status, m.Note)
		}
	}

	lock, _, err := lockfile.Load(s.lockPath)
	if err != nil {
		t.Fatal(err)
	}
	st := lock.Modules["multitool"]
	if st.Status != "ok" || st.FallbackKind != "release" || st.FallbackRef != "v1.0.0" {
		t.Fatalf("lock state = %+v", st)
	}
	byName := map[string]lockfile.PackageState{}
	for _, p := range st.Packages {
		byName[p.Name] = p
	}
	if !byName["beta"].InstalledByOmnishell || byName["beta"].Manager != "apt" {
		t.Fatalf("beta not recorded as installed by omnishell: %+v", st.Packages)
	}
	if byName["alpha"].InstalledByOmnishell {
		t.Fatalf("alpha recorded as a manager install although the release binary stands in: %+v", st.Packages)
	}

	// settled: nothing is installed or downloaded again
	res, err = s.apply(t)
	if err != nil || !res.NothingToDo {
		t.Fatalf("second Apply: NothingToDo=%v err=%v", res.NothingToDo, err)
	}
	if len(s.mgr.InstallCalls) != 1 || len(s.dl.calls) != 1 {
		t.Fatalf("second apply installed again: %v %v", s.mgr.InstallCalls, s.dl.calls)
	}
}

func TestApplyPartialFallbackManagerFailureDegrades(t *testing.T) {
	s := newMultiSandbox(t, "alpha")
	s.mgr.InstallErr = errors.New("exit status 100")
	_, err := s.apply(t)
	if !errors.Is(err, engine.ErrDegraded) {
		t.Fatalf("err = %v, want ErrDegraded", err)
	}
	if len(s.dl.calls) != 0 {
		t.Fatalf("the release binary was downloaded although the manager install failed: %v", s.dl.calls)
	}
}

func TestApplyPartialFallbackDownloadFailureDegrades(t *testing.T) {
	s := newMultiSandbox(t, "alpha")
	s.dl.err = errors.New("HTTP 503")
	_, err := s.apply(t)
	if !errors.Is(err, engine.ErrDegraded) {
		t.Fatalf("err = %v, want ErrDegraded", err)
	}
	init, rerr := os.ReadFile(filepath.Join(s.home, ".config", "omnishell", "init.bash"))
	if rerr == nil && strings.Contains(string(init), "omnishell:multitool") {
		t.Fatalf("a degraded module kept its section:\n%s", init)
	}
}
