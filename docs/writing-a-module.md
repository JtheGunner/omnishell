# Writing an omnishell module

A module is a folder. Built-in modules live in `modules/<id>/` in the repo and
are compiled into the binary; **your** modules live under
`~/.config/omnishell/modules/<id>/` in the exact same format, read at runtime
with no rebuild. If a user module has the same `id` as a built-in, the user
module wins and `omnishell doctor` flags the shadowing.

## Folder layout

```
~/.config/omnishell/modules/direnv/
├── manifest.toml     # required: metadata, platforms/shells, deps, packages, option schema
├── zsh.tmpl          # optional: the zsh snippet (Go text/template)
├── bash.tmpl         # optional: the bash snippet
└── hooks/            # optional: escape hatch for logic the manifest can't express
    ├── check.sh      #   exit 0 = requirement satisfied, exit 1 = missing
    ├── install.sh    #   run when check.sh fails and the package path didn't help
    └── remove.sh     #   run by `omnishell remove <id> --purge`
```

If a module ships no `.tmpl` for an active shell, it is treated as **not
supported on that shell** (a hint, not an error).

## Annotated `manifest.toml`

TOML scoping matters: the root-level keys (`platforms`, `shells`, `requires`,
`after`, `conflicts`) must appear **before** the first `[table]`, otherwise the
parser reads them as belonging to that table.

```toml
# ─── root-level keys FIRST (before any [table]) ───

platforms = ["macos", "linux"]   # where the module is applicable at all
shells    = ["zsh", "bash"]      # which shells it ships a snippet for

requires  = []                   # hard dependencies: another module that MUST
                                 #   be active first; missing/disabled → exit 2
after     = ["completion"]       # soft ordering: only sorts relative to modules
                                 #   that are also active; never an error
conflicts = []                   # incompatible modules: if any listed id is also
                                 #   active, apply/doctor/diff stop with exit 2.
                                 #   One-directional — either side declaring it is
                                 #   enough. Cannot overlap `requires`.

# ─── module metadata ───

[module]
id          = "direnv"           # must match the folder name
name        = "direnv"           # human-readable label for `omnishell list`
description = "Per-directory environment variables via .envrc files"
homepage    = "https://github.com/direnv/direnv"  # optional: link to the upstream
                                 #   project, shown in the README and `omnishell
                                 #   list --json`; omit for config-only modules or
                                 #   ones bundling several tools with no single home
version     = "1.0.0"            # the module's own version; recorded in the lockfile
schema      = 1                  # manifest schema version (compatibility gate)

# ─── packages: the concrete package name per package manager ───
# The engine installs the list for the ONE detected manager. A manager with no
# entry falls through to [[packages.fallback]]; if there's no fallback either,
# the module is skipped with a warning. The six recognised keys:

[packages]
brew   = ["direnv"]
apt    = ["direnv"]
dnf    = ["direnv"]
pacman = ["direnv"]
zypper = ["direnv"]
apk    = ["direnv"]

# ─── optional git fallback, used when the detected manager has no entry ───

[[packages.fallback]]
type = "git"                                    # only "git" in v1
repo = "https://github.com/direnv/direnv.git"   # clone source
dest = "{{.VendorDir}}/direnv"                  # clone target (templated)
run  = "make -C {{.VendorDir}}/direnv install"  # optional build/install command

# ─── option schema: allowed keys, types, defaults, validation ───
# Each option table carries `type`, `default`, optional `help`, and — for enum
# and list<enum> — a `values` list. Types: bool | string | int | enum |
# list<string> | list<enum>.

[options.log_format]
type    = "string"
default = ""
help    = "DIRENV_LOG_FORMAT value; empty silences direnv's own logging"

[options.whitelist]
type    = "list<string>"
default = []
help    = "Directories direnv trusts without an explicit `direnv allow`"

# enum example (from modern-aliases):
#   [options.replace]
#   type    = "list<enum>"
#   values  = ["ls", "cat", "find"]
#   default = ["ls", "cat", "find"]

# A type = "string" option may also carry `pattern`, a regexp every accepted
# value must match. Use it to constrain a value that lands in shell command
# position to a safe shape (from zoxide):
#   [options.cmd]
#   type    = "string"
#   default = "z"
#   pattern = "^[A-Za-z_][A-Za-z0-9_-]*$"
```

`omnishell set <id>.<key> <value>` validates against this schema and rejects
unknown keys, invalid values, or values that fail an option's `pattern` with
exit code 2 (nothing is changed). A missing option falls back to the manifest
`default`.

## Templates

`zsh.tmpl` / `bash.tmpl` are plain Go `text/template`. An unknown field is an
error (templates run with `missingkey=error`). Available context:

| Field | Meaning |
|-------|---------|
| `.Options.<key>` | validated, default-filled option value (map) |
| `.Platform` | `"macos"` or `"linux"` |
| `.Shell` | `"zsh"` or `"bash"` |
| `.VendorDir` | the `vendor/` path inside the config dir |
| `.ConfigDir` | the omnishell config dir |
| `.Bin.<tool>` | path to a binary omnishell installed (map; empty if omnishell didn't install it) |
| `.Active.<id>` | whether another module is active (map; prefer the `has` func) |

Template functions:

| Func | Use |
|------|-----|
| `shellquote` | single-quote a value safely for the shell: `{{ .Options.log_format \| shellquote }}` |
| `pathjoin` | join path segments: `{{ pathjoin .VendorDir "direnv" "bin" }}` |
| `has` | is another module active? `{{ if has "fzf" }}…{{ end }}` |
| `has_item` | is a value in a list option? `{{ if has_item .Options.replace "ls" }}…{{ end }}` |

The engine — not the module — writes the rendered snippet into the module's
section of `init.<shell>`, so templates stay pure data + text.

### Quoting is not optional

The generated init file is shell **code**, sourced at every shell startup. Every
`{{ .Options.<string-or-list> }}` value that lands in command position — an
argument to a command, the right-hand side of an `export`, anything the shell
parses — MUST be piped through `shellquote`. Numbers and bools are safe
unquoted. A missed `shellquote` on a free-form string option is arbitrary code
execution: `omnishell set <id>.<key> 'x; rm -rf ~'` would otherwise run at every
login. The `modules` test suite fails the build if a builtin template emits a
string option without `shellquote`.

## `requires` vs `after` vs `conflicts`

- `requires` is a **hard** dependency. If module A `requires` B and B is not
  active (disabled or missing), `apply` / `doctor` stop with exit 2 and name
  the problem.
- `after` is **soft** ordering. It only affects the sort order of the init
  file, and only when both modules are active. An `after` entry pointing at an
  inactive module is silently ignored.
- `conflicts` is a **hard** incompatibility. If module A lists B in `conflicts`
  and both are active, `apply` / `doctor` / `diff` stop with exit 2
  (`module "A" conflicts with "B", which is also enabled`). It is
  **one-directional**: only one of the two modules needs to declare it. An entry
  pointing at an inactive module is ignored. `enable` does *not* pre-check it —
  the same as `requires` — so a conflict surfaces at the next plan, not at
  `omnishell enable`. An id cannot appear in both `requires` and `conflicts` of
  the same manifest. Using `conflicts` requires an omnishell build new enough to
  know the key (older builds reject the manifest as having an unknown key).

Sections are topologically sorted, ties broken by `id`, so the same config
always produces a byte-identical init file.

## Hook contract

Hooks are optional POSIX `sh` scripts in `hooks/`. They receive the module
context as environment variables:

| Variable | Value |
|----------|-------|
| `OMNISHELL_VENDOR_DIR` | the `vendor/` path |
| `OMNISHELL_CONFIG_DIR` | the config dir |
| `OMNISHELL_PLATFORM` | `macos` or `linux` |
| `OMNISHELL_SHELL` | `zsh` or `bash` |
| `OMNISHELL_OPT_<UPPERCASE_KEY>` | one per option, e.g. `OMNISHELL_OPT_LOG_FORMAT` |

- `check.sh` — exit `0` = requirement satisfied, exit `1` = missing. Consulted
  when the module declares no `[packages]` entry for the detected manager, or
  to gate rendering.
- `install.sh` — run only when `check.sh` fails and the package path did not
  satisfy the requirement.
- `remove.sh` — run by `omnishell remove <id> --purge`.

## Worked example: a `direnv` module

`~/.config/omnishell/modules/direnv/manifest.toml`:

```toml
platforms = ["macos", "linux"]
shells    = ["zsh", "bash"]
requires  = []
after     = ["completion"]

[module]
id          = "direnv"
name        = "direnv"
description = "Per-directory environment variables via .envrc files"
homepage    = "https://github.com/direnv/direnv"
version     = "1.0.0"
schema      = 1

[packages]
brew   = ["direnv"]
apt    = ["direnv"]
dnf    = ["direnv"]
pacman = ["direnv"]
zypper = ["direnv"]
apk    = ["direnv"]

[[packages.fallback]]
type = "git"
repo = "https://github.com/direnv/direnv.git"
dest = "{{.VendorDir}}/direnv"
run  = "make -C {{.VendorDir}}/direnv install"

[options.log_format]
type    = "string"
default = ""
help    = "DIRENV_LOG_FORMAT value; empty silences direnv's own logging"
```

`~/.config/omnishell/modules/direnv/zsh.tmpl`:

```zsh
{{- if .Bin.direnv -}}
eval "$({{ .Bin.direnv }} hook zsh)"
{{- else -}}
command -v direnv >/dev/null 2>&1 && eval "$(direnv hook zsh)"
{{- end }}
{{- if .Options.log_format }}
export DIRENV_LOG_FORMAT={{ .Options.log_format | shellquote }}
{{- end }}
```

`~/.config/omnishell/modules/direnv/bash.tmpl`:

```bash
{{- if .Bin.direnv -}}
eval "$({{ .Bin.direnv }} hook bash)"
{{- else -}}
command -v direnv >/dev/null 2>&1 && eval "$(direnv hook bash)"
{{- end }}
{{- if .Options.log_format }}
export DIRENV_LOG_FORMAT={{ .Options.log_format | shellquote }}
{{- end }}
```

Then:

```sh
omnishell list                      # direnv now appears, src = user
omnishell enable direnv
omnishell set direnv.log_format ""   # optional; shown here for completeness
omnishell apply
exec $SHELL
```

## Testing your module

```sh
omnishell apply --dry-run   # (or: omnishell diff) show the plan, change nothing
omnishell apply --no-packages --dry-run   # skip package checks while iterating
omnishell doctor            # after applying: confirm no drift (exit 0)
```

`apply --dry-run` prints which packages would be installed and which init
sections are new / changed / removed. Once applied, `omnishell doctor` should
exit `0`; if you hand-edit `init.<shell>` afterwards it will report
`initfile-edited` drift and `apply` will refuse to overwrite without `--force`.
