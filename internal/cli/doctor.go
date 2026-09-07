package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/JtheGunner/omnishell/internal/engine"
	"github.com/spf13/cobra"
)

func newDoctorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check the installed shell environment for drift",
		Long: `Check the installed shell environment for drift. Exit 3 if any drift is found.

With --fix, repair the drift that a re-apply resolves (a missing or stale
init file, a missing rc source line, orphaned lockfile entries). A
hand-edited init file and missing packages are never touched automatically —
those need 'omnishell apply' (or 'apply --force') run by hand.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			fix, _ := cmd.Flags().GetBool("fix")
			yes, _ := cmd.Flags().GetBool("yes")
			return runDoctor(cmd, fix, yes)
		},
	}
	cmd.Flags().Bool("fix", false, "repair the drift a re-apply resolves")
	cmd.Flags().BoolP("yes", "y", false, "with --fix, skip the confirmation prompt")
	return cmd
}

func runDoctor(cmd *cobra.Command, fix, yes bool) error {
	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()

	e, cfgPath, lockPath, err := buildEngine(out, errOut)
	if err != nil {
		return err
	}

	// A missing config is not fatal for doctor: it reports "never applied" via
	// Doctor's step 1. Treat ErrNotFound as an empty config.
	cfg, err := config.Load(cfgPath)
	if err != nil && !errors.Is(err, config.ErrNotFound) {
		return err
	}

	rep, err := e.Doctor(cfg, cfgPath, lockPath)
	if err != nil {
		return err
	}
	printFindings(out, rep)

	if !rep.HasDrift() {
		_, _ = fmt.Fprintln(out, "no drift detected")
		return nil
	}
	if !fix {
		return errDrift
	}

	fixable, blocked := rep.DriftFixability()
	if len(blocked) > 0 {
		hint := "run 'omnishell apply' to resolve it"
		for _, f := range blocked {
			if strings.HasPrefix(f.Code, "initfile-edited") {
				hint = "run 'omnishell apply --force' to resolve it"
				break
			}
		}
		_, _ = fmt.Fprintf(errOut, "cannot auto-fix: %s — %s\n", strings.Join(codeList(blocked), ", "), hint)
		return errDrift
	}

	if !yes && !promptFn(fmt.Sprintf("Fix %d drift issue(s) by re-applying?", len(fixable))) {
		_, _ = fmt.Fprintln(errOut, "aborted")
		return errDrift
	}

	if _, aerr := e.Apply(cfg, cfgPath, lockPath, engine.ApplyOptions{Yes: true, NoPackages: true, Refresh: true}); aerr != nil {
		return aerr
	}

	rep2, derr := e.Doctor(cfg, cfgPath, lockPath)
	if derr != nil {
		return derr
	}
	if rep2.HasDrift() {
		printFindings(out, rep2)
		_, _ = fmt.Fprintln(out, "some drift remains; run 'omnishell apply'")
		return errDrift
	}
	_, _ = fmt.Fprintf(out, "fixed %d drift issue(s)\n", len(fixable))
	return nil
}

func printFindings(out io.Writer, rep engine.DoctorReport) {
	for _, f := range rep.Findings {
		_, _ = fmt.Fprintf(out, "[%s] %s  %s\n", f.Severity, f.Code, f.Message)
	}
}

func codeList(fs []engine.Finding) []string {
	out := make([]string, len(fs))
	for i, f := range fs {
		out[i] = f.Code
	}
	return out
}
