# omnishell — final whole-branch review, fix wave

Branch: `feat/omnishell-v1` (confirmed via `git branch --show-current`; no new branch).
Date: 2026-09-03.

## Commits (all on `feat/omnishell-v1`)

| SHA | Subject |
|-----|---------|
| `fd82793` | fix: quote zoxide --cmd and constrain string options by pattern (C1) |
| `c0e7b57` | fix: repair a dangling omnishell marker block instead of appending (rcfile) |
| `719ddb2` | fix: stop a stably-degraded module breaking apply idempotency and doctor (C2) |
| `3fefb92` | fix: apply/doctor robustness — guard ordering, unknown & malformed modules (I1-I5) |
| `aed9b69` | fix: verify the release tarball checksum in install.sh (I6) |
| `221491b` | fix: mechanical minors — argv --, []string fallback run, error handling, perms |

## Verification (run before every commit and at the end)

- `go test ./... -race -count=1` — **green**, 14 packages, no failures/panics
- `go vet ./...` — clean
- `gofmt -l internal/ cmd/ modules/` — empty
- `go build ./...` — clean
- `sh -n install.sh` — ok
- `go run ./cmd/omnishell --help` — lists all commands

## Per-finding status

### MUST FIX

**C1 — shell injection in zoxide templates — FIXED**
- Both `modules/builtin/zoxide/{zsh,bash}.tmpl` now `--cmd {{ .Options.cmd | shellquote }}`.
- `module.OptionSchema` gained optional `pattern` (regexp) for `type = "string"`;
  enforced in `ValidateManifest` (compile check + rejects `pattern` on non-string),
  in `coerce`/`ValidateOptions`, and in `OptionSchema.ParseValue` (the `set` path).
  Compiled once via a `sync.Map` cache. Mismatch → `OptionError` → exit 2.
- `zoxide.cmd` pinned to `^[A-Za-z_][A-Za-z0-9_-]*$`.
- Goldens regenerated: `--cmd 'z'` (single-quoted) as expected.
- New guard `modules.TestNoUnquotedStringOptionsInCommandPosition` walks every
  builtin `{zsh,bash}.tmpl` and fails on a value-emitting `{{ .Options.<string> }}`
  without `| shellquote` (control actions like `has_item`/`if not` are excluded).
- New `module` unit tests: pattern accept/reject via `ValidateOptions` and
  `ParseValue`; `ValidateManifest` rejects `pattern` on a bool option and an
  uncompilable regexp.
- `docs/writing-a-module.md`: documents `pattern` and a "Quoting is not optional"
  paragraph.

**C1 reproduction evidence**

Before: `omnishell set zoxide.cmd 'z; rm -rf ~'` written verbatim into
`config.toml`; generated `init.zsh` line was
`... eval "$(zoxide init zsh --cmd z; rm -rf ~)"` — arbitrary code at every shell start.

After:
```
$ omnishell set zoxide.cmd 'z; rm -rf ~'
error: .../config.toml: invalid value for zoxide.cmd: "z; rm -rf ~" does not match required pattern ^[A-Za-z_][A-Za-z0-9_-]*$
  exit=2
# even hand-editing config.toml to cmd = 'z;echo X' is rejected by apply:
$ omnishell apply --yes --no-packages
error: option "cmd": "z;echo X" does not match required pattern ^[A-Za-z_][A-Za-z0-9_-]*$   (no init file written)
# with a valid value:
$ omnishell set zoxide.cmd zi && omnishell apply --yes --no-packages
$ grep 'zoxide init' init.zsh
command -v zoxide >/dev/null 2>&1 && eval "$(zoxide init zsh --cmd 'zi')"
```

**C2 — degraded module breaks apply idempotency + doctor — FIXED**
- New `engine.plannedDegraded(plan)` helper. `Apply`, `Doctor`, and
  `initOrRCDrift` all seed their `degraded` map from it, so all three compute
  `initfile.ContentHash` over the identical section set.
- `ComputePlan`: a planned-degraded module's expected `ShellsRendered` is treated
  as empty for the `ActionUpdate` comparison, so it settles on `ActionUnchanged`
  and stops pinning `HasChanges` true.
- `Apply`'s no-op short-circuit now returns `ErrDegraded` (exit 1) **without any
  disk write** when a module is stably degraded.
- `Doctor` suppresses `pending-apply:<id>` for a module already flagged
  `module-degraded:<id>`; `initfile-stale` no longer fires because the hashes now
  agree.
- Regression tests: `internal/cli/e2e_test.go TestJourneyNoPackageManager`
  extended (second `apply --yes` → no write, no new backup, exit 1; `doctor`
  exit 3, contains `module-degraded:fzf`, NOT `initfile-stale`, NOT
  `pending-apply`, has `[drift] ` prefix). Focused
  `internal/engine TestApplyStablyDegradedIsIdempotent` added.

**C2 reproduction evidence** (fake `$HOME`, `PATH=/usr/bin:/bin` so no package manager)
```
$ omnishell init && omnishell enable fzf
$ omnishell apply --yes            # exit 1: "degraded fzf  no package manager detected"
backup dirs after 1st apply: 1
$ omnishell apply --yes            # exit 1 again, writes nothing
backup dirs after 2nd apply: 1     # UNCHANGED
init.zsh mtime: UNCHANGED between runs
$ omnishell doctor                 # exit 3
[drift] module-degraded:fzf  module "fzf" is degraded: no package manager detected
doctor findings count: 1           # only module-degraded; no initfile-stale, no pending-apply
```

**rcfile — unterminated marker block grows unbounded — FIXED**
- `EnsureBlock`: a `BlockStart` with no matching `BlockEnd` is now repaired —
  discard from the dangling marker to EOF, write one fresh block, idempotent
  after. `RemoveBlock`: a dangling `BlockStart` is stripped (marker → EOF).
- New `rcfile` unit tests: `TestEnsureBlockRepairsDanglingStart`,
  `TestRemoveBlockStripsDanglingStart`.

### SHOULD FIX

**I1 — hand-edit guard ran after package install — FIXED**
- The per-shell hand-edit guard now runs BEFORE `installPackages` and
  `runCheckHooks`. Ordering is
  PLAN → dry-run → no-op → confirm → **hand-edit guard** → installPackages →
  runCheckHooks → renderAll → buildSections → backup → write.
- `TestApplyHandEditGuard` still passes; new
  `TestApplyHandEditGuardRunsBeforeInstall` asserts `mgr.InstallCalls` is empty
  when the guard fires without `--force`.

**I2 — unknown module id silently ignored — FIXED**
- `Plan.UnknownModules []string` (sorted) collects enabled config ids no module
  provides. `Apply` prints `warning: unknown module %q in config (ignored)` to
  `e.Stderr` per id. `Doctor` emits a `notice` `unknown-module:<id>`. The lying
  comment is gone. New `TestComputePlanCollectsUnknownModules`.

**I3 — `--verbose` plumbed but unread — FIXED**
- `ApplyOptions.Verbose` and its assignment in `cli/apply.go` removed. The
  cobra-standard root `--verbose` persistent flag stays (harmless). README's
  "verbose output" line removed.

**I4 — `set` silently enabled a disabled module — FIXED**
- `config.editTable` no longer writes `enabled = true` when creating an absent
  parent `[modules.<id>]` table for a `set` on an options key. New
  `TestSetOptionDoesNotEnableAbsentModule` (module absent → `Enabled == false`,
  option still written).

**I5 — one malformed user module dir bricked every command — FIXED**
- `module.LoadRegistry` skips a malformed USER module dir (missing/invalid
  manifest, folder/id mismatch) and records it; new `(Registry) Malformed()
  []string` (sorted `path: reason`). Built-in modules stay a hard error.
- `cli/buildEngine` wraps a genuinely unreadable modules dir / broken builtin as
  `config.Error` (exit 2), not a bare error.
- `Doctor` emits a `notice` `malformed-module:<path>` per entry.
- New `TestLoadRegistrySkipsMalformedUserModule` and
  `TestLoadRegistryBuiltinMismatchIsHardError`. The old
  `TestLoadRegistryRejectsFolderNameMismatch` (which asserted a user mismatch was
  fatal) was replaced.

**I6 — install.sh didn't verify the download — FIXED**
- Downloads the tarball and the release `checksums.txt` to a temp dir, extracts
  the expected sha256 for `omnishell_${os}_${arch}.tar.gz`, verifies with
  `sha256sum` / `shasum -a 256` (warning-only if neither present), then
  `tar -xz -f`. `set -eu` preserved; a failed check aborts. `sh -n` passes;
  checksum extraction + match + mismatch paths were exercised locally.

### MECHANICAL MINORS — all FIXED

- `plan.go` shell-loop `mod.Template(sh)` error is handled (module degrades with
  `template read failed for <sh>: ...` instead of silently "no snippet").
- `uninstall.go` `lockfile.Load` error captured — a corrupt lock aborts with a
  wrapped error; only `!exists` is benign.
- `ActionSkip` is wired: an enabled, known module not supported on the host OS is
  planned `ActionSkip` (rendered by `RenderPlan` as a `skip` line + trailing
  `N skipped ...`, surfaced in the apply summary). New
  `TestComputePlanSkipsModuleNotSupportedOnPlatform`. The `summarise` "skipped"
  branch and the `ModuleResult` doc mention are now reachable/accurate.
- `ComputePlan` validates options in sorted id order → deterministic
  `ConfigError` with several invalid modules.
- rc/init writes (`engine.ensureRC`, `cli/init.go hookShell`) `os.Stat` the
  existing rc file and reuse its `Mode().Perm()`; `0o644` only for a new file.
- `plan.go managedShells` intersects the configured/auto shell list with shells
  that are actually `Present`, matching `omnishell init` — no more creating
  `~/.bashrc` on a host with no bash.
- `pkgmgr` install + uninstall argv for apt/dnf/pacman/zypper/apk/brew get a
  `--` terminator before the package list. argv tests updated.
- `module.Fallback.Run` is now `[]string` (argv), rendered element-by-element in
  `InstallGitFallback`; `strings.Fields` split removed. fzf manifest
  (`run = ["{{.VendorDir}}/fzf/install", "--bin", "--no-update-rc"]`) and all
  four test fixtures updated. New
  `TestInstallGitFallbackHandlesSpacesInVendorDir`.
- `cli/apply.go` + `cli/uninstall.go` print `aborted` to `cmd.ErrOrStderr()`.
- `doctor` `[drift] ` / `[notice] ` line-prefix assertion folded into the C2
  e2e test.
- `homeRelative` de-duplicated: `engine.HomeRelative(path, home string)` exported;
  `(e Engine) homeRelative` and the CLI helper both delegate to it, so `init`
  and `apply` cannot emit divergent rc source lines.

### DEFERRED (left untouched, as instructed)

`apply_helpers.go` file split; `CHANGELOG.md`; `runHookScript` global-env
approach; `hookEnv` `OMNISHELL_SHELL=""` doc note; `renderAll` `Active` snapshot;
`LICENSE` file (handled separately by the controller).

## Concerns / notes

- `install.sh` checksum verification depends on the release actually publishing
  `checksums.txt` (goreleaser already does). Not exercised end-to-end against a
  real GitHub release — only the local checksum-extraction/verify/mismatch logic.
- `managedShells` now drops a shell the user *explicitly* lists in
  `[omnishell].shells` if it is not `Present` on the host (matches `init`; the
  task called this the acceptable simplest-consistent behaviour).
- `Doctor` reports `malformed-module` / `unknown-module` as `notice` severity, so
  they do not by themselves make `doctor` exit 3. Chosen for consistency with the
  existing `module-override` notice.
- Nothing could not be fixed in this pass.
