package engine

import (
	"fmt"
	"os"

	"github.com/JtheGunner/omnishell/internal/atomicfile"
	"github.com/JtheGunner/omnishell/internal/backup"
	"github.com/JtheGunner/omnishell/internal/lockfile"
	"github.com/JtheGunner/omnishell/internal/platform"
	"github.com/JtheGunner/omnishell/internal/rcfile"
)

// UninstallOptions are the flags of `omnishell uninstall`.
type UninstallOptions struct {
	PurgeConfigDir bool
	Yes            bool
}

// Uninstall tears down omnishell's shell integration: the marker block in every
// managed rc file and the generated init.<shell> files under ConfigDir. Each
// touched file is backed up first and, when rewritten, written atomically.
//
// It does NOT uninstall packages omnishell installed for modules — that is the
// job of `omnishell remove <id> --purge`.
//
// With opts.PurgeConfigDir the whole ConfigDir (config.toml, state.lock.json,
// modules/, vendor/, backups/) is deleted after the integration is removed. The
// backup session for that run is then written to a fresh temp dir OUTSIDE
// ConfigDir — and its path printed to Stdout — so the backup survives the
// RemoveAll. Without the flag ConfigDir is left intact apart from the init
// files, and the backup lives under <ConfigDir>/backups/ as usual.
func (e Engine) Uninstall(lockPath string, opts UninstallOptions) (Result, error) {
	_, lockExists, _ := lockfile.Load(lockPath)

	initPaths := make(map[string]string, len(platform.SupportedShells))
	anyInit := false
	for _, shell := range platform.SupportedShells {
		p := e.initPath(shell)
		initPaths[shell] = p
		if _, err := os.Stat(p); err == nil {
			anyInit = true
		}
	}

	// Step 1: nothing to uninstall — no lock and no init files on disk.
	if !lockExists && !anyInit {
		return Result{
			Modules: []ModuleResult{{
				ID:     "uninstall",
				Status: "unchanged",
				Note:   "nothing to uninstall",
			}},
		}, nil
	}

	// Step 2: confirmation, unless --yes.
	if !opts.Yes && e.Prompt != nil {
		if !e.Prompt("Remove omnishell's shell integration?") {
			return Result{}, ErrAborted
		}
	}

	// Step 6: pick the backup-session directory. When purging, ConfigDir is
	// about to be removed, so the session must live elsewhere.
	sessionDir := e.Platform.ConfigDir
	if opts.PurgeConfigDir {
		tmp, err := os.MkdirTemp("", "omnishell-uninstall-*")
		if err != nil {
			return Result{}, fmt.Errorf("create uninstall backup dir: %w", err)
		}
		sessionDir = tmp
		fmt.Fprintf(e.Stdout, "backups saved to %s\n", tmp)
	}

	bk, err := backup.NewSession(sessionDir, e.now())
	if err != nil {
		return Result{}, fmt.Errorf("create backup session: %w", err)
	}

	res := Result{BackupDir: bk.Dir}

	// Step 4: strip the marker block from every managed rc file that has one.
	for _, shell := range platform.SupportedShells {
		rcPath := e.rcPath(shell)
		if rcPath == "" {
			continue
		}
		data, rerr := os.ReadFile(rcPath)
		if rerr != nil {
			continue
		}
		content := string(data)
		if !rcfile.BlockPresent(content) {
			continue
		}
		if _, err := bk.Save(rcPath); err != nil {
			return res, err
		}
		updated, _ := rcfile.RemoveBlock(content)
		if err := atomicfile.WriteFile(rcPath, []byte(updated), 0o644); err != nil {
			return res, fmt.Errorf("write rc file %s: %w", rcPath, err)
		}
		res.Modules = append(res.Modules, ModuleResult{ID: "rc:" + shell, Status: "removed"})
		res.Changed = true
	}

	// Step 5: delete the generated init files (backed up first).
	for _, shell := range platform.SupportedShells {
		initPath := initPaths[shell]
		if _, serr := os.Stat(initPath); serr != nil {
			continue
		}
		if _, err := bk.Save(initPath); err != nil {
			return res, err
		}
		if err := os.Remove(initPath); err != nil {
			return res, fmt.Errorf("remove init file %s: %w", initPath, err)
		}
		res.Changed = true
	}

	// Step 7: optionally purge the entire config dir.
	if opts.PurgeConfigDir {
		if err := os.RemoveAll(e.Platform.ConfigDir); err != nil {
			return res, fmt.Errorf("purge config dir %s: %w", e.Platform.ConfigDir, err)
		}
		res.Changed = true
	}

	return res, nil
}
