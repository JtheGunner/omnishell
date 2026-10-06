package pkgmgr_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JtheGunner/omnishell/internal/module"
	"github.com/JtheGunner/omnishell/internal/pkgmgr"
)

func TestInstallGitFallbackClonesWhenAbsent(t *testing.T) {
	vendor := t.TempDir()
	r := &pkgmgr.MockRunner{}
	fb := module.Fallback{
		Type: "git",
		Repo: "https://example.com/fzf.git",
		Dest: "{{.VendorDir}}/fzf",
		Run:  []string{"{{.VendorDir}}/fzf/install", "--bin"},
	}
	got, err := pkgmgr.InstallGitFallback(fb, pkgmgr.FallbackContext{VendorDir: vendor, Platform: "linux", Shell: "bash"}, r)
	if err != nil {
		t.Fatalf("InstallGitFallback: %v", err)
	}
	want := filepath.Join(vendor, "fzf")
	if got != want {
		t.Fatalf("vendorPath = %q, want %q", got, want)
	}
	if len(r.Calls) != 2 {
		t.Fatalf("calls = %v, want clone then run", r.Calls)
	}
	if r.Calls[0] != "git clone --depth 1 https://example.com/fzf.git "+want {
		t.Fatalf("clone call = %q", r.Calls[0])
	}
	if r.Calls[1] != filepath.Join(vendor, "fzf", "install")+" --bin" {
		t.Fatalf("run call = %q", r.Calls[1])
	}
}

func TestInstallGitFallbackHandlesSpacesInVendorDir(t *testing.T) {
	vendor := filepath.Join(t.TempDir(), "dir with spaces")
	if err := os.MkdirAll(vendor, 0o755); err != nil {
		t.Fatal(err)
	}
	r := &pkgmgr.MockRunner{}
	fb := module.Fallback{
		Type: "git",
		Repo: "https://example.com/fzf.git",
		Dest: "{{.VendorDir}}/fzf",
		Run:  []string{"{{.VendorDir}}/fzf/install", "--bin"},
	}
	if _, err := pkgmgr.InstallGitFallback(fb, pkgmgr.FallbackContext{VendorDir: vendor, Platform: "linux"}, r); err != nil {
		t.Fatalf("InstallGitFallback: %v", err)
	}
	// The run step must be a single argv element for the script path, not two
	// tokens split on the space in "dir with spaces".
	want := filepath.Join(vendor, "fzf", "install") + " --bin"
	if r.Calls[1] != want {
		t.Fatalf("run call = %q, want %q", r.Calls[1], want)
	}
}

func TestInstallGitFallbackSkipsWhenDestPopulated(t *testing.T) {
	vendor := t.TempDir()
	dest := filepath.Join(vendor, "fzf")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dest, "bin"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := &pkgmgr.MockRunner{}
	fb := module.Fallback{Type: "git", Repo: "r", Dest: "{{.VendorDir}}/fzf"}
	got, err := pkgmgr.InstallGitFallback(fb, pkgmgr.FallbackContext{VendorDir: vendor}, r)
	if err != nil || got != dest {
		t.Fatalf("got %q err %v", got, err)
	}
	if len(r.Calls) != 0 {
		t.Fatalf("should not clone when dest populated; calls=%v", r.Calls)
	}
}

func TestInstallGitFallbackRejectsUnknownType(t *testing.T) {
	_, err := pkgmgr.InstallGitFallback(module.Fallback{Type: "tarball"}, pkgmgr.FallbackContext{VendorDir: t.TempDir()}, &pkgmgr.MockRunner{})
	if err == nil {
		t.Fatal("want error for unsupported fallback type")
	}
}

func TestInstallGitFallbackClonesPinnedRef(t *testing.T) {
	vendor := t.TempDir()
	r := &pkgmgr.MockRunner{}
	fb := module.Fallback{Type: "git", Repo: "https://example.com/fzf.git", Dest: "{{.VendorDir}}/fzf", Ref: "v1.2.3"}
	dest, err := pkgmgr.InstallGitFallback(fb, pkgmgr.FallbackContext{VendorDir: vendor}, r)
	if err != nil {
		t.Fatalf("InstallGitFallback: %v", err)
	}
	want := "git -c advice.detachedHead=false clone --depth 1 --branch v1.2.3 https://example.com/fzf.git " + dest
	if len(r.Calls) != 1 || r.Calls[0] != want {
		t.Fatalf("clone calls = %v, want [%q]", r.Calls, want)
	}
}

func populatedClone(t *testing.T) (vendor, dest string) {
	t.Helper()
	vendor = t.TempDir()
	dest = filepath.Join(vendor, "fzf")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dest, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dest, "file"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	return vendor, dest
}

func updateFallbackSpec() module.Fallback {
	return module.Fallback{
		Type: "git", Repo: "https://example.com/fzf.git", Dest: "{{.VendorDir}}/fzf", Ref: "v2",
		Run: []string{"{{.VendorDir}}/fzf/install", "--bin"},
	}
}

func TestUpdateGitFallbackFetchesChecksOutAndRebuilds(t *testing.T) {
	vendor, dest := populatedClone(t)
	r := &pkgmgr.MockRunner{}
	got, err := pkgmgr.UpdateGitFallback(updateFallbackSpec(), pkgmgr.FallbackContext{VendorDir: vendor}, r)
	if err != nil || got != dest {
		t.Fatalf("UpdateGitFallback = %q, %v; want %q, nil", got, err, dest)
	}
	want := []string{
		"git -C " + dest + " status --porcelain --untracked-files=no",
		"git -C " + dest + " fetch --depth 1 https://example.com/fzf.git v2",
		"git -C " + dest + " -c advice.detachedHead=false checkout --detach FETCH_HEAD",
		filepath.Join(dest, "install") + " --bin",
	}
	if strings.Join(r.Calls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("calls:\n%s\nwant:\n%s", strings.Join(r.Calls, "\n"), strings.Join(want, "\n"))
	}
}

func TestUpdateGitFallbackLeavesAModifiedCloneAlone(t *testing.T) {
	vendor, dest := populatedClone(t)
	status := "git -C " + dest + " status --porcelain --untracked-files=no"
	r := &pkgmgr.MockRunner{Responses: map[string]pkgmgr.MockResponse{status: {Out: []byte(" M install\n")}}}
	got, err := pkgmgr.UpdateGitFallback(updateFallbackSpec(), pkgmgr.FallbackContext{VendorDir: vendor}, r)
	if !errors.Is(err, pkgmgr.ErrCloneModified) || got != dest {
		t.Fatalf("UpdateGitFallback = %q, %v; want %q, ErrCloneModified", got, err, dest)
	}
	if len(r.Calls) != 1 {
		t.Fatalf("a modified clone must not be fetched or rebuilt; calls = %v", r.Calls)
	}
}

func TestUpdateGitFallbackFetchFailureStopsBeforeTheBuild(t *testing.T) {
	vendor, dest := populatedClone(t)
	fetch := "git -C " + dest + " fetch --depth 1 https://example.com/fzf.git v2"
	r := &pkgmgr.MockRunner{Responses: map[string]pkgmgr.MockResponse{fetch: {Err: errors.New("network down")}}}
	if _, err := pkgmgr.UpdateGitFallback(updateFallbackSpec(), pkgmgr.FallbackContext{VendorDir: vendor}, r); err == nil || !strings.Contains(err.Error(), "network down") {
		t.Fatalf("err = %v, want the fetch error", err)
	}
	for _, c := range r.Calls {
		if strings.Contains(c, "install --bin") {
			t.Fatalf("the build must not run after a failed fetch: %v", r.Calls)
		}
	}
}

func TestUpdateGitFallbackKeepsTheCloneWhenTheBuildFails(t *testing.T) {
	vendor, dest := populatedClone(t)
	build := filepath.Join(dest, "install") + " --bin"
	r := &pkgmgr.MockRunner{Responses: map[string]pkgmgr.MockResponse{build: {Err: errors.New("build failed")}}}
	if _, err := pkgmgr.UpdateGitFallback(updateFallbackSpec(), pkgmgr.FallbackContext{VendorDir: vendor}, r); err == nil {
		t.Fatal("want the build error")
	}
	if _, err := os.Stat(dest); err != nil {
		t.Fatalf("a failed update must not delete the existing clone: %v", err)
	}
}

func TestUpdateGitFallbackRejectsBadInput(t *testing.T) {
	emptyVendor := t.TempDir() // no clone inside
	if _, err := pkgmgr.UpdateGitFallback(updateFallbackSpec(), pkgmgr.FallbackContext{VendorDir: emptyVendor}, &pkgmgr.MockRunner{}); err == nil {
		t.Fatal("want an error for a missing clone")
	}
	vendor, _ := populatedClone(t)
	fb := updateFallbackSpec()
	fb.Ref = ""
	if _, err := pkgmgr.UpdateGitFallback(fb, pkgmgr.FallbackContext{VendorDir: vendor}, &pkgmgr.MockRunner{}); err == nil {
		t.Fatal("want an error without a ref")
	}
}

func TestUpdateGitFallbackNeverRunsGitInADirectoryThatIsNotAClone(t *testing.T) {
	vendor := t.TempDir()
	dest := filepath.Join(vendor, "fzf")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dest, "file"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := &pkgmgr.MockRunner{}
	got, err := pkgmgr.UpdateGitFallback(updateFallbackSpec(), pkgmgr.FallbackContext{VendorDir: vendor}, r)
	if !errors.Is(err, pkgmgr.ErrNotAClone) || got != dest {
		t.Fatalf("UpdateGitFallback = %q, %v; want %q, ErrNotAClone", got, err, dest)
	}
	if len(r.Calls) != 0 {
		t.Fatalf("git must not run in a directory that is not a clone (it could reach an enclosing repository): %v", r.Calls)
	}
}
