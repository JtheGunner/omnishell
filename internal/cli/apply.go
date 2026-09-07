package cli

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/JtheGunner/omnishell/internal/engine"
	"github.com/spf13/cobra"
)

func newApplyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Bring your shells up to date with the config",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runApply(cmd, false)
		},
	}
	cmd.Flags().Bool("dry-run", false, "show the plan without changing anything")
	cmd.Flags().BoolP("yes", "y", false, "apply without the confirmation prompt")
	cmd.Flags().Bool("no-packages", false, "skip package installation")
	cmd.Flags().Bool("force", false, "overwrite init files that were edited by hand")
	cmd.Flags().Bool("reload", false, "re-exec $SHELL after a successful apply")
	return cmd
}

func newDiffCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "diff",
		Short: "Show what apply would change (apply --dry-run)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runApply(cmd, true)
		},
	}
}

// runApply is the shared body of `apply` and `diff`. When forceDryRun is true it
// ignores the apply-only flags and runs a plan-only pass, so `diff` is exactly
// `apply --dry-run`.
func runApply(cmd *cobra.Command, forceDryRun bool) error {
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

	opts := engine.ApplyOptions{}
	reload := false
	if forceDryRun {
		opts.DryRun = true
	} else {
		opts.DryRun, _ = cmd.Flags().GetBool("dry-run")
		opts.Yes, _ = cmd.Flags().GetBool("yes")
		opts.NoPackages, _ = cmd.Flags().GetBool("no-packages")
		opts.Force, _ = cmd.Flags().GetBool("force")
		reload, _ = cmd.Flags().GetBool("reload")
	}

	res, err := e.Apply(cfg, cfgPath, lockPath, opts)

	if res.DryRun {
		_, _ = fmt.Fprintln(out, res.PlanText)
		return err
	}

	if errors.Is(err, engine.ErrAborted) {
		_, _ = fmt.Fprintln(errOut, "aborted")
		return err
	}

	var cfgErr engine.ConfigError
	if errors.As(err, &cfgErr) {
		return err
	}

	printApplySummary(out, res)

	// --reload re-execs the login shell, but only after a fully successful,
	// non-dry-run apply and only in an interactive session.
	if reload && err == nil && !res.DryRun {
		handleReload(out, errOut)
	}
	return err
}

// handleReload replaces the process with a fresh $SHELL. In a non-interactive
// session, or with $SHELL unset, it prints a hint and returns instead — so it
// is safe in scripts and CI.
func handleReload(out, errOut io.Writer) {
	if !reloadInteractive() {
		_, _ = fmt.Fprintln(out, "not an interactive shell; skipping --reload (run: exec $SHELL)")
		return
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		_, _ = fmt.Fprintln(out, "$SHELL is not set; skipping --reload (run: exec $SHELL)")
		return
	}
	_, _ = fmt.Fprintf(out, "reloading %s\n", shell)
	if err := reloadExec(shell); err != nil {
		_, _ = fmt.Fprintf(errOut, "reload failed: %v\n", err)
	}
}

// printApplySummary writes one line per module plus the backup dir, if any.
func printApplySummary(out io.Writer, res engine.Result) {
	for _, m := range res.Modules {
		if m.Note != "" {
			_, _ = fmt.Fprintf(out, "  %s  %s  %s\n", m.Status, m.ID, m.Note)
			continue
		}
		_, _ = fmt.Fprintf(out, "  %s  %s\n", m.Status, m.ID)
	}
	if res.BackupDir != "" {
		_, _ = fmt.Fprintf(out, "backup: %s\n", res.BackupDir)
	}
}
