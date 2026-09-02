package engine_test

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/JtheGunner/omnishell/internal/engine"
	"github.com/JtheGunner/omnishell/internal/module"
)

var update = flag.Bool("update", false, "update golden files")

func TestRenderPlanFresh(t *testing.T) {
	p := engine.Plan{
		Order:          []string{"completion", "fzf"},
		ManagedShells:  []string{"bash"},
		PackageManager: "apt",
		ManagerAvailable: true,
		HasChanges:     true,
		Modules: map[string]engine.ModulePlan{
			"completion": {ID: "completion", Action: engine.ActionInstall, Shells: []string{"bash"},
				Manifest: module.Manifest{Module: module.ModuleMeta{ID: "completion", Version: "1.0.0"}}},
			"fzf": {ID: "fzf", Action: engine.ActionInstall, Shells: []string{"bash"},
				MissingPackages: []engine.PackagePlan{{Name: "fzf", Manager: "apt"}},
				Manifest: module.Manifest{Module: module.ModuleMeta{ID: "fzf", Version: "1.0.0"}}},
			"zoxide": {ID: "zoxide", Action: engine.ActionRemove, Reason: "no longer enabled"},
		},
	}
	got := engine.RenderPlan(p)
	golden := filepath.Join("testdata", "plan_fresh.golden")
	if *update {
		os.WriteFile(golden, []byte(got), 0o644)
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("RenderPlan mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}
