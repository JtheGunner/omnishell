package cli

import (
	"errors"
	"io"
	"os"

	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/JtheGunner/omnishell/internal/engine"
	"github.com/JtheGunner/omnishell/internal/modedit"
	"github.com/JtheGunner/omnishell/internal/tui"
)

// Seams for tests: whether the process is attached to a terminal, and the
// terminal UI itself. Tests reassign them via SetTUIForTest.
var (
	tuiIsTerminal = defaultTUIIsTerminal
	tuiRun        = tui.Run
)

// SetTUIForTest swaps the terminal check and the UI runner. Passing nil for
// either restores the real implementation.
func SetTUIForTest(isTerminal func(in io.Reader, out io.Writer) bool, run func(b tui.Backend, in io.Reader, out io.Writer) (tui.Result, error)) {
	tuiIsTerminal = defaultTUIIsTerminal
	if isTerminal != nil {
		tuiIsTerminal = isTerminal
	}
	tuiRun = tui.Run
	if run != nil {
		tuiRun = run
	}
}

// defaultTUIIsTerminal reports whether both in and out are terminals.
func defaultTUIIsTerminal(in io.Reader, out io.Writer) bool {
	inFile, ok := in.(*os.File)
	if !ok {
		return false
	}
	outFile, ok := out.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(inFile.Fd()) && term.IsTerminal(outFile.Fd())
}

// tuiBackend adapts a modedit.Editor, and the engine inside it, to the
// tui.Backend the UI consumes.
type tuiBackend struct {
	editor   modedit.Editor
	cfgPath  string
	lockPath string
}

func (b tuiBackend) Modules() ([]modedit.ModuleView, error) { return b.editor.Views() }

func (b tuiBackend) Statuses() (map[string]modedit.Status, error) { return b.editor.Statuses() }

func (b tuiBackend) Enable(id string) error  { return userMessage(b.editor.Enable(id)) }
func (b tuiBackend) Disable(id string) error { return userMessage(b.editor.Disable(id)) }

// Options lists a module's options with their current values; SetOption writes
// one. Both reduce a config.Error to its message, like Enable and Disable.
func (b tuiBackend) Options(id string) ([]modedit.OptionView, error) {
	opts, err := b.editor.Options(id)
	return opts, userMessage(err)
}

func (b tuiBackend) SetOption(id, key, raw string) error {
	return userMessage(b.editor.SetOption(id, key, raw))
}

// Plan shows what `omnishell apply` would do for the config as it is now. It
// reads config.toml again, so toggles made in the UI are included.
func (b tuiBackend) Plan() (tui.PlanPreview, error) {
	cfg, err := config.Load(b.cfgPath)
	if err != nil {
		return tui.PlanPreview{}, userMessage(err)
	}
	preview, err := b.editor.Engine.Preview(cfg, b.lockPath)
	if err != nil {
		return tui.PlanPreview{}, userMessage(err)
	}
	return tui.PlanPreview{Text: preview.Text, NeedsApply: preview.NeedsApply}, nil
}

// userMessage reduces a config.Error to its message. The TUI shows it on a
// one-line status bar, where the config path that prefixes every config.Error
// would push the actual reason off the screen.
func userMessage(err error) error {
	var cfgErr config.Error
	if errors.As(err, &cfgErr) {
		return errors.New(cfgErr.Msg)
	}
	return err
}

func newTUICmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Browse modules in a full-screen terminal UI",
		Long: `Browse modules in a full-screen terminal UI.

Shows every known module with its description, homepage, package status,
platforms and shells. Move with the arrow keys, press space to enable or
disable the selected module, press o to edit its options, type / to filter,
q to quit. Space writes config.toml at once, exactly like 'omnishell enable'
and 'disable'; it never touches your shells. Press a to preview the plan;
confirming it closes the UI and runs 'omnishell apply', which still asks
before it changes anything.

Needs an interactive terminal; in scripts use 'omnishell list'.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			in, out := cmd.InOrStdin(), cmd.OutOrStdout()
			if !tuiIsTerminal(in, out) {
				return engine.ConfigError{Err: errors.New(
					"omnishell tui needs an interactive terminal; use 'omnishell list', 'enable' and 'apply' in scripts")}
			}

			// The engine's runner copies a package manager's output to the writers
			// it is given, and the UI owns the terminal: send it nowhere.
			e, cfgPath, lockPath, err := buildEngine(io.Discard, io.Discard)
			if err != nil {
				return err
			}
			// A missing or malformed config.toml must stop us before the screen
			// is taken over, so the message lands in the normal terminal.
			if _, err := config.Load(cfgPath); err != nil {
				return hintIfUninitialised(cmd, err)
			}

			backend := tuiBackend{
				editor:   modedit.Editor{Engine: e, CfgPath: cfgPath},
				cfgPath:  cfgPath,
				lockPath: lockPath,
			}
			result, err := tuiRun(backend, in, out)
			if err != nil || !result.ApplyRequested {
				return err
			}
			return handOffToApply(cmd)
		},
	}
}

// handOffToApply runs `omnishell apply` the way the user would have typed it,
// now that the UI has given the terminal back: its plan, its confirmation
// prompt, sudo, hooks and exit codes are exactly those of the real command.
// The UI's plan screen is a preview, not a confirmation.
func handOffToApply(cmd *cobra.Command) error {
	apply := newApplyCmd() // every flag at its default
	apply.SetIn(cmd.InOrStdin())
	apply.SetOut(cmd.OutOrStdout())
	apply.SetErr(cmd.ErrOrStderr())
	return runApply(apply, false)
}
