# Changelog

Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

## [0.7.0] - 2026-10-07

### Added
- `omnishell tui`: a full-screen terminal UI to browse modules (description,
  homepage, package status, platforms, shells) with a `/` filter. Space enables
  or disables the selected module in `config.toml`, like `enable` / `disable`.
  `o` edits the module's options (bool, enum, string and int), validated like
  `omnishell set`; text fields have a movable cursor and accept pasted text.
  `a` previews the plan; confirming it closes the UI and runs
  `omnishell apply`, which still asks before it changes anything. Modules that
  cannot run on this host are dimmed with the reason shown; one that only lacks
  OS support can still be enabled, but `apply` skips it. It needs an
  interactive terminal. It brings the first runtime dependencies, Bubble Tea
  and Lip Gloss, so the binary grows by about 1.5 MiB.
- `apply` now says "Nothing to apply" when it finds nothing to do, instead of
  printing nothing.

### Fixed
- `list` no longer prints control characters from a module manifest's text to
  the terminal.
- `list`, `diff` / `apply --dry-run` and `doctor` no longer print the package
  manager's answers (for example `brew list --versions fzf`) into their own
  output. `list --json` was not valid JSON on hosts where the package manager
  writes to stdout. `apply`, `doctor --fix`, `remove` and `uninstall` still show
  installation and hook output.
- `omnishell set` no longer writes a `config.toml` that no command can load
  when a value contains a BEL or vertical-tab character: control characters are
  now written as `\u00XX`, the escape TOML defines, instead of Go's `\a`, `\v`
  and `\x..`. Values that were not valid UTF-8 used to come back as different
  characters; they are now rejected with exit code 2 and the file is left
  untouched. Values that worked before are written exactly as before.

## [0.6.0] - 2026-10-07

### Added
- 32-bit ARM Linux (armv6 and armv7): release builds, `install.sh` support, and
  release-binary installs for `starship` (armv6 and armv7) and `mise` (armv7).
  `broot` still builds from source there. Release assets accept an optional
  `goarm` key to tell the variants apart.
- `install.sh` honours `OMNISHELL_VERSION` to install a specific release.

### Fixed
- `install.sh` no longer calls the rate-limited GitHub API to find the latest
  release (it failed with HTTP 403 on shared IPs such as CI runners); it reads
  the `releases/latest` redirect instead. A failed lookup now reports the HTTP
  status.

## [0.5.0] - 2026-10-06

### Added
- When a release binary replaces an earlier Cargo build, `apply` removes the
  build's leftovers (the cloned source tree and this tool's entries in cargo's
  install metadata) after verifying they are its own, and lists them in the
  plan. A Cargo build already at the pinned tag is kept as the release binary
  and only cleaned up. `doctor` notes another copy of the binary on `PATH`.
- The `fallback-tags` workflow re-pins the SHA-256 of release assets together
  with the tag.
- `[[packages.fallback]]` accepts `type = "release"`: a pinned, SHA-256-verified
  binary per OS and architecture (`[[packages.fallback.assets]]`), installed to
  `<vendor>/bin`. It is listed before the `git` entry, so the order is system
  package → release binary → source build. A host with no matching asset moves
  on to the source build; a failed first download or a checksum mismatch
  degrades the module.
- `mise`, `starship` and `broot` install their upstream release binary on
  Linux x86_64 and arm64 instead of building with Cargo (about 22 minutes for
  `mise`). Their `requires` now apply only to the source build.

### Changed
- The first `apply` after upgrading moves `mise`, `starship` and `broot` to the
  release binary where no package exists: a Cargo build at the pinned tag is
  kept and only cleaned up, an older one is replaced by the download. A failed
  update of a binary that already works keeps the module and its shell
  integration and is retried on the next `apply`. A first install that fails,
  and any checksum mismatch, still degrades the module.
- A `[[packages.fallback]]` table now needs a valid `type` (`git` or
  `release`). A user module that left it out is reported as malformed and
  skipped.

## [0.4.0] - 2026-10-06

### Changed
- `apply` moves an existing `git` fallback clone in `vendor/` to the tag the
  module's manifest pins and rebuilds it. The plan lists the update first. A
  clone with local changes, or a directory that is not a git clone, is left
  alone and not retried until the pinned tag changes; a failed update keeps the
  installed tool and its shell integration. The first `apply` after upgrading
  rebuilds each fallback-built tool once.
- The pinned `git` fallback tag of the `direnv` module moved to v2.38.0.

## [0.3.3] - 2026-10-06

### Added
- `[[packages.fallback]]` accepts an optional `requires` list (`"cmake"`,
  `"cargo>=1.85"`). `omnishell apply` checks it before cloning and, when a tool
  is missing or too old, degrades the module with a message naming what is
  missing instead of failing minutes later with a raw build error. The built-in
  cargo and cmake fallbacks declare their prerequisites, which the README lists
  per module.
- `[[packages.fallback]]` accepts an optional `ref` (tag or branch) that the
  `git` clone checks out. Without it the default branch is cloned, as before.
- `apply` announces when a `git` build replaces a distro package that the
  manager could not provide, naming the pinned ref or that the build is
  unpinned.

### Changed
- The built-in modules pin their `git` fallback to a release tag, so a
  fallback build is reproducible. An existing clone in `vendor/` is kept
  as is; remove it to pick up the pinned tag.

### Fixed
- A module whose package is listed for the active package manager but cannot
  be provided by its repositories (for example `starship` or `broot` on Ubuntu
  24.04's `apt`) now uses its `git` fallback instead of ending up degraded. If
  the fallback fails too, the reason names both attempts; genuine install
  errors such as network or `sudo` failures still surface as before.

## [0.3.2] - 2026-09-23

### Changed
- Minimum Go version for building from source raised from 1.23 (no longer
  receiving security fixes) to 1.26. Release binaries are now built with the
  latest stable Go toolchain, so they ship the newest standard-library
  security fixes.

### Fixed
- CI: the `govulncheck` job runs on the stable Go toolchain again; it broke
  when govulncheck started requiring a newer Go version than the pinned one.

## [0.3.1] - 2026-09-08

### Added
- `tmux` module: `auto_attach` option (bool, default `true`). When set to
  `false` the module still installs and manages the `tmux` package but emits
  nothing into shell startup, so an interactive shell no longer drops into the
  multiplexer. The default preserves the previous behaviour.

## [0.3.0] - 2026-09-08

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

[Unreleased]: https://github.com/JtheGunner/omnishell/compare/v0.7.0...HEAD
[0.7.0]: https://github.com/JtheGunner/omnishell/compare/v0.6.0...v0.7.0
[0.6.0]: https://github.com/JtheGunner/omnishell/compare/v0.5.0...v0.6.0
[0.5.0]: https://github.com/JtheGunner/omnishell/compare/v0.4.0...v0.5.0
[0.4.0]: https://github.com/JtheGunner/omnishell/compare/v0.3.3...v0.4.0
[0.3.3]: https://github.com/JtheGunner/omnishell/compare/v0.3.2...v0.3.3
[0.3.2]: https://github.com/JtheGunner/omnishell/compare/v0.3.1...v0.3.2
[0.3.1]: https://github.com/JtheGunner/omnishell/compare/v0.3.0...v0.3.1
[0.3.0]: https://github.com/JtheGunner/omnishell/compare/v0.2.1...v0.3.0
[0.2.1]: https://github.com/JtheGunner/omnishell/compare/v0.2.0...v0.2.1
[0.2.0]: https://github.com/JtheGunner/omnishell/compare/v0.1.2...v0.2.0
[0.1.2]: https://github.com/JtheGunner/omnishell/compare/v0.1.1...v0.1.2
[0.1.1]: https://github.com/JtheGunner/omnishell/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/JtheGunner/omnishell/releases/tag/v0.1.0
