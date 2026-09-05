package engine_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/JtheGunner/omnishell/internal/engine"
	"github.com/JtheGunner/omnishell/internal/module"
	"github.com/JtheGunner/omnishell/internal/pkgmgr"
	"github.com/JtheGunner/omnishell/internal/platform"
)

func asConfigError(err error, target *engine.ConfigError) bool { return errors.As(err, target) }

func mustManifest(t *testing.T, e engine.Engine, id string) module.Manifest {
	t.Helper()
	m, ok := e.Registry.Get(id)
	if !ok {
		t.Fatalf("module %q not in registry", id)
	}
	return m.Manifest
}

func rollbackEngine(t *testing.T, home string, mgr *pkgmgr.MockManager, out *bytes.Buffer, now func() time.Time) engine.Engine {
	t.Helper()
	reg, err := module.LoadRegistry(nil, "testdata/modules")
	if err != nil {
		t.Fatal(err)
	}
	return engine.Engine{
		Platform: platform.Info{
			OS:        platform.Linux,
			HomeDir:   home,
			ConfigDir: filepath.Join(home, ".config", "omnishell"),
			Shells: []platform.ShellInfo{
				{Name: "zsh", RCPath: filepath.Join(home, ".zshrc"), Present: false},
				{Name: "bash", RCPath: filepath.Join(home, ".bashrc"), Present: true},
			},
		},
		Registry:  reg,
		Manager:   mgr,
		ManagerOK: true,
		Runner:    &pkgmgr.MockRunner{},
		Now:       now,
		Stdout:    out,
		Stderr:    out,
		Prompt:    func(string) bool { return true },
	}
}

// advancingClock returns a Now func that moves forward one minute on every
// call, so sequential engine operations in a test never collide on the same
// second-resolution backup-session timestamp.
func advancingClock(start time.Time) func() time.Time {
	t := start
	return func() time.Time {
		t = t.Add(time.Minute)
		return t
	}
}

func writeConfig(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
