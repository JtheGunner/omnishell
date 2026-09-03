package cli

import (
	"errors"
	"fmt"

	"github.com/JtheGunner/omnishell/internal/engine"
	"github.com/spf13/cobra"
)

func newUninstallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove omnishell's shell integration",
		Long: `Remove omnishell's shell integration: the marker block in every managed rc
file and the generated init.<shell> files under the config dir. Every touched
file is backed up first.

This does NOT uninstall packages omnishell installed for modules. Use
'omnishell remove <id> --purge' for that.

With --purge the whole config dir (config.toml, state.lock.json, modules/,
vendor/, backups/) is deleted too; that run's backups are written to a temp
dir outside the config dir and its path is printed.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runUninstall(cmd)
		},
	}
	cmd.Flags().Bool("purge", false, "also delete ~/.config/omnishell entirely")
	cmd.Flags().BoolP("yes", "y", false, "uninstall without the confirmation prompt")
	return cmd
}

func runUninstall(cmd *cobra.Command) error {
	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()

	e, _, lockPath, err := buildEngine(out, errOut)
	if err != nil {
		return err
	}

	purge, _ := cmd.Flags().GetBool("purge")
	yes, _ := cmd.Flags().GetBool("yes")

	res, err := e.Uninstall(lockPath, engine.UninstallOptions{PurgeConfigDir: purge, Yes: yes})

	if errors.Is(err, engine.ErrAborted) {
		fmt.Fprintln(out, "aborted")
		return err
	}

	for _, m := range res.Modules {
		if m.Note != "" {
			fmt.Fprintf(out, "  %s  %s  %s\n", m.Status, m.ID, m.Note)
			continue
		}
		fmt.Fprintf(out, "  %s  %s\n", m.Status, m.ID)
	}
	if res.BackupDir != "" {
		fmt.Fprintf(out, "backup: %s\n", res.BackupDir)
	}
	if !res.Changed && err == nil {
		fmt.Fprintln(out, "nothing to uninstall")
	}

	return err
}
