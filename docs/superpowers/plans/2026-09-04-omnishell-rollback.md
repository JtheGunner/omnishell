# omnishell rollback Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `omnishell rollback`, a command that restores files, rc marker
blocks, and the lockfile to their state immediately before a chosen prior
`apply`/`remove`/`uninstall` run (or a chain of them), without touching
packages.

**Architecture:** Backup sessions (`internal/backup`) gain a `manifest.json`
recording every file each session touched, with enough metadata (original
path, whether it existed before, its backup filename) to invert the change.
`Apply` is fixed to also back up `state.lock.json` before overwriting it — a
gap that exists today. A new `Engine.Rollback` reads manifests newest-first,
builds a restore plan by walking the target-through-newest range oldest-first
(so the oldest/target entry wins per path), and replays it — first taking its
own safety backup of what it's about to overwrite, so a rollback is itself
reversible.

**Tech Stack:** Go 1.23, Cobra CLI, `encoding/json`, existing
`internal/atomicfile` for atomic writes. No new external dependencies.

**Spec:** `docs/superpowers/specs/2026-09-04-omnishell-rollback-design.md`

## Global Constraints

- Go 1.23, no new third-party dependencies.
- Every disk write to a path rollback restores must go through
  `internal/atomicfile.WriteFile` (temp file + rename) — never a direct
  `os.WriteFile`, matching the rest of the codebase.
- Rollback never uninstalls or installs packages, and never touches
  `config.toml` — restores files/lockfile only (per the approved design).
- `uninstall --purge` backup sessions (which live outside `ConfigDir` in a
  temp directory) are out of scope for `rollback` — it only scans
  `<ConfigDir>/backups/`.
- `go build ./...`, `go vet ./...`, and `go test ./... -race -count=1` must
  pass after every task.
- Every mutating engine method (`Save`, `WriteManifest`, `Rollback`) must
  tolerate being interrupted partway through without corrupting the on-disk
  state worse than it already is — this codebase's existing convention
  (see `internal/atomicfile`, the hand-edit guard in `Apply`).

---

### Task 1: `internal/backup` — manifest support and a stateful `Session`

**Files:**
- Modify: `internal/backup/backup.go`
- Modify: `internal/backup/backup_test.go`
- Modify: `internal/engine/apply_helpers.go:420` (`ensureRC`'s `bk` parameter
  type)

**Interfaces:**
- Produces: `backup.FileEntry{OriginalPath, ExistedBefore, BackupName}`,
  `backup.Manifest{Schema, Kind, CreatedAt, OmnishellVersion, Files}`,
  `backup.NewSession(configDir string, now time.Time) (*Session, error)`,
  `(*Session).Save(path string) (string, error)` (signature unchanged, now
  pointer receiver and always records an entry),
  `(*Session).WriteManifest(kind string, now time.Time) error`,
  `backup.ReadManifest(sessionDir string) (Manifest, error)`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/backup/backup_test.go` (keep the two existing tests
unchanged):

```go
func TestSaveRecordsEntryForMissingFile(t *testing.T) {
	s, err := backup.NewSession(t.TempDir(), fixedTime())
	if err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	if _, err := s.Save(missing); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := s.WriteManifest("apply", fixedTime()); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}
	m, err := backup.ReadManifest(s.Dir)
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	if len(m.Files) != 1 {
		t.Fatalf("Files = %+v, want 1 entry", m.Files)
	}
	f := m.Files[0]
	if f.OriginalPath != missing || f.ExistedBefore || f.BackupName != "" {
		t.Fatalf("entry = %+v, want {OriginalPath:%q ExistedBefore:false BackupName:\"\"}", f, missing)
	}
}

func TestSaveRecordsEntryForExistingFile(t *testing.T) {
	cfgDir := t.TempDir()
	src := filepath.Join(t.TempDir(), ".zshrc")
	if err := os.WriteFile(src, []byte("rc contents"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := backup.NewSession(cfgDir, fixedTime())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save(src); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := s.WriteManifest("apply", fixedTime()); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}
	m, err := backup.ReadManifest(s.Dir)
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	if len(m.Files) != 1 {
		t.Fatalf("Files = %+v, want 1 entry", m.Files)
	}
	f := m.Files[0]
	if f.OriginalPath != src || !f.ExistedBefore || f.BackupName != ".zshrc" {
		t.Fatalf("entry = %+v, want {OriginalPath:%q ExistedBefore:true BackupName:\".zshrc\"}", f, src)
	}
	if m.Kind != "apply" || m.Schema != backup.SchemaVersion {
		t.Fatalf("manifest header wrong: %+v", m)
	}
}

func TestSaveAccumulatesAcrossMultipleCalls(t *testing.T) {
	cfgDir := t.TempDir()
	srcDir := t.TempDir()
	a := filepath.Join(srcDir, "a")
	b := filepath.Join(srcDir, "b")
	if err := os.WriteFile(a, []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := backup.NewSession(cfgDir, fixedTime())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save(a); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save(b); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteManifest("uninstall", fixedTime()); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}
	m, err := backup.ReadManifest(s.Dir)
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	if len(m.Files) != 2 {
		t.Fatalf("Files = %+v, want 2 entries (accumulation bug if only 1)", m.Files)
	}
}

func TestReadManifestMissingReturnsError(t *testing.T) {
	if _, err := backup.ReadManifest(t.TempDir()); err == nil {
		t.Fatal("ReadManifest on a dir with no manifest.json: want error, got nil")
	}
}
```

- [ ] **Step 2: Run the new tests to verify they fail**

Run: `go test ./internal/backup/... -v`
Expected: FAIL — `Save`/`WriteManifest`/`ReadManifest`/`SchemaVersion`
undefined, and `NewSession` still returns a value `Session` not `*Session`.

- [ ] **Step 3: Rewrite `internal/backup/backup.go`**

```go
// Package backup copies files into a per-run timestamped directory under
// <configDir>/backups/ before omnishell overwrites them, and records what it
// copied in a manifest.json so a later rollback can invert the change.
package backup

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/JtheGunner/omnishell/internal/atomicfile"
	"github.com/JtheGunner/omnishell/internal/buildinfo"
)

// SchemaVersion is the manifest schema this build writes and accepts.
const SchemaVersion = 1

// FileEntry records one file a session touched: where it lived, whether it
// existed before the session's run started, and (if it did) the name it was
// copied under inside the session directory.
type FileEntry struct {
	OriginalPath  string `json:"original_path"`
	ExistedBefore bool   `json:"existed_before"`
	BackupName    string `json:"backup_name,omitempty"`
}

// Manifest is the <session-dir>/manifest.json document: which run created
// the session and which files it captured.
type Manifest struct {
	Schema           int         `json:"schema"`
	Kind             string      `json:"kind"`
	CreatedAt        string      `json:"created_at"`
	OmnishellVersion string      `json:"omnishell_version"`
	Files            []FileEntry `json:"files"`
}

// Session is one backup directory for a single omnishell invocation.
type Session struct {
	Dir     string
	entries []FileEntry
}

// NewSession computes (but does not yet create) the backup directory
// <configDir>/backups/<RFC3339-basic-utc>/.
func NewSession(configDir string, now time.Time) (*Session, error) {
	stamp := now.UTC().Format("20060102T150405Z")
	return &Session{Dir: filepath.Join(configDir, "backups", stamp)}, nil
}

// Save copies path into the session directory, keeping its base name, and
// records the attempt as a FileEntry regardless of outcome. A missing source
// file is not an error: it is recorded with ExistedBefore=false, so a later
// rollback knows to delete rather than overwrite that path.
func (s *Session) Save(path string) (string, error) {
	in, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			s.entries = append(s.entries, FileEntry{OriginalPath: path, ExistedBefore: false})
			return "", nil
		}
		return "", fmt.Errorf("open %s for backup: %w", path, err)
	}
	defer func() { _ = in.Close() }() // read-only source; nothing to act on

	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return "", fmt.Errorf("create backup dir %s: %w", s.Dir, err)
	}
	base := filepath.Base(path)
	dst := filepath.Join(s.Dir, base)
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return "", fmt.Errorf("create backup file %s: %w", dst, err)
	}
	defer func() { _ = out.Close() }() // best-effort; the checked Close below covers the success path
	if _, err := io.Copy(out, in); err != nil {
		return "", fmt.Errorf("copy to backup %s: %w", dst, err)
	}
	if err := out.Close(); err != nil {
		return "", fmt.Errorf("close backup file %s: %w", dst, err)
	}
	s.entries = append(s.entries, FileEntry{OriginalPath: path, ExistedBefore: true, BackupName: base})
	return dst, nil
}

// WriteManifest writes <Dir>/manifest.json recording every file this session
// has captured via Save so far. Safe to call even after a partial run — it
// reflects whatever entries were recorded before the caller returned, so a
// run that fails partway through still leaves a manifest describing what it
// actually changed.
func (s *Session) WriteManifest(kind string, now time.Time) error {
	m := Manifest{
		Schema:           SchemaVersion,
		Kind:             kind,
		CreatedAt:        now.UTC().Format(time.RFC3339),
		OmnishellVersion: buildinfo.Version,
		Files:            s.entries,
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	return atomicfile.WriteFile(filepath.Join(s.Dir, "manifest.json"), append(data, '\n'), 0o644)
}

// ReadManifest reads and parses <sessionDir>/manifest.json.
func ReadManifest(sessionDir string) (Manifest, error) {
	data, err := os.ReadFile(filepath.Join(sessionDir, "manifest.json"))
	if err != nil {
		return Manifest{}, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return Manifest{}, fmt.Errorf("parse manifest %s: %w", sessionDir, err)
	}
	return m, nil
}
```

- [ ] **Step 4: Update `ensureRC`'s parameter type**

In `internal/engine/apply_helpers.go:420`, change:

```go
func (e Engine) ensureRC(shell, initPath string, bk backup.Session, lock *lockfile.Lock) error {
```

to:

```go
func (e Engine) ensureRC(shell, initPath string, bk *backup.Session, lock *lockfile.Lock) error {
```

(The body is unchanged — `bk.Save(rcPath)` works identically on a pointer.
This is required for correctness, not just to compile: a value-typed
parameter would let `Save`'s pointer-receiver mutate only the parameter's own
copy of the `entries` slice header, silently dropping entries the caller
never sees.)

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go build ./... && go test ./internal/backup/... ./internal/engine/... -v`
Expected: PASS. (The engine package must still compile and its existing
tests must still pass — `bk, err := backup.NewSession(...)` call sites are
unaffected since Go's `:=` adopts the new `*Session` return type
automatically.)

- [ ] **Step 6: Commit**

```bash
git add internal/backup/backup.go internal/backup/backup_test.go internal/engine/apply_helpers.go
git commit -m "feat(backup): record a manifest of every file a session touches"
```

---

### Task 2: Close the lockfile-backup gap and wire manifests into `Apply`/`Uninstall`

**Files:**
- Modify: `internal/engine/apply.go`
- Modify: `internal/engine/uninstall.go`
- Modify: `internal/engine/apply_test.go`

**Interfaces:**
- Consumes: `backup.NewSession` (`*Session`), `(*Session).Save`,
  `(*Session).WriteManifest`, `backup.ReadManifest` from Task 1.
- Produces: nothing new — this task makes existing `Apply`/`Uninstall`
  produce a `manifest.json` per session and back up `state.lock.json`,
  which Task 3+ read.

- [ ] **Step 1: Write the failing test**

Append to `internal/engine/apply_test.go` (add `"github.com/JtheGunner/omnishell/internal/backup"`
to its import block):

```go
func TestApplyWritesManifestAndBacksUpLockfile(t *testing.T) {
	home := t.TempDir()
	var out bytes.Buffer
	mgr := &pkgmgr.MockManager{NameV: "apt", DetectV: true, Installed: map[string]bool{}}
	e := applyEngine(t, home, mgr, &out)

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
	if res.BackupDir == "" {
		t.Fatal("res.BackupDir empty")
	}

	m, err := backup.ReadManifest(res.BackupDir)
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	if m.Kind != "apply" {
		t.Fatalf("manifest kind = %q, want %q", m.Kind, "apply")
	}
	foundLock := false
	for _, f := range m.Files {
		if f.OriginalPath == lockPath {
			foundLock = true
			if f.ExistedBefore {
				t.Fatalf("first-ever apply's lockfile entry has ExistedBefore=true, want false")
			}
		}
	}
	if !foundLock {
		t.Fatalf("manifest does not record %s: %+v", lockPath, m.Files)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/engine/... -run TestApplyWritesManifestAndBacksUpLockfile -v`
Expected: FAIL — `res.BackupDir` has no `manifest.json` yet (ReadManifest
returns an error), and the lockfile was never passed to `bk.Save`.

- [ ] **Step 3: Wire it up in `internal/engine/apply.go`**

Right after the existing backup-session setup (find this block — it is
unchanged above this point):

```go
	bk, err := backup.NewSession(e.Platform.ConfigDir, e.now())
	if err != nil {
		return res, fmt.Errorf("create backup session: %w", err)
	}
	res.BackupDir = bk.Dir
	if err := os.MkdirAll(bk.Dir, 0o755); err != nil {
		return res, fmt.Errorf("create backup dir %s: %w", bk.Dir, err)
	}
```

add immediately after it:

```go
	defer func() { _ = bk.WriteManifest("apply", e.now()) }()
```

Then, immediately before the existing:

```go
	if err := newLock.Write(lockPath); err != nil {
		return res, fmt.Errorf("write lockfile %s: %w", lockPath, err)
	}
```

add:

```go
	if _, err := bk.Save(lockPath); err != nil {
		return res, err
	}
```

so the full sequence reads `bk.Save(lockPath)` then `newLock.Write(lockPath)`.

- [ ] **Step 4: Wire it up in `internal/engine/uninstall.go`**

Right after the existing:

```go
	bk, err := backup.NewSession(sessionDir, e.now())
	if err != nil {
		return Result{}, fmt.Errorf("create backup session: %w", err)
	}

	res := Result{BackupDir: bk.Dir}
```

add:

```go
	defer func() { _ = bk.WriteManifest("uninstall", e.now()) }()
```

(`Uninstall` never rewrites `state.lock.json`, so no lockfile-save call is
needed here — only `Apply` does.)

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go build ./... && go test ./internal/engine/... -v`
Expected: PASS, including `TestApplyWritesManifestAndBacksUpLockfile` and
every pre-existing `apply`/`uninstall`/`remove`/`doctor` test.

- [ ] **Step 6: Commit**

```bash
git add internal/engine/apply.go internal/engine/uninstall.go internal/engine/apply_test.go
git commit -m "fix(engine): back up the lockfile before overwriting it; write a manifest per backup session"
```

---

### Task 3: `Engine.ListSnapshots`

**Files:**
- Create: `internal/engine/rollback.go`
- Create: `internal/engine/rollback_test.go` (package `engine_test`)
- Modify: `internal/engine/helpers_test.go`

**Interfaces:**
- Consumes: `backup.ReadManifest` (Task 1), `e.Platform.ConfigDir`
  (`platform.Info`, already on `Engine`).
- Produces: `type SnapshotInfo struct { Timestamp, Kind string; FileCount int }`,
  `func (e Engine) ListSnapshots() ([]SnapshotInfo, error)` — later tasks in
  this plan depend on exactly this name and shape.

- [ ] **Step 1: Add shared test helpers to `internal/engine/helpers_test.go`**

The existing `applyEngine` helper hard-codes a single fixed clock, which
collides on itself if reused across the multiple sequential engine calls
rollback tests need (two `Apply`s and a `Rollback` would all compute the same
session-directory timestamp). Add an engine builder that takes an explicit
clock, and a helper to advance one:

```go
func rollbackEngine(t *testing.T, home string, mgr *pkgmgr.MockManager, out *bytes.Buffer, now func() time.Time) engine.Engine {
	t.Helper()
	reg, err := module.LoadRegistry(nil, "testdata/modules")
	if err != nil {
		t.Fatal(err)
	}
	return engine.Engine{
		Platform: platform.Info{
			OS:        platform.Linux,
			HomeDir:   home,
			ConfigDir: filepath.Join(home, ".config", "omnishell"),
			Shells: []platform.ShellInfo{
				{Name: "zsh", RCPath: filepath.Join(home, ".zshrc"), Present: false},
				{Name: "bash", RCPath: filepath.Join(home, ".bashrc"), Present: true},
			},
		},
		Registry:  reg,
		Manager:   mgr,
		ManagerOK: true,
		Runner:    &pkgmgr.MockRunner{},
		Now:       now,
		Stdout:    out,
		Stderr:    out,
		Prompt:    func(string) bool { return true },
	}
}

// advancingClock returns a Now func that moves forward one minute on every
// call, so sequential engine operations in a test never collide on the same
// second-resolution backup-session timestamp.
func advancingClock(start time.Time) func() time.Time {
	t := start
	return func() time.Time {
		t = t.Add(time.Minute)
		return t
	}
}
```

Add `"bytes"`, `"path/filepath"`, `"time"`,
`"github.com/JtheGunner/omnishell/internal/platform"`,
`"github.com/JtheGunner/omnishell/internal/pkgmgr"` to this file's imports
(reuse whichever are already there; `bytes`/`pkgmgr` are new to this file).

- [ ] **Step 2: Write the failing test**

Create `internal/engine/rollback_test.go`:

```go
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
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/engine/... -run TestListSnapshots -v`
Expected: FAIL — `e.ListSnapshots` undefined.

- [ ] **Step 4: Create `internal/engine/rollback.go`**

```go
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
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go build ./... && go test ./internal/engine/... -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/engine/rollback.go internal/engine/rollback_test.go internal/engine/helpers_test.go
git commit -m "feat(engine): add ListSnapshots to enumerate backup sessions"
```

---

### Task 4: restore-plan selection logic

**Files:**
- Modify: `internal/engine/rollback.go`
- Create: `internal/engine/rollback_internal_test.go` (package `engine`,
  white-box — mirrors the existing `apply_helpers_internal_test.go`
  convention for testing unexported helpers)

**Interfaces:**
- Consumes: `SnapshotInfo` (Task 3), `backup.Manifest`/`backup.FileEntry`/
  `backup.ReadManifest` (Task 1).
- Produces: `var ErrNoSuchSnapshot = errors.New(...)`,
  `type restorePlanEntry struct { backup.FileEntry; SessionDir string }`,
  `func buildRestorePlan(backupsDir string, snapshots []SnapshotInfo, target string) ([]restorePlanEntry, error)`
  — Task 5's `Rollback` calls this directly by name.

- [ ] **Step 1: Write the failing test**

Create `internal/engine/rollback_internal_test.go`:

```go
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
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/engine/... -run TestBuildRestorePlan -v`
Expected: FAIL — `buildRestorePlan`/`ErrNoSuchSnapshot`/`restorePlanEntry`
undefined.

- [ ] **Step 3: Add to `internal/engine/rollback.go`**

Add `"errors"` to the import block, then append:

```go
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
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go build ./... && go test ./internal/engine/... -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/engine/rollback.go internal/engine/rollback_internal_test.go
git commit -m "feat(engine): build a restore plan from a chain of backup manifests"
```

---

### Task 5: `Engine.Rollback`

**Files:**
- Modify: `internal/engine/rollback.go`
- Modify: `internal/engine/rollback_test.go`

**Interfaces:**
- Consumes: `buildRestorePlan`, `ErrNoSuchSnapshot` (Task 4),
  `backup.NewSession`, `(*Session).Save`, `(*Session).WriteManifest`
  (Task 1), `ErrAborted` (already defined in `apply.go`).
- Produces: `type RollbackOptions struct { DryRun, Yes bool }`,
  `type RollbackResult struct { RestoredTo string; FilesRestored, FilesRemoved []string; BackupDir string }`,
  `func (e Engine) Rollback(target string, opts RollbackOptions) (RollbackResult, error)`
  — Task 7's CLI command depends on exactly this signature.

- [ ] **Step 1: Write the failing tests**

Append to `internal/engine/rollback_test.go` (add `"strings"` to its
imports):

```go
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
		t.Fatalf("init.bash should still have completion (from before target):\n%s", body)
	}
	if !strings.Contains(string(body), "omnishell:fzf") {
		t.Fatalf("init.bash should have fzf's pre-target state, not be wiped back to apply0:\n%s", body)
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
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/engine/... -run TestRollback -v`
Expected: FAIL — `e.Rollback`/`engine.RollbackOptions`/`engine.ErrNoSuchSnapshot`
undefined (or `ErrNoSuchSnapshot` unexported comparison, once Task 4's
version resolves — either way, `Rollback` itself doesn't exist yet).

- [ ] **Step 3: Add to `internal/engine/rollback.go`**

Add `"fmt"` to the import block if not already present (it's already
imported by `apply.go` in this same package but each file needs its own
import list) — check first; add only if missing. Also add
`"github.com/JtheGunner/omnishell/internal/atomicfile"`. Then append:

```go
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
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go build ./... && go test ./internal/engine/... -v`
Expected: PASS, all `TestRollback*` tests plus every pre-existing test.

- [ ] **Step 5: Commit**

```bash
git add internal/engine/rollback.go internal/engine/rollback_test.go
git commit -m "feat(engine): add Rollback to restore a chain of backup snapshots"
```

---

### Task 6: CLI exit code for `ErrNoSuchSnapshot`

**Files:**
- Modify: `internal/cli/exit.go`
- Modify: `internal/cli/exit_test.go`

**Interfaces:**
- Consumes: `engine.ErrNoSuchSnapshot` (Task 5).

- [ ] **Step 1: Write the failing test**

Append to `internal/cli/exit_test.go`'s `TestClassifyError` function body
(inside the existing `func TestClassifyError(t *testing.T) { ... }`, next to
the other `if got := ...` checks):

```go
	if got := cli.ClassifyError(engine.ErrNoSuchSnapshot); got != 2 {
		t.Fatalf("ClassifyError(ErrNoSuchSnapshot) = %d, want 2", got)
	}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/cli/... -run TestClassifyError -v`
Expected: FAIL — `ClassifyError(engine.ErrNoSuchSnapshot)` currently returns
1 (falls through to the default case).

- [ ] **Step 3: Update `internal/cli/exit.go`**

Change:

```go
	if errors.As(err, &engCfg) || errors.As(err, &cfgErr) || errors.Is(err, config.ErrNotFound) {
		return 2
	}
	if errors.Is(err, errDrift) {
		return 3
	}
```

to:

```go
	if errors.As(err, &engCfg) || errors.As(err, &cfgErr) || errors.Is(err, config.ErrNotFound) ||
		errors.Is(err, engine.ErrNoSuchSnapshot) {
		return 2
	}
	if errors.Is(err, errDrift) {
		return 3
	}
```

Also update the doc comment above `ClassifyError` to mention it:

```go
//	nil                                       -> 0
//	config.Error / engine.ConfigError / ErrNotFound / engine.ErrNoSuchSnapshot -> 2
//	errDrift                                  -> 3
//	engine.ErrDegraded / engine.ErrAborted    -> 1
//	anything else                             -> 1
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/cli/... -run TestClassifyError -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/exit.go internal/cli/exit_test.go
git commit -m "fix(cli): map ErrNoSuchSnapshot to exit 2"
```

---

### Task 7: `omnishell rollback` CLI command, wiring, and docs

**Files:**
- Create: `internal/cli/rollback.go`
- Create: `internal/cli/rollback_test.go`
- Modify: `internal/cli/root.go`
- Modify: `README.md`

**Interfaces:**
- Consumes: `e.ListSnapshots`, `e.Rollback`, `engine.RollbackOptions`,
  `engine.RollbackResult`, `engine.ErrAborted` (Tasks 3/5), `buildEngine`
  (`internal/cli/context.go`, already exists).

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/rollback_test.go`:

```go
package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JtheGunner/omnishell/internal/cli"
)

func TestRollbackListsSnapshotsWithoutTo(t *testing.T) {
	setupModuleCLITest(t)
	cli.SetLookPathForTest(zshPresentLookPath)

	var out, errb bytes.Buffer
	if code := cli.Execute([]string{"init"}, &out, &errb); code != 0 {
		t.Fatalf("init exit %d: %s", code, errb.String())
	}
	if code := cli.Execute([]string{"enable", "completion"}, &out, &errb); code != 0 {
		t.Fatalf("enable exit %d: %s", code, errb.String())
	}
	if code := cli.Execute([]string{"apply", "--yes"}, &out, &errb); code != 0 {
		t.Fatalf("apply exit %d: %s\n%s", code, errb.String(), out.String())
	}

	out.Reset()
	errb.Reset()
	if code := cli.Execute([]string{"rollback"}, &out, &errb); code != 0 {
		t.Fatalf("rollback list exit %d: %s", code, errb.String())
	}
	if !strings.Contains(out.String(), "apply") {
		t.Fatalf("rollback listing missing a snapshot line:\n%s", out.String())
	}
}

func TestRollbackToRestoresPriorState(t *testing.T) {
	home, _ := setupModuleCLITest(t)
	configDir := filepath.Join(home, ".config", "omnishell")
	cli.SetLookPathForTest(zshPresentLookPath)

	var out, errb bytes.Buffer
	if code := cli.Execute([]string{"init"}, &out, &errb); code != 0 {
		t.Fatalf("init exit %d: %s", code, errb.String())
	}
	if code := cli.Execute([]string{"enable", "completion"}, &out, &errb); code != 0 {
		t.Fatalf("enable exit %d: %s", code, errb.String())
	}
	if code := cli.Execute([]string{"apply", "--yes"}, &out, &errb); code != 0 {
		t.Fatalf("apply exit %d: %s\n%s", code, errb.String(), out.String())
	}

	initFile := filepath.Join(configDir, "init.zsh")
	if _, err := os.Stat(initFile); err != nil {
		t.Fatalf("init.zsh missing before rollback: %v", err)
	}

	out.Reset()
	errb.Reset()
	if code := cli.Execute([]string{"rollback"}, &out, &errb); code != 0 {
		t.Fatalf("rollback list exit %d: %s", code, errb.String())
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) == 0 || lines[0] == "" {
		t.Fatalf("no snapshot listed:\n%s", out.String())
	}
	target := strings.Fields(lines[0])[0]

	out.Reset()
	errb.Reset()
	if code := cli.Execute([]string{"rollback", "--to", target, "--yes"}, &out, &errb); code != 0 {
		t.Fatalf("rollback exit %d: %s\n%s", code, errb.String(), out.String())
	}

	if _, err := os.Stat(initFile); !os.IsNotExist(err) {
		t.Fatalf("init.zsh still exists after rollback: err=%v", err)
	}
}

func TestRollbackUnknownTargetExitsNonZero(t *testing.T) {
	setupModuleCLITest(t)
	cli.SetLookPathForTest(zshPresentLookPath)

	var out, errb bytes.Buffer
	if code := cli.Execute([]string{"init"}, &out, &errb); code != 0 {
		t.Fatalf("init exit %d: %s", code, errb.String())
	}

	out.Reset()
	errb.Reset()
	code := cli.Execute([]string{"rollback", "--to", "20200101T000000Z", "--yes"}, &out, &errb)
	if code != 2 {
		t.Fatalf("rollback unknown target exit = %d, want 2 (stderr: %s)", code, errb.String())
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/cli/... -run TestRollback -v`
Expected: FAIL to compile — `rollback` is not a registered subcommand yet.

- [ ] **Step 3: Create `internal/cli/rollback.go`**

```go
package cli

import (
	"errors"
	"fmt"
	"io"

	"github.com/JtheGunner/omnishell/internal/engine"
	"github.com/spf13/cobra"
)

func newRollbackCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rollback",
		Short: "List backup snapshots, or restore files/lockfile to before one",
		Long: `Without --to, lists available backup snapshots (newest first).

With --to <timestamp>, restores every file changed since (and including) that
snapshot back to its state immediately before it, chaining through every
later apply/remove/uninstall run in reverse. Packages are never touched; use
'omnishell remove --purge' for that. config.toml is never touched either —
only the files apply/remove/uninstall write.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runRollback(cmd)
		},
	}
	cmd.Flags().String("to", "", "backup snapshot timestamp to roll back to")
	cmd.Flags().Bool("dry-run", false, "show what would change without changing anything")
	cmd.Flags().BoolP("yes", "y", false, "roll back without the confirmation prompt")
	return cmd
}

func runRollback(cmd *cobra.Command) error {
	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()

	e, _, _, err := buildEngine(out, errOut)
	if err != nil {
		return err
	}

	target, _ := cmd.Flags().GetString("to")
	if target == "" {
		snapshots, lerr := e.ListSnapshots()
		if lerr != nil {
			return lerr
		}
		if len(snapshots) == 0 {
			_, _ = fmt.Fprintln(out, "no backup snapshots found")
			return nil
		}
		for _, s := range snapshots {
			_, _ = fmt.Fprintf(out, "%s  %s  %d file(s)\n", s.Timestamp, s.Kind, s.FileCount)
		}
		return nil
	}

	dryRun, _ := cmd.Flags().GetBool("dry-run")
	yes, _ := cmd.Flags().GetBool("yes")
	res, err := e.Rollback(target, engine.RollbackOptions{DryRun: dryRun, Yes: yes})

	if errors.Is(err, engine.ErrAborted) {
		_, _ = fmt.Fprintln(errOut, "aborted")
		return err
	}

	printRollbackSummary(out, res)
	return err
}

func printRollbackSummary(out io.Writer, res engine.RollbackResult) {
	for _, f := range res.FilesRestored {
		_, _ = fmt.Fprintf(out, "  restored  %s\n", f)
	}
	for _, f := range res.FilesRemoved {
		_, _ = fmt.Fprintf(out, "  removed   %s\n", f)
	}
	if res.BackupDir != "" {
		_, _ = fmt.Fprintf(out, "backup: %s\n", res.BackupDir)
	}
}
```

- [ ] **Step 4: Register the command in `internal/cli/root.go`**

Change:

```go
	root.AddCommand(newRemoveCmd())
	root.AddCommand(newUninstallCmd())
	return root
```

to:

```go
	root.AddCommand(newRemoveCmd())
	root.AddCommand(newUninstallCmd())
	root.AddCommand(newRollbackCmd())
	return root
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go build ./... && go test ./internal/cli/... -v`
Expected: PASS.

- [ ] **Step 6: Update `README.md`**

In the Commands table, add a row after the `omnishell doctor` row:

```markdown
| `omnishell rollback` | List backup snapshots, or restore files/lockfile to their state before a chosen one (`--to <timestamp>`), undoing that run and everything after it. Never touches packages or `config.toml`. | `--to <timestamp>`, `--dry-run`, `-y` / `--yes` |
```

In "Not in v1", delete this line (it's no longer true):

```markdown
- `omnishell rollback` (backups are written; v1 restoration is manual)
```

In "Safety", change the last bullet from:

```markdown
- `omnishell uninstall` backs the integration out; restoration from a backup is
  manual in v1 (the path is printed).
```

to:

```markdown
- `omnishell rollback` restores files and the lockfile from a backup snapshot
  (see Commands). The one exception is an `uninstall --purge` backup: it's
  saved outside the config directory (since `--purge` deletes it) and
  restoring it is still manual — the path is printed when it runs.
```

- [ ] **Step 7: Commit**

```bash
git add internal/cli/rollback.go internal/cli/rollback_test.go internal/cli/root.go README.md
git commit -m "feat(cli): add omnishell rollback command"
```

---

### Task 8: E2E coverage

**Files:**
- Modify: `test/e2e/run.sh`

**Interfaces:**
- Consumes: the `rollback` subcommand (Task 7) and `doctor` (already
  exists).

- [ ] **Step 1: Locate the existing cycle**

Read `test/e2e/run.sh` and find the existing
init→enable→apply→doctor→remove→uninstall sequence (each step is a call to
the built `omnishell` binary followed by an assertion, following that
file's existing style — inspect it for the exact assertion helper it uses,
e.g. a `check` or `assert_exit_code` shell function, and match it).

- [ ] **Step 2: Insert a rollback step**

Immediately after the existing `apply` step and its assertion (before the
existing `doctor` step that follows it), add, following the same
style/helper the surrounding steps already use:

```sh
# Capture the snapshot just taken by the apply above, then roll back to it
# and confirm doctor reports no drift against the restored (pre-apply) state.
snapshot=$("$BIN" rollback | head -n1 | awk '{print $1}')
"$BIN" rollback --to "$snapshot" --yes
"$BIN" doctor
```

Adjust variable naming (`$BIN`, or whatever this script's existing variable
for the built binary path is called) and the assertion idiom to match
exactly what the rest of the script already does — do not introduce a new
style. If the script uses `set -e` (check its top), a non-zero `doctor` exit
here will already fail the script; if it instead checks `$?` explicitly
after each call, follow that pattern for this new step too.

- [ ] **Step 3: Run it**

Run: `bash test/e2e/run.sh` (per `CLAUDE.md`, this is meant to run as root in
a minimal distro container — run it however CI does, e.g. inside the same
Docker image CI uses, never against your real `$HOME`).
Expected: the full cycle passes, including the new rollback step.

- [ ] **Step 4: Commit**

```bash
git add test/e2e/run.sh
git commit -m "test(e2e): cover rollback in the end-to-end cycle"
```
