# omnishell — Design Spec

- **Status:** Draft for review
- **Date:** 2026-09-02
- **Repo:** https://github.com/JtheGunner/omnishell.git
- **Local path:** `/Users/jeffry/Projects/omnishell`

---

## 1. Purpose & Scope

`omnishell` is a single-binary CLI that applies **terminal configuration** on
macOS and Linux in a **modular, declarative, reversible** way.

The user maintains a plain local config file listing which *modules* are
enabled (and their options). Running `omnishell apply` then:

1. installs any programs a module needs (via the detected system package
   manager, with a `git` fallback),
2. renders each module's shell snippet,
3. writes everything into a single tool-managed init file per shell, which is
   sourced from `~/.zshrc` / `~/.bashrc` via one marker block,
4. records the result in a lockfile for idempotency, drift detection and
   clean removal.

**Design goals (in priority order):**

1. **Modularity** — adding a new module is dropping in a folder; existing
   modules are structurally never affected.
2. **Safety** — the user's own `.zshrc` is never rewritten beyond one marker
   block; every write is atomic and backed up.
3. **Reproducibility** — same `config.toml` + same module registry ⇒ byte-identical
   init files.
4. **Standalone** — a plain local file, no git repo, no account, no network
   required for normal use.
5. **Maintainability** — one Go package per responsibility, high test coverage.

### Non-negotiable behaviours

- Nothing changes on the system without `omnishell apply` or `omnishell remove`.
  `enable` / `disable` / `set` only edit `config.toml`.
- Every write to an rc file or init file is preceded by a timestamped backup.
- `apply` run twice with no config change is a no-op (no writes, no new backup).
- No `sudo` unless a Linux package manager requires it; when it does, the
  `sudo` invocation is visible and announced.

---

## 2. Decisions (locked)

| # | Decision | Choice |
|---|----------|--------|
| D1 | Language / runtime | **Go** — single static binary, no runtime dependency, `darwin/{arm64,amd64}` + `linux/{amd64,arm64}` |
| D2 | Module delivery | **Declarative manifests** — a module is a folder (`manifest.toml` + templates + optional hook scripts); built-ins via `go:embed`, user modules from `~/.config/omnishell/modules/` |
| D3 | How config is applied to the shell | **One tool-managed init file per shell** (`~/.config/omnishell/init.zsh`, `init.bash`); the rc file gets exactly one `source` marker block |
| D4 | Source of truth | **`~/.config/omnishell/config.toml`**, hand-editable, also mutated by `enable`/`disable`/`set`. Plain local file — no repo, no commit needed. |
| D5 | Package installation | **In scope** — detect `brew`/`apt`/`dnf`/`pacman`/`zypper`/`apk`; `git`-clone fallback where no package exists |
| D6 | Shells for v1 | **zsh + bash** |
| D7 | Architecture | **Manifest engine + lockfile** (approach "A") |
| D8 | v1 built-in modules | `completion`, `history`, `autosuggestions`, `syntax-highlighting`, `fzf`, `zoxide`, `modern-aliases` |

---

## 3. CLI Surface

Binary name: `omnishell` (optional shell alias `oms`, not installed by default).
Command framework: Cobra.

| Command | Behaviour |
|---------|-----------|
| `omnishell init` | Create `~/.config/omnishell/config.toml` (everything disabled) and insert the `source` marker block into the detected shells' rc files. Idempotent. |
| `omnishell list` | List all discoverable modules (built-in + user), showing: enabled/disabled, installed/missing, supported platforms/shells, and whether a user module overrides a built-in. |
| `omnishell enable <id>` | Set `modules.<id>.enabled = true` in `config.toml`. Does **not** run `apply`; prints a hint to do so. |
| `omnishell disable <id>` | Set `modules.<id>.enabled = false`. Does not run `apply`. |
| `omnishell set <id>.<key> <value>` | Set a module option in `config.toml`, validated against that module's option schema. Rejects unknown keys and invalid values (exit 2). |
| `omnishell apply` | Bring system + init files to the desired state. Flags: `--dry-run` (show plan, change nothing), `--yes` (no prompt), `--no-packages` (skip package install/checks), `--force` (overwrite an init file whose content hash shows manual edits), `--verbose`. |
| `omnishell diff` | Alias for `omnishell apply --dry-run`. |
| `omnishell doctor` | Report drift: missing packages, manually edited init files (hash mismatch), missing/broken `source` line, orphaned lockfile entries, user module shadowing a built-in. Exit 3 if any drift is found (CI-friendly). |
| `omnishell remove <id>` | Mutating command (like `apply`): set `enabled = false` **and** immediately roll the module back — rewrite the init files without its section, update the lockfile, backup first. With `--purge`, also uninstall packages omnishell installed for it (`installed_by_omnishell = true` only). Honours `--dry-run` / `--yes`. |
| `omnishell uninstall` | Full rollback: remove all module sections, remove the `source` marker block from rc files, keep or delete `~/.config/omnishell` (prompt / `--purge`). |
| `omnishell version` | Print version stamped via `-ldflags`. |

### Exit codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | Application error (e.g. package install failed, ≥1 module degraded) |
| 2 | Config / schema error — nothing was changed |
| 3 | Drift detected (`doctor` only) |

---

## 4. Config File — `~/.config/omnishell/config.toml`

The only file the user deliberately maintains. Hand-editable or mutated by
`enable`/`disable`/`set`. TOML for comments + good Go libraries
(`go-toml/v2`).

```toml
# ~/.config/omnishell/config.toml
[omnishell]
version = 1                 # schema version, for future migrations
shells  = ["zsh", "bash"]   # which rc files omnishell may manage;
                            # empty / omitted = auto-detect login shell

[modules.completion]
enabled = true

[modules.syntax-highlighting]
enabled = true

[modules.autosuggestions]
enabled = true
[modules.autosuggestions.options]
highlight_style = "fg=8"

[modules.fzf]
enabled = true
[modules.fzf.options]
ctrl_r       = true
ctrl_t       = false
default_opts = "--height 40% --reverse"

[modules.history]
enabled = true
[modules.history.options]
size = 50000

[modules.zoxide]
enabled = false

[modules.modern-aliases]
enabled = false
[modules.modern-aliases.options]
replace = ["ls", "cat", "find"]
```

### Rules

- **Unknown `modules.<id>`** where no such module exists → `apply`/`doctor`
  emit a warning and skip it (typo protection); not fatal.
- **Missing option** → the module's manifest default applies.
- **Invalid option value** → exit 2 with a clear message listing allowed
  values; the system is not touched.
- **Order in the file is irrelevant.** Load order in the init file is
  determined by the dependency graph (§7).
- omnishell **never** rewrites `config.toml` except through
  `enable`/`disable`/`set`, and then preserves comments/formatting as far as
  the TOML library allows, touching only the affected table.

---

## 5. Module Format

A module is a folder. Built-in modules live under `modules/<id>/` in the repo
and are compiled into the binary via `go:embed`. User modules live under
`~/.config/omnishell/modules/<id>/` with the **identical format**, read at
runtime, no rebuild. On a name clash the **user module wins** (override) and
`doctor` flags it.

```
modules/fzf/
├── manifest.toml        # required: metadata, packages, option schema, deps
├── zsh.tmpl             # snippet template for zsh (Go text/template)
├── bash.tmpl            # snippet template for bash
└── hooks/               # optional: escape hatch for complex logic
    ├── check.sh         #   exit 0 = "present / ok", exit 1 = "missing"
    ├── install.sh       #   only when the package-manager path is insufficient
    └── remove.sh
```

### `manifest.toml`

```toml
[module]
id          = "fzf"
name        = "FZF Fuzzy Finder"
description = "Ctrl+R history search as a searchable list"
version     = "1.0.0"          # module's own version; recorded in the lockfile
schema      = 1                # manifest schema version (compatibility gate)

platforms = ["macos", "linux"]   # where the module is applicable at all
shells    = ["zsh", "bash"]      # which shells it ships a snippet for

# Other modules that must load first (order in the init file).
#   requires = hard dependency (missing → error)
#   after    = soft ordering (only sorts when both are active)
requires = []
after    = ["completion"]

# Package installation: the concrete package name per package manager.
# A missing PM entry falls back to [[packages.fallback]], else skip + warn.
[packages]
brew   = ["fzf"]
apt    = ["fzf"]
dnf    = ["fzf"]
pacman = ["fzf"]
zypper = ["fzf"]
apk    = ["fzf"]

[[packages.fallback]]           # optional, when no package is available
type = "git"
repo = "https://github.com/junegunn/fzf.git"
dest = "{{.VendorDir}}/fzf"
run  = "{{.VendorDir}}/fzf/install --bin"   # optional build command

# Option schema: allowed keys, types, defaults, validation.
[options.ctrl_r]
type    = "bool"
default = true
help    = "Bind Ctrl+R to the fzf history widget"

[options.default_opts]
type    = "string"
default = "--height 40% --reverse"
help    = "FZF_DEFAULT_OPTS"

[options.replace]              # example: enum list
type    = "list<enum>"
values  = ["ls", "cat", "find"]
default = []
```

**Option schema types (v1):** `bool`, `string`, `int`, `enum` (with
`values`), `list<string>`, `list<enum>` (with `values`). Each option table
carries `type`, `default`, optional `help`, and for enums `values`.

### Templates (`zsh.tmpl` / `bash.tmpl`)

Plain Go `text/template`. Variables available:

| Variable | Meaning |
|----------|---------|
| `.Options` | validated, default-filled module options (map) |
| `.Platform` | `"macos"` or `"linux"` |
| `.Shell` | `"zsh"` or `"bash"` |
| `.VendorDir` | `~/.config/omnishell/vendor` |
| `.ConfigDir` | `~/.config/omnishell` |
| `.Bin` | map of tool name → path for binaries omnishell installed |

Template functions: `shellquote`, `pathjoin`, `has "<module-id>"` (is another
module active?).

If a `.tmpl` for an active shell is missing, the module is treated as
**not supported on that shell** (hint, not an error).

Example `zsh.tmpl`:

```zsh
# fzf key-bindings
{{ if .Options.ctrl_r -}}
source "$(brew --prefix fzf 2>/dev/null)/shell/key-bindings.zsh" 2>/dev/null || \
  source /usr/share/fzf/key-bindings.zsh 2>/dev/null
{{- end }}
export FZF_DEFAULT_OPTS={{ .Options.default_opts | shellquote }}
```

### Hook scripts (`hooks/*.sh`)

Optional. The escape hatch for cases `manifest.toml` + template cannot cover.
Invoked with a POSIX `sh`. They receive the same values as environment
variables: `OMNISHELL_VENDOR_DIR`, `OMNISHELL_CONFIG_DIR`, `OMNISHELL_PLATFORM`,
`OMNISHELL_SHELL`, and `OMNISHELL_OPT_<KEY>` for each option.

- `check.sh` — exit 0 = requirement satisfied, exit 1 = missing. Consulted
  when a module declares no `[packages]` for the detected manager, or to
  gate rendering.
- `install.sh` — run only when `check.sh` fails and the package path did not
  satisfy the requirement.
- `remove.sh` — run by `omnishell remove <id> --purge`.

### Per-module lifecycle during `apply`

1. **resolve** — check platform/shell; validate options against schema; fill defaults.
2. **packages** — for each declared package: already present? else install via
   the detected PM (or `hooks/check.sh` → `hooks/install.sh`, or
   `[[packages.fallback]]`). Anything omnishell installs is marked
   `installed_by_omnishell` in the lockfile.
3. **render** — template → snippet string.
4. The **engine** (not the module) writes that snippet into the module's
   section of the init file. This separation keeps modules pure data + text.

---

## 6. Init File, rc Integration, Safety

### Managed init file

One fully generated file per managed shell:
`~/.config/omnishell/init.zsh` and `~/.config/omnishell/init.bash`.

```zsh
# ─────────────────────────────────────────────────────────────
#  GENERATED BY omnishell — DO NOT EDIT
#  Source of truth: ~/.config/omnishell/config.toml
#  Regenerate:      omnishell apply
#  Generated:       2026-09-02T22:41:03+02:00
#  Content hash:    sha256:0f3a…   (checked by `omnishell doctor`)
# ─────────────────────────────────────────────────────────────

# >>> omnishell:completion (v1.0.0) >>>
autoload -Uz compinit && compinit
zstyle ':completion:*' matcher-list 'm:{a-zA-Z}={A-Za-z}'
# <<< omnishell:completion <<<

# >>> omnishell:syntax-highlighting (v1.0.0) >>>
source /opt/homebrew/share/zsh-syntax-highlighting/zsh-syntax-highlighting.zsh
# <<< omnishell:syntax-highlighting <<<
```

- Each module section wrapped in `>>> omnishell:<id> (v<ver>) >>>` /
  `<<< omnishell:<id> <<<` markers.
- Section order = topological sort of the dependency graph (§7).
- Written **atomically** (`tmp` file + `rename`); the previous file is copied
  to `backups/<ts>/` first.
- The header `Content hash` covers the concatenated module sections; `doctor`
  and `apply` use it to detect "edited by hand". `apply` refuses to overwrite
  a hand-edited init file without `--force`.

### rc integration

omnishell inserts **exactly one** marker block, at the end of `.zshrc` /
`.bashrc`:

```zsh
# >>> omnishell >>>
[[ -f "$HOME/.config/omnishell/init.zsh" ]] && source "$HOME/.config/omnishell/init.zsh"
# <<< omnishell <<<
```

- Block missing → `init` / `apply` adds it (rc file backed up first).
  Block already present → left untouched.
- End-of-file position is deliberate: `syntax-highlighting` and
  `autosuggestions` must load after everything else.
- The rc file is otherwise **never** modified. `uninstall` removes exactly
  this block.

### Dependency graph & ordering

- Nodes = active modules. Edges from `requires` (hard) + `after` (soft).
- `requires` pointing at an inactive/missing module → exit 2 with a clear
  message (`"fzf requires completion, which is disabled"`).
- A cycle → exit 2 naming the cycle.
- Topological sort; ties broken stably by module `id` ⇒ deterministic,
  reproducible init file.
- Built-in base ordering via `after`:
  `completion` → `history` → `fzf` / `zoxide` / `modern-aliases` →
  `autosuggestions` → `syntax-highlighting` (last two load last).

### Safety net (summary)

| Risk | Mitigation |
|------|------------|
| Corrupting the rc file | Single marker block; atomic write; backup per change |
| Hand-edited init file lost on `apply` | Hash mismatch → `doctor` + `apply` warn; `--force` required to overwrite; previous version in backup |
| Package install fails | Module marked `degraded`; its init section is written only if `check` passes, else omitted + warning; other modules proceed |
| Abort mid-`apply` | Init file is swapped in atomically only at the end; on failure the old state remains |
| Rollback | `backups/<ts>/` holds rc + init files; restoration is manual in v1 (path is printed); `omnishell rollback` is a post-v1 command |

---

## 7. Lockfile & `apply` Algorithm

### Lockfile — `~/.config/omnishell/state.lock.json`

Machine-managed, not for editing. Purpose: idempotency, clean `remove`,
drift detection.

```json
{
  "schema": 1,
  "omnishell_version": "1.0.0",
  "last_apply": "2026-09-02T22:41:03+02:00",
  "platform": "macos",
  "package_manager": "brew",
  "modules": {
    "fzf": {
      "module_version": "1.0.0",
      "enabled": true,
      "options_hash": "sha256:1a2b…",
      "shells_rendered": ["zsh", "bash"],
      "packages": [
        { "name": "fzf", "manager": "brew", "installed_by_omnishell": true }
      ],
      "vendor_paths": [],
      "status": "ok"
    }
  },
  "init_files": {
    "zsh":  { "path": "~/.config/omnishell/init.zsh",  "content_hash": "sha256:0f3a…" },
    "bash": { "path": "~/.config/omnishell/init.bash", "content_hash": "sha256:77c1…" }
  },
  "rc_files": {
    "zsh":  { "path": "~/.zshrc",  "block_present": true },
    "bash": { "path": "~/.bashrc", "block_present": true }
  }
}
```

`installed_by_omnishell` distinguishes "omnishell installed `fzf` via brew"
from "it was already there". Only the former is removed by `remove --purge`.

`status` per module: `ok` | `degraded` (package missing, section omitted) |
`disabled`.

### `apply` algorithm (deterministic, fail-safe)

```
1. load config.toml            → error? exit 2, nothing changed
2. load module registry        (embedded built-ins + ~/.config/omnishell/modules/)
3. resolve                     active modules ∩ platform; validate options
4. build dependency graph      requires/after; cycle / missing → exit 2
5. topo-sort                   stable by id
6. detect package manager      brew | apt | dnf | pacman | zypper | apk  (once)
7. PLAN phase (no writes):
      per module: which packages are missing; which section is
                  new / changed / removed
      → on --dry-run / diff: print the plan, STOP
8. CONFIRM                     show the plan, prompt (unless --yes)
9. APPLY phase:
      a. install missing packages; per module result: ok | degraded
      b. run hooks/check.sh for modules without a package declaration
      c. render all ok modules → assemble init-file content in memory
      d. backup rc + old init files → backups/<ts>/
      e. write init.zsh / init.bash atomically
      f. ensure the rc marker block (atomic)
      g. write state.lock.json
10. REPORT                     per module: applied / degraded / skipped + hints
                              exit 0, or 1 if ≥1 module degraded
```

- **Removed modules:** a module present in the lockfile but now
  `disabled`/gone → its section is omitted from the init file;
  `installed_by_omnishell` packages stay (removal only via
  `remove --purge`, deliberately conservative).
- **Nothing to do:** `apply` compares `options_hash` + `module_version` +
  registry hash; if all current → "nothing to do", no writes, no new backup.

### Package manager abstraction

```go
type PackageManager interface {
    Name() string                          // "brew"
    Detect() bool                          // in PATH and usable?
    IsInstalled(pkg string) (bool, error)
    Install(pkgs []string) error           // invokes sudo visibly where required
    NeedsSudo() bool
}
```

- Detection order: macOS → `brew`; Linux → `apt` → `dnf` → `pacman` →
  `zypper` → `apk` (first whose `Detect()` succeeds).
- No `brew` on macOS → clear message with an install hint; `apply` still
  completes for config-only modules (`history`, `completion`), package-dependent
  ones become `degraded`.
- `--no-packages` → steps 9a/9b skipped; only modules whose `check` already
  passes are rendered.
- Adding a new manager = one new file implementing the interface + an entry
  in the detection order. Module manifests gain a new `[packages.<name>]` key.

---

## 8. Repo Layout, Error Handling, Testing, Distribution

### Repo layout

```
omnishell/
├── cmd/omnishell/main.go            # wiring only: Cobra root, version
├── internal/
│   ├── cli/                         # one file per command: init, list, enable,
│   │                                #   disable, set, apply, diff, doctor,
│   │                                #   remove, uninstall, version
│   ├── config/                      # load/write/validate config.toml
│   ├── module/                      # manifest parser, registry, option schema
│   ├── graph/                       # dependency graph, topo-sort, cycle check
│   ├── render/                      # text/template + template functions
│   ├── initfile/                    # build init file, markers, atomic write, hash
│   ├── rcfile/                      # insert/remove rc block, backup
│   ├── lockfile/                    # read/write state.lock.json
│   ├── pkg/                         # PackageManager interface + brew/apt/…
│   ├── platform/                    # OS / arch / shell detection
│   ├── plan/                        # PLAN phase: compute & render the diff
│   └── backup/                      # manage backups/<ts>/
├── modules/                         # BUILT-IN modules (go:embed)
│   ├── completion/
│   ├── history/
│   ├── autosuggestions/
│   ├── syntax-highlighting/
│   ├── fzf/
│   ├── zoxide/
│   └── modern-aliases/
├── docs/
│   ├── superpowers/specs/           # this design spec
│   └── writing-a-module.md          # "your own module in 5 minutes"
├── testdata/                        # fixture configs, fake-HOME trees
├── .github/workflows/ci.yml         # test + lint + goreleaser (on tag)
├── .goreleaser.yaml
├── go.mod
└── README.md
```

Many small files; one package = one responsibility. `internal/` because
none of it is a public library API.

### Error handling

- A single error type carrying a category → exit-code mapping
  (`ConfigError`→2, `DriftError`→3, else 1).
- Every error states **what**, **where** (file:line for TOML/manifest), and
  **how to fix**. No bare Go panics at the surface — `recover` in the root
  command, then a formatted message + exit 1.
- `apply` is transactional up to step 9c: anything before that can abort with
  no effect. From 9d on, backups exist → on failure the backup path is printed.
- `--verbose` adds structured step/command/path logging; default output is
  terse and human-readable.

### Testing (target ≥ 80% coverage)

| Level | Content |
|-------|---------|
| Unit | manifest parser, option validation, topo-sort incl. cycle/missing, template rendering, marker replacement in the init file, lockfile round-trip, idempotent rc-block insert/remove |
| Golden files | per built-in module: `manifest + options → expected zsh/bash snippet` as `.golden`, updated with a `-update` flag |
| Integration (fake HOME) | full `apply` against a temp `$HOME` with a fake `.zshrc`; `PackageManager` mocked (nothing installed for real); asserts init file, rc block, lockfile, idempotency (2× apply identical), `remove`/`doctor` behaviour, drift detection after a manual edit |
| E2E (CI, opt-in) | per-distro containers (`debian`, `fedora`, `arch`, `alpine`) run a real `apply` with the real PM for 1–2 modules; macOS runner with `brew`. Separate CI job, not in the fast PR run |

No network in unit/integration. The real PM is touched only in E2E.

### Distribution

- **GoReleaser** on a git tag builds binaries for `darwin/{arm64,amd64}` and
  `linux/{amd64,arm64}` → GitHub Releases.
- **Homebrew tap** `JtheGunner/homebrew-tap` (maintained by GoReleaser) →
  `brew install jthegunner/tap/omnishell`.
- **`install.sh`** (curl-pipe) in the repo: detects OS/arch, downloads the
  matching binary to `~/.local/bin`. For servers without brew.
- **`go install github.com/JtheGunner/omnishell/cmd/omnishell@latest`** for Go users.
- Version stamped into the binary via `-ldflags`; `omnishell version` prints
  it and the lockfile records it.

---

## 9. v1 Built-in Modules

| id | packages | shells | options | notes |
|----|----------|--------|---------|-------|
| `completion` | — (config only) | zsh, bash | — | `autoload -Uz compinit && compinit`; case-insensitive `matcher-list`. bash: `bash-completion` sourcing if present. |
| `history` | — (config only) | zsh, bash | `size` (int, default 50000) | `HISTSIZE`/`SAVEHIST`, `HIST_IGNORE_ALL_DUPS`, `HIST_SAVE_NO_DUPS`, `INC_APPEND_HISTORY`, `SHARE_HISTORY`; prefix search on arrow keys (`up/down-line-or-beginning-search`). bash: `HISTSIZE`/`HISTFILESIZE`, `histappend`, `HISTCONTROL=ignoreboth`. |
| `autosuggestions` | `zsh-autosuggestions` (brew/apt/…); git fallback → `{{.VendorDir}}/zsh-autosuggestions` | zsh | `highlight_style` (string, default `fg=8`) | Sources the plugin; sets `ZSH_AUTOSUGGEST_HIGHLIGHT_STYLE`. `shells = ["zsh"]` only. |
| `syntax-highlighting` | `zsh-syntax-highlighting` (brew/apt/…); git fallback | zsh | — | Must load last; `after` all other modules. `shells = ["zsh"]` only. |
| `fzf` | `fzf` (all PMs); git fallback → `{{.VendorDir}}/fzf` + `install --bin` | zsh, bash | `ctrl_r` (bool, true), `ctrl_t` (bool, false), `default_opts` (string) | Sources fzf key-bindings + completion; the "Ctrl+R list". |
| `zoxide` | `zoxide` (all PMs); git fallback build | zsh, bash | `cmd` (string, default `z`) | `eval "$(zoxide init <shell>)"`. |
| `modern-aliases` | `eza`, `bat`, `fd` (per selection) | zsh, bash | `replace` (`list<enum>` of `ls`,`cat`,`find`, default `[]`) | Installs only the tools needed for the selected aliases; sets `ls`→`eza …`, `cat`→`bat`, `find`→`fd`. |

`syntax-highlighting` and `autosuggestions` carry `after` edges to the other
modules so they always render last, with `syntax-highlighting` after
`autosuggestions`.

---

## 10. Explicitly NOT in v1 (YAGNI)

- **fish support** — different syntax; later as a third per-module template.
- **Remote module registry / `omnishell install <url>`** — v1 knows only
  local + embedded modules.
- **Profiles** (`work-mac`, `server-minimal` with different module sets).
- **`omnishell rollback` command** — backups are written; v1 restoration is
  manual (path printed).
- **Self-update** (`omnishell upgrade`) — covered by brew / `install.sh`.
- **TUI / interactive module browser** — flag-only CLI.
- **Windows / PowerShell.**
- **Package version pinning** — omnishell installs the PM's "latest".
- **Parallel installs** — sequential, predictable.

### Sensible next steps after v1 (notes, not commitments)

fish support · profiles · `rollback` command · remote modules · more
built-ins (starship / prompt themes, `direnv`, `atuin`, keychain / ssh-agent,
`eza` config, `bat` themes).

---

## 11. Open questions for reviewer

- None outstanding. All D1–D8 decisions confirmed during brainstorming.
