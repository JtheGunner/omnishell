package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/JtheGunner/omnishell/internal/modedit"
	"github.com/spf13/cobra"
)

// envShell returns the base name of $SHELL; tests override it via
// SetEnvShellForTest.
var envShell = func() string { return shellBaseName(os.Getenv("SHELL")) }

// shellBaseName returns the base name of a $SHELL value, or "" when it is
// empty (filepath.Base would turn that into ".").
func shellBaseName(value string) string {
	if value == "" {
		return ""
	}
	return filepath.Base(value)
}

// SetEnvShellForTest pins the shell name the cheatsheet command reads from
// $SHELL and returns a function that restores the real lookup.
func SetEnvShellForTest(name string) (restore func()) {
	prev := envShell
	envShell = func() string { return name }
	return func() { envShell = prev }
}

func newCheatsheetCmd() *cobra.Command {
	var shell string
	cmd := &cobra.Command{
		Use:   "cheatsheet [module-id ...]",
		Short: "Show the commands and aliases your active modules provide",
		Long: `Show the commands, aliases and key bindings your active modules provide.

Each module may ship a cheatsheet.tmpl; this command renders them for one shell
with the options from config.toml. With module ids it shows only those modules
(each must be active); without, every active module. Nothing is written.

The shell is chosen in this order:
  1. --shell
  2. the shell named by $SHELL, when omnishell manages it
  3. the first managed shell (zsh, then bash)

Modules without a cheatsheet.tmpl are listed at the end. A module whose
cheatsheet fails to render is reported on stderr and the command exits 1.`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			errOut := cmd.ErrOrStderr()
			e, cfgPath, _, err := buildQueryEngine(out, errOut)
			if err != nil {
				return err
			}

			sheet, err := modedit.Editor{Engine: e, CfgPath: cfgPath}.Cheatsheet(modedit.CheatsheetOptions{
				IDs:      args,
				Shell:    shell,
				EnvShell: envShell(),
			})
			if err != nil {
				return err
			}

			if err := printCheatsheet(out, sheet); err != nil {
				return err
			}
			for _, f := range sheet.Failed {
				_, _ = fmt.Fprintf(errOut, "warning: module %s: %s\n",
					modedit.Clean(f.ID), modedit.Clean(f.Err.Error()))
			}
			if n := len(sheet.Failed); n > 0 {
				return fmt.Errorf("cheatsheet: %d module(s) failed to render", n)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&shell, "shell", "", "shell to render for (zsh or bash); default: $SHELL, else the first managed shell")
	return cmd
}

// printCheatsheet writes sheet as Markdown. Every piece of module-derived text
// goes through CleanLines so it cannot reach the terminal as a control sequence.
func printCheatsheet(w io.Writer, sheet modedit.Cheatsheet) error {
	var b strings.Builder
	if len(sheet.Entries) == 0 && len(sheet.NoCheatsheet) == 0 && len(sheet.Failed) == 0 {
		_, err := fmt.Fprintf(w, "No active modules for %s.\n", sheet.Shell)
		return err
	}

	fmt.Fprintf(&b, "# omnishell cheatsheet (%s)\n", sheet.Shell)
	for _, entry := range sheet.Entries {
		fmt.Fprintf(&b, "\n## %s (%s)\n", modedit.CleanLines(entry.Name), modedit.CleanLines(entry.ID))
		if description := modedit.CleanLines(entry.Description); description != "" {
			fmt.Fprintf(&b, "%s\n", description)
		}
		if entry.PackagesMissing {
			b.WriteString("Packages missing — run 'omnishell apply'.\n")
		}
		b.WriteString("\n")
		body := modedit.CleanLines(entry.Body)
		b.WriteString(body)
		if !strings.HasSuffix(body, "\n") {
			b.WriteString("\n")
		}
	}
	if len(sheet.NoCheatsheet) > 0 {
		ids := make([]string, len(sheet.NoCheatsheet))
		for i, id := range sheet.NoCheatsheet {
			ids[i] = modedit.CleanLines(id)
		}
		fmt.Fprintf(&b, "\nNo cheatsheet: %s\n", strings.Join(ids, ", "))
	}
	_, err := io.WriteString(w, b.String())
	return err
}
