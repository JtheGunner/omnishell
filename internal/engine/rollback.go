// Package engine: Rollback restores files, rc marker blocks, and the
// lockfile to their state before a chosen prior apply/remove/uninstall run
// (or a chain of them), using the manifests backup.Session now records.
package engine

import (
	"errors"
	"os"
	"path/filepath"
	"sort"

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
