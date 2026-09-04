# omnishell rollback — design

## Problem

`omnishell` already takes a timestamped backup of every rc/init file it is
about to overwrite (`internal/backup`, `<ConfigDir>/backups/<timestamp>/`),
but there is no way to restore from one: "restoration from a backup is
manual in v1" (README). This design adds `omnishell rollback`, which restores
the system to the state it was in immediately before a chosen prior
`apply`/`remove`/`uninstall` run, undoing that run and everything after it.

Two scope decisions were made up front (confirmed with the project owner):

- **Chained rollback**: `rollback --to <timestamp>` may target *any* recorded
  snapshot, not just the most recent one, by replaying every snapshot from
  the target through the newest in reverse-chronological order.
- **Packages are out of scope**: rollback restores files and the lockfile,
  never uninstalls packages a rolled-back run installed. This matches
  `uninstall`'s existing boundary (packages are only ever removed via
  `remove --purge`). A rolled-back run's packages are left in place; `doctor`
  may subsequently report them as no-longer-referenced, which is acceptable
  for v1.

## Current gaps this design closes

1. `internal/backup.Session` records raw file copies by basename with no
   metadata: no original path, no "did this file exist before" flag, no link
   between a snapshot and the run that created it. Rollback cannot know what
   to restore or whether a path should be deleted (never existed before) vs.
   overwritten.
2. `state.lock.json` is never backed up before `Apply` overwrites it. Without
   this, rollback cannot restore lockfile state, which would leave `doctor`
   and future `apply` runs looking at a lockfile inconsistent with the
   restored files.

## Architecture

### 1. `internal/backup`: stateful `Session` + manifest

`Session` becomes a pointer type (`NewSession` returns `*Session`) so it can
accumulate entries across multiple `Save()` calls from different call sites
(`apply.go`, `apply_helpers.go`'s `ensureRC`, `uninstall.go`) without the
classic Go "copy of a struct holding a slice" aliasing bug. This is a
mechanical change — call sites are unaffected since `bk, err :=
backup.NewSession(...)` and `bk.Save(...)` remain syntactically identical;
only `ensureRC`'s parameter type changes from `backup.Session` to
`*backup.Session`.

`Save(path)` now always records an entry, even when the source file doesn't
exist:

```go
type FileEntry struct {
    OriginalPath  string `json:"original_path"`
    ExistedBefore bool   `json:"existed_before"`
    BackupName    string `json:"backup_name,omitempty"` // set iff ExistedBefore
}
```

A new `Manifest` type and `(*Session) WriteManifest(kind string, now
time.Time) error` write `<Dir>/manifest.json`:

```go
type Manifest struct {
    Schema           int         `json:"schema"`
    Kind             string      `json:"kind"` // "apply" | "uninstall" | "rollback"
    CreatedAt        string      `json:"created_at"`
    OmnishellVersion string      `json:"omnishell_version"`
    Files            []FileEntry `json:"files"`
}
```

`Apply` and `Uninstall` call `defer bk.WriteManifest(kind, e.now())`
immediately after a successful `NewSession`, so a manifest is written
best-effort even if the run later fails mid-write (a partial manifest
reflecting whatever was actually backed up beats an unreadable orphan
directory).

### 2. Lockfile backup gap

In `Apply`, add `bk.Save(lockPath)` immediately before `newLock.Write(lockPath)`.
This is the only place `state.lock.json` is overwritten (`Remove` and
`Uninstall` don't independently rewrite it — `Remove` calls `Apply`
internally, and `Uninstall` never touches the lockfile). No other engine
method needs a lockfile backup.

### 3. `config.toml` stays out of scope

`enable`/`disable`/`set` write `config.toml` directly via `atomicfile`, never
through a `backup.Session` — consistent with the existing safety model
("nothing touches your system except apply/remove/uninstall"). Rollback
restores the *applied* artifacts (init files, rc marker blocks, lockfile),
not the config that produced them. After a rollback, `config.toml` may
disagree with the restored state; a subsequent `doctor`/`apply` surfaces that
as ordinary drift. This is intended, not a bug to fix here.

### 4. `Engine.Rollback`

New `internal/engine/rollback.go`:

```go
type RollbackOptions struct {
    DryRun, Yes bool
}

type RollbackResult struct {
    RestoredTo    string   // target timestamp
    FilesRestored []string
    FilesRemoved  []string
    BackupDir     string   // the new pre-rollback safety snapshot
}

func (e Engine) Rollback(target string, opts RollbackOptions) (RollbackResult, error)
func (e Engine) ListSnapshots() ([]SnapshotInfo, error)
```

`SnapshotInfo{Timestamp string, Kind string, FileCount int}` is used both by
`rollback` (no `--to`, list only) and internally to build the restore plan.

**Discovery** (`ListSnapshots`): `os.ReadDir(<ConfigDir>/backups/)`; for each
entry, read `manifest.json`; skip silently (not an error) if missing or
fails to parse — covers pre-this-feature backups and orphaned directories
from a run that failed before `WriteManifest` ran. Sort descending
(newest first).

**Restore plan** (pure function, reused by `--dry-run`): given `target`,
select all snapshots with `Timestamp >= target` (i.e. target through
newest). Walk them **oldest to newest**; for each path in a snapshot's
manifest, keep only the **first** (= oldest / closest-to-target) entry seen
for that path — that snapshot captured the path's state immediately before
the earliest run in the rolled-back range that touched it, which is the
correct restore target regardless of what later runs did to the same path.
Reject with `ErrNoSuchSnapshot` if `target` doesn't match any directory name
exactly (no fuzzy matching).

**Apply the plan** (skipped under `--dry-run`): open a fresh `backup.Session`
(kind `"rollback"`) and back up the *current* content of every path about to
be touched — same safety property as every other mutating engine method:
the rollback is itself reversible via a later rollback. Then, per plan
entry: `ExistedBefore == true` → atomically write the backed-up content to
`OriginalPath`; `ExistedBefore == false` → `os.Remove(OriginalPath)`
(`IsNotExist` is not an error — idempotent). `state.lock.json` and rc files
are restored through this exact same generic mechanism — no special-casing,
since both are now captured like any other file (rc files are restored
whole, which folds the marker-block state back in automatically).

A failure partway through returns the partial `RollbackResult` and the
error; because the pre-rollback safety snapshot (the "back up current
content" step above) completes in full before any destructive write begins,
retrying the same `rollback --to` afterward is safe.

### 5. CLI

New `internal/cli/rollback.go`, modeled on `remove.go`:

```
omnishell rollback                        # list available snapshots
omnishell rollback --to <timestamp>       # restore up to and including <timestamp>
omnishell rollback --to <timestamp> --dry-run
omnishell rollback --to <timestamp> -y/--yes
```

`<timestamp>` is the literal backup directory name (e.g.
`20260904T182318Z`) as shown in the listing.

No hand-edit-guard equivalent: rollback overwrites regardless of whether an
init file was hand-edited since — that is the point of rolling back. The
listing/`--dry-run` output makes the affected files explicit so this is
never a silent surprise.

`uninstall --purge` snapshots remain out of scope: they already live outside
`ConfigDir` in a temp directory (since `ConfigDir` is being deleted) with
only their path printed once to stdout. `rollback` only scans
`<ConfigDir>/backups/`; recovering a purge is unchanged from today (manual,
using the printed path).

### 6. Error handling

New sentinel `engine.ErrNoSuchSnapshot`. `ClassifyError` gains:

```go
if errors.Is(err, engine.ErrNoSuchSnapshot) {
    return 2 // nothing was changed
}
```

Every other rollback failure (a restore write error, etc.) falls through to
the existing default (exit 1).

## Testing

- `internal/backup`: `Save` records an entry for both existing and missing
  source files; `WriteManifest` round-trips; multiple `Save` calls on one
  `*Session` accumulate correctly (regression test for the pointer refactor).
- `internal/engine` (`rollback_test.go`, fixtures mirroring `apply_test.go`):
  - single-run rollback (apply → rollback to before it → init file gone, rc
    marker block gone, lockfile entry gone)
  - chained rollback across two applies (rollback to before the first
    restores the state from before both, not just the second)
  - a path touched by only one run in a multi-run range is restored from
    that run's manifest specifically
  - rollback is itself reversible (a second rollback to just-before-the-
    first-rollback restores the pre-rollback state)
  - `--dry-run` makes no filesystem writes but reports the correct plan
  - `ErrNoSuchSnapshot` for an unknown `--to`
  - a missing/corrupt manifest is skipped when listing, not fatal
  - lockfile is now backed up and correctly restored (regression test for
    the closed gap)
- `internal/cli` (`rollback_test.go`, mirroring `remove_test.go`): listing
  output, successful rollback exit 0, `ErrNoSuchSnapshot` exit 2 with no
  changes made.
- E2E (`test/e2e/run.sh`, extended, no new script): after the existing
  init→enable→apply→doctor→remove→uninstall cycle, add
  enable→apply→rollback and assert `doctor` reports no drift afterward.

No coverage target beyond the project's existing convention — rollback is
pure filesystem logic with no network or external processes, fully
unit-testable via injected fakes.
