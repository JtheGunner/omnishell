# Module cheatsheet (`omnishell cheatsheet`) — design

Issue: OMNIS-54. Size: a compact project (one new read-only command, one new
module file type, content for all built-in modules (27 when this was written), docs).

## Intent

After `omnishell apply`, the user's shell has aliases, functions and key
bindings from many modules, and nothing says which. Today the answer is in the
module manifests or in the generated `init.<shell>`. The user wants a short,
readable cheatsheet that describes **this installation**: only the modules that
are active, with the aliases/keys that the chosen options actually produce.

## Goals

- `omnishell cheatsheet` prints, per active module, a short description and what
  the module gives the user: commands, aliases, functions, key bindings.
- The text follows the effective option values (config value, else the default).
  `modern-aliases` with `replace = ["ls"]` mentions `ls`/`ll`, not `cat`.
- Read-only: no writes to disk, config, lockfile or packages. Works before
  `apply` has ever run (it needs `config.toml` only, like `list`).
- User modules and user overrides of built-ins take part with the same file
  format.

## Non-goals

- A category view (all key bindings across modules in one list).
- TUI integration, interactive search, `--json`, HTML/PDF/Markdown export, a pager.
- Localisation: the text is English.
- Documenting tools beyond what a module configures.
- Deriving entries by parsing `zsh.tmpl` / `bash.tmpl` or the generated init file.

## Anti-goals

- A cheatsheet that states a binding or alias the module does not set up. Wrong
  help is worse than none, so every entry must be checked against the module's
  own template (and, for upstream defaults such as fzf's keys, against upstream).
- A second template language or a new condition syntax. The existing template
  functions (`has_item`, `has`, `eq`, `if`) are all authors get.

## Approaches considered

| | Approach | Trade-off |
| --- | --- | --- |
| **1 (chosen)** | `cheatsheet.tmpl` per module, rendered with the module's template context | Text follows options for free; easy to write; no structure (no categories) |
| 2 | Structured `[[cheatsheet]]` entries in `manifest.toml` | Fixed structure and a later category view; manifests get long; option-dependent entries need a new condition syntax |
| 3 | Derive entries from the rendered templates | No text to maintain; cannot see plugin key bindings, gives bare alias lines without explanation |

The user chose approach 1 and the per-module layout.

## Design

### Module file

An optional `cheatsheet.tmpl` next to `manifest.toml` (and `zsh.tmpl`/`bash.tmpl`).
It is Markdown text rendered through `render.Render` with the same `render.Context`
as the shell snippets (`.Options`, `.Platform`, `.Shell`, `.Active`, `has`,
`has_item`, `shellquote`, …). It is one file for all shells; `.Shell` lets it
branch where bash and zsh differ. `Module` gets a `Cheatsheet() (string, bool,
error)` reader next to `Template(shell)`, mirroring it.

### Command

```
omnishell cheatsheet [module-id ...] [--shell zsh|bash]
```

- Lives in `internal/cli/cheatsheet.go`, one file per subcommand like the others.
  It builds the engine with `buildQueryEngine`, the way `list` does, and calls a
  new read-only method; it maps errors through `ClassifyError`.
- **Which modules:** enabled in `config.toml`, applicable on this platform, and
  shipping a snippet for the selected shell. Sorted by id (easier to scan than
  apply order). With module ids given, only those, and an id that is not active is
  an error (config/schema error, exit 2) naming it.
- **Shell:** `--shell`, else the shell in `$SHELL` if omnishell manages it, else the
  first managed shell. A module that does not support the shell is left out.
- **Options:** the effective options, built the same way `ComputePlan` builds a
  module's options (config values validated against the schema, defaults merged).
  Reuse that code; do not re-implement the merge.
- **Output:** Markdown on stdout, readable as plain text in a terminal and pipeable
  into a Markdown viewer:

  ```
  # omnishell cheatsheet (zsh)

  ## Modern CLI aliases (modern-aliases)
  Replace ls/cat/find with eza, bat and fd.

  - `ls`, `ll` — eza with icons and git status
  - `cat` — bat

  ## fzf (fzf)
  …

  No cheatsheet: my-user-module
  ```

  A module without `cheatsheet.tmpl` is listed in the final `No cheatsheet:` line
  (not silently dropped). A module whose packages are not installed carries one
  line, `Packages missing — run 'omnishell apply'.`, taken from the existing
  `modedit` package state.
- **Safety:** all text that comes from a module (name, description, rendered
  body) passes through a line-wise variant of `modedit.Clean` (control and bidi
  characters become spaces, newlines are kept). Add it next to `Clean` and test it.
- **Errors:** a template that fails to render writes a warning naming the module to
  stderr, the other modules are still printed, and the command exits 1 at the end.
  Never swallowed, never fatal for the rest.

### Content for the built-in modules

All built-in modules get a `cheatsheet.tmpl`; `modules/builtin_test.go` fails when one is
missing (same pattern as the pinned-fallback-ref check). A module with nothing the
user types (for example `completion`, `history`) still gets one short entry saying
what changes and that it adds no commands. Every entry is written from the module's
own template and verified against it (aliases, `bindkey` lines, functions), and
for plugin defaults (fzf, atuin, zoxide, …) against the upstream documentation.

### Tests

- Unit tests for the module reader, the active-module selection (disabled, wrong
  platform, wrong shell, unknown id), the option effect (`modern-aliases`,
  `spell-correction`), the missing-file footer, the render-error path, and the
  line-wise clean.
- Golden files per built-in module with default options in
  `modules/testdata/<id>/cheatsheet.golden`, using the existing `-update` flow,
  plus golden variants for the option-dependent modules.
- A CLI test with fakes, in the style of `list_test.go`, including "writes nothing".

### Docs

README command reference, `docs/writing-a-module.md` (folder layout and a
"Cheatsheet" section with the template context), the CLAUDE.md architecture note
only if a package boundary changes. Man page and shell completions are generated
from the Cobra command; nothing to write by hand.

## Calls I made (not discussed with you)

- English text and no localisation; sorted by id; Markdown output; no pager.
- One `cheatsheet.tmpl` per module for all shells, not one per shell.
- Footer for modules without a cheatsheet instead of hiding them.
- Render errors warn and continue, exit 1.
- Shell selection via `--shell` / `$SHELL` / first managed.
- Disabled modules are not shown (the ticket's open question): the command is about
  what is active.

## Left to the builder

- The exact API for "effective options of a module" and for "which modules are
  active here" — reuse what `ComputePlan` / `modedit.Views` already do.
- The final wording of each built-in entry, and how short it is.
- Where the new engine/modedit method lives (`modedit` is the likely home, since
  `list` and the TUI already share it).
