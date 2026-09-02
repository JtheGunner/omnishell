// Package engine orchestrates plan/apply/remove/doctor/uninstall over the
// config, module registry, package manager, and on-disk files.
package engine

import (
	"io"
	"time"

	"github.com/JtheGunner/omnishell/internal/module"
	"github.com/JtheGunner/omnishell/internal/pkgmgr"
	"github.com/JtheGunner/omnishell/internal/platform"
)

// Engine holds every dependency the operations need.
type Engine struct {
	Platform  platform.Info
	Registry  module.Registry
	Manager   pkgmgr.Manager
	ManagerOK bool
	Runner    pkgmgr.Runner
	Now       func() time.Time
	Stdout    io.Writer
	Stderr    io.Writer
	Prompt    func(question string) bool
}

func (e Engine) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

// ConfigError marks an error the CLI should map to exit code 2.
type ConfigError struct{ Err error }

func (e ConfigError) Error() string { return e.Err.Error() }
func (e ConfigError) Unwrap() error { return e.Err }
