package cli

import (
	"errors"
	"fmt"

	"github.com/JtheGunner/omnishell/internal/config"
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

// setModuleEnabled is the shared body of `enable` and `disable`: it resolves the
// config path and registry, requires an existing config, validates the module
// id, flips modules.<id>.enabled, and prints a confirmation. Every error path
// classifies to exit code 2 and prints nothing to stdout.
func setModuleEnabled(cmd *cobra.Command, id string, enabled bool) error {
	e, cfgPath, _, err := buildEngine(cmd.OutOrStdout(), cmd.ErrOrStderr())
	if err != nil {
		return err
	}
	if _, err := loadConfigOrHint(cmd, cfgPath); err != nil {
		return err
	}
	if _, ok := e.Registry.Get(id); !ok {
		return config.Error{Path: cfgPath, Msg: fmt.Sprintf("unknown module %q", id)}
	}
	if err := config.SetEnabled(cfgPath, id, enabled); err != nil {
		return err
	}

	verb := "enabled"
	if !enabled {
		verb = "disabled"
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s %s — run 'omnishell apply' to apply\n", verb, id)
	return nil
}

// loadConfigOrHint loads config.toml. When the file is missing it writes the
// `run 'omnishell init' first` hint to stderr; the returned error (ErrNotFound
// or a config.Error) classifies to exit code 2.
func loadConfigOrHint(cmd *cobra.Command, cfgPath string) (config.Config, error) {
	cfg, err := config.Load(cfgPath)
	if err == nil {
		return cfg, nil
	}
	if errors.Is(err, config.ErrNotFound) {
		_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "run 'omnishell init' first")
	}
	return config.Config{}, err
}
