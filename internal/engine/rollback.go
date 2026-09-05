// Package engine: Rollback restores files, rc marker blocks, and the
// lockfile to their state before a chosen prior apply/remove/uninstall run
// (or a chain of them), using the manifests backup.Session now records.
package engine

import (
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
