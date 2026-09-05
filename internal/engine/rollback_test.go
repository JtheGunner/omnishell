package engine_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/JtheGunner/omnishell/internal/engine"
	"github.com/JtheGunner/omnishell/internal/pkgmgr"
)

func TestListSnapshotsSkipsMissingOrCorruptManifest(t *testing.T) {
	home := t.TempDir()
	var out bytes.Buffer
	mgr := &pkgmgr.MockManager{NameV: "apt", DetectV: true, Installed: map[string]bool{}}
	clock := advancingClock(time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC))
	e := rollbackEngine(t, home, mgr, &out, clock)

	cfgPath := filepath.Join(home, ".config", "omnishell", "config.toml")
	lockPath := filepath.Join(home, ".config", "omnishell", "state.lock.json")
	writeConfig(t, cfgPath, `
[omnishell]
version = 1
shells = ["bash"]
[modules.completion]
enabled = true
`)
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	res, err := e.Apply(cfg, cfgPath, lockPath, engine.ApplyOptions{Yes: true})
	if err != nil {
		t.Fatalf("Apply: %v\n%s", err, out.String())
	}
	validTS := filepath.Base(res.BackupDir)

	backupsDir := filepath.Join(home, ".config", "omnishell", "backups")
	corruptDir := filepath.Join(backupsDir, "20200101T000000Z")
	if err := os.MkdirAll(corruptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(corruptDir, "manifest.json"), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	noManifestDir := filepath.Join(backupsDir, "20200102T000000Z")
	if err := os.MkdirAll(noManifestDir, 0o755); err != nil {
		t.Fatal(err)
	}

	snapshots, err := e.ListSnapshots()
	if err != nil {
		t.Fatalf("ListSnapshots: %v", err)
	}
	if len(snapshots) != 1 || snapshots[0].Timestamp != validTS {
		t.Fatalf("snapshots = %+v, want exactly [%s]", snapshots, validTS)
	}
	if snapshots[0].Kind != "apply" || snapshots[0].FileCount == 0 {
		t.Fatalf("snapshot info wrong: %+v", snapshots[0])
	}
}

func TestListSnapshotsEmptyWhenNoBackupsDir(t *testing.T) {
	home := t.TempDir()
	e := rollbackEngine(t, home, &pkgmgr.MockManager{NameV: "apt", DetectV: true}, &bytes.Buffer{}, advancingClock(time.Now()))
	snapshots, err := e.ListSnapshots()
	if err != nil {
		t.Fatalf("ListSnapshots: %v", err)
	}
	if len(snapshots) != 0 {
		t.Fatalf("snapshots = %+v, want none", snapshots)
	}
}
