package cli

import (
	"errors"

	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/JtheGunner/omnishell/internal/engine"
)

// errDrift is returned only by the doctor command (Task 25); the CLI maps it to
// exit code 3.
var errDrift = errors.New("drift detected")

// ClassifyError maps an error to a process exit code:
//
//	nil                                       -> 0
//	config.Error / engine.ConfigError / ErrNotFound -> 2
//	errDrift                                  -> 3
//	engine.ErrDegraded / engine.ErrAborted    -> 1
//	anything else                             -> 1
func ClassifyError(err error) int {
	if err == nil {
		return 0
	}
	var engCfg engine.ConfigError
	var cfgErr config.Error
	if errors.As(err, &engCfg) || errors.As(err, &cfgErr) || errors.Is(err, config.ErrNotFound) {
		return 2
	}
	if errors.Is(err, errDrift) {
		return 3
	}
	if errors.Is(err, engine.ErrDegraded) || errors.Is(err, engine.ErrAborted) {
		return 1
	}
	return 1
}
