package engine_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/JtheGunner/omnishell/internal/engine"
	"github.com/JtheGunner/omnishell/internal/lockfile"
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

func TestRollbackSingleRunRestoresPriorState(t *testing.T) {
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
	res1, err := e.Apply(cfg, cfgPath, lockPath, engine.ApplyOptions{Yes: true})
	if err != nil {
		t.Fatalf("Apply: %v\n%s", err, out.String())
	}
	target := filepath.Base(res1.BackupDir)

	initBash := filepath.Join(home, ".config", "omnishell", "init.bash")
	rcBash := filepath.Join(home, ".bashrc")
	if _, err := os.Stat(initBash); err != nil {
		t.Fatalf("init.bash missing after apply: %v", err)
	}
	if _, err := os.Stat(rcBash); err != nil {
		t.Fatalf(".bashrc missing after apply: %v", err)
	}

	rres, err := e.Rollback(target, engine.RollbackOptions{Yes: true})
	if err != nil {
		t.Fatalf("Rollback: %v\n%s", err, out.String())
	}

	if _, err := os.Stat(initBash); !os.IsNotExist(err) {
		t.Fatalf("init.bash still exists after rollback: err=%v", err)
	}
	if _, err := os.Stat(rcBash); !os.IsNotExist(err) {
		t.Fatalf(".bashrc still exists after rollback: err=%v", err)
	}
	if _, exists, _ := lockfile.Load(lockPath); exists {
		t.Fatal("lockfile still exists after rollback")
	}
	if len(rres.FilesRemoved) != 3 {
		t.Fatalf("FilesRemoved = %v, want 3 (init.bash, .bashrc, state.lock.json)", rres.FilesRemoved)
	}
}

func TestRollbackChainedAcrossTwoRuns(t *testing.T) {
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
	cfg1, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	res1, err := e.Apply(cfg1, cfgPath, lockPath, engine.ApplyOptions{Yes: true})
	if err != nil {
		t.Fatalf("Apply 1: %v\n%s", err, out.String())
	}
	target := filepath.Base(res1.BackupDir)

	writeConfig(t, cfgPath, `
[omnishell]
version = 1
shells = ["bash"]
[modules.completion]
enabled = true
[modules.fzf]
enabled = true
[modules.fzf.options]
ctrl_r = true
`)
	cfg2, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Apply(cfg2, cfgPath, lockPath, engine.ApplyOptions{Yes: true}); err != nil {
		t.Fatalf("Apply 2: %v\n%s", err, out.String())
	}

	initBash := filepath.Join(home, ".config", "omnishell", "init.bash")
	body, err := os.ReadFile(initBash)
	if err != nil || !strings.Contains(string(body), "omnishell:fzf") {
		t.Fatalf("init.bash missing fzf section before rollback:\n%s", body)
	}

	if _, err := e.Rollback(target, engine.RollbackOptions{Yes: true}); err != nil {
		t.Fatalf("Rollback: %v\n%s", err, out.String())
	}

	if _, err := os.Stat(initBash); !os.IsNotExist(err) {
		t.Fatalf("init.bash still exists after chained rollback: err=%v", err)
	}
	if _, exists, _ := lockfile.Load(lockPath); exists {
		t.Fatal("lockfile still exists after chained rollback")
	}
}

func TestRollbackOnlyReplaysSessionsFromTargetOnward(t *testing.T) {
	home := t.TempDir()
	var out bytes.Buffer
	mgr := &pkgmgr.MockManager{NameV: "apt", DetectV: true, Installed: map[string]bool{}}
	clock := advancingClock(time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC))
	e := rollbackEngine(t, home, mgr, &out, clock)

	cfgPath := filepath.Join(home, ".config", "omnishell", "config.toml")
	lockPath := filepath.Join(home, ".config", "omnishell", "state.lock.json")

	// apply0 (baseline, BEFORE the rollback range): completion only.
	writeConfig(t, cfgPath, `
[omnishell]
version = 1
shells = ["bash"]
[modules.completion]
enabled = true
`)
	cfg0, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Apply(cfg0, cfgPath, lockPath, engine.ApplyOptions{Yes: true}); err != nil {
		t.Fatalf("Apply 0: %v\n%s", err, out.String())
	}

	// apply1 (the rollback target): add fzf.
	writeConfig(t, cfgPath, `
[omnishell]
version = 1
shells = ["bash"]
[modules.completion]
enabled = true
[modules.fzf]
enabled = true
[modules.fzf.options]
ctrl_r = true
`)
	cfg1, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	res1, err := e.Apply(cfg1, cfgPath, lockPath, engine.ApplyOptions{Yes: true})
	if err != nil {
		t.Fatalf("Apply 1: %v\n%s", err, out.String())
	}
	target := filepath.Base(res1.BackupDir)

	// apply2 (after the target): change an fzf option.
	writeConfig(t, cfgPath, `
[omnishell]
version = 1
shells = ["bash"]
[modules.completion]
enabled = true
[modules.fzf]
enabled = true
[modules.fzf.options]
ctrl_r = true
ctrl_t = true
`)
	cfg2, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Apply(cfg2, cfgPath, lockPath, engine.ApplyOptions{Yes: true}); err != nil {
		t.Fatalf("Apply 2: %v\n%s", err, out.String())
	}

	if _, err := e.Rollback(target, engine.RollbackOptions{Yes: true}); err != nil {
		t.Fatalf("Rollback: %v\n%s", err, out.String())
	}

	initBash := filepath.Join(home, ".config", "omnishell", "init.bash")
	body, err := os.ReadFile(initBash)
	if err != nil {
		t.Fatalf("init.bash missing after rollback: %v", err)
	}
	if !strings.Contains(string(body), "omnishell:completion") {
		t.Fatalf("init.bash should still have completion (from apply0, before target):\n%s", body)
	}
	if strings.Contains(string(body), "omnishell:fzf") {
		t.Fatalf("init.bash should NOT have fzf — target's own run must be undone too, landing on apply0's state:\n%s", body)
	}
}

func TestRollbackIsItselfReversible(t *testing.T) {
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
	res1, err := e.Apply(cfg, cfgPath, lockPath, engine.ApplyOptions{Yes: true})
	if err != nil {
		t.Fatalf("Apply: %v\n%s", err, out.String())
	}
	target1 := filepath.Base(res1.BackupDir)

	rres1, err := e.Rollback(target1, engine.RollbackOptions{Yes: true})
	if err != nil {
		t.Fatalf("Rollback 1: %v\n%s", err, out.String())
	}
	target2 := filepath.Base(rres1.BackupDir)

	initBash := filepath.Join(home, ".config", "omnishell", "init.bash")
	if _, err := os.Stat(initBash); !os.IsNotExist(err) {
		t.Fatalf("init.bash should be gone after first rollback: err=%v", err)
	}

	if _, err := e.Rollback(target2, engine.RollbackOptions{Yes: true}); err != nil {
		t.Fatalf("Rollback 2: %v\n%s", err, out.String())
	}

	body, err := os.ReadFile(initBash)
	if err != nil || !strings.Contains(string(body), "omnishell:completion") {
		t.Fatalf("init.bash not restored by second rollback: err=%v\n%s", err, body)
	}
}

func TestRollbackDryRunMakesNoChanges(t *testing.T) {
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
	res1, err := e.Apply(cfg, cfgPath, lockPath, engine.ApplyOptions{Yes: true})
	if err != nil {
		t.Fatalf("Apply: %v\n%s", err, out.String())
	}
	target := filepath.Base(res1.BackupDir)

	initBash := filepath.Join(home, ".config", "omnishell", "init.bash")
	before, err := os.ReadFile(initBash)
	if err != nil {
		t.Fatal(err)
	}

	rres, err := e.Rollback(target, engine.RollbackOptions{DryRun: true})
	if err != nil {
		t.Fatalf("Rollback dry-run: %v\n%s", err, out.String())
	}
	if rres.BackupDir != "" {
		t.Fatalf("dry run created a safety backup: %s", rres.BackupDir)
	}
	if len(rres.FilesRemoved) == 0 {
		t.Fatal("dry run plan reported no files, want the init/rc/lock removal plan")
	}

	after, err := os.ReadFile(initBash)
	if err != nil || string(before) != string(after) {
		t.Fatalf("dry run modified init.bash: err=%v", err)
	}
}

func TestRollbackUnknownTargetReturnsErrNoSuchSnapshot(t *testing.T) {
	home := t.TempDir()
	e := rollbackEngine(t, home, &pkgmgr.MockManager{NameV: "apt", DetectV: true}, &bytes.Buffer{}, advancingClock(time.Now()))
	_, err := e.Rollback("20200101T000000Z", engine.RollbackOptions{Yes: true})
	if err != engine.ErrNoSuchSnapshot {
		t.Fatalf("err = %v, want ErrNoSuchSnapshot", err)
	}
}
