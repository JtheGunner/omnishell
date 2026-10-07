package cli

import (
	"errors"
	"fmt"

	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/JtheGunner/omnishell/internal/modedit"
	"github.com/spf13/cobra"
)

func newEnableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "enable <module>",
		Short: "Enable a module in the config",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return setModuleEnabled(cmd, args[0], true)
		},
	}
}

func newDisableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "disable <module>",
		Short: "Disable a module in the config",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return setModuleEnabled(cmd, args[0], false)
		},
	}
}

// setModuleEnabled is the shared body of `enable` and `disable`: it builds the
// engine, hands the change to modedit (which owns the rules), and prints a
// confirmation. Every error path classifies to exit code 2 and prints nothing
// to stdout.
func setModuleEnabled(cmd *cobra.Command, id string, enabled bool) error {
	e, cfgPath, _, err := buildEngine(cmd.OutOrStdout(), cmd.ErrOrStderr())
	if err != nil {
		return err
	}
	editor := modedit.Editor{Engine: e, CfgPath: cfgPath}
	if enabled {
		err = editor.Enable(id)
	} else {
		err = editor.Disable(id)
	}
	if err != nil {
		return hintIfUninitialised(cmd, err)
	}

	verb := "enabled"
	if !enabled {
		verb = "disabled"
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s %s — run 'omnishell apply' to apply\n", verb, id)
	return nil
}

// hintIfUninitialised writes the `run 'omnishell init' first` hint to stderr
// when err says config.toml is missing, and returns err unchanged (ErrNotFound
// classifies to exit code 2).
func hintIfUninitialised(cmd *cobra.Command, err error) error {
	if errors.Is(err, config.ErrNotFound) {
		_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "run 'omnishell init' first")
	}
	return err
}
