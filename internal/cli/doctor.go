package cli

import (
	"errors"
	"fmt"

	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/spf13/cobra"
)

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check the installed shell environment for drift",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()

			e, cfgPath, lockPath, err := buildEngine(out, cmd.ErrOrStderr())
			if err != nil {
				return err
			}

			// A missing config is not fatal for doctor: it reports "never
			// applied" via Doctor's step 1. Treat ErrNotFound as an empty config.
			cfg, err := config.Load(cfgPath)
			if err != nil && !errors.Is(err, config.ErrNotFound) {
				return err
			}

			rep, err := e.Doctor(cfg, cfgPath, lockPath)
			if err != nil {
				return err
			}

			for _, f := range rep.Findings {
				_, _ = fmt.Fprintf(out, "[%s] %s  %s\n", f.Severity, f.Code, f.Message)
			}

			if rep.HasDrift() {
				return errDrift
			}
			_, _ = fmt.Fprintln(out, "no drift detected")
			return nil
		},
	}
}
