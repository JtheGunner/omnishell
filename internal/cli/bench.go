package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/JtheGunner/omnishell/internal/engine"
	"github.com/spf13/cobra"
)

// DefaultStartupBudgetMs is the per-shell added-cost threshold `omnishell
// bench` warns above when config.toml sets no startup_budget_ms.
const DefaultStartupBudgetMs = 200

type benchShellResult struct {
	Shell      string `json:"shell"`
	InitFile   bool   `json:"init_file"`
	BaselineMs int    `json:"baseline_ms"`
	SourcedMs  int    `json:"sourced_ms"`
	AddedMs    int    `json:"added_ms"`
	OverBudget bool   `json:"over_budget"`
}

func newBenchCmd() *cobra.Command {
	var asJSON bool
	var runs int
	cmd := &cobra.Command{
		Use:   "bench",
		Short: "Measure how much the generated init file adds to shell startup",
		Long: `Measure how much sourcing the generated init.<shell> file adds to shell
startup, per managed shell, and warn when it exceeds the startup budget
(config.toml [omnishell] startup_budget_ms, default 200).

The figure is the median added cost over several runs: it is a rough guide,
not a precise benchmark, and moves with machine load. bench always exits 0.`,
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

			budget := cfg.Omnishell.StartupBudgetMs
			if budget == 0 {
				budget = DefaultStartupBudgetMs
			}

			var results []benchShellResult
			for _, shell := range engine.ManagedShells(cfg, e.Platform) {
				r := benchShellResult{Shell: shell}
				initPath := filepath.Join(e.Platform.ConfigDir, "init."+shell)
				if !fileExists(initPath) {
					results = append(results, r)
					continue
				}
				shellPath, lerr := lookPath(shell)
				if lerr != nil {
					shellPath = shell
				}
				baseline, sourced, berr := benchRun(shellPath, initPath, runs)
				if berr != nil {
					return fmt.Errorf("bench %s: %w", shell, berr)
				}
				added := sourced - baseline
				if added < 0 {
					added = 0
				}
				r.InitFile = true
				r.BaselineMs = int(baseline.Milliseconds())
				r.SourcedMs = int(sourced.Milliseconds())
				r.AddedMs = int(added.Milliseconds())
				r.OverBudget = r.AddedMs > budget
				results = append(results, r)
			}

			if asJSON {
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(struct {
					BudgetMs int                `json:"budget_ms"`
					Shells   []benchShellResult `json:"shells"`
				}{BudgetMs: budget, Shells: results})
			}

			tw := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
			for _, r := range results {
				if !r.InitFile {
					_, _ = fmt.Fprintf(tw, "%s\tno init file yet (run omnishell apply)\n", r.Shell)
					continue
				}
				line := fmt.Sprintf("%s\tinit +%dms\t(source %dms, baseline %dms)",
					r.Shell, r.AddedMs, r.SourcedMs, r.BaselineMs)
				if r.OverBudget {
					line += fmt.Sprintf("\t⚠ over the %dms startup budget", budget)
				}
				_, _ = fmt.Fprintln(tw, line)
			}
			return tw.Flush()
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit a JSON object instead of the text report")
	cmd.Flags().IntVar(&runs, "runs", 5, "timed runs per measurement (a warm-up run is always discarded)")
	return cmd
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
