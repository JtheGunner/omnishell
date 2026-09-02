package engine_test

import (
	"errors"
	"testing"

	"github.com/JtheGunner/omnishell/internal/engine"
	"github.com/JtheGunner/omnishell/internal/module"
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
