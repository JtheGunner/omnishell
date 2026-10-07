# Interactive TUI mode (OMNIS-14)

## Goal

`omnishell tui` is a guided, full-screen way to pick modules: browse them with
their description, homepage and package status, toggle them, edit their
options, preview the plan, and hand off to `apply`. New users no longer have to
read `list` and then run `enable` one module at a time.

The TUI is additive. It replaces no existing command and writes only
`config.toml`, under exactly the rules `enable`, `disable` and `set` enforce.
`apply` stays the only command that mutates the system.

Out of scope: editing list-valued options, authoring user modules, mouse
support, theming, driving the interactive UI in the E2E sandbox.

## Architecture

```text
internal/cli/tui.go         cobra command; TTY guard; wires the backend; runs the
                            program; after an "apply" exit calls runApply
internal/tui/               Bubbletea model/update/view; imports neither cli nor engine
internal/modedit/ (new)     rules shared by the CLI commands and the TUI
```

### `internal/modedit`

Takes over logic that is inline in the CLI handlers today, behaviour unchanged:

- `Enable(id)` / `Disable(id)`: unknown-id check, shell-compatibility check
  (`checkShellCompatible`), then `config.SetEnabled`.
- `SetOption(id, key, raw)`: unknown-key check, `OptionSchema.ParseValue`, then
  `config.SetOption`.
- `Views()`: the per-module data `list` builds today (status, packages,
  platforms, shells, homepage, source) as typed `ModuleView` values, not
  formatted strings.
- Errors stay `config.Error`, so `ClassifyError` and the exit codes keep
  working.

`enable.go`, `set.go` and `list.go` become thin callers. Their existing tests
are the regression net for the extraction.

### `internal/tui`

Consumes a narrow interface declared in the tui package:

```go
type Backend interface {
    Modules() []modedit.ModuleView
    Enable(id string) error
    Disable(id string) error
    SetOption(id, key, raw string) error
    Plan() (string, error) // rendered dry-run plan; no side effects
}
```

`internal/cli/tui.go` builds the real implementation from the engine and the
config path; tests pass a fake. `Plan()` wraps `ComputePlan` and `RenderPlan`
only, so `ComputePlan` stays side-effect-free. The TUI never calls `Apply`.

### Invariants kept

- Only `config.toml` is written, atomically through `atomicfile`.
- `apply` is the only mutating path.
- The new dependencies appear only under `internal/tui` and
  `internal/cli/tui.go`.

## Screens and interaction

One full-screen program; the model is a single struct with a `screen` enum and
`Update` is a pure function of `(model, msg)`.

### Browser (home)

- Left pane: filterable module list with a status checkbox read from
  `config.toml`. Right pane: detail for the selected module (description,
  homepage, package status, platforms, shells, option count).
- **Space** calls `Enable` or `Disable` immediately and writes `config.toml`,
  so the file always matches the screen. A rejected toggle (for example a
  zsh-only module on a bash-only host) shows the `modedit` error in a status
  line and leaves the checkbox unchanged.
- **/** filters by id or description. Modules that cannot run on this host are
  dimmed, not hidden, with the reason in the detail pane.
- A header counter shows the number of changes since launch.
- Visible state is refreshed from `Views()` after every write.

### Options editor (`o`)

One row per option of the selected module, by schema type: bool toggles with
Space, enum options (`values`) cycle with ←/→, string and int options use an
inline text input. Enter validates through `SetOption`; errors, including
`pattern` mismatches, show inline and nothing is written. Esc goes back.

### Plan preview (`a`)

Shows the rendered dry-run plan in a scrollable viewport. **y** or **Enter**
exits the TUI and hands off to `apply`; **n** or Esc returns to the browser. A
no-op plan says so and offers only Back.

### Quit

`q` or Ctrl-C exits without applying. Every toggle is written at once, so there
is no unsaved state.

## Apply handoff

The model sets `applyRequested` and returns `tea.Quit`. `tui.Run` returns a
`Result{ApplyRequested bool}` after Bubbletea has restored the terminal, and
`internal/cli/tui.go` then calls the existing `runApply(cmd, false)`. Prompts,
sudo, streamed output, the hand-edit guard, `ErrDegraded` and the exit codes
behave exactly as with `omnishell apply`.

`apply`'s own confirmation prompt still runs after the plan preview. The
preview is informational; the prompt remains the gate, and the TUI adds no way
around it.

## Startup guards and failure behavior

Checked in `cli/tui.go` before the program starts:

- stdin and stdout must be TTYs, otherwise exit 2 with a message pointing to
  `list`, `enable` and `apply` for scripts.
- Missing `config.toml`: print `run 'omnishell init' first`, exit 2. The TUI
  does not create the config.
- Malformed config: exit 2 with the usual `config.Error`, before any screen
  draws.
- Below a minimum terminal size (80×20), a single "terminal too small" line
  replaces the layout.

Inside the program:

- Backend errors never crash it; they go to a status line and the model stays
  consistent.
- A failed `Plan()` shows the error on the preview screen with Back only.
- External edits to `config.toml` are tolerated: `editTable` re-reads the file
  on every write and the screen refreshes from `Views()`.
- Ctrl-C restores the terminal and leaves `config.toml` intact.

## Dependencies and release impact

- New runtime dependencies: `charmbracelet/bubbletea`, `charmbracelet/lipgloss`
  and their transitive packages. Test-only: `teatest`. `govulncheck` in CI
  covers them.
- The binary-size delta is measured before and after and recorded in the PR.
- The release targets (including 32-bit ARM) must still build; the CI build
  matrix is the check.
- README gains a short `tui` entry and `CHANGELOG.md` an Unreleased line.

## Testing

| Layer | What | How |
|---|---|---|
| `modedit` | enable/disable/set rules, `Views()` | table-driven tests against temp `config.toml` files: unknown id, shell-incompatible module, bad option value, pattern mismatch, comment preservation. Reuses the existing CLI cases, moved down a layer. |
| `internal/tui` model | `Update` transitions: navigation, filter, toggle success and failure, option validation, screen changes, quit vs apply request | unit tests feeding `tea.Msg` values to the model with a fake `Backend`; no terminal needed. |
| `internal/tui` rendering | browser, options and preview screens at a fixed size | a small set of golden-file tests with colors disabled; goldens are updated deliberately. |
| `internal/cli` | TTY guard, missing or malformed config, handoff reaches `runApply` | cobra-level tests with injected TTY detection and the existing prompt and runner test hooks. |

Failure paths get their own cases: backend errors, an empty module list, a
too-small terminal, a no-op plan. `test/e2e/run.sh` is unchanged.

## Sub-tasks

Each becomes its own YouTrack issue linked to OMNIS-14, with its own branch and
PR. Order: 1 → 2 → 3 → 4 → 5; 4 and 5 are independent once 3 lands.

1. **Extract `modedit`.** Behaviour-preserving refactor of the enable/set rules
   and the `list` view builder. No new dependencies, no TUI.
2. **TUI skeleton and browser.** Dependencies, the `tui` command, the TTY
   guard, the read-only browser with detail pane and filter, the model and
   golden-test harness.
3. **Toggle.** Space writes `config.toml` through `modedit`; error status line
   and changes counter.
4. **Options editor.** The screen, per-type inputs and validation.
5. **Plan preview and apply handoff.** `Backend.Plan()`, the preview screen,
   the exit-and-`runApply` handoff, README and CHANGELOG entries.

An implementation plan is written per sub-task when it is picked up, starting
with sub-task 1.
