// Package engine: Rollback restores files, rc marker blocks, and the
// lockfile to their state before a chosen prior apply/remove/uninstall run
// (or a chain of them), using the manifests backup.Session now records.
package engine

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/JtheGunner/omnishell/internal/atomicfile"
	"github.com/JtheGunner/omnishell/internal/backup"
)

// SnapshotInfo describes one backup session found under
// <ConfigDir>/backups/, for `omnishell rollback` (no --to) to list.
type SnapshotInfo struct {
	Timestamp string // the directory name, e.g. "20260904T182318Z"
	Kind      string
	FileCount int
}

// ListSnapshots returns every backup session under ConfigDir/backups with a
// readable manifest, newest first. A session with a missing or corrupt
// manifest.json is skipped, not reported as an error — it predates this
// feature, or its run failed before a manifest could be written.
func (e Engine) ListSnapshots() ([]SnapshotInfo, error) {
	dir := filepath.Join(e.Platform.ConfigDir, "backups")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []SnapshotInfo
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		m, merr := backup.ReadManifest(filepath.Join(dir, entry.Name()))
		if merr != nil {
			continue
		}
		out = append(out, SnapshotInfo{Timestamp: entry.Name(), Kind: m.Kind, FileCount: len(m.Files)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Timestamp > out[j].Timestamp })
	return out, nil
}

// ErrNoSuchSnapshot is returned when a rollback target does not match any
// recorded backup session.
var ErrNoSuchSnapshot = errors.New("no such backup snapshot")

// restorePlanEntry is one path a rollback will change, carrying the entry
// from whichever session owns the version that should be restored.
type restorePlanEntry struct {
	backup.FileEntry
	SessionDir string
}

// buildRestorePlan selects every backup session with Timestamp >= target
// (target through the newest), walks them oldest-first, and keeps only the
// first (oldest) entry seen per path: that snapshot captured the path's
// state immediately before the earliest rolled-back run that touched it,
// which is the correct restore target regardless of what later runs did to
// the same path. Sessions older than target are never consulted.
func buildRestorePlan(backupsDir string, snapshots []SnapshotInfo, target string) ([]restorePlanEntry, error) {
	found := false
	for _, s := range snapshots {
		if s.Timestamp == target {
			found = true
			break
		}
	}
	if !found {
		return nil, ErrNoSuchSnapshot
	}

	var selected []SnapshotInfo
	for _, s := range snapshots {
		if s.Timestamp >= target {
			selected = append(selected, s)
		}
	}
	sort.Slice(selected, func(i, j int) bool { return selected[i].Timestamp < selected[j].Timestamp })

	seen := map[string]bool{}
	var plan []restorePlanEntry
	for _, s := range selected {
		sessionDir := filepath.Join(backupsDir, s.Timestamp)
		m, err := backup.ReadManifest(sessionDir)
		if err != nil {
			continue // already validated readable by ListSnapshots; defensive only
		}
		for _, f := range m.Files {
			if seen[f.OriginalPath] {
				continue
			}
			seen[f.OriginalPath] = true
			plan = append(plan, restorePlanEntry{FileEntry: f, SessionDir: sessionDir})
		}
	}
	return plan, nil
}

// RollbackOptions are the flags of `omnishell rollback`.
type RollbackOptions struct {
	DryRun, Yes bool
}

// RollbackResult is the outcome of Rollback.
type RollbackResult struct {
	RestoredTo    string
	FilesRestored []string
	FilesRemoved  []string
	BackupDir     string // the new pre-rollback safety snapshot; empty on a dry run
}

// Rollback restores every path changed by target's run and every run after
// it back to its state immediately before target, using the chain-of-
// manifests restore plan from buildRestorePlan. Packages are never touched.
// Before writing anything, it takes its own backup of the current content of
// every path about to change, so a rollback is itself reversible via a later
// Rollback to that new snapshot.
func (e Engine) Rollback(target string, opts RollbackOptions) (RollbackResult, error) {
	backupsDir := filepath.Join(e.Platform.ConfigDir, "backups")
	snapshots, err := e.ListSnapshots()
	if err != nil {
		return RollbackResult{}, err
	}
	plan, err := buildRestorePlan(backupsDir, snapshots, target)
	if err != nil {
		return RollbackResult{}, err
	}

	res := RollbackResult{RestoredTo: target}
	for _, p := range plan {
		if p.ExistedBefore {
			res.FilesRestored = append(res.FilesRestored, p.OriginalPath)
		} else {
			res.FilesRemoved = append(res.FilesRemoved, p.OriginalPath)
		}
	}

	if opts.DryRun {
		return res, nil
	}

	if !opts.Yes && e.Prompt != nil {
		for _, f := range res.FilesRestored {
			_, _ = fmt.Fprintf(e.Stdout, "  restore  %s\n", f)
		}
		for _, f := range res.FilesRemoved {
			_, _ = fmt.Fprintf(e.Stdout, "  remove   %s\n", f)
		}
		if !e.Prompt(fmt.Sprintf("Roll back %d file(s) to %s?", len(plan), target)) {
			return res, ErrAborted
		}
	}

	bk, err := backup.NewSession(e.Platform.ConfigDir, e.now())
	if err != nil {
		return res, fmt.Errorf("create backup session: %w", err)
	}
	res.BackupDir = bk.Dir
	if err := os.MkdirAll(bk.Dir, 0o755); err != nil {
		return res, fmt.Errorf("create backup dir %s: %w", bk.Dir, err)
	}
	defer func() { _ = bk.WriteManifest("rollback", e.now()) }()

	for _, p := range plan {
		if _, err := bk.Save(p.OriginalPath); err != nil {
			return res, err
		}
	}

	for _, p := range plan {
		if p.ExistedBefore {
			data, rerr := os.ReadFile(filepath.Join(p.SessionDir, p.BackupName))
			if rerr != nil {
				return res, fmt.Errorf("read backed-up %s: %w", p.OriginalPath, rerr)
			}
			perm := os.FileMode(0o644)
			if fi, serr := os.Stat(p.OriginalPath); serr == nil {
				perm = fi.Mode().Perm()
			}
			if err := atomicfile.WriteFile(p.OriginalPath, data, perm); err != nil {
				return res, fmt.Errorf("restore %s: %w", p.OriginalPath, err)
			}
		} else {
			if err := os.Remove(p.OriginalPath); err != nil && !os.IsNotExist(err) {
				return res, fmt.Errorf("remove %s: %w", p.OriginalPath, err)
			}
		}
	}

	return res, nil
}
