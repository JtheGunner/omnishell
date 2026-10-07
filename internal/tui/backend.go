// Package tui is the full-screen module browser behind `omnishell tui`.
//
// It knows nothing about the engine, the config file or the CLI: everything it
// needs comes through Backend, so the model can be tested with a fake and the
// package never writes to disk.
package tui

import "github.com/JtheGunner/omnishell/internal/modedit"

// Backend is everything the TUI needs from the rest of omnishell.
type Backend interface {
	// Modules returns one view per known module, sorted by ID.
	Modules() ([]modedit.ModuleView, error)
}
