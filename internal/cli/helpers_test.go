package cli_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/JtheGunner/omnishell/internal/cli"
	"github.com/JtheGunner/omnishell/internal/pkgmgr"
)

// setupModuleCLITest wires a hermetic environment for command tests that need
// the fixture modules in the registry: an isolated HOME/XDG config dir with the
// testdata modules installed, a no-op shell probe, and a mock package-manager
// runner. It returns the temp home and the config.toml path under it.
func setupModuleCLITest(t *testing.T) (home, cfgPath string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	configDir := filepath.Join(home, ".config", "omnishell")
	installFixtureModules(t, configDir)

	cli.SetLookPathForTest(func(string) (string, error) { return "", os.ErrNotExist })
	t.Cleanup(func() { cli.SetLookPathForTest(nil) })
	cli.SetRunnerForTest(&pkgmgr.MockRunner{})
	t.Cleanup(func() { cli.SetRunnerForTest(nil) })

	return home, filepath.Join(configDir, "config.toml")
}

// installFixtureModules copies internal/cli/testdata/modules/* into
// <configDir>/modules/ so a `list`/`init` flow test has at least one module in
// the registry (modules.FS() is empty until Phase 9). Reused by Tasks 24/25/26.
func installFixtureModules(t *testing.T, configDir string) {
	t.Helper()
	src := filepath.Join("testdata", "modules")
	dst := filepath.Join(configDir, "modules")

	err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(src, path)
		if rerr != nil {
			return rerr
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatalf("installFixtureModules: %v", err)
	}
}
