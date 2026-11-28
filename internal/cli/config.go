package cli

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/JtheGunner/omnishell/internal/atomicfile"
	"github.com/JtheGunner/omnishell/internal/backup"
	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/JtheGunner/omnishell/internal/configsync"
	"github.com/JtheGunner/omnishell/internal/module"
	"github.com/spf13/cobra"
)

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Maintain config.toml",
	}
	cmd.AddCommand(newConfigSyncCmd())
	return cmd
}

func newConfigSyncCmd() *cobra.Command {
	var write bool
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Bring config.toml up to date with the installed modules",
		Long: `Compare config.toml with the installed modules after an upgrade.

Shows the options a module gained (with default and description), and the
options and modules that no longer exist. Without --write nothing is changed.

With --write, new options are added as commented-out lines holding their
default (so the behaviour of your config does not change), and options and
module tables that no longer exist are removed. Your comments, values and the
order of the file are kept. The file is backed up first; 'omnishell rollback'
restores it. Only modules that already have a [modules.<id>] table are
looked at: a module you never configured is not added.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runConfigSync(cmd, write)
		},
	}
	cmd.Flags().BoolVar(&write, "write", false, "write the changes (after taking a backup) instead of only showing them")
	return cmd
}

func runConfigSync(cmd *cobra.Command, write bool) error {
	out := cmd.OutOrStdout()

	e, cfgPath, _, err := buildQueryEngine(out, cmd.ErrOrStderr())
	if err != nil {
		return err
	}

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return hintIfUninitialised(cmd, fmt.Errorf("%w: %s", config.ErrNotFound, cfgPath))
		}
		return config.Error{Path: cfgPath, Msg: err.Error()}
	}

	lookup := func(id string) (map[string]module.OptionSchema, bool) {
		m, ok := e.Registry.Get(id)
		if !ok {
			return nil, false
		}
		return m.Manifest.Options, true
	}
	broken := brokenModuleIDs(e.Registry.Malformed())
	skip := func(id string) bool { return broken[id] }

	rep, err := configsync.Plan(cfgPath, data, lookup, skip)
	if err != nil {
		return err
	}

	if len(rep.Findings) == 0 {
		_, _ = fmt.Fprintln(out, "config.toml is up to date")
		printUnconfigured(out, e.Registry, cfgPath)
		return nil
	}

	_, _ = fmt.Fprintln(out, "config.toml differs from the installed modules:")
	printSyncFindings(out, rep.Findings)

	if !write {
		_, _ = fmt.Fprintln(out, "\nRun 'omnishell config sync --write' to apply these changes (a backup is taken first).")
		printUnconfigured(out, e.Registry, cfgPath)
		return nil
	}

	updated, open, err := configsync.Apply(cfgPath, data, lookup, skip)
	if err != nil {
		return err
	}

	sess, err := backup.NewSession(e.Platform.ConfigDir, e.Now())
	if err != nil {
		return err
	}
	if _, err := sess.Save(cfgPath); err != nil {
		return err
	}
	if err := sess.WriteManifest("config-sync", e.Now()); err != nil {
		return err
	}

	perm := os.FileMode(0o644)
	if fi, serr := os.Stat(cfgPath); serr == nil {
		perm = fi.Mode().Perm()
	}
	if err := atomicfile.WriteFile(cfgPath, updated, perm); err != nil {
		return fmt.Errorf("write %s: %w", cfgPath, err)
	}

	_, _ = fmt.Fprintf(out, "\nupdated %s (backup: %s)\n", cfgPath, sess.Dir)
	if len(open) > 0 {
		_, _ = fmt.Fprintln(out, "\nNot edited automatically (inline tables, dotted keys or multi-line values); change these by hand:")
		printSyncFindings(out, open)
	}
	printUnconfigured(out, e.Registry, cfgPath)
	return nil
}

func printSyncFindings(out io.Writer, findings []configsync.Finding) {
	for _, f := range findings {
		switch f.Kind {
		case configsync.MissingOption:
			line := fmt.Sprintf("  + %s.%s = %s", f.Module, f.Option, f.Default)
			if f.Help != "" {
				line += "   # " + f.Help
			}
			_, _ = fmt.Fprintln(out, line)
		case configsync.OrphanOption:
			_, _ = fmt.Fprintf(out, "  - %s.%s   (module %s has no such option)\n", f.Module, f.Option, f.Module)
		case configsync.OrphanModule:
			_, _ = fmt.Fprintf(out, "  - [modules.%s]   (no installed module has this id)\n", f.Module)
		}
	}
}

// printUnconfigured points at modules config.toml has no table for. They are
// not added to the file: without a record of what the file has already seen,
// "new" cannot be told from "never wanted".
func printUnconfigured(out io.Writer, reg module.Registry, cfgPath string) {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return
	}
	n := 0
	for _, m := range reg.All() {
		if _, ok := cfg.Modules[m.Manifest.Module.ID]; !ok {
			n++
		}
	}
	if n > 0 {
		_, _ = fmt.Fprintf(out, "\n%d installed module(s) have no table in config.toml; see 'omnishell list'.\n", n)
	}
}

// brokenModuleIDs turns Registry.Malformed entries ("<dir>/<id>: <reason>")
// into the set of ids whose user module could not be loaded, so config sync
// does not mistake them for removed modules.
func brokenModuleIDs(malformed []string) map[string]bool {
	ids := map[string]bool{}
	for _, m := range malformed {
		path, _, _ := strings.Cut(m, ": ")
		ids[filepath.Base(path)] = true
	}
	return ids
}
