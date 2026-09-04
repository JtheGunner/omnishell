# omnishell

Modular, declarative terminal configuration for macOS and Linux.

## What it is

`omnishell` is a single static binary that applies **terminal configuration** on
macOS and Linux in a **modular, declarative, reversible** way. You keep a plain
local file — `config.toml` — that lists which *modules* are enabled and their
options. Running `omnishell apply` then:

1. installs any programs a module needs (via the detected system package
   manager — `brew`, `apt`, `dnf`, `pacman`, `zypper`, `apk` — with a `git`
   clone fallback),
2. renders each module's shell snippet,
3. writes everything into a single tool-managed init file per shell
   (`init.zsh` / `init.bash`), sourced from your `~/.zshrc` / `~/.bashrc` via
   one marker block,
4. records the result in a lockfile for idempotency, drift detection and clean
   removal.

Your own `.zshrc` / `.bashrc` is never rewritten beyond that one marker block,
every write is backed up first, and running `apply` twice with no config change
is a no-op. No git repo, no account, and no network are required for normal use.

## Install

Homebrew:

```sh
brew install jthegunner/tap/omnishell
```

Curl (servers without Homebrew — downloads the matching binary):

```sh
curl -fsSL https://raw.githubusercontent.com/JtheGunner/omnishell/main/install.sh | sh
```

Go:

```sh
go install github.com/JtheGunner/omnishell/cmd/omnishell@latest
```

## Quick start

```sh
omnishell init                 # create the config, hook it into your shells
omnishell enable fzf           # or edit ~/.config/omnishell/config.toml by hand
omnishell apply                # install packages + write the init files
exec $SHELL                    # open a new shell to pick up the changes
```

`omnishell init` creates `~/.config/omnishell/config.toml` with everything
disabled and inserts the `source` marker block into the detected shells' rc
files. `enable` / `disable` / `set` only edit `config.toml` — nothing touches
your system until you run `omnishell apply`.

## Commands

`-h` / `--help` works on every command.

| Command | Purpose | Flags |
|---------|---------|-------|
| `omnishell init` | Create the omnishell config and hook it into your shells. Idempotent. | — |
| `omnishell list` | List known modules and their status (enabled/disabled, packages, platforms, shells, source, description). | `--json` (emit a JSON array instead of the table; also includes each module's `homepage`) |
| `omnishell enable <module>` | Enable a module in the config. Does not apply. | — |
| `omnishell disable <module>` | Disable a module in the config. Does not apply. | — |
| `omnishell set <module>.<key> <value>` | Set a module option in the config, validated against that module's option schema. | — |
| `omnishell apply` | Bring your shells up to date with the config. | `--dry-run` (show the plan without changing anything), `--force` (overwrite init files that were edited by hand), `--no-packages` (skip package installation), `-y` / `--yes` (apply without the confirmation prompt) |
| `omnishell diff` | Show what apply would change (`apply --dry-run`). | — |
| `omnishell doctor` | Check the installed shell environment for drift. Exit 3 if any drift is found. | — |
| `omnishell remove <id>` | Disable a module and drop its shell section, then re-apply. | `--dry-run`, `--purge` (also uninstall the module's packages, run its remove hook, and delete vendored files), `-y` / `--yes` |
| `omnishell uninstall` | Remove omnishell's shell integration (marker block + generated `init.<shell>` files). Every touched file is backed up first. | `--purge` (also delete `~/.config/omnishell` entirely), `-y` / `--yes` |
| `omnishell version` | Print the omnishell version. | — |
| `omnishell completion` | Cobra-generated shell autocompletion script generator (distinct from the `completion` module). | — |

## How it works

The config directory is `$XDG_CONFIG_HOME/omnishell`, or `~/.config/omnishell`
when `XDG_CONFIG_HOME` is unset:

| Path | What it is |
|------|------------|
| `config.toml` | The file you edit (by hand, or via `enable` / `disable` / `set`). |
| `init.zsh` / `init.bash` | Tool-generated. **Do not edit** — `apply` regenerates them and `doctor` flags manual edits. |
| `state.lock.json` | Machine-managed lockfile (idempotency, drift detection, clean removal). Not for editing. |
| `backups/<timestamp>/` | Every rc-file and init-file write is copied here first. |
| `modules/<id>/` | Your own modules (same format as the built-ins). |
| `vendor/` | Clones made by the `git` package fallback. |

Each managed rc file gets **exactly one** marker block, appended at the end:

```sh
# >>> omnishell >>>
[[ -f "$HOME/.config/omnishell/init.zsh" ]] && source "$HOME/.config/omnishell/init.zsh"
# <<< omnishell <<<
```

The generated `init.<shell>` file carries a header with a `Content hash`, then
one section per module wrapped in
`# >>> omnishell:<id> (v<ver>) >>>` … `# <<< omnishell:<id> <<<` markers, in
dependency order. It is written atomically (temp file + rename) after the old
copy is backed up. Only `apply`, `remove`, and `uninstall` change your system
(and `init` inserts the rc block); everything else just reads or edits
`config.toml`.

## Built-in modules

| id | Description | Packages | Shells | Options |
|----|-------------|----------|--------|---------|
| `completion` | Enable shell completion with case-insensitive matching | — (config only) | zsh, bash | — |
| `history` | Larger, de-duplicated, shared shell history and prefix search | — (config only) | zsh, bash | `size` (int, default `50000`) |
| `autosuggestions` | Fish-style grey inline command suggestions from history (zsh) — [zsh-users/zsh-autosuggestions](https://github.com/zsh-users/zsh-autosuggestions) | `zsh-autosuggestions`; `git` fallback | zsh | `highlight_style` (string, default `fg=8`) |
| `syntax-highlighting` | Colour commands green/red as you type depending on validity (zsh) — [zsh-users/zsh-syntax-highlighting](https://github.com/zsh-users/zsh-syntax-highlighting) | `zsh-syntax-highlighting`; `git` fallback | zsh | — |
| `fzf` | Ctrl+R history search as a fuzzy, scrollable list (+ optional Ctrl+T) — [junegunn/fzf](https://github.com/junegunn/fzf) | `fzf`; `git` fallback | zsh, bash | `ctrl_r` (bool, default `true`), `ctrl_t` (bool, default `false`), `default_opts` (string, default `--height 40% --reverse --border`) |
| `zoxide` | Smarter cd that learns your most-used directories — [ajeetdsouza/zoxide](https://github.com/ajeetdsouza/zoxide) | `zoxide`; `git` fallback | zsh, bash | `cmd` (string, default `z`) |
| `modern-aliases` | Replace ls/cat/find with [eza](https://github.com/eza-community/eza), [bat](https://github.com/sharkdp/bat) and [fd](https://github.com/sharkdp/fd) when selected | `eza`, `bat`, `fd` | zsh, bash | `replace` (`list<enum>` of `ls`, `cat`, `find`; default `["ls", "cat", "find"]`) |

`autosuggestions` and `syntax-highlighting` are zsh-only and always render last
(with `syntax-highlighting` after `autosuggestions`).

## Writing your own module

A module is just a folder: a `manifest.toml`, one `zsh.tmpl` / `bash.tmpl` per
shell, and an optional `hooks/` directory. Drop it in
`~/.config/omnishell/modules/<id>/` and it shows up in `omnishell list` with no
rebuild; a user module with the same id as a built-in overrides it (and
`doctor` flags the shadowing). See
[docs/writing-a-module.md](docs/writing-a-module.md) for the full manifest
reference, the template context, and a worked example.

## Safety

- Nothing changes on your system without `omnishell apply`, `omnishell remove`,
  or `omnishell uninstall` (`init` also inserts the rc marker block).
- Every write to an rc file or init file is preceded by a timestamped backup
  under `~/.config/omnishell/backups/<timestamp>/`.
- `apply` refuses to overwrite an `init.<shell>` file that was edited by hand
  (exit 1); it tells you to re-run with `--force`. `omnishell doctor` reports
  the same condition as `initfile-edited` drift.
- Init files are swapped in atomically only at the very end of `apply`; an
  abort before that leaves the old state intact.
- `omnishell doctor` reports drift (missing packages, hand-edited init files,
  missing `source` line, orphaned lockfile entries, shadowed built-ins) and is
  CI-friendly via exit code 3.
- `omnishell uninstall` backs the integration out; restoration from a backup is
  manual in v1 (the path is printed).

## Exit codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | Application error — e.g. package install failed, or ≥1 module degraded |
| 2 | Config / schema error — nothing was changed |
| 3 | Drift detected (`omnishell doctor` only) |

## Not in v1

- fish support
- remote module registry / installing modules from a URL
- profiles (different module sets per machine)
- `omnishell rollback` (backups are written; v1 restoration is manual)
- self-update (`brew` / `install.sh` cover it)
- Windows / PowerShell

## License

MIT — see [LICENSE](LICENSE). © 2026 Jeffry Würmli.
