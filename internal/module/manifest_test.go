package module_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JtheGunner/omnishell/internal/module"
)

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParseManifestFzf(t *testing.T) {
	m, err := module.ParseManifest(loadFixture(t, "fzf-manifest.toml"))
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	if m.Module.ID != "fzf" || m.Module.Version != "1.0.0" || m.Module.Schema != 1 {
		t.Fatalf("meta = %+v", m.Module)
	}
	if m.Module.Homepage != "https://github.com/junegunn/fzf" {
		t.Fatalf("homepage = %q", m.Module.Homepage)
	}
	if len(m.Platforms) != 2 || len(m.Shells) != 2 {
		t.Fatalf("platforms/shells = %v / %v", m.Platforms, m.Shells)
	}
	if len(m.After) != 1 || m.After[0] != "completion" {
		t.Fatalf("after = %v", m.After)
	}
	if len(m.Packages.Brew) != 1 || m.Packages.Brew[0] != "fzf" {
		t.Fatalf("brew packages = %v", m.Packages.Brew)
	}
	if len(m.Packages.Fallback) != 1 || m.Packages.Fallback[0].Type != "git" {
		t.Fatalf("fallback = %+v", m.Packages.Fallback)
	}
	if m.Options["ctrl_r"].Type != "bool" || m.Options["ctrl_r"].Default != true {
		t.Fatalf("ctrl_r schema = %+v", m.Options["ctrl_r"])
	}
	if m.Options["replace"].Type != "list<enum>" || len(m.Options["replace"].Values) != 3 {
		t.Fatalf("replace schema = %+v", m.Options["replace"])
	}
	if err := module.ValidateManifest(m); err != nil {
		t.Fatalf("ValidateManifest: %v", err)
	}
}

func TestValidateManifestRejectsBadID(t *testing.T) {
	m, _ := module.ParseManifest(loadFixture(t, "fzf-manifest.toml"))
	m.Module.ID = "Fzf_Bad"
	if err := module.ValidateManifest(m); err == nil {
		t.Fatal("want error for bad id")
	}
}

func TestValidateManifestRejectsUnknownOptionType(t *testing.T) {
	m, _ := module.ParseManifest(loadFixture(t, "fzf-manifest.toml"))
	opt := m.Options["ctrl_r"]
	opt.Type = "float"
	m.Options["ctrl_r"] = opt
	if err := module.ValidateManifest(m); err == nil {
		t.Fatal("want error for unknown option type")
	}
}

func TestValidateManifestRejectsWrongSchema(t *testing.T) {
	m, _ := module.ParseManifest(loadFixture(t, "fzf-manifest.toml"))
	m.Module.Schema = 2
	if err := module.ValidateManifest(m); err == nil {
		t.Fatal("want error for schema 2")
	}
}

func TestValidateManifestAcceptsConflicts(t *testing.T) {
	m, _ := module.ParseManifest(loadFixture(t, "fzf-manifest.toml"))
	m.Conflicts = []string{"atuin"}
	if err := module.ValidateManifest(m); err != nil {
		t.Fatalf("ValidateManifest: %v", err)
	}
}

func TestValidateManifestRejectsSelfConflict(t *testing.T) {
	m, _ := module.ParseManifest(loadFixture(t, "fzf-manifest.toml"))
	m.Conflicts = []string{"fzf"}
	if err := module.ValidateManifest(m); err == nil {
		t.Fatal("want error for a module conflicting with itself")
	}
}

func TestValidateManifestRejectsBadConflictID(t *testing.T) {
	m, _ := module.ParseManifest(loadFixture(t, "fzf-manifest.toml"))
	m.Conflicts = []string{"Not_An_ID"}
	if err := module.ValidateManifest(m); err == nil {
		t.Fatal("want error for a malformed conflicts id")
	}
}

func TestValidateManifestRejectsRequiresConflictsOverlap(t *testing.T) {
	m, _ := module.ParseManifest(loadFixture(t, "fzf-manifest.toml"))
	m.Requires = []string{"completion"}
	m.Conflicts = []string{"completion"}
	if err := module.ValidateManifest(m); err == nil {
		t.Fatal("want error when an id is in both requires and conflicts")
	}
}

func TestParseManifestRejectsUnknownKey(t *testing.T) {
	_, err := module.ParseManifest([]byte("[module]\nid=\"x\"\nversion=\"1\"\nschema=1\nbogus=true\n"))
	if err == nil {
		t.Fatal("want error for unknown key")
	}
}

const fallbackRefManifest = `
platforms = ["linux"]
shells    = ["bash"]

[module]
id      = "pinned"
name    = "pinned"
version = "1.0.0"
schema  = 1

[[packages.fallback]]
type = "git"
repo = "https://example.com/pinned.git"
dest = "{{.VendorDir}}/pinned"
ref  = %q
`

func manifestWithRef(t *testing.T, ref string) module.Manifest {
	t.Helper()
	m, err := module.ParseManifest([]byte(fmt.Sprintf(fallbackRefManifest, ref)))
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	return m
}

func TestParseManifestFallbackRef(t *testing.T) {
	m := manifestWithRef(t, "v1.2.3")
	if got := m.Packages.Fallback[0].Ref; got != "v1.2.3" {
		t.Fatalf("Ref = %q, want v1.2.3", got)
	}
	if err := module.ValidateManifest(m); err != nil {
		t.Fatalf("ValidateManifest: %v", err)
	}
}

func TestValidateManifestAcceptsFallbackWithoutRef(t *testing.T) {
	m, _ := module.ParseManifest(loadFixture(t, "fzf-manifest.toml"))
	if err := module.ValidateManifest(m); err != nil {
		t.Fatalf("a fallback without ref must stay valid: %v", err)
	}
}

func TestValidateManifestRejectsUnsafeFallbackRef(t *testing.T) {
	for _, ref := range []string{"--upload-pack=evil", "-v1", "v1 2", "v1;rm", "$(x)"} {
		if err := module.ValidateManifest(manifestWithRef(t, ref)); err == nil {
			t.Errorf("ref %q must be rejected", ref)
		}
	}
}

func TestParseRequirement(t *testing.T) {
	tests := []struct {
		in      string
		tool    string
		min     []int
		wantErr bool
	}{
		{in: "cmake", tool: "cmake"},
		{in: "cargo>=1.85", tool: "cargo", min: []int{1, 85}},
		{in: "cargo>=1.85.2", tool: "cargo", min: []int{1, 85, 2}},
		{in: "", wantErr: true},
		{in: ">=1.85", wantErr: true},
		{in: "cargo>=", wantErr: true},
		{in: "cargo>=abc", wantErr: true},
		{in: "cargo>1.85", wantErr: true},
		{in: "cargo >= 1.85", wantErr: true},
		{in: "car go", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got, err := module.ParseRequirement(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseRequirement(%q) = %+v, want error", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseRequirement(%q): %v", tc.in, err)
			}
			if got.Tool != tc.tool || !equalInts(got.Min, tc.min) {
				t.Fatalf("ParseRequirement(%q) = %+v, want tool %q min %v", tc.in, got, tc.tool, tc.min)
			}
		})
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestValidateManifestChecksFallbackRequires(t *testing.T) {
	m, _ := module.ParseManifest(loadFixture(t, "fzf-manifest.toml"))
	m.Packages.Fallback[0].Requires = []string{"cargo>=1.85", "cmake"}
	if err := module.ValidateManifest(m); err != nil {
		t.Fatalf("valid requires rejected: %v", err)
	}
	m.Packages.Fallback[0].Requires = []string{"cargo>=nope"}
	if err := module.ValidateManifest(m); err == nil {
		t.Fatal("want error for malformed requires entry")
	}
}

func TestParseManifestReadsFallbackRequires(t *testing.T) {
	src := string(loadFixture(t, "fzf-manifest.toml")) + "\n"
	src = replaceFirst(src, "[[packages.fallback]]", "[[packages.fallback]]\nrequires = [\"cargo>=1.85\"]")
	m, err := module.ParseManifest([]byte(src))
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	if got := m.Packages.Fallback[0].Requires; len(got) != 1 || got[0] != "cargo>=1.85" {
		t.Fatalf("requires = %v", got)
	}
}

func replaceFirst(s, old, repl string) string {
	i := strings.Index(s, old)
	if i < 0 {
		return s
	}
	return s[:i] + repl + s[i+len(old):]
}
