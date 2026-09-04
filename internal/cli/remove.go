package cli

import (
	"errors"
	"fmt"

	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/JtheGunner/omnishell/internal/engine"
	"github.com/spf13/cobra"
)

func newRemoveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove <id>",
		Short: "Disable a module and drop its shell section (optionally purge its packages)",
		Long: `Disable a module in the config, then re-apply so its init section and lock
entry are dropped.

With --purge it also uninstalls the packages omnishell installed for the
module, runs the module's remove hook, and deletes any vendored files.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRemove(cmd, args[0])
		},
	}
	cmd.Flags().Bool("purge", false, "also uninstall the module's packages, run its remove hook, and delete vendored files")
	cmd.Flags().BoolP("yes", "y", false, "remove without the confirmation prompt")
	cmd.Flags().Bool("dry-run", false, "show the plan without changing anything")
	return cmd
}

func runRemove(cmd *cobra.Command, id string) error {
	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()

	e, cfgPath, lockPath, err := buildEngine(out, errOut)
	if err != nil {
		return err
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		if errors.Is(err, config.ErrNotFound) {
			_, _ = fmt.Fprintln(errOut, "run 'omnishell init' first")
		}
		return err
	}

	purge, _ := cmd.Flags().GetBool("purge")
	yes, _ := cmd.Flags().GetBool("yes")
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	res, err := e.Remove(cfg, cfgPath, lockPath, id, purge, engine.ApplyOptions{DryRun: dryRun, Yes: yes})

	if res.DryRun {
		_, _ = fmt.Fprintln(out, res.PlanText)
		return err
	}

	printApplySummary(out, res)
	return err
}
