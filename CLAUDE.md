# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`omnishell` is a single static Go binary that applies terminal configuration
(shell plugins/settings) on macOS and Linux in a modular, declarative,
reversible way. Users list enabled *modules* in `~/.config/omnishell/config.toml`;
`omnishell apply` installs each module's packages, renders its shell template,
and writes everything into one tool-managed `init.zsh` / `init.bash` sourced
from the user's rc file via a single marker block. See README.md for the full
user-facing command reference, on-disk layout, and safety model — don't
duplicate it here.

## Commands

```sh
make build   # go build with version ldflags -> ./omnishell
make test    # go test ./... -race -count=1
make vet     # go vet ./...
make lint    # golangci-lint run (CI uses golangci-lint-action v9, config v2.13)
```

Run a single test: `go test ./internal/engine/... -run TestApply_Foo -v`

E2E: `test/e2e/run.sh` builds the binary and runs a real
init→enable→apply→doctor→rollback→remove→uninstall cycle against a throwaway
sandbox `HOME` (`mktemp -d`), so it runs either unprivileged (macOS runner) or as
root (minimal distro container); CI does both per-OS. Package installs still hit
the real system, so only run it in a disposable environment.

CI (`.github/workflows/ci.yml`) runs vet, race tests, and lint on every push/PR;
`build.yml`-equivalent job builds on ubuntu+macos and runs `omnishell version`.
Releases are tagged (`vX.Y.Z`) and built via GoReleaser (`.goreleaser.yaml`),
which also pushes a formula to the `JtheGunner/homebrew-tap` repo (needs
`HOMEBREW_TAP_GITHUB_TOKEN`). `install.sh` downloads the matching release
tarball and verifies its checksum before extracting. The GoReleaser
`before` hook runs `make dist-assets` to generate bash/zsh completions and
man pages (hidden `omnishell docs man <dir>`, via `cobra/doc`); they ride in
the archives and the Homebrew formula, and `install.sh` best-effort-installs
them into `$XDG_DATA_HOME`.

## Architecture

Execution flows top-down through these packages; understanding the flow across
files matters more than any single file:

1. **`internal/cli`** — Cobra commands (one file per subcommand). Each command
   loads config/lockfile, builds an `engine.Engine`, calls one engine method,
   and maps the returned error to an exit code via `ClassifyError`
   (`internal/cli/exit.go`). Exit codes: `0` ok, `1` application error,
   `2` config/schema error, `3` drift (`doctor` only).

2. **`internal/engine`** — the orchestrator, holding all injected dependencies
   (`platform.Info`, `module.Registry`, `pkgmgr.Manager`/`Runner`, clock,
   stdout/stderr, a `Prompt` func). Everything is a method on `Engine` so tests
   can inject fakes instead of touching the real filesystem/network/shell.
   - `plan.go`: `ComputePlan` — pure function from `(Engine, config, lockfile)`
     to a `Plan` describing what would change. No side effects.
   - `apply.go`: `Apply` — the only place that mutates disk/packages. Order
     matters: compute plan → check idempotent no-op → prompt → **hand-edit
     guard** (must run before any package install / hook, so a tampered init
     file aborts before anything sudo-touches the system) → install packages →
     run check hooks → render → backup → write init files atomically → update
     rc marker block → write lockfile.
   - `doctor.go` / `remove.go` / `uninstall.go`: read-mostly / narrower mutating
     operations built on the same plan machinery.
   - Degradation is a first-class state threaded through Plan → apply →
     lockfile → doctor, not an exception: a module can be "degraded" (e.g. no
     package manager found) and still get a snippet section, but the CLI must
     still exit non-zero (`ErrDegraded`).

3. **`internal/module`** — manifest schema (`manifest.go`), option
   validation (`options.go`), and the `Registry` (`registry.go`) that merges
   built-in modules (compiled in via `modules.FS()`, a malformed one is a hard
   build error) with user modules from `~/.config/omnishell/modules/<id>/`
   (a malformed user module is skipped and reported via `Registry.Malformed()`,
   never fatal). A user module with the same id as a built-in **overrides** it
   (`Registry.Overrides()`); `doctor` flags this.

4. **`internal/graph`** — topological sort of active modules by `requires`
   (hard dependency, missing one is a config error) and `after` (soft ordering
   hint, ignored if the target isn't active). Deterministic: ties broken by
   sorted id, so output ordering never depends on map iteration.

5. **`internal/config`** — loads/saves `config.toml` and implements the
   `enable`/`disable`/`set` edits (`edit.go`), validating option values against
   a module's schema before writing. Every command except `apply`/`remove`/
   `uninstall` only touches this package.

6. **`internal/platform`** — detects OS (macos/linux) and which shells
   (zsh/bash) are present/managed on the current machine; feeds both
   `ComputePlan` (module platform/shell filtering) and package-manager
   detection.

7. **`internal/render`**, **`internal/initfile`**, **`internal/rcfile`** —
   template rendering per module, assembling the marked sections into one
   `init.<shell>` file (with content hash for drift detection), and inserting/
   removing the single rc-file marker block, respectively.

8. **`internal/pkgmgr`** — `Manager` interface implemented per package manager
   (brew/apt/dnf/pacman/zypper/apk), detected via `DetectManager` in OS-specific
   priority order, plus a `git`-clone-based fallback (`gitfallback.go`) used
   when a module has no package for the detected manager (or none was found).
   All shelling out goes through the injectable `Runner` interface — tests use
   `mock.go`, never real `exec.Command`.

9. **`internal/lockfile`**, **`internal/backup`**, **`internal/atomicfile`** —
   idempotency/drift-detection state, pre-write timestamped backups, and
   atomic (temp file + rename) writes. Every disk write to an rc file or init
   file goes through backup then atomicfile — never write these directly.

10. **`modules/builtin/<id>/`** — one folder per built-in module: `manifest.toml`
   + `zsh.tmpl`/`bash.tmpl` + optional `hooks/`, embedded via `modules/embed.go`.
   This is the reference shape for user-authored modules too — see
   `docs/writing-a-module.md` for the manifest schema and template context.

### Key invariants to preserve when touching engine/apply code

- Nothing touches the real system except `apply`, `remove`, `uninstall` (and
  `init`'s one-time rc insertion). Every other command only reads or edits
  `config.toml`.
- The hand-edit guard in `Apply` must run before package installation and
  before any hook execution — it's the abort point for "don't touch a system
  a human has hand-modified."
- Init files are only swapped in atomically at the very end of `Apply`; an
  aborted run must leave prior on-disk state intact.
- `ComputePlan` must stay side-effect-free — `doctor` and the apply dry-run
  path both depend on being able to call it without mutating anything.
