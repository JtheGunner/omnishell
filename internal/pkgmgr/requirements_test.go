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

func versionRunner(responses map[string]string) *pkgmgr.MockRunner {
	r := &pkgmgr.MockRunner{Responses: map[string]pkgmgr.MockResponse{}}
	for tool, out := range responses {
		if out == "" {
			r.Responses[tool+" --version"] = pkgmgr.MockResponse{Err: errors.New("exec: not found")}
			continue
		}
		r.Responses[tool+" --version"] = pkgmgr.MockResponse{Out: []byte(out)}
	}
	return r
}

func TestCheckRequirements(t *testing.T) {
	tests := []struct {
		name     string
		requires []string
		tools    map[string]string
		want     []string // substrings of the error; nil means no error
	}{
		{
			name:     "all met",
			requires: []string{"cargo>=1.85", "cmake"},
			tools:    map[string]string{"cargo": "cargo 1.88.0 (873a06493 2025-05-10)\n", "cmake": "cmake version 3.28.3\n"},
		},
		{
			name:     "exact minimum met",
			requires: []string{"cargo>=1.85"},
			tools:    map[string]string{"cargo": "cargo 1.85.0 (d73d2caf9 2024-12-31)"},
		},
		{
			name:     "too old",
			requires: []string{"cargo>=1.85"},
			tools:    map[string]string{"cargo": "cargo 1.75.0 (1d8b05cdd 2023-11-20)"},
			want:     []string{"cargo >= 1.85", "found 1.75.0"},
		},
		{
			name:     "minor compared numerically not lexically",
			requires: []string{"cargo>=1.9"},
			tools:    map[string]string{"cargo": "cargo 1.10.0"},
		},
		{
			name:     "missing tool",
			requires: []string{"cmake"},
			tools:    map[string]string{"cmake": ""},
			want:     []string{"cmake (not found)"},
		},
		{
			name:     "unparsable version output",
			requires: []string{"cargo>=1.85"},
			tools:    map[string]string{"cargo": "cargo, a package manager"},
			want:     []string{"cargo >= 1.85", "version not recognised"},
		},
		{
			name:     "unparsable output is fine without a minimum",
			requires: []string{"cmake"},
			tools:    map[string]string{"cmake": "whatever"},
		},
		{
			name:     "all problems reported together",
			requires: []string{"cargo>=1.85", "cmake"},
			tools:    map[string]string{"cargo": "cargo 1.75.0", "cmake": ""},
			want:     []string{"cargo >= 1.85 (found 1.75.0)", "cmake (not found)"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := pkgmgr.CheckRequirements(tc.requires, versionRunner(tc.tools))
			if tc.want == nil {
				if err != nil {
					t.Fatalf("CheckRequirements: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("want error, got nil")
			}
			if !strings.HasPrefix(err.Error(), "fallback prerequisites missing: ") {
				t.Fatalf("error = %q, want prerequisites prefix", err)
			}
			for _, w := range tc.want {
				if !strings.Contains(err.Error(), w) {
					t.Fatalf("error = %q, want it to contain %q", err, w)
				}
			}
		})
	}
}

func TestCheckRequirementsNoneProbesNothing(t *testing.T) {
	r := &pkgmgr.MockRunner{}
	if err := pkgmgr.CheckRequirements(nil, r); err != nil {
		t.Fatalf("CheckRequirements(nil): %v", err)
	}
	if len(r.Calls) != 0 {
		t.Fatalf("calls = %v, want none", r.Calls)
	}
}

func TestCheckRequirementsRejectsMalformedEntry(t *testing.T) {
	if err := pkgmgr.CheckRequirements([]string{"cargo>=x"}, &pkgmgr.MockRunner{}); err == nil {
		t.Fatal("want error for malformed requirement")
	}
}

func TestInstallGitFallbackFailsBeforeCloneWhenRequirementMissing(t *testing.T) {
	vendor := t.TempDir()
	r := versionRunner(map[string]string{"cargo": "cargo 1.75.0"})
	fb := module.Fallback{
		Type:     "git",
		Repo:     "https://example.com/mise.git",
		Dest:     "{{.VendorDir}}/mise",
		Run:      []string{"cargo", "install", "--path", "{{.VendorDir}}/mise"},
		Requires: []string{"cargo>=1.85"},
	}
	_, err := pkgmgr.InstallGitFallback(fb, pkgmgr.FallbackContext{VendorDir: vendor, Platform: "linux"}, r)
	if err == nil || !strings.Contains(err.Error(), "cargo >= 1.85 (found 1.75.0)") {
		t.Fatalf("err = %v, want prerequisites error", err)
	}
	for _, c := range r.Calls {
		if strings.HasPrefix(c, "git ") || strings.HasPrefix(c, "cargo install") {
			t.Fatalf("unexpected call %q after failed prerequisite check", c)
		}
	}
	if _, statErr := os.Stat(filepath.Join(vendor, "mise")); !os.IsNotExist(statErr) {
		t.Fatalf("dest must not exist after failed check, stat err = %v", statErr)
	}
}

func TestInstallGitFallbackSkipsCheckWhenDestPopulated(t *testing.T) {
	vendor := t.TempDir()
	dest := filepath.Join(vendor, "mise")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dest, "marker"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	r := &pkgmgr.MockRunner{}
	fb := module.Fallback{
		Type:     "git",
		Repo:     "https://example.com/mise.git",
		Dest:     "{{.VendorDir}}/mise",
		Requires: []string{"cargo>=1.85"},
	}
	if _, err := pkgmgr.InstallGitFallback(fb, pkgmgr.FallbackContext{VendorDir: vendor, Platform: "linux"}, r); err != nil {
		t.Fatalf("InstallGitFallback: %v", err)
	}
	if len(r.Calls) != 0 {
		t.Fatalf("calls = %v, want none for a populated dest", r.Calls)
	}
}

func TestInstallGitFallbackProceedsWhenRequirementsMet(t *testing.T) {
	vendor := t.TempDir()
	r := versionRunner(map[string]string{"cargo": "cargo 1.88.0"})
	fb := module.Fallback{
		Type:     "git",
		Repo:     "https://example.com/mise.git",
		Dest:     "{{.VendorDir}}/mise",
		Run:      []string{"cargo", "install", "--path", "{{.VendorDir}}/mise"},
		Requires: []string{"cargo>=1.85"},
	}
	if _, err := pkgmgr.InstallGitFallback(fb, pkgmgr.FallbackContext{VendorDir: vendor, Platform: "linux"}, r); err != nil {
		t.Fatalf("InstallGitFallback: %v", err)
	}
	if len(r.Calls) != 3 || r.Calls[0] != "cargo --version" || !strings.HasPrefix(r.Calls[1], "git clone") {
		t.Fatalf("calls = %v, want probe, clone, run", r.Calls)
	}
}
