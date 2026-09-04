package pkgmgr_test

import (
	"os"
	"path/filepath"
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
