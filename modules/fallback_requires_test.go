package modules_test

import (
	"strings"
	"testing"

	"github.com/JtheGunner/omnishell/internal/module"
	"github.com/JtheGunner/omnishell/modules"
)

// TestBuiltinFallbackRequirements pins the prerequisites each built-in
// fallback build declares, so a manifest cannot drop its toolchain check.
func TestBuiltinFallbackRequirements(t *testing.T) {
	reg, err := module.LoadRegistry(modules.FS(), "")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{
		"mise":         {"cargo>=1.95", "cmake"},
		"starship":     {"cargo>=1.95"},
		"broot":        {"cargo>=1.85"},
		"atuin":        {"cargo>=1.95"},
		"pay-respects": {"cargo>=1.85"},
		"welcome":      {"cmake"},
	}
	for id, wantReqs := range want {
		m, ok := reg.Get(id)
		if !ok {
			t.Fatalf("module %q not embedded", id)
		}
		fbs := m.Manifest.Packages.Fallback
		if len(fbs) == 0 {
			t.Fatalf("%s has no fallback", id)
		}
		if got := strings.Join(fbs[0].Requires, ","); got != strings.Join(wantReqs, ",") {
			t.Errorf("%s requires = %q, want %q", id, got, strings.Join(wantReqs, ","))
		}
	}
}

// TestBuiltinCargoFallbacksDeclareCargo guards new modules: any fallback that
// runs cargo must say so in requires.
func TestBuiltinCargoFallbacksDeclareCargo(t *testing.T) {
	reg, err := module.LoadRegistry(modules.FS(), "")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range reg.All() {
		for _, fb := range m.Manifest.Packages.Fallback {
			if len(fb.Run) == 0 || fb.Run[0] != "cargo" {
				continue
			}
			declared := false
			for _, raw := range fb.Requires {
				if req, err := module.ParseRequirement(raw); err == nil && req.Tool == "cargo" && req.Min != nil {
					declared = true
				}
			}
			if !declared {
				t.Errorf("%s: cargo fallback must declare requires = [\"cargo>=X.Y\"]", m.Manifest.Module.ID)
			}
		}
	}
}
