package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/JtheGunner/omnishell/internal/engine"
	"github.com/JtheGunner/omnishell/internal/module"
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
	cfg, err := loadConfigOrHint(cmd, cfgPath)
	if err != nil {
		return err
	}
	mod, ok := e.Registry.Get(id)
	if !ok {
		return config.Error{Path: cfgPath, Msg: fmt.Sprintf("unknown module %q", id)}
	}
	if enabled {
		if err := checkShellCompatible(cfgPath, cfg, e, mod); err != nil {
			return err
		}
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

// checkShellCompatible refuses to enable a module that none of the shells
// omnishell currently manages on this host could ever run — e.g. a zsh-only
// module (autosuggestions, syntax-highlighting) on a bash-only system.
// Enabling it anyway would leave it permanently degraded ("no snippet for
// any managed shell") with no way to notice besides `doctor`/`list`.
func checkShellCompatible(cfgPath string, cfg config.Config, e engine.Engine, mod module.Module) error {
	managed := engine.ManagedShells(cfg, e.Platform)
	for _, sh := range managed {
		if contains(mod.Manifest.Shells, sh) {
			return nil
		}
	}
	return config.Error{Path: cfgPath, Msg: fmt.Sprintf(
		"module %q only supports %s, but none of your managed shells (%s) do — install one of those shells first",
		mod.Manifest.Module.ID, strings.Join(mod.Manifest.Shells, ", "), joinOrNone(managed),
	)}
}

func contains(ss []string, v string) bool {
	for _, s := range ss {
		if s == v {
			return true
		}
	}
	return false
}

func joinOrNone(ss []string) string {
	if len(ss) == 0 {
		return "none"
	}
	return strings.Join(ss, ", ")
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
