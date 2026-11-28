# Module Cheatsheet Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `omnishell cheatsheet` prints, per active module, what it gives the user (aliases, functions, key bindings), rendered from a per-module `cheatsheet.tmpl` with the user's effective option values.

**Architecture:** A module may ship `cheatsheet.tmpl` (Markdown, rendered with the same `render.Context` as `zsh.tmpl`). A new read-only `modedit.Editor.Cheatsheet` selects the active modules for a shell, validates options with `module.ValidateOptions`, renders each file and returns structured results. A thin Cobra command formats them. All 25 built-in modules get a file, enforced by a test.

**Tech Stack:** Go, Cobra, `text/template` via `internal/render`, golden-file tests.

**Spec:** `docs/superpowers/specs/2026-11-28-module-cheatsheet-design.md`

## Global Constraints

- Text from a module (name, description, rendered body) reaches the terminal only through `modedit.CleanLines` (line-wise `modedit.Clean`, newlines kept).
- The command is read-only: no writes to config, lockfile, init files or packages. It uses `buildQueryEngine`, like `list`.
- Cheatsheet text is English. Output is Markdown on stdout, modules sorted by id, no pager.
- Every statement in a built-in `cheatsheet.tmpl` must be true for the module's own `zsh.tmpl`/`bash.tmpl` (aliases, `bindkey` lines, functions); upstream-default key bindings are checked against upstream docs. An option that changes an alias name, a key binding, or whether a command exists must change the cheatsheet text (template conditions), and gets a variant golden test.
- Exit codes: `0` ok, `1` a module failed to render, `2` config/schema error (missing `config.toml`, unknown or inactive module id, shell not managed, invalid option).
- Git: English Conventional Commits, no attribution or co-author trailers. Run `gofmt`, `make vet`, and `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2 run ./...` before the last commit.
- Other open tickets (OMNIS-48 to OMNIS-53) add or change built-in modules. The branch was rebased on `main` at 27 built-ins (`dynamic-completion` and `you-should-use` are placed in Tasks 4 and 6). Before Task 8, rebase on `main` again and re-list `modules/builtin/`: any module that arrived without `cheatsheet.tmpl` gets one in Task 8.

## Review Focus

- A rendered body that contains terminal control sequences (user module): printed with them replaced by spaces. Test in Task 1 and Task 3.
- `--shell fish`, a shell that is not managed, and unset `$SHELL`: exit 2 with the managed shells named; never a panic or empty output. Task 2.
- No `config.toml` yet: exit 2 with a hint to run `omnishell init`, not an empty cheatsheet. Task 2.
- An enabled module with an invalid option value, and a `config.toml` that lists a module id the registry no longer has: the first is exit 2 naming the module and option, the second is ignored (`doctor`/`validate` report it). Task 2.
- A user module that overrides a built-in, a user module without `cheatsheet.tmpl`, and a user template that fails to render (`.Options.nope` with `missingkey=error`): the override's text is used, the footer names the module without a file, the render failure warns on stderr, the other modules still print, exit 1. Tasks 2 and 3.

---

### Task 1: Module reader and line-wise clean

**Files:**
- Modify: `internal/module/module.go` (next to `Template`)
- Modify: `internal/modedit/clean.go`
- Test: `internal/module/module_test.go`, `internal/modedit/clean_test.go`

**Interfaces:**
- Produces: `func (m Module) Cheatsheet() (string, bool, error)` — reads `cheatsheet.tmpl` from `m.FS`; `("", false, nil)` when the file does not exist (same contract as `Template`).
- Produces: `func CleanLines(s string) string` in package `modedit` — applies `Clean` to every line, keeps each `\n`.

- [ ] **Step 1: Write the failing tests.** `TestModuleCheatsheetReadsFile`, `TestModuleCheatsheetMissingFileIsNotAnError` (`has == false`, `err == nil`) using an `fstest.MapFS`, in the style of the existing `Template` tests. `TestCleanLinesKeepsNewlinesAndCleansEachLine`: `CleanLines("a\x1b[2Jb\nline2\n")` equals `"a [2Jb\nline2\n"`; also a bidi override (`‮`) becomes a space.
- [ ] **Step 2: Run `go test ./internal/module ./internal/modedit -run 'Cheatsheet|CleanLines' -v`.** Expected: FAIL, undefined symbols.
- [ ] **Step 3: Implement both functions.** `Cheatsheet` mirrors `Template` with the file name `cheatsheet.tmpl`. `CleanLines` splits on `"\n"`, maps `Clean`, joins.
- [ ] **Step 4: Run the same command.** Expected: PASS.
- [ ] **Step 5: Commit** the design doc, this plan and the code: `git add docs/superpowers internal/module internal/modedit && git commit -m "feat: add cheatsheet.tmpl reader and CleanLines"`.

### Task 2: Read-only cheatsheet selection and rendering in modedit

**Files:**
- Create: `internal/modedit/cheatsheet.go`
- Test: `internal/modedit/cheatsheet_test.go` (use the `newEditor` helper style of `views_test.go`/`editor_test.go`; add fixture modules via `fstest.MapFS` or a temp user-modules dir as the existing tests do)
- Modify (only if needed): `internal/engine` to export the vendor dir (see Step 3)

**Interfaces:**
- Consumes: `module.Module.Cheatsheet()` (Task 1), `module.ValidateOptions`, `engine.ManagedShells`, `render.Render`, `Editor.packageState`, `Editor.loadConfigAllowMissing`.
- Produces:

```go
type CheatsheetOptions struct {
    IDs      []string // empty: every active module
    Shell    string   // "" or "zsh"/"bash"
    EnvShell string   // base name of $SHELL, used only when Shell == ""
}
type CheatsheetEntry struct {
    ID, Name, Description string
    PackagesMissing       bool
    Body                  string // rendered, not yet cleaned
}
type CheatsheetFailure struct { ID string; Err error }
type Cheatsheet struct {
    Shell        string
    Entries      []CheatsheetEntry
    NoCheatsheet []string // active module ids without cheatsheet.tmpl
    Failed       []CheatsheetFailure
}
func (ed Editor) Cheatsheet(opts CheatsheetOptions) (Cheatsheet, error)
```

Behaviour, all from the spec: missing `config.toml` → `config.Error` with a hint to run `omnishell init`. Shell = `opts.Shell` if set, else `opts.EnvShell` if managed, else the first managed shell; a requested shell that is not managed → `config.Error` listing the managed ones; no managed shell → `config.Error`. A module is active when enabled in config, `mf.Platforms` contains the OS, and `mf.Shells` contains the shell. Ids in config that are not in the registry are ignored. With `opts.IDs`, each id must exist and be active, else `config.Error` naming it. Options come from `module.ValidateOptions(mf.Options, cfg.Modules[id].Options)`; an error is returned as `engine.ConfigError{Err: fmt.Errorf("module %q: %w", id, err)}`. The render context's `Active` set holds every module enabled and supported on this OS. Entries and `NoCheatsheet` are sorted by id. A render error goes into `Failed` and does not stop the others.

- [ ] **Step 1: Write failing tests** (names are the contract): `TestCheatsheetListsOnlyActiveModulesForShell` (disabled, wrong-OS, wrong-shell modules absent), `TestCheatsheetUsesEffectiveOptions` (template with `{{if has_item .Options.replace "ls"}}`; once with the default, once with a configured value), `TestCheatsheetShellSelection` (flag wins, `EnvShell` used when managed, falls back to first managed, `Shell: "fish"` errors naming the managed shells), `TestCheatsheetMissingConfigIsAConfigError`, `TestCheatsheetInvalidOptionIsAConfigError`, `TestCheatsheetIgnoresUnknownModuleIDInConfig`, `TestCheatsheetIDsMustBeActive`, `TestCheatsheetModuleWithoutFileLandsInNoCheatsheet`, `TestCheatsheetRenderFailureIsCollectedNotFatal` (`{{.Options.nope}}`), `TestCheatsheetUserOverrideWins`, `TestCheatsheetReportsMissingPackages`, `TestCheatsheetWritesNothing` (config file bytes and directory listing unchanged).
- [ ] **Step 2: Run `go test ./internal/modedit -run TestCheatsheet -v`.** Expected: FAIL, `Editor.Cheatsheet` undefined.
- [ ] **Step 3: Implement `Editor.Cheatsheet`.** `render.Context` needs `VendorDir`: use the value `engine.renderContext` uses (`e.vendorDir()`); if unexported, add a small exported `func (e Engine) VendorDir() string` that `vendorDir` already backs, instead of duplicating the path.
- [ ] **Step 4: Run the same command, then `go test ./internal/modedit ./internal/engine`.** Expected: PASS.
- [ ] **Step 5: Commit** `feat: add read-only cheatsheet selection to modedit`.

### Task 3: `omnishell cheatsheet` command and docs

**Files:**
- Create: `internal/cli/cheatsheet.go`
- Modify: `internal/cli/root.go` (add `newCheatsheetCmd()` after `newListCmd()`), `README.md`, `docs/writing-a-module.md`
- Test: `internal/cli/cheatsheet_test.go` (style of `list_test.go`; reuse `installFixtureModules`, extending the fixtures with a `cheatsheet.tmpl` if that helper allows it, otherwise write a fixture in the test)

**Interfaces:**
- Consumes: `modedit.Editor.Cheatsheet`, `modedit.CleanLines`, `buildQueryEngine`.
- Produces: `func newCheatsheetCmd() *cobra.Command`, `Use: "cheatsheet [module-id ...]"`, flag `--shell` (string).

Output format (exact): a first line `# omnishell cheatsheet (<shell>)`; per entry a blank line, `## <Name> (<id>)`, the description line, then `Packages missing — run 'omnishell apply'.` when `PackagesMissing`, a blank line, the cleaned body; at the end, if any, a blank line and `No cheatsheet: a, b`. No active module: `No active modules for <shell>.` and exit 0. Name, description and body all go through `CleanLines`. Each `Failed` item prints `warning: module <id>: <err>` on stderr; after printing everything the command returns `fmt.Errorf("cheatsheet: %d module(s) failed to render", n)` (exit 1).

- [ ] **Step 1: Write failing tests:** `TestCheatsheetCommandPrintsActiveModules` (golden string of the exact format above), `TestCheatsheetCommandShellFlag`, `TestCheatsheetCommandUnknownModuleExits2`, `TestCheatsheetCommandNoConfigExits2`, `TestCheatsheetCommandRenderFailureWarnsAndExits1` (other module still printed), `TestCheatsheetCommandCleansControlSequences` (body containing `\x1b[2J`), `TestCheatsheetCommandFooterListsModulesWithoutFile`.
- [ ] **Step 2: Run `go test ./internal/cli -run TestCheatsheetCommand -v`.** Expected: FAIL.
- [ ] **Step 3: Implement the command.** Pass `os.Getenv("SHELL")` base name as `EnvShell` through a small injectable variable the tests can set, like `lookPath`/`runnerOverride` in `context.go`. Long help names the three shell-selection rules.
- [ ] **Step 4: Docs.** `README.md`: add the command to the command reference with one example block. `docs/writing-a-module.md`: add `cheatsheet.tmpl` to the folder layout, and a "Cheatsheet" section: Markdown, same template context as `zsh.tmpl`, one file for all shells (`.Shell` to branch), what to list (commands, aliases, functions, key bindings), "describe only what this module sets up", example using `has_item`. Run the docs generation test: `go test ./internal/cli -run Docs -v` (man page and completions come from Cobra).
- [ ] **Step 5: Run `go test ./internal/cli ./internal/modedit` and `go vet ./...`.** Expected: PASS.
- [ ] **Step 6: Commit** `feat: add 'omnishell cheatsheet' command`.

### Task 4: Golden harness and group 1 (shell basics)

**Files:**
- Create: `modules/cheatsheet_test.go`, `modules/builtin/<id>/cheatsheet.tmpl` and `modules/testdata/<id>/cheatsheet-<shell>.golden` for: `completion`, `dynamic-completion`, `history`, `zsh-completions`, `autosuggestions`, `syntax-highlighting`, `spell-correction`, `fzf-tab`

**Interfaces:**
- Produces (test helper, used by Tasks 5-8): `func renderCheatsheet(t *testing.T, id, shell string, opts map[string]any) string` — loads `module.LoadRegistry(modules.FS(), "")`, reads `Cheatsheet()`, `module.ValidateOptions`, `render.Render` with the same context fields as `renderModuleOn` (platform `macos`), fails the test if the file is missing.
- Produces: `TestBuiltinCheatsheets` — for every built-in module that has a `cheatsheet.tmpl`, for each shell in its manifest, asserts the golden `cheatsheet-<shell>` (via `assertGoldenNamed`), non-empty output, no `\x1b` byte.

- [ ] **Step 1: Write the harness and the variant test `TestSpellCorrectionCheatsheetFollowsOptions`** (`scope = "all"` and `nocorrect_commands = ["git"]` change the text; default does not mention `git`). Run `go test ./modules -run Cheatsheet -v`. Expected: FAIL (no files yet).
- [ ] **Step 2: Write the eight `cheatsheet.tmpl` files.** For each: read the module's manifest and templates, list what the user gets, branch on `.Shell` and options where the template does. Modules with no typed commands (`completion`, `history`, `zsh-completions`) get one short entry that says what changes (for example the history size and sharing) and that it adds no commands.
- [ ] **Step 3: Run `go test ./modules -run Cheatsheet -update`, then read every new golden file** and compare each statement with the module's template. Fix the template, not the golden.
- [ ] **Step 4: Run `go test ./modules -run Cheatsheet -v`.** Expected: PASS.
- [ ] **Step 5: Commit** `feat: add cheatsheets for completion, history and plugin modules`.

### Task 5: Group 2 (search and navigation)

**Files:** `modules/builtin/<id>/cheatsheet.tmpl` and goldens for `fzf`, `atuin`, `zoxide`, `broot`, `direnv`, `mise`; extend `modules/cheatsheet_test.go`.

- [ ] **Step 1: Write failing variant tests.** `TestFzfCheatsheetFollowsOptions`: with `ctrl_r = false` the text has no `Ctrl-R`, with `ctrl_t = true` it has `Ctrl-T`. `TestZoxideCheatsheetFollowsCmd`: `cmd = "j"` produces `j`/`ji` and no `z`. Add one for any other option of these modules that changes a name or binding (check each manifest).
- [ ] **Step 2: Write the six files**, same rules as Task 4. Key bindings that come from the tool (atuin, fzf, broot) are checked against the tool's documentation and against the module's `bindkey`/init lines.
- [ ] **Step 3: Run `-update`, review every golden against its template, then `go test ./modules -run Cheatsheet -v`.** Expected: PASS.
- [ ] **Step 4: Commit** `feat: add cheatsheets for search and navigation modules`.

### Task 6: Group 3 (aliases and CLI helpers)

**Files:** `modules/builtin/<id>/cheatsheet.tmpl` and goldens for `modern-aliases`, `pay-respects`, `tealdeer`, `you-should-use`, `colorized-man`, `pager-defaults`, `ls-colors`; extend `modules/cheatsheet_test.go`.

- [ ] **Step 1: Write failing variant test `TestModernAliasesCheatsheetFollowsReplace`:** `replace = ["ls"]` mentions `ls` and `ll`, not `cat` or `find`; `replace = ["cat","find"]` the reverse. Add variants for options of the other five modules where a name or command changes.
- [ ] **Step 2: Write the seven files.** `modern-aliases` mirrors the `has_item` conditions of its template.
- [ ] **Step 3: Run `-update`, review goldens against templates, run `go test ./modules -run Cheatsheet -v`.** Expected: PASS.
- [ ] **Step 4: Commit** `feat: add cheatsheets for alias and CLI helper modules`.

### Task 7: Group 4 (prompt and session)

**Files:** `modules/builtin/<id>/cheatsheet.tmpl` and goldens for `starship`, `omnishell-prompt`, `tmux`, `welcome`, `window-title`, `root-loops`; extend `modules/cheatsheet_test.go`.

- [ ] **Step 1: Write failing variant tests** for any option of these modules that changes a visible command, alias or binding (check each manifest; `tmux` auto-attach behaviour and `welcome` options are the likely ones).
- [ ] **Step 2: Write the six files.** Modules that only change appearance get one short entry.
- [ ] **Step 3: Run `-update`, review goldens against templates, run `go test ./modules -run Cheatsheet -v`.** Expected: PASS.
- [ ] **Step 4: Commit** `feat: add cheatsheets for prompt and session modules`.

### Task 8: Enforce coverage and verify the branch

**Files:** Modify `modules/builtin_test.go` (or `modules/cheatsheet_test.go`); possibly add files for modules that arrived on `main` meanwhile.

- [ ] **Step 1: Rebase on `main`** (`git fetch origin && git rebase origin/main`) and list `modules/builtin/`.
- [ ] **Step 2: Write `TestEveryBuiltinModuleHasACheatsheet`:** for every module in the built-in registry, `Cheatsheet()` returns `has == true`, and the rendered default output is non-empty for each manifest shell. Run it: it fails for any module added after Task 7.
- [ ] **Step 3: Add `cheatsheet.tmpl` and goldens** for the modules it names (same rules as Task 4); run `go test ./modules -update -run Cheatsheet`, review the new goldens.
- [ ] **Step 4: Full verification:** `make test`, `make vet`, `gofmt -l .` (empty), `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2 run ./...`. Then build and try it by hand against a throwaway home: `make build && HOME=$(mktemp -d) ./omnishell init && ./omnishell enable fzf && ./omnishell cheatsheet`. (Use `mktemp -d` for `HOME`; do not touch the real config.)
- [ ] **Step 5: Check each acceptance criterion** of OMNIS-54 against the output and tests, then commit `test: require a cheatsheet for every built-in module`.
