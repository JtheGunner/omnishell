package tui

import (
	"fmt"
	"io"

	tea "charm.land/bubbletea/v2"
)

// Run loads the modules from b and runs the browser on in and out until the
// user quits. The modules are loaded before the terminal is touched, so a
// failure reaches the caller as a plain error and never leaves a half-drawn
// screen behind.
func Run(b Backend, in io.Reader, out io.Writer) error {
	views, err := b.Modules()
	if err != nil {
		return err
	}
	if _, err := tea.NewProgram(New(views), tea.WithInput(in), tea.WithOutput(out)).Run(); err != nil {
		return fmt.Errorf("run terminal UI: %w", err)
	}
	return nil
}
