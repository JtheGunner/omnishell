package modules_test

import (
	"strings"
	"testing"

	"github.com/JtheGunner/omnishell/internal/graph"
	"github.com/JtheGunner/omnishell/internal/module"
	"github.com/JtheGunner/omnishell/modules"
)

func TestDeltaGolden(t *testing.T) {
	all := map[string]any{"side_by_side": true, "navigate": true, "line_numbers": true}
	none := map[string]any{"side_by_side": false, "navigate": false, "line_numbers": false}
	for _, shell := range []string{"zsh", "bash"} {
		assertGoldenNamed(t, "delta", shell+"-default", renderModule(t, "delta", shell, nil))
		assertGoldenNamed(t, "delta", shell+"-all", renderModule(t, "delta", shell, all))
		assertGoldenNamed(t, "delta", shell+"-none", renderModule(t, "delta", shell, none))
	}
}

func TestDeltaFlagsAndGuards(t *testing.T) {
	for _, shell := range []string{"zsh", "bash"} {
		out := renderModule(t, "delta", shell, map[string]any{"side_by_side": true})
		for _, want := range []string{
			"command -v delta", `[ -z "${GIT_PAGER+x}" ]`,
			"export GIT_PAGER='delta --navigate --line-numbers --side-by-side'",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("%s: missing %q in:\n%s", shell, want, out)
			}
		}
		if strings.Contains(out, "git config") {
			t.Errorf("%s: snippet must not write git configuration:\n%s", shell, out)
		}
	}
}

func TestDeltaManifest(t *testing.T) {
	reg, err := module.LoadRegistry(modules.FS(), "")
	if err != nil {
		t.Fatal(err)
	}
	m, ok := reg.Get("delta")
	if !ok {
		t.Fatal("module delta not embedded")
	}
	for mgr, want := range map[string]string{
		"brew": "git-delta", "apt": "git-delta", "dnf": "git-delta",
		"pacman": "git-delta", "zypper": "git-delta", "apk": "delta",
	} {
		if got := m.Manifest.Packages.ForManager(mgr); len(got) != 1 || got[0] != want {
			t.Errorf("%s packages = %v, want [%s]", mgr, got, want)
		}
	}
	pd, ok := reg.Get("pager-defaults")
	if !ok {
		t.Fatal("pager-defaults missing")
	}
	order, err := graph.Order(map[string]module.Manifest{"delta": m.Manifest, "pager-defaults": pd.Manifest})
	if err != nil {
		t.Fatal(err)
	}
	if order[0] != "pager-defaults" {
		t.Errorf("order = %v, want pager-defaults before delta", order)
	}
}
