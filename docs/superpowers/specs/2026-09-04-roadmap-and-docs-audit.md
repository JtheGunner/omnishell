# Roadmap and documentation audit — 2026-09-04

## Documentation audit

README.md and CLAUDE.md were reviewed for completeness against the current
codebase (`internal/*`, `modules/builtin/*`, `.github/workflows/*`,
`.goreleaser.yaml`, `install.sh`). No factually wrong or stale statements were
found. Gaps closed in this pass:

- CLAUDE.md's architecture list omitted `internal/config` (config.toml
  load/save, `enable`/`disable`/`set` persistence) and `internal/platform`
  (OS/shell detection) even though both are used by nearly every other
  package. Added as items 5–6, renumbering the rest.
- CLAUDE.md didn't mention that `install.sh` verifies the release tarball
  checksum before extracting — added a clause under the CI/release paragraph.
- No contributor-facing entry point existed (CLAUDE.md is AI-agent context,
  not a human contributing guide). Added `CONTRIBUTING.md`.
- No changelog existed despite four tagged releases (v0.1.0–v0.2.0). Added
  `CHANGELOG.md` (Keep a Changelog format), backfilled from git history, to
  be maintained going forward under an "Unreleased" heading per PR.
- README now links both new files from a "Contributing" and "Changelog"
  section.

## Roadmap

Ideas below come from the README's existing "Not in v1" list plus patterns
observed in the codebase. Windows/PowerShell support is explicitly **out of
scope** — omnishell targets macOS and Linux only, by decision.

### Priority: `omnishell rollback`

The backup infrastructure (`internal/backup`, `backups/<timestamp>/`) already
exists — every rc-file and init-file write is preceded by a timestamped copy.
What's missing is the reverse direction:

- A manifest per backup snapshot recording which files were captured and
  what the rc/lockfile state was at that point, so a rollback target can be
  addressed unambiguously (not just "the newest backup dir").
- A `Rollback` engine method with the same safety properties as `Apply`:
  atomic writes, a pre-rollback backup of the *current* state (so a rollback
  is itself reversible), and lockfile reconciliation afterward so `doctor`
  doesn't immediately report drift against the restored files.
- CLI: `omnishell rollback [--to <timestamp>] [--dry-run] [-y]`, listing
  available snapshots when `--to` is omitted.

This is the most natural next step because it completes a safety story that's
already half-built, rather than opening new surface area.

### Other ideas (uncommitted, unordered)

- **fish support** — a third shell alongside zsh/bash. Touches
  `internal/rcfile`/`internal/initfile` (fish uses a different marker/comment
  syntax and config location, `~/.config/fish/config.fish`), `internal/module`
  (an `fish.tmpl` per module), and `internal/platform/shells.go` (detection).
  Existing modules would need fish templates written or would render as
  "not supported on this shell" until then.
- **profiles** — different module sets per machine (e.g. `work` vs.
  `personal`), likely a `--profile` flag plus a namespaced section in
  `config.toml`. Touches `internal/config` and `ComputePlan`.
- **remote module registry** — install modules from a URL instead of only
  local built-ins / `~/.config/omnishell/modules/`. Raises the same
  authenticity question `install.sh` already solves for the binary itself
  (checksum/signature verification) — should reuse that pattern rather than
  invent a new one.
- **self-update** — `omnishell self-update` pulling the latest GoReleaser
  artifact directly, for servers without Homebrew (currently covered by
  `install.sh` re-run, but not from inside the binary).
- **`omnishell doctor --fix`** — automatically repair the drift cases
  `doctor` already knows how to detect (missing source line, orphaned
  lockfile entries) instead of only reporting them.
- **`omnishell validate`** — check `config.toml` against the module option
  schema without computing a full plan; useful in CI for people who version
  their `config.toml` in a dotfiles repo.
- **New built-in modules**, following the existing manifest/template
  pattern: `direnv`, a prompt module (e.g. `starship`), `atuin` (as a
  richer alternative to the `history` module's shell-builtin approach),
  a minimal `tmux` config module, `thefuck`/command-correction.

None of these are scoped as implementation-ready yet — each would go through
its own brainstorming → design → plan cycle when picked up.
