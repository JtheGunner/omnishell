package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/JtheGunner/omnishell/internal/modedit"
	"github.com/spf13/cobra"
)

type listRow struct {
	Module      string `json:"module"`
	Status      string `json:"status"`
	Description string `json:"description"`
	Homepage    string `json:"homepage,omitempty"`
	Packages    string `json:"packages"`
	Platforms   string `json:"platforms"`
	Shells      string `json:"shells"`
	Src         string `json:"src"`
}

func newListCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List known modules and their status",
		Long: `List known modules and their status.

PACKAGES is one of:
  ok       all of the module's packages for the detected package manager are installed
  missing  at least one is not installed yet (run 'omnishell apply')
  n/a      the module needs no external package — it only configures your
           shell's own built-ins (e.g. completion, history), or no package
           manager was detected`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			e, cfgPath, _, err := buildQueryEngine(out, cmd.ErrOrStderr())
			if err != nil {
				return err
			}

			views, err := modedit.Editor{Engine: e, CfgPath: cfgPath}.Views()
			if err != nil {
				return err
			}

			rows := make([]listRow, 0, len(views))
			for _, v := range views {
				rows = append(rows, listRow{
					Module:      v.ID,
					Status:      string(v.Status),
					Description: v.Description,
					Homepage:    v.Homepage,
					Packages:    string(v.Packages),
					Platforms:   strings.Join(v.Platforms, ","),
					Shells:      strings.Join(v.Shells, ","),
					Src:         string(v.Origin),
				})
			}

			if asJSON {
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(rows)
			}

			tw := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
			_, _ = fmt.Fprintln(tw, "MODULE\tSTATUS\tPACKAGES\tPLATFORMS\tSHELLS\tSRC\tDESCRIPTION")
			for _, r := range rows {
				_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
					r.Module, r.Status, r.Packages, r.Platforms, r.Shells, r.Src, r.Description)
			}
			return tw.Flush()
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit a JSON array instead of the table")
	return cmd
}
