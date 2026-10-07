package tui

import (
	"errors"
	"fmt"
	"io"

	tea "charm.land/bubbletea/v2"
)

// Result is what the user decided before the UI closed.
type Result struct {
	// ApplyRequested is true when the user confirmed the plan screen. The UI
	// does not apply anything itself: the caller runs apply once the terminal
	// is back to normal.
	ApplyRequested bool
}

// Run loads the modules from b and runs the browser on in and out until the
// user quits. The modules are loaded before the terminal is touched, so a
// failure reaches the caller as a plain error and never leaves a half-drawn
// screen behind.
func Run(b Backend, in io.Reader, out io.Writer) (Result, error) {
	views, err := b.Modules()
	if err != nil {
		return Result{}, err
	}
	return finish(tea.NewProgram(New(b, views), tea.WithInput(in), tea.WithOutput(out)).Run())
}

// finish turns what the program ended with into Run's result. An interrupt
// (SIGINT from outside) is the user leaving: it ends quietly and never leads on
// to apply.
func finish(final tea.Model, err error) (Result, error) {
	switch {
	case errors.Is(err, tea.ErrInterrupted):
		return Result{}, nil
	case err != nil:
		return Result{}, fmt.Errorf("run terminal UI: %w", err)
	}
	return resultOf(final), nil
}

// resultOf reads the user's decision off the model the program ended with.
func resultOf(final tea.Model) Result {
	m, ok := final.(Model)
	return Result{ApplyRequested: ok && m.applyRequested}
}
