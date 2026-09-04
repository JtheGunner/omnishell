package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/JtheGunner/omnishell/internal/atomicfile"
	"github.com/JtheGunner/omnishell/internal/backup"
	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/JtheGunner/omnishell/internal/rcfile"
	"github.com/spf13/cobra"
)

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Create the omnishell config and hook it into your shells",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			e, cfgPath, _, err := buildEngine(out, cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			configDir := e.Platform.ConfigDir

			if err := ensureConfig(out, cfgPath); err != nil {
				return err
			}

			sess, err := backup.NewSession(configDir, e.Now())
			if err != nil {
				return err
			}

			for _, sh := range e.Platform.Shells {
				if !sh.Present {
					continue
				}
				if err := hookShell(out, sess, configDir, e.Platform.HomeDir, sh.Name, sh.RCPath); err != nil {
					return err
				}
			}
			return nil
		},
	}
}

// ensureConfig writes the default config.toml only when it is absent.
func ensureConfig(out io.Writer, cfgPath string) error {
	switch _, err := os.Stat(cfgPath); {
	case err == nil:
		_, _ = fmt.Fprintf(out, "config already exists: %s\n", cfgPath)
		return nil
	case os.IsNotExist(err):
		if werr := atomicfile.WriteFile(cfgPath, config.RenderDefault(), 0o644); werr != nil {
			return fmt.Errorf("write %s: %w", cfgPath, werr)
		}
		_, _ = fmt.Fprintf(out, "created %s\n", cfgPath)
		return nil
	default:
		return err
	}
}

// hookShell ensures the rc file carries the omnishell marker block sourcing the
// tool-managed init.<shell> file. It is idempotent: an rc file already carrying
// an up-to-date block is left untouched and not backed up. The init file itself
// is created by `apply`; the rc block guards its source with a `-f` test, so it
// no-ops safely until then.
func hookShell(out io.Writer, sess backup.Session, configDir, home, shell, rcPath string) error {
	initPath := filepath.Join(configDir, "init."+shell)
	sourceTarget := homeRelative(initPath, home)

	content := ""
	perm := os.FileMode(0o644)
	if fi, err := os.Stat(rcPath); err == nil {
		perm = fi.Mode().Perm()
	}
	if data, err := os.ReadFile(rcPath); err == nil {
		content = string(data)
	} else if !os.IsNotExist(err) {
		return err
	}

	updated, changed := rcfile.EnsureBlock(content, shell, sourceTarget)
	if !changed {
		_, _ = fmt.Fprintf(out, "%s already sources omnishell\n", rcPath)
		return nil
	}
	if _, err := sess.Save(rcPath); err != nil {
		return err
	}
	if err := atomicfile.WriteFile(rcPath, []byte(updated), perm); err != nil {
		return fmt.Errorf("write %s: %w", rcPath, err)
	}
	_, _ = fmt.Fprintf(out, "hooked %s -> %s\n", rcPath, sourceTarget)
	return nil
}
