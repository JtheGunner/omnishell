package cli

import (
	"errors"
	"fmt"
	"io"

	"github.com/JtheGunner/omnishell/internal/engine"
	"github.com/spf13/cobra"
)

func newRollbackCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rollback",
		Short: "List backup snapshots, or restore files/lockfile to before one",
		Long: `Without --to, lists available backup snapshots (newest first).

With --to <timestamp>, restores every file changed since (and including) that
snapshot back to its state immediately before it, chaining through every
later apply/remove/uninstall run in reverse. Packages are never touched; use
'omnishell remove --purge' for that. config.toml is never touched either —
only the files apply/remove/uninstall write.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runRollback(cmd)
		},
	}
	cmd.Flags().String("to", "", "backup snapshot timestamp to roll back to")
	cmd.Flags().Bool("dry-run", false, "show what would change without changing anything")
	cmd.Flags().BoolP("yes", "y", false, "roll back without the confirmation prompt")
	return cmd
}

func runRollback(cmd *cobra.Command) error {
	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()

	e, _, _, err := buildEngine(out, errOut)
	if err != nil {
		return err
	}

	target, _ := cmd.Flags().GetString("to")
	if target == "" {
		snapshots, lerr := e.ListSnapshots()
		if lerr != nil {
			return lerr
		}
		if len(snapshots) == 0 {
			_, _ = fmt.Fprintln(out, "no backup snapshots found")
			return nil
		}
		for _, s := range snapshots {
			_, _ = fmt.Fprintf(out, "%s  %s  %d file(s)\n", s.Timestamp, s.Kind, s.FileCount)
		}
		return nil
	}

	dryRun, _ := cmd.Flags().GetBool("dry-run")
	yes, _ := cmd.Flags().GetBool("yes")
	res, err := e.Rollback(target, engine.RollbackOptions{DryRun: dryRun, Yes: yes})

	if errors.Is(err, engine.ErrAborted) {
		_, _ = fmt.Fprintln(errOut, "aborted")
		return err
	}

	printRollbackSummary(out, res)
	return err
}

func printRollbackSummary(out io.Writer, res engine.RollbackResult) {
	for _, f := range res.FilesRestored {
		_, _ = fmt.Fprintf(out, "  restored  %s\n", f)
	}
	for _, f := range res.FilesRemoved {
		_, _ = fmt.Fprintf(out, "  removed   %s\n", f)
	}
	if res.BackupDir != "" {
		_, _ = fmt.Fprintf(out, "backup: %s\n", res.BackupDir)
	}
}
