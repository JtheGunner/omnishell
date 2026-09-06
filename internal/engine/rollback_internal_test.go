package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/JtheGunner/omnishell/internal/backup"
)

// writeTestManifest writes a manifest.json for a fake session so
// buildRestorePlan can be tested without running real Apply/Uninstall
// cycles.
func writeTestManifest(t *testing.T, backupsDir, timestamp string, files []backup.FileEntry) {
	t.Helper()
	dir := filepath.Join(backupsDir, timestamp)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	m := backup.Manifest{Schema: backup.SchemaVersion, Kind: "apply", CreatedAt: timestamp, Files: files}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestBuildRestorePlanUnknownTargetReturnsErrNoSuchSnapshot(t *testing.T) {
	backupsDir := t.TempDir()
	writeTestManifest(t, backupsDir, "20260101T000000Z", []backup.FileEntry{
		{OriginalPath: "/x", ExistedBefore: false},
	})
	snapshots := []SnapshotInfo{{Timestamp: "20260101T000000Z", Kind: "apply", FileCount: 1}}

	_, err := buildRestorePlan(backupsDir, snapshots, "20269999T000000Z")
	if err != ErrNoSuchSnapshot {
		t.Fatalf("err = %v, want ErrNoSuchSnapshot", err)
	}
}

func TestBuildRestorePlanTakesOldestEntryPerPath(t *testing.T) {
	backupsDir := t.TempDir()
	// T1 (target): path /a first touched here — this is the entry that must win.
	writeTestManifest(t, backupsDir, "20260101T000000Z", []backup.FileEntry{
		{OriginalPath: "/a", ExistedBefore: false},
	})
	// T2 (after target): path /a touched again with a different pre-image,
	// and a second path /b touched for the first time.
	writeTestManifest(t, backupsDir, "20260102T000000Z", []backup.FileEntry{
		{OriginalPath: "/a", ExistedBefore: true, BackupName: "a-at-t2"},
		{OriginalPath: "/b", ExistedBefore: false},
	})
	snapshots := []SnapshotInfo{
		{Timestamp: "20260102T000000Z", Kind: "apply", FileCount: 2},
		{Timestamp: "20260101T000000Z", Kind: "apply", FileCount: 1},
	}

	plan, err := buildRestorePlan(backupsDir, snapshots, "20260101T000000Z")
	if err != nil {
		t.Fatalf("buildRestorePlan: %v", err)
	}
	if len(plan) != 2 {
		t.Fatalf("plan = %+v, want 2 entries", plan)
	}
	byPath := map[string]restorePlanEntry{}
	for _, p := range plan {
		byPath[p.OriginalPath] = p
	}
	if byPath["/a"].ExistedBefore {
		t.Fatalf("/a entry = %+v, want the T1 (ExistedBefore=false) version, not T2's", byPath["/a"])
	}
	if byPath["/b"].ExistedBefore {
		t.Fatalf("/b entry = %+v, want ExistedBefore=false (only touched at T2)", byPath["/b"])
	}
}

func TestBuildRestorePlanExcludesSnapshotsBeforeTarget(t *testing.T) {
	backupsDir := t.TempDir()
	// T0 (before target): must never appear in the plan.
	writeTestManifest(t, backupsDir, "20260101T000000Z", []backup.FileEntry{
		{OriginalPath: "/before-target", ExistedBefore: false},
	})
	// T1 (target):
	writeTestManifest(t, backupsDir, "20260102T000000Z", []backup.FileEntry{
		{OriginalPath: "/at-target", ExistedBefore: false},
	})
	snapshots := []SnapshotInfo{
		{Timestamp: "20260102T000000Z", Kind: "apply", FileCount: 1},
		{Timestamp: "20260101T000000Z", Kind: "apply", FileCount: 1},
	}

	plan, err := buildRestorePlan(backupsDir, snapshots, "20260102T000000Z")
	if err != nil {
		t.Fatalf("buildRestorePlan: %v", err)
	}
	if len(plan) != 1 || plan[0].OriginalPath != "/at-target" {
		t.Fatalf("plan = %+v, want exactly [/at-target]", plan)
	}
}
