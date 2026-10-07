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
	// Enable and Disable write the module's state to config.toml. The error
	// text is shown to the user as it is, so it should read as a sentence.
	Enable(id string) error
	Disable(id string) error
	// Statuses returns every module's current state in config.toml, keyed by
	// id. It must be cheap: the UI calls it after every write, whereas Modules
	// can take seconds because it asks the package manager about every package.
	Statuses() (map[string]modedit.Status, error)
	// Plan computes what applying the current config would do. It must not
	// change anything, and it may take seconds (it asks the package manager),
	// so the UI calls it in a command and shows a waiting screen meanwhile.
	Plan() (PlanPreview, error)
	// Options returns the options of one module with their current values. It
	// reads only config.toml and the manifest, so it is cheap.
	Options(id string) ([]modedit.OptionView, error)
	// SetOption writes one option of one module, validated against the
	// module's schema; nothing is written for a rejected value. The error text
	// is shown to the user as it is.
	SetOption(id, key, raw string) error
}

// PlanPreview is what the plan screen shows.
type PlanPreview struct {
	// Text is the plan, one item per line.
	Text string
	// NeedsApply is false when applying would do nothing, in which case the
	// plan screen offers no way forward.
	NeedsApply bool
}
