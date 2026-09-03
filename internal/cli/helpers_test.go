package cli_test

import (
	"os"
	"path/filepath"
	"testing"
)

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
