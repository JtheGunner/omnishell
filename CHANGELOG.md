# Changelog

Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Added
- Manifest `conflicts` key: a module can declare ids it is incompatible with.
  When two conflicting modules are both enabled, `apply` / `doctor` / `diff`
  stop with exit 2 and name the pair. One-directional (either side may declare
  it); cannot overlap `requires`.
- `omnishell validate`: check `config.toml` against the module option schemas
  and dependency rules (`requires` / `conflicts` / cycles) without computing a
  plan or probing the host. Exit 2 on any problem; `--json` for machine output.
- `omnishell apply --reload`: re-exec `$SHELL` after a successful apply so the
  changes take effect immediately. No-op (with a hint) in a non-interactive
  shell or when `$SHELL` is unset.
- `omnishell doctor --fix`: repair the drift a re-apply resolves (missing /
  stale init file, missing rc source line, orphaned lockfile entries), with a
  confirmation prompt (`-y` skips it). A hand-edited init file and missing
  packages are never touched automatically. Backed by a new
  `ApplyOptions.Refresh` that rebuilds init/rc files even on a no-op plan.
- `omnishell bench`: measure how much sourcing the generated `init.<shell>`
  adds to shell startup per managed shell, and warn above the startup budget
  (`[omnishell] startup_budget_ms` in `config.toml`, default 200). Always
  exits 0; `--json` and `--runs <n>` supported.
- Four config-only built-in modules for visual polish: `colorized-man`
  (bat-backed man pages with a `less` fallback), `ls-colors` (a curated
  `LS_COLORS` / `LSCOLORS` palette), `pager-defaults` (sensible `less` env),
  and `window-title` (terminal title follows `$PWD`).
- Built-in `direnv` module: `.envrc`-based per-directory environment via
  `direnv hook`, with `log_format` and `whitelist` options and a `git`
  fallback.

### Fixed
- `omnishell init` now writes a backup manifest (`kind = "init"`) for the rc
  file it backs up, so that pre-omnishell state shows up in
  `omnishell rollback` and can be restored like any other snapshot.

## [0.2.0] - 2026-09-04

### Added
- `description` and `homepage` fields on modules, surfaced in `omnishell list`
  (table and `--json`) and in the README's built-in modules table.

### Fixed
- `enable` now refuses a module with no compatible managed shell instead of
  silently accepting it.
- `modern-aliases` falls back to `batcat` when `bat` isn't on `PATH`
  (Debian/Ubuntu package naming).

## [0.1.2] - 2026-09-04

### Fixed
- `enable` refuses a module with no compatible managed shell.

## [0.1.1] - 2026-09-04

### Fixed
- `modern-aliases` falls back to `batcat` when `bat` is unavailable.

## [0.1.0] - 2026-09-04

Initial release.

### Added
- Core engine: plan computation (`ComputePlan`), human-readable plan
  rendering, `apply` (install packages, render templates, write init files,
  update lockfile), `doctor` (drift detection), `remove`, `uninstall`.
- CLI commands: `init`, `list`, `enable`, `disable`, `set`, `apply`, `diff`,
  `doctor`, `remove`, `uninstall`, `version`, `completion`.
- Module system: manifest parsing/validation, option schema validation,
  built-in + user module registry with override/shadow detection, dependency
  graph ordering (`requires`/`after`).
- Package manager support: brew, apt, dnf, pacman, zypper, apk, plus a
  `git`-clone fallback when a module has no package for the detected manager.
- Built-in modules: `completion`, `history`, `autosuggestions`,
  `syntax-highlighting`, `fzf`, `zoxide`, `modern-aliases`.
- Safety infrastructure: timestamped backups before every rc/init-file write,
  atomic (temp file + rename) writes, hand-edit detection guard, content-hash
  drift detection.
- Release tooling: GoReleaser config, `install.sh` (checksum-verified),
  Homebrew tap formula push, per-OS CI (vet, race tests, lint) and opt-in
  per-distro E2E tests.

[Unreleased]: https://github.com/JtheGunner/omnishell/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/JtheGunner/omnishell/compare/v0.1.2...v0.2.0
[0.1.2]: https://github.com/JtheGunner/omnishell/compare/v0.1.1...v0.1.2
[0.1.1]: https://github.com/JtheGunner/omnishell/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/JtheGunner/omnishell/releases/tag/v0.1.0
