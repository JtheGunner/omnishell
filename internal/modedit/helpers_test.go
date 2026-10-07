package modedit_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/JtheGunner/omnishell/internal/engine"
	"github.com/JtheGunner/omnishell/internal/modedit"
	"github.com/JtheGunner/omnishell/internal/module"
	"github.com/JtheGunner/omnishell/internal/pkgmgr"
	"github.com/JtheGunner/omnishell/internal/platform"
)

// newEditor builds an Editor over the fixture modules in testdata/modules and a
// config.toml in a temp dir. withConfig writes the default config; otherwise
// the file does not exist. presentShells names the shells reported as present
// on the fake host. It returns the editor and the config path.
func newEditor(t *testing.T, withConfig bool, presentShells ...string) (modedit.Editor, string) {
	t.Helper()
	reg, err := module.LoadRegistry(nil, "testdata/modules")
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	present := map[string]bool{}
	for _, s := range presentShells {
		present[s] = true
	}
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	if withConfig {
		if err := os.WriteFile(cfgPath, config.RenderDefault(), 0o644); err != nil {
			t.Fatalf("write config: %v", err)
		}
	}
	ed := modedit.Editor{
		Engine: engine.Engine{
			Platform: platform.Info{
				OS:        platform.Linux,
				HomeDir:   t.TempDir(),
				ConfigDir: t.TempDir(),
				Shells: []platform.ShellInfo{
					{Name: "zsh", RCPath: "/x/.zshrc", Present: present["zsh"]},
					{Name: "bash", RCPath: "/x/.bashrc", Present: present["bash"]},
				},
			},
			Registry:  reg,
			Manager:   &pkgmgr.MockManager{NameV: "apt", DetectV: true, Installed: map[string]bool{}},
			ManagerOK: true,
			Runner:    &pkgmgr.MockRunner{},
		},
		CfgPath: cfgPath,
	}
	return ed, cfgPath
}
