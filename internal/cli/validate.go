package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"text/tabwriter"

	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/JtheGunner/omnishell/internal/engine"
	"github.com/spf13/cobra"
)

func newValidateCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Check config.toml against the module option schemas",
		Long: `Check config.toml against the module option schemas and dependency rules.

Unlike 'omnishell diff', validate computes no plan and probes nothing on the
host: it only checks that every enabled module resolves, its options are valid,
and the enabled set has no missing 'requires', no 'conflicts' pair, and no
dependency cycle. Exit 0 if the config is valid, exit 2 if not.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()

			e, cfgPath, _, err := buildEngine(out, cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			cfg, err := config.Load(cfgPath)
			if err != nil {
				if errors.Is(err, config.ErrNotFound) {
					_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "run 'omnishell init' first")
				}
				return err
			}

			rep := e.Validate(cfg)

			if asJSON {
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				if err := enc.Encode(struct {
					Valid    bool                     `json:"valid"`
					Problems []engine.ValidateProblem `json:"problems"`
				}{Valid: rep.OK(), Problems: rep.Problems}); err != nil {
					return err
				}
			} else if rep.OK() {
				_, _ = fmt.Fprintln(out, "config.toml is valid")
			} else {
				tw := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
				for _, p := range rep.Problems {
					mod := p.Module
					if mod == "" {
						mod = "-"
					}
					_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\n", mod, p.Code, p.Message)
				}
				_ = tw.Flush()
			}

			if !rep.OK() {
				return engine.ConfigError{Err: fmt.Errorf("config.toml has %d schema problem(s)", len(rep.Problems))}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit a JSON object instead of the text report")
	return cmd
}
