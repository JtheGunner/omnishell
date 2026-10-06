package engine_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
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

const relPayload = "#!/bin/sh\necho reltool\n"

type relDownloader struct {
	body  []byte
	calls []string
}

func (d *relDownloader) Download(url string, dst io.Writer, _ int64) error {
	d.calls = append(d.calls, url)
	_, err := dst.Write(d.body)
	return err
}

func relSHA(body string) string {
	s := sha256.Sum256([]byte(body))
	return hex.EncodeToString(s[:])
}

// writeRelModule writes a user module "reltool" with a release fallback ahead
// of a git fallback, the release asset pinned to sha.
func writeRelModule(t *testing.T, dir, sha string) {
	t.Helper()
	modDir := filepath.Join(dir, "reltool")
	if err := os.MkdirAll(modDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `platforms = ["macos", "linux"]
shells    = ["bash"]
requires  = []
after     = []

[module]
id          = "reltool"
name        = "reltool"
description = "Fixture: a module with a release fallback"
version     = "1.0.0"
schema      = 1

[[packages.fallback]]
type = "release"
repo = "https://example.com/reltool"
ref  = "v1.0.0"
bin  = "reltool"

[[packages.fallback.assets]]
os     = "linux"
arch   = "amd64"
url    = "https://example.com/reltool/{{.Ref}}/reltool-linux-amd64"
sha256 = "` + sha + `"

[[packages.fallback]]
type = "git"
repo = "https://example.com/reltool.git"
dest = "{{.VendorDir}}/reltool"
ref  = "v1.0.0"
run  = ["cargo", "install", "--path", "{{.VendorDir}}/reltool"]

[options.flag]
type    = "bool"
default = false
help    = "Changing it forces a re-apply in tests"
`
	if err := os.WriteFile(filepath.Join(modDir, "manifest.toml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modDir, "bash.tmpl"), []byte("echo reltool\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

type relSandbox struct {
	e                 engine.Engine
	home              string
	modDir            string
	cfgPath, lockPath string
	out               *bytes.Buffer
	dl                *relDownloader
	runner            *pkgmgr.MockRunner
}

func newRelSandbox(t *testing.T) relSandbox {
	t.Helper()
	home := t.TempDir()
	out := &bytes.Buffer{}
	mgr := &pkgmgr.MockManager{NameV: "apt", DetectV: true, Installed: map[string]bool{}}
	e := applyEngine(t, home, mgr, out)
	modDir := t.TempDir()
	writeRelModule(t, modDir, relSHA(relPayload))
	reg, err := module.LoadRegistry(nil, modDir)
	if err != nil {
		t.Fatal(err)
	}
	runner := &pkgmgr.MockRunner{}
	dl := &relDownloader{body: []byte(relPayload)}
	e.Registry, e.Runner, e.Downloader = reg, runner, dl
	e.Platform.Arch = "amd64"
	cfgPath := filepath.Join(home, ".config", "omnishell", "config.toml")
	writeConfig(t, cfgPath, "[omnishell]\nversion=1\nshells=[\"bash\"]\n[modules.reltool]\nenabled=true\n")
	return relSandbox{e: e, home: home, modDir: modDir, cfgPath: cfgPath,
		lockPath: filepath.Join(home, ".config", "omnishell", "state.lock.json"), out: out, dl: dl, runner: runner}
}

func (s relSandbox) binPath() string {
	return filepath.Join(s.home, ".config", "omnishell", "vendor", "bin", "reltool")
}

func (s relSandbox) applyWith(t *testing.T, opts engine.ApplyOptions) (engine.Result, error) {
	t.Helper()
	cfg, err := config.Load(s.cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	return s.e.Apply(cfg, s.cfgPath, s.lockPath, opts)
}

func (s relSandbox) apply(t *testing.T) (engine.Result, error) {
	t.Helper()
	return s.applyWith(t, engine.ApplyOptions{Yes: true})
}

func (s relSandbox) lockState(t *testing.T) lockfile.ModuleState {
	t.Helper()
	lock, ok, err := lockfile.Load(s.lockPath)
	if err != nil || !ok {
		t.Fatalf("lock: ok=%v err=%v", ok, err)
	}
	return lock.Modules["reltool"]
}

func (s relSandbox) findings(t *testing.T) map[string]string {
	t.Helper()
	cfg, err := config.Load(s.cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	rep, err := s.e.Doctor(cfg, s.cfgPath, s.lockPath)
	if err != nil {
		t.Fatal(err)
	}
	codes := map[string]string{}
	for _, f := range rep.Findings {
		codes[f.Code] = f.Severity
	}
	return codes
}

func TestApplyInstallsTheReleaseBinary(t *testing.T) {
	s := newRelSandbox(t)
	if _, err := s.apply(t); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	got, err := os.ReadFile(s.binPath())
	if err != nil || string(got) != relPayload {
		t.Fatalf("binary = %q (err %v)", got, err)
	}
	if len(s.dl.calls) != 1 || s.dl.calls[0] != "https://example.com/reltool/v1.0.0/reltool-linux-amd64" {
		t.Fatalf("downloads = %v", s.dl.calls)
	}
	if len(s.runner.Calls) != 0 {
		t.Fatalf("a release install must not run commands: %v", s.runner.Calls)
	}
	if !strings.Contains(s.out.String(), "installing reltool v1.0.0 from its release binary") {
		t.Fatalf("output lacks the install line:\n%s", s.out.String())
	}
	st := s.lockState(t)
	if st.FallbackKind != "release" || st.FallbackRef != "v1.0.0" || st.FallbackSHA256 != relSHA(relPayload) {
		t.Fatalf("lock state = %+v", st)
	}
	found := false
	for _, vp := range st.VendorPaths {
		found = found || vp == s.binPath()
	}
	if !found {
		t.Fatalf("VendorPaths %v lacks the binary %s, so remove would leave it behind", st.VendorPaths, s.binPath())
	}
}

func TestApplyReleaseIsIdempotent(t *testing.T) {
	s := newRelSandbox(t)
	if _, err := s.apply(t); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if _, err := s.apply(t); err != nil {
		t.Fatalf("second Apply: %v", err)
	}
	if len(s.dl.calls) != 1 {
		t.Fatalf("the second apply downloaded again: %v", s.dl.calls)
	}
}

func TestApplyReleaseChecksumMismatchDegradesTheModule(t *testing.T) {
	s := newRelSandbox(t)
	s.dl.body = []byte("tampered")
	res, err := s.apply(t)
	if !errors.Is(err, engine.ErrDegraded) {
		t.Fatalf("err = %v, want ErrDegraded", err)
	}
	mr := moduleResult(t, res, "reltool")
	if mr.Status != "degraded" || !strings.Contains(mr.Note, "checksum mismatch") {
		t.Fatalf("status=%q note=%q", mr.Status, mr.Note)
	}
	if _, statErr := os.Stat(s.binPath()); statErr == nil {
		t.Fatal("a binary with a bad checksum was installed")
	}
	if anyCallContains(s.runner.Calls, "cargo") || anyCallContains(s.runner.Calls, "git ") {
		t.Fatalf("a failed download must not fall through to the Cargo build: %v", s.runner.Calls)
	}
}

func TestApplyUsesTheGitFallbackOnAnUnsupportedArchitecture(t *testing.T) {
	s := newRelSandbox(t)
	s.e.Platform.Arch = "riscv64"
	if _, err := s.apply(t); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(s.dl.calls) != 0 {
		t.Fatalf("downloaded on an unsupported architecture: %v", s.dl.calls)
	}
	if !anyCallContains(s.runner.Calls, "clone") {
		t.Fatalf("the git fallback did not run: %v", s.runner.Calls)
	}
	if got := s.lockState(t).FallbackKind; got != "git" {
		t.Fatalf("recorded kind = %q, want git", got)
	}
}

func TestApplyReplacesAGitBuildWithTheReleaseBinary(t *testing.T) {
	s := newRelSandbox(t)
	s.e.Platform.Arch = "riscv64"
	if _, err := s.apply(t); err != nil { // records a git build
		t.Fatalf("Apply: %v", err)
	}
	// The recorded build is older than the pinned tag, and a leftover binary
	// from the Cargo build sits where the release binary goes.
	lock, _, err := lockfile.Load(s.lockPath)
	if err != nil {
		t.Fatal(err)
	}
	st := lock.Modules["reltool"]
	st.FallbackRef = "v0.9.0"
	lock.Modules["reltool"] = st
	if err := lock.Write(s.lockPath); err != nil {
		t.Fatal(err)
	}
	s.e.Platform.Arch = "amd64"
	if err := os.MkdirAll(filepath.Dir(s.binPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.binPath(), []byte("cargo-built"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := s.apply(t); err != nil {
		t.Fatalf("second Apply: %v", err)
	}
	if got, _ := os.ReadFile(s.binPath()); string(got) != relPayload {
		t.Fatalf("binary = %q, want the release binary", got)
	}
	if got := s.lockState(t).FallbackKind; got != "release" {
		t.Fatalf("recorded kind = %q, want release", got)
	}
}

func TestApplyWithoutPackagesKeepsTheRecordedReleaseState(t *testing.T) {
	s := newRelSandbox(t)
	if _, err := s.apply(t); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	// Changing an option forces a re-apply; doctor --fix runs apply without
	// packages, and that must not forget how the binary was installed.
	writeConfig(t, s.cfgPath, "[omnishell]\nversion=1\nshells=[\"bash\"]\n[modules.reltool]\nenabled=true\n[modules.reltool.options]\nflag=true\n")
	if _, err := s.applyWith(t, engine.ApplyOptions{Yes: true, NoPackages: true}); err != nil {
		t.Fatalf("Apply without packages: %v", err)
	}
	st := s.lockState(t)
	if st.FallbackKind != "release" || st.FallbackSHA256 != relSHA(relPayload) || st.FallbackRef != "v1.0.0" {
		t.Fatalf("a no-packages apply forgot the release state: %+v", st)
	}
}

func TestDoctorReportsAnOutdatedReleaseBinary(t *testing.T) {
	s := newRelSandbox(t)
	if _, err := s.apply(t); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	lock, _, err := lockfile.Load(s.lockPath)
	if err != nil {
		t.Fatal(err)
	}
	st := lock.Modules["reltool"]
	st.FallbackRef = "v0.9.0"
	lock.Modules["reltool"] = st
	if err := lock.Write(s.lockPath); err != nil {
		t.Fatal(err)
	}
	codes := s.findings(t)
	if codes["fallback-outdated:reltool"] != engine.SeverityNotice {
		t.Fatalf("doctor lacks the fallback-outdated notice: %v", codes)
	}
	if _, bad := codes["packages-missing:reltool"]; bad {
		t.Fatalf("an outdated binary must not be reported as a missing package: %v", codes)
	}
}

// seedCargoBuild lays out an old Cargo build for reltool under the sandbox's
// vendor dir: a clone of the git fallback's repo, a binary, and cargo metadata
// that also lists another tool.
func (s relSandbox) seedCargoBuild(t *testing.T) (tree, crates string) {
	t.Helper()
	vendor := filepath.Join(s.home, ".config", "omnishell", "vendor")
	tree = filepath.Join(vendor, "reltool")
	if err := os.MkdirAll(filepath.Join(tree, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	gitConfig := "[remote \"origin\"]\n\turl = https://example.com/reltool.git\n"
	if err := os.WriteFile(filepath.Join(tree, ".git", "config"), []byte(gitConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tree, "Cargo.toml"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(vendor, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.binPath(), []byte("cargo-built"), 0o755); err != nil {
		t.Fatal(err)
	}
	crates = filepath.Join(vendor, ".crates.toml")
	body := "[v1]\n\"reltool 1.0.0 (path+file://" + tree + ")\" = [\"reltool\"]\n\"other 2.0.0 (path+file://" + filepath.Join(vendor, "other") + ")\" = [\"other\"]\n"
	if err := os.WriteFile(crates, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return tree, crates
}

func (s relSandbox) recordGitBuild(t *testing.T, ref string) {
	t.Helper()
	lock := lockfile.Lock{Schema: lockfile.SchemaVersion, Modules: map[string]lockfile.ModuleState{
		"reltool": {ModuleVersion: "1.0.0", Enabled: true, FallbackKind: "git", FallbackRef: ref, Status: "ok"},
	}}
	if err := lock.Write(s.lockPath); err != nil {
		t.Fatal(err)
	}
}

func TestApplyAdoptsAMatchingCargoBuild(t *testing.T) {
	s := newRelSandbox(t)
	tree, crates := s.seedCargoBuild(t)
	s.recordGitBuild(t, "v1.0.0") // built from the pinned tag

	if _, err := s.apply(t); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(s.dl.calls) != 0 {
		t.Fatalf("downloaded although the Cargo build already is the pinned version: %v", s.dl.calls)
	}
	if got, _ := os.ReadFile(s.binPath()); string(got) != "cargo-built" {
		t.Fatalf("the Cargo binary was replaced by %q", got)
	}
	if _, err := os.Stat(tree); !os.IsNotExist(err) {
		t.Fatalf("source tree not cleaned up (err %v)", err)
	}
	meta, _ := os.ReadFile(crates)
	if strings.Contains(string(meta), "reltool 1.0.0") || !strings.Contains(string(meta), "other 2.0.0") {
		t.Fatalf(".crates.toml after cleanup:\n%s", meta)
	}
	st := s.lockState(t)
	if st.FallbackKind != "release" || st.FallbackSHA256 != "" || st.FallbackRef != "v1.0.0" {
		t.Fatalf("lock state = %+v, want an adopted release record with no checksum", st)
	}
	if !strings.Contains(s.out.String(), "removing build leftover") {
		t.Fatalf("output does not log the cleanup:\n%s", s.out.String())
	}

	before := len(s.dl.calls)
	if _, err := s.apply(t); err != nil {
		t.Fatalf("second Apply: %v", err)
	}
	if len(s.dl.calls) != before {
		t.Fatalf("the run after adoption downloaded: %v", s.dl.calls)
	}
}

func TestApplyReplacesAnOlderCargoBuildAndCleansUp(t *testing.T) {
	s := newRelSandbox(t)
	tree, _ := s.seedCargoBuild(t)
	s.recordGitBuild(t, "v0.9.0")

	if _, err := s.apply(t); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got, _ := os.ReadFile(s.binPath()); string(got) != relPayload {
		t.Fatalf("binary = %q, want the release binary", got)
	}
	if _, err := os.Stat(tree); !os.IsNotExist(err) {
		t.Fatalf("source tree not cleaned up (err %v)", err)
	}
}

func TestApplyLeavesAForeignDirectoryAndStillSucceeds(t *testing.T) {
	s := newRelSandbox(t)
	tree, _ := s.seedCargoBuild(t)
	other := "[remote \"origin\"]\n\turl = https://example.com/someone-elses.git\n"
	if err := os.WriteFile(filepath.Join(tree, ".git", "config"), []byte(other), 0o644); err != nil {
		t.Fatal(err)
	}
	s.recordGitBuild(t, "v0.9.0")

	if _, err := s.apply(t); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tree, "Cargo.toml")); err != nil {
		t.Fatalf("a directory that is not our clone was deleted: %v", err)
	}
}

func TestDryRunPlansTheCleanupWithoutTouchingDisk(t *testing.T) {
	s := newRelSandbox(t)
	tree, _ := s.seedCargoBuild(t)
	s.recordGitBuild(t, "v1.0.0")

	res, err := s.applyWith(t, engine.ApplyOptions{DryRun: true})
	if err != nil {
		t.Fatalf("dry-run Apply: %v", err)
	}
	if !strings.Contains(res.PlanText, "remove build leftover") {
		t.Fatalf("the plan does not show the cleanup:\n%s", res.PlanText)
	}
	if _, err := os.Stat(filepath.Join(tree, "Cargo.toml")); err != nil {
		t.Fatalf("a dry run deleted the source tree: %v", err)
	}
	if len(s.dl.calls) != 0 {
		t.Fatalf("a dry run downloaded: %v", s.dl.calls)
	}
}
