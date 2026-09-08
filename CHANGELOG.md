# Changelog

Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Added
- Built-in `tmux` module: installs [tmux](https://github.com/tmux/tmux) (the
  `tmux` package on every supported manager) and emits a shell snippet that, on
  an interactive shell not already inside tmux, runs
  `tmux attach-session -t <session> || tmux new-session -s <session>`. Guarded on
  `[[ $- == *i* ]]`, `$TMUX` unset, and `command -v tmux`. The `session` option
  (string, default `default`) names the session. Ordered ahead of the prompt
  modules (`starship`, `omnishell-prompt`) via their `after` lists so the
  multiplexer is running before the prompt initialises.
- Built-in `root-loops` module: config-only, no package. On shell start it
  pushes a [rootloops.sh](https://rootloops.sh)-generated 16-colour palette
  (plus foreground/background) into the terminal via OSC 4/10/11 escapes, with
  a tmux passthrough wrapper, guarded on `[ -t 1 ]`. The `appearance` option
  (`enum` `auto` | `dark` | `light`, default `auto`) selects a light or dark
  variant; `auto` reads `AppleInterfaceStyle` on macOS and the freedesktop
  `org.freedesktop.appearance color-scheme` portal (via `gdbus`) on Linux,
  falling back to dark. No once-only latch, so re-sourcing after a system
  light/dark switch re-colours immediately.
- Built-in `starship` module: the [Starship](https://starship.rs) cross-shell
  prompt via `eval "$(starship init <shell>)"`. Seeds a curated single-line
  theme to `~/.config/omnishell/starship.toml` on first shell start (never
  overwritten afterwards) and points `STARSHIP_CONFIG` at it. Orders after
  `completion` / `fzf-tab`; `conflicts` with `omnishell-prompt`. `starship`
  package on `brew` / `apt` / `pacman` / `apk`, a `git` + `cargo` fallback
  elsewhere. `remove --purge` deletes the seeded config.
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
- Built-in `mise` module: per-project runtime versions via `mise activate`,
  with a `mode` (activate | shims) option. System package on `brew` / `pacman`,
  a `git` + `cargo` fallback elsewhere.
- Built-in `pay-respects` module: a corrected-command suggestion alias (Rust
  `thefuck` alternative), `alias` option (default `f`). `brew` package, `git`
  + `cargo` fallback elsewhere.
- Built-in `atuin` module: SQLite-backed shell history with fuzzy search,
  `bind_ctrl_r` / `bind_up_arrow` options, and `conflicts = ["fzf"]` (both bind
  Ctrl+R). `brew` / `pacman` package, `git` + `cargo` fallback elsewhere.
- Built-in `fzf-tab` module (zsh): replace the completion menu with an fzf
  picker, `cd_preview` option. `git`-clone plugin, `requires = ["fzf"]`; loads
  after `completion`/`fzf` and before `autosuggestions`/`syntax-highlighting`.
- Built-in `omnishell-prompt` module: a dependency-free two-line git-aware
  prompt, `style` / `show_duration` / `char` options. Opt-in — it sets
  `PROMPT` / `PS1`.
- Built-in `broot` module: the `br` navigable-tree TUI with cd-on-exit, `cmd`
  option to rename the function. `brew` / `apt` / `dnf` / `pacman` package,
  `git` + `cargo` fallback elsewhere.
- Built-in `welcome` module: run `fastfetch` on interactive shell start, with
  an `only_ssh` option. Opt-in — it adds visible startup latency.
- Release archives and the Homebrew formula now ship bash/zsh completions and
  man pages for the `omnishell` command itself. Man pages are generated from
  the command tree via a hidden `omnishell docs man <dir>`; `install.sh`
  places both into `$XDG_DATA_HOME` on a best-effort basis.

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
