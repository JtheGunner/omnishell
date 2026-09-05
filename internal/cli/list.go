package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/JtheGunner/omnishell/internal/engine"
	"github.com/JtheGunner/omnishell/internal/module"
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
			e, cfgPath, _, err := buildEngine(out, cmd.ErrOrStderr())
			if err != nil {
				return err
			}

			cfg, cfgMissing, err := loadConfigForList(cfgPath)
			if err != nil {
				return err
			}

			overrides := map[string]bool{}
			for _, id := range e.Registry.Overrides() {
				overrides[id] = true
			}

			rows := make([]listRow, 0)
			for _, m := range e.Registry.All() {
				mf := m.Manifest
				id := mf.Module.ID

				status := "—"
				if !cfgMissing {
					status = "disabled"
					if mc, ok := cfg.Modules[id]; ok && mc.Enabled {
						status = "enabled"
					}
				}

				src := "builtin"
				if m.Source == module.SourceUser {
					src = "user"
				}
				if overrides[id] {
					src = "user*"
				}

				rows = append(rows, listRow{
					Module:      id,
					Status:      status,
					Description: mf.Module.Description,
					Homepage:    mf.Module.Homepage,
					Packages:    packageStatus(e, mf),
					Platforms:   strings.Join(mf.Platforms, ","),
					Shells:      strings.Join(mf.Shells, ","),
					Src:         src,
				})
			}
			sort.Slice(rows, func(i, j int) bool { return rows[i].Module < rows[j].Module })

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

// loadConfigForList loads config.toml, treating a missing file as "no config"
// (every module renders as —/disabled) rather than an error.
func loadConfigForList(cfgPath string) (config.Config, bool, error) {
	cfg, err := config.Load(cfgPath)
	if err == nil {
		return cfg, false, nil
	}
	if errors.Is(err, config.ErrNotFound) {
		return config.Default(), true, nil
	}
	return config.Config{}, false, err
}

// packageStatus reports ok/missing/n/a for a module's packages under the
// detected manager. Rendering choices: with no manager detected, or when the
// module declares no packages for that manager and no fallback, it is n/a;
// fallback-only modules also render n/a here (fallback satisfaction needs the
// apply-time vendor context).
func packageStatus(e engine.Engine, mf module.Manifest) string {
	var pkgs []string
	if e.ManagerOK {
		pkgs = mf.Packages.ForManager(e.Manager.Name())
	}
	hasFallback := len(mf.Packages.Fallback) > 0

	if len(pkgs) == 0 && !hasFallback {
		return "n/a"
	}
	if !e.ManagerOK || len(pkgs) == 0 {
		return "n/a"
	}
	for _, p := range pkgs {
		installed, err := e.Manager.IsInstalled(p)
		if err != nil || !installed {
			return "missing"
		}
	}
	return "ok"
}
