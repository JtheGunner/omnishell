# Plan Preview and Apply Handoff Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** In `omnishell tui`, the key `a` shows the plan `apply` would execute, in a scrollable screen; confirming it closes the UI and runs the real `omnishell apply`, whose own prompt stays the gate.

**Architecture:** A new `Engine.Preview` returns the plan text and whether applying would do anything, using `apply`'s own "nothing to do" rule. The TUI's `Backend` gains `Plan()`, which the model calls in a `tea.Cmd` behind a waiting screen. The plan screen only ever *requests* an apply (`Result.ApplyRequested`); after Bubble Tea has given the terminal back, `internal/cli/tui.go` runs the apply command with default flags.

**Tech Stack:** Go 1.26 and the dependencies already on `main` (Bubble Tea v2, Lip Gloss v2, x/ansi, x/term). No new dependencies.

**Spec:** `docs/superpowers/specs/2026-10-07-interactive-tui-design.md` (Screens → Plan preview, Apply handoff, Failure behavior; Sub-tasks item 5). YouTrack: OMNIS-34.

## Global Constraints

- `go.mod` and `go.sum` do not change.
- The TUI never applies anything itself and never writes a file. `Engine.Preview` is read-only: it calls `ComputePlan` and `RenderPlan` and compares against the lockfile.
- `apply`'s confirmation prompt stays the gate: the plan screen is a preview, the handoff uses `newApplyCmd()` with every flag at its default (no `--yes`), and a declined prompt writes nothing and exits with apply's own code.
- `Update` does no I/O: the plan is computed inside a returned `tea.Cmd`.
- Every piece of text that reaches the screen from a backend (plan text, plan errors) goes through `sanitize`.
- `omnishell apply`, `diff`, `doctor`, `enable`, `disable`, `list` behave exactly as before.
- Existing tests are not deleted; edits to them are limited to the signature changes this plan lists (`Run` returns a `Result`, the test runner returns `(tui.Result, error)`).
- Every commit builds and passes `go test ./...`. Before the PR: `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2 run ./...` reports 0 issues (CI runs exactly this version).
- Write invisible or format Unicode characters in Go sources as `\uXXXX` escapes, never as raw characters.
- All identifiers, comments and commit messages are in English; commit subjects use `<type>: <description>` with no attribution trailers.

## Rulings carried into this plan

- **`Engine.Preview` instead of a bare `ComputePlan` + `RenderPlan`.** The spec says the backend's `Plan()` wraps those two only. That cannot decide "nothing to apply" correctly: `apply` takes its no-op path only when there are no planned changes *and* the init and rc files still match the lockfile (`initOrRCDrift`, which is unexported and subtle: a merely deleted init file is deliberately not drift to a plain `apply`). `Preview` adds exactly that check, still read-only, and a test pins it against the real `Apply` in four states.
- **The preview shows the plan before `apply` shows it again.** After `y` the real `apply` prints its plan and asks `Proceed?`. That is the double confirmation the spec accepts; the TUI adds no way around the prompt.
- **A waiting screen, not a freeze.** The plan probes the package manager and can take seconds, so `a` switches screens at once and computes in a command. Esc cancels (the answer is dropped when it arrives, using a request counter), and `a` is ignored while a toggle is being written.
- **Hand-written scrolling.** Up/down, page up/down, home/end (also `j`/`k`/`g`/`G`); the Bubbles viewport would add a dependency for forty lines.
- **`omnishell tui` exits with `apply`'s exit code** after a handoff, because the handoff returns `runApply`'s error; if the UI itself fails, no apply runs.
- **No plan, no apply:** `y` does nothing while the plan is loading, failed, or says there is nothing to apply.

## Review Focus

- `y` and Enter reach `apply` only when the plan was shown and applying would do something (Task 3 tests).
- A declined `apply` prompt after the handoff writes nothing and exits 1; the terminal is usable for the prompt (Task 5 tests, and the pseudo-terminal check in Task 6, which exercises the real binary).
- `Preview.NeedsApply` agrees with what a real `Apply` does, including the deleted-init-file case (Task 1 test).
- A plan answer that arrives after the user went back, or after a newer request, is dropped (Task 3 tests).
- Plan text and errors with control characters, very long lines, a tall or short terminal and resizing while scrolled never break the layout or leave the scroll position out of range (Tasks 3 and 4 tests).

---

### Task 1: `Engine.Preview`

**Files:**
- Create: `internal/engine/preview.go`
- Test: `internal/engine/preview_test.go`

**Interfaces:**
- Consumes: `lockfile.Load`, `ComputePlan`, `RenderPlan`, `Engine.initOrRCDrift` (existing); the test helpers `applyEngine` and `writeConfig` in `internal/engine/apply_test.go`.
- Produces:
  ```go
  type Preview struct {
      Text       string // the rendered plan plus one warning per unknown module
      NeedsApply bool   // false exactly when apply would take its "nothing to do" path
  }
  func (e Engine) Preview(cfg config.Config, lockPath string) (Preview, error)
  ```

- [ ] **Step 1: Write the failing tests**

`internal/engine/preview_test.go`:

```go
package engine_test

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/JtheGunner/omnishell/internal/engine"
	"github.com/JtheGunner/omnishell/internal/pkgmgr"
)

const previewConfig = "[omnishell]\nversion=1\nshells=[\"bash\"]\n[modules.completion]\nenabled=true\n"

// previewFixture is a converged-able setup: a bash-only host with the
// completion module enabled and nothing applied yet.
type previewFixture struct {
	e                 engine.Engine
	home              string
	cfgPath, lockPath string
}

func newPreviewFixture(t *testing.T, cfgBody string) previewFixture {
	t.Helper()
	home := t.TempDir()
	var out bytes.Buffer
	mgr := &pkgmgr.MockManager{NameV: "apt", DetectV: true, Installed: map[string]bool{}}
	f := previewFixture{
		e:        applyEngine(t, home, mgr, &out),
		home:     home,
		cfgPath:  filepath.Join(home, ".config", "omnishell", "config.toml"),
		lockPath: filepath.Join(home, ".config", "omnishell", "state.lock.json"),
	}
	writeConfig(t, f.cfgPath, cfgBody)
	return f
}

func (f previewFixture) load(t *testing.T) config.Config {
	t.Helper()
	cfg, err := config.Load(f.cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	return cfg
}

// previewThenApply returns what Preview says about the next apply and whether
// a real apply then changed anything.
func (f previewFixture) previewThenApply(t *testing.T) (needsApply, changed bool) {
	t.Helper()
	cfg := f.load(t)
	pv, err := f.e.Preview(cfg, f.lockPath)
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	res, err := f.e.Apply(cfg, f.cfgPath, f.lockPath, engine.ApplyOptions{Yes: true})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	return pv.NeedsApply, res.Changed
}

// The preview is only worth showing if it agrees with apply about whether
// there is anything to do, in every state apply distinguishes.
func TestPreviewNeedsApplyAgreesWithApply(t *testing.T) {
	f := newPreviewFixture(t, previewConfig)

	needs, changed := f.previewThenApply(t)
	if !needs || !changed {
		t.Fatalf("fresh config: preview needs=%v, apply changed=%v, want both true", needs, changed)
	}

	needs, changed = f.previewThenApply(t)
	if needs || changed {
		t.Fatalf("converged: preview needs=%v, apply changed=%v, want both false", needs, changed)
	}

	// A deleted init file is not drift to a plain apply (only Refresh and doctor
	// notice it), so the preview must not promise a change either.
	if err := os.Remove(filepath.Join(f.home, ".config", "omnishell", "init.bash")); err != nil {
		t.Fatal(err)
	}
	needs, changed = f.previewThenApply(t)
	if needs != changed {
		t.Fatalf("deleted init file: preview needs=%v but apply changed=%v", needs, changed)
	}

	writeConfig(t, f.cfgPath, previewConfig+"[modules.fzf]\nenabled=true\n")
	needs, changed = f.previewThenApply(t)
	if !needs || !changed {
		t.Fatalf("after enabling fzf: preview needs=%v, apply changed=%v, want both true", needs, changed)
	}
}

func TestPreviewTextIsThePlanApplyWouldShow(t *testing.T) {
	f := newPreviewFixture(t, previewConfig)
	cfg := f.load(t)

	pv, err := f.e.Preview(cfg, f.lockPath)
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	dry, err := f.e.Apply(cfg, f.cfgPath, f.lockPath, engine.ApplyOptions{DryRun: true})
	if err != nil {
		t.Fatalf("Apply dry run: %v", err)
	}

	if strings.TrimRight(dry.PlanText, "\n") != pv.Text {
		t.Fatalf("preview text differs from the dry-run plan:\n--- preview ---\n%s\n--- dry run ---\n%s", pv.Text, dry.PlanText)
	}
	if !strings.Contains(pv.Text, "completion") {
		t.Fatalf("the plan should mention the enabled module:\n%s", pv.Text)
	}
}

func TestPreviewAppendsAWarningPerUnknownModule(t *testing.T) {
	f := newPreviewFixture(t, previewConfig+"[modules.ghost]\nenabled=true\n")

	pv, err := f.e.Preview(f.load(t), f.lockPath)
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}

	if !strings.HasSuffix(pv.Text, `warning: unknown module "ghost" in config (ignored)`) {
		t.Fatalf("the warning should end the text:\n%s", pv.Text)
	}
}

func TestPreviewChangesNothingOnDisk(t *testing.T) {
	f := newPreviewFixture(t, previewConfig)
	snapshot := func() map[string]string {
		files := map[string]string{}
		err := filepath.WalkDir(f.home, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() {
				data, rerr := os.ReadFile(path)
				if rerr != nil {
					return rerr
				}
				files[path] = string(data)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk: %v", err)
		}
		return files
	}
	before := snapshot()

	if _, err := f.e.Preview(f.load(t), f.lockPath); err != nil {
		t.Fatalf("Preview: %v", err)
	}

	after := snapshot()
	if len(before) != len(after) {
		t.Fatalf("files before=%d after=%d: Preview must not create or remove files", len(before), len(after))
	}
	for path, content := range before {
		if after[path] != content {
			t.Fatalf("%s changed", path)
		}
	}
}

func TestPreviewReportsAConfigErrorForAnInvalidOption(t *testing.T) {
	f := newPreviewFixture(t, previewConfig+"[modules.fzf]\nenabled=true\n[modules.fzf.options]\nctrl_r=\"yes please\"\n")

	_, err := f.e.Preview(f.load(t), f.lockPath)

	var cfgErr engine.ConfigError
	if !errors.As(err, &cfgErr) {
		t.Fatalf("err = %v, want an engine.ConfigError", err)
	}
}

func TestPreviewReportsAMalformedLockfile(t *testing.T) {
	f := newPreviewFixture(t, previewConfig)
	if err := os.WriteFile(f.lockPath, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := f.e.Preview(f.load(t), f.lockPath)

	if err == nil || !strings.Contains(err.Error(), "lockfile") {
		t.Fatalf("err = %v, want a lockfile error", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/engine/ -count=1 -run Preview`
Expected: FAIL to build, `f.e.Preview undefined (type engine.Engine has no field or method Preview)`.

- [ ] **Step 3: Implement**

`internal/engine/preview.go`:

```go
package engine

import (
	"fmt"
	"strings"

	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/JtheGunner/omnishell/internal/lockfile"
)

// Preview is what `omnishell apply` would show for a config, and whether
// applying would do anything at all.
type Preview struct {
	// Text is the rendered plan, followed by a warning for every module the
	// config enables that no registered module provides (apply prints those
	// to stderr).
	Text string
	// NeedsApply is false exactly when apply would take its "nothing to do"
	// path: no planned changes and init and rc files that still match the
	// lockfile.
	NeedsApply bool
}

// Preview computes the plan for cfg without touching anything, the way the
// first step of Apply does, and decides with Apply's own rule whether applying
// would change anything. It exists for front ends that show the plan before
// handing over to apply.
func (e Engine) Preview(cfg config.Config, lockPath string) (Preview, error) {
	lock, _, err := lockfile.Load(lockPath)
	if err != nil {
		return Preview{}, err
	}
	plan, err := ComputePlan(e, cfg, lock, false)
	if err != nil {
		return Preview{}, err // a ConfigError propagates unchanged
	}

	var text strings.Builder
	text.WriteString(strings.TrimRight(RenderPlan(plan), "\n"))
	for _, id := range plan.UnknownModules {
		_, _ = fmt.Fprintf(&text, "\nwarning: unknown module %q in config (ignored)", id)
	}
	return Preview{
		Text:       text.String(),
		NeedsApply: plan.HasChanges || e.initOrRCDrift(plan, lock),
	}, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `gofmt -l internal/engine && go vet ./internal/engine/ && go test ./internal/engine/ -race -count=1 -v -run Preview`
Expected: `gofmt -l` prints nothing; PASS for the six `TestPreview…` tests. `TestPreviewNeedsApplyAgreesWithApply` compares the preview with a real `Apply` for a fresh config, a converged one, a deleted init file and a newly enabled module.

Run: `go test ./internal/engine/ -race -count=1`
Expected: PASS (the rest of the package is untouched).

- [ ] **Step 5: Commit**

```bash
git add internal/engine/preview.go internal/engine/preview_test.go
git commit -m "feat: add Engine.Preview to show what apply would do"
```

---

### Task 2: `Backend.Plan` and the CLI adapter

**Files:**
- Modify: `internal/tui/backend.go`, `internal/cli/tui.go`
- Test: `internal/tui/helpers_test.go` (the fake backend), `internal/cli/tui_test.go`

**Interfaces:**
- Consumes: `Engine.Preview` (Task 1); `config.Load`, `userMessage` (existing).
- Produces:
  ```go
  type PlanPreview struct { Text string; NeedsApply bool }   // package tui
  // Backend gains:
  Plan() (PlanPreview, error)
  // test double in package tui gains: preview PlanPreview, planErr error, planCalls int, and Plan()
  ```
  `tuiBackend` (package cli) gains the fields `cfgPath` and `lockPath` and the method `Plan`.

- [ ] **Step 1: Write the failing tests**

In `internal/tui/helpers_test.go`, replace the `fakeBackend` struct (everything from `type fakeBackend struct {` up to, not including, `func (f *fakeBackend) Modules()`) with:

```go
type fakeBackend struct {
	views       []modedit.ModuleView
	modulesErr  error    // returned by Modules
	statusesErr error    // returned by Statuses
	toggleErr   error    // returned by Enable and Disable, before anything changes
	calls       []string // "enable fzf", "disable fzf", in order
	modulesRead int      // how often Modules was called
	preview     PlanPreview
	planErr     error // returned by Plan
	planCalls   int   // how often Plan was called
}

func (f *fakeBackend) Plan() (PlanPreview, error) {
	f.planCalls++
	return f.preview, f.planErr
}
```

In `internal/cli/tui_test.go`, add `"path/filepath"` to the import block (between `"os"` and `"slices"`), and append:

```go
func TestTUIBackendPlanDescribesTheConfigAsItIsNow(t *testing.T) {
	setTestInit(t)
	cli.SetLookPathForTest(bashPresentLookPath)
	t.Cleanup(func() { cli.SetLookPathForTest(nil) })
	b := backendFor(t)

	before, err := b.Plan()
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if !strings.HasPrefix(before.Text, "Plan (") || !before.NeedsApply {
		t.Fatalf("a fresh init has work for apply, got needs=%v:\n%s", before.NeedsApply, before.Text)
	}
	if strings.Contains(before.Text, "fzf") {
		t.Fatalf("fzf is not enabled yet:\n%s", before.Text)
	}

	if err := b.Enable("fzf"); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	after, err := b.Plan()
	if err != nil {
		t.Fatalf("Plan after enabling: %v", err)
	}
	if !strings.Contains(after.Text, "fzf") {
		t.Fatalf("the plan must include what was just toggled:\n%s", after.Text)
	}
}

func TestTUIBackendPlanReportsAMalformedLockfile(t *testing.T) {
	cfgPath := setTestInit(t)
	b := backendFor(t)
	lockPath := filepath.Join(filepath.Dir(cfgPath), "state.lock.json")
	if err := os.WriteFile(lockPath, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write lockfile: %v", err)
	}

	_, err := b.Plan()

	if err == nil || !strings.Contains(err.Error(), "lockfile") {
		t.Fatalf("err = %v, want a lockfile error", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ ./internal/cli/ -count=1`
Expected: FAIL to build. `internal/tui` reports `undefined: PlanPreview`; `internal/cli` reports `b.Plan undefined (type tui.Backend has no field or method Plan)`.

- [ ] **Step 3: Implement**

Replace `internal/tui/backend.go`:

```go
// Package tui is the full-screen module browser behind `omnishell tui`.
//
// It knows nothing about the engine, the config file or the CLI: everything it
// needs comes through Backend, so the model can be tested with a fake and the
// package never writes to disk.
package tui

import "github.com/JtheGunner/omnishell/internal/modedit"

// Backend is everything the TUI needs from the rest of omnishell.
type Backend interface {
	// Modules returns one view per known module, sorted by ID.
	Modules() ([]modedit.ModuleView, error)
	// Enable and Disable write the module's state to config.toml. The error
	// text is shown to the user as it is, so it should read as a sentence.
	Enable(id string) error
	Disable(id string) error
	// Statuses returns every module's current state in config.toml, keyed by
	// id. It must be cheap: the UI calls it after every write, whereas Modules
	// can take seconds because it asks the package manager about every package.
	Statuses() (map[string]modedit.Status, error)
	// Plan computes what applying the current config would do. It must not
	// change anything, and it may take seconds (it asks the package manager),
	// so the UI calls it in a command and shows a waiting screen meanwhile.
	Plan() (PlanPreview, error)
}

// PlanPreview is what the plan screen shows.
type PlanPreview struct {
	// Text is the plan, one item per line.
	Text string
	// NeedsApply is false when applying would do nothing, in which case the
	// plan screen offers no way forward.
	NeedsApply bool
}
```

Replace `internal/cli/tui.go` (adds the `Plan` method and the two path fields; the command still passes the whole backend to `tuiRun` and has no handoff yet):

```go
package cli

import (
	"errors"
	"io"
	"os"

	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/JtheGunner/omnishell/internal/engine"
	"github.com/JtheGunner/omnishell/internal/modedit"
	"github.com/JtheGunner/omnishell/internal/tui"
)

// Seams for tests: whether the process is attached to a terminal, and the
// terminal UI itself. Tests reassign them via SetTUIForTest.
var (
	tuiIsTerminal = defaultTUIIsTerminal
	tuiRun        = tui.Run
)

// SetTUIForTest swaps the terminal check and the UI runner. Passing nil for
// either restores the real implementation.
func SetTUIForTest(isTerminal func(in io.Reader, out io.Writer) bool, run func(b tui.Backend, in io.Reader, out io.Writer) error) {
	tuiIsTerminal = defaultTUIIsTerminal
	if isTerminal != nil {
		tuiIsTerminal = isTerminal
	}
	tuiRun = tui.Run
	if run != nil {
		tuiRun = run
	}
}

// defaultTUIIsTerminal reports whether both in and out are terminals.
func defaultTUIIsTerminal(in io.Reader, out io.Writer) bool {
	inFile, ok := in.(*os.File)
	if !ok {
		return false
	}
	outFile, ok := out.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(inFile.Fd()) && term.IsTerminal(outFile.Fd())
}

// tuiBackend adapts a modedit.Editor, and the engine inside it, to the
// tui.Backend the UI consumes.
type tuiBackend struct {
	editor   modedit.Editor
	cfgPath  string
	lockPath string
}

func (b tuiBackend) Modules() ([]modedit.ModuleView, error) { return b.editor.Views() }

func (b tuiBackend) Statuses() (map[string]modedit.Status, error) { return b.editor.Statuses() }

func (b tuiBackend) Enable(id string) error  { return userMessage(b.editor.Enable(id)) }
func (b tuiBackend) Disable(id string) error { return userMessage(b.editor.Disable(id)) }

// Plan shows what `omnishell apply` would do for the config as it is now. It
// reads config.toml again, so toggles made in the UI are included.
func (b tuiBackend) Plan() (tui.PlanPreview, error) {
	cfg, err := config.Load(b.cfgPath)
	if err != nil {
		return tui.PlanPreview{}, userMessage(err)
	}
	preview, err := b.editor.Engine.Preview(cfg, b.lockPath)
	if err != nil {
		return tui.PlanPreview{}, userMessage(err)
	}
	return tui.PlanPreview{Text: preview.Text, NeedsApply: preview.NeedsApply}, nil
}

// userMessage reduces a config.Error to its message. The TUI shows it on a
// one-line status bar, where the config path that prefixes every config.Error
// would push the actual reason off the screen.
func userMessage(err error) error {
	var cfgErr config.Error
	if errors.As(err, &cfgErr) {
		return errors.New(cfgErr.Msg)
	}
	return err
}

func newTUICmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Browse modules in a full-screen terminal UI",
		Long: `Browse modules in a full-screen terminal UI.

Shows every known module with its description, homepage, package status,
platforms and shells. Move with the arrow keys, press space to enable or disable
the selected module, type / to filter, q to quit. Space writes config.toml at
once, exactly like 'omnishell enable' and 'disable'; it never touches your
shells. Run 'omnishell apply' afterwards to apply the changes.

Needs an interactive terminal; in scripts use 'omnishell list'.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			in, out := cmd.InOrStdin(), cmd.OutOrStdout()
			if !tuiIsTerminal(in, out) {
				return engine.ConfigError{Err: errors.New(
					"omnishell tui needs an interactive terminal; use 'omnishell list', 'enable' and 'apply' in scripts")}
			}

			// The engine's runner copies a package manager's output to the writers
			// it is given, and the UI owns the terminal: send it nowhere.
			e, cfgPath, lockPath, err := buildEngine(io.Discard, io.Discard)
			if err != nil {
				return err
			}
			// A missing or malformed config.toml must stop us before the screen
			// is taken over, so the message lands in the normal terminal.
			if _, err := config.Load(cfgPath); err != nil {
				return hintIfUninitialised(cmd, err)
			}

			backend := tuiBackend{
				editor:   modedit.Editor{Engine: e, CfgPath: cfgPath},
				cfgPath:  cfgPath,
				lockPath: lockPath,
			}
			return tuiRun(backend, in, out)
		},
	}
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `gofmt -l internal && go build ./... && go vet ./... && go test ./internal/tui/ ./internal/cli/ ./internal/engine/ ./internal/modedit/ -race -count=1`
Expected: `gofmt -l` prints nothing; build and vet clean; all four packages PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui internal/cli
git commit -m "feat: let the tui backend compute the plan"
```

---

### Task 3: The plan screen in the model

**Files:**
- Create: `internal/tui/plan.go`
- Modify (replace): `internal/tui/model.go`
- Test: `internal/tui/plan_test.go`

**Interfaces:**
- Consumes: `Backend.Plan`, `PlanPreview` (Task 2); `sanitize` (existing); the helpers `newBackedModel`, `sized`, `press`, `settle`, `space`, `key`.
- Produces:
  ```go
  type screen int // screenBrowser, screenPlan
  type planState struct { loading bool; err string; lines []string; needsApply bool; offset int }
  type planMsg struct { seq int; preview PlanPreview; err error }
  const nothingToApply = "Nothing to apply: no module or package changes are planned."
  func planCmd(b Backend, seq int) tea.Cmd
  func (m Model) openPlan() (tea.Model, tea.Cmd)
  func (m Model) applyPlan(msg planMsg) Model
  func (m Model) updatePlan(msg tea.KeyPressMsg) (tea.Model, tea.Cmd)
  func (m Model) planRows() int
  func (m Model) maxPlanOffset() int
  func (m *Model) scrollPlan(delta int)
  // Model gains: screen screen; plan planState; planSeq int; applyRequested bool
  ```
  Test helpers produced here and used by Task 4: `planText(n)`, `withPlan(preview, err) (Model, *fakeBackend)`, `openPlan(m) Model`.

- [ ] **Step 1: Write the failing tests**

`internal/tui/plan_test.go`:

```go
package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// planText returns n numbered lines, enough to need scrolling.
func planText(n int) string {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %03d", i+1)
	}
	return strings.Join(lines, "\n")
}

// withPlan returns a sized model over sampleViews whose backend answers Plan
// with preview, and the backend itself.
func withPlan(preview PlanPreview, err error) (Model, *fakeBackend) {
	m, b := newBackedModel(sampleViews())
	b.preview, b.planErr = preview, err
	return sized(m, 80, 20), b
}

// openPlan presses a and lets the plan finish computing.
func openPlan(m Model) Model {
	next, cmd := m.Update(key("a"))
	return settle(next.(Model), cmd)
}

func TestPressingAOpensTheWaitingScreenAndComputesThePlanInACommand(t *testing.T) {
	m, b := withPlan(PlanPreview{Text: "Plan (apt)\n  install  completion", NeedsApply: true}, nil)

	next, cmd := m.Update(key("a"))
	waiting := next.(Model)

	if waiting.screen != screenPlan || !waiting.plan.loading {
		t.Fatalf("screen=%v loading=%v, want the plan screen waiting for the plan", waiting.screen, waiting.plan.loading)
	}
	if cmd == nil {
		t.Fatal("the plan must be computed by a command")
	}
	if b.planCalls != 0 {
		t.Fatalf("Update computed the plan itself (%d calls); it must do no I/O", b.planCalls)
	}

	ready := settle(waiting, cmd)
	if b.planCalls != 1 || ready.plan.loading {
		t.Fatalf("planCalls=%d loading=%v, want the plan computed once and shown", b.planCalls, ready.plan.loading)
	}
	if want := []string{"Plan (apt)", "  install  completion"}; fmt.Sprint(ready.plan.lines) != fmt.Sprint(want) {
		t.Fatalf("lines = %q, want %q", ready.plan.lines, want)
	}
}

func TestYAndEnterLeadOnToApplyOnceThePlanIsShown(t *testing.T) {
	for _, name := range []string{"y", "enter"} {
		m, _ := withPlan(PlanPreview{Text: "Plan", NeedsApply: true}, nil)
		m = openPlan(m)

		next, cmd := m.Update(key(name))

		if cmd == nil {
			t.Fatalf("%s: expected the UI to close", name)
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Fatalf("%s: the command did not quit", name)
		}
		if !next.(Model).applyRequested {
			t.Fatalf("%s: the apply was not requested", name)
		}
	}
}

func TestYDoesNothingWhileThePlanIsStillBeingComputed(t *testing.T) {
	m, _ := withPlan(PlanPreview{Text: "Plan", NeedsApply: true}, nil)
	waiting, _ := m.Update(key("a"))

	next, cmd := waiting.(Model).Update(key("y"))

	if cmd != nil || next.(Model).applyRequested {
		t.Fatal("y before the plan has been seen must not lead to apply")
	}
}

func TestAPlanWithNothingToApplyOffersOnlyBack(t *testing.T) {
	m, _ := withPlan(PlanPreview{Text: "Plan (apt)\n\n0 modules", NeedsApply: false}, nil)
	m = openPlan(m)

	if got := m.plan.lines[len(m.plan.lines)-1]; got != nothingToApply {
		t.Fatalf("last line = %q, want the nothing-to-apply note", got)
	}
	for _, name := range []string{"y", "enter"} {
		next, cmd := m.Update(key(name))
		if cmd != nil || next.(Model).applyRequested {
			t.Fatalf("%s on a plan with nothing to apply must not lead to apply", name)
		}
	}
}

func TestAPlanErrorIsShownAndOffersOnlyBack(t *testing.T) {
	m, _ := withPlan(PlanPreview{}, errors.New("module \"fzf\": option ctrl_r: not a bool"))
	m = openPlan(m)

	if !strings.Contains(m.plan.err, `module "fzf"`) {
		t.Fatalf("err = %q, want the message", m.plan.err)
	}
	if next, cmd := m.Update(key("y")); cmd != nil || next.(Model).applyRequested {
		t.Fatal("y on a failed plan must not lead to apply")
	}
	if back := press(t, m, "esc"); back.screen != screenBrowser {
		t.Fatal("esc must leave a failed plan")
	}
}

func TestEscNAndBGoBackAndKeepTheBrowserState(t *testing.T) {
	for _, name := range []string{"esc", "n", "b"} {
		m, _ := withPlan(PlanPreview{Text: "Plan", NeedsApply: true}, nil)
		m = press(t, m, "/", "z", "enter", "down") // filter z, cursor on zshonly
		m = openPlan(m)

		back := press(t, m, name)

		if back.screen != screenBrowser {
			t.Fatalf("%s: still on the plan screen", name)
		}
		if v, _ := back.selected(); v.ID != "zshonly" || back.filter != "z" {
			t.Fatalf("%s: selected=%q filter=%q, want the selection and filter kept", name, v.ID, back.filter)
		}
	}
}

// The plan can take seconds. A user who has gone back must not be thrown onto
// the plan screen when the answer finally arrives, nor see it later.
func TestAPlanThatArrivesAfterGoingBackIsDropped(t *testing.T) {
	m, b := withPlan(PlanPreview{Text: "Plan", NeedsApply: true}, nil)
	waiting, cmd := m.Update(key("a"))
	back := press(t, waiting.(Model), "esc")

	late := settle(back, cmd)

	if late.screen != screenBrowser || late.plan.lines != nil {
		t.Fatalf("screen=%v lines=%v, want the late plan ignored", late.screen, late.plan.lines)
	}
	if b.planCalls != 1 {
		t.Fatalf("planCalls = %d, want the one request that was made", b.planCalls)
	}
}

func TestAnOldPlanDoesNotAnswerANewerRequest(t *testing.T) {
	m, _ := withPlan(PlanPreview{Text: "fresh", NeedsApply: true}, nil)
	first, firstCmd := m.Update(key("a"))
	back := press(t, first.(Model), "esc")
	second, secondCmd := back.Update(key("a"))

	afterOld := settle(second.(Model), firstCmd)
	if !afterOld.plan.loading {
		t.Fatal("the answer to the first request must not fill the second screen")
	}
	afterNew := settle(afterOld, secondCmd)
	if afterNew.plan.loading || len(afterNew.plan.lines) == 0 {
		t.Fatal("the answer to the second request must be shown")
	}
}

func TestThePlanIsComputedAgainEachTimeItIsOpened(t *testing.T) {
	m, b := withPlan(PlanPreview{Text: "Plan", NeedsApply: true}, nil)

	m = press(t, openPlan(m), "esc")
	m = space(m) // the config changes in between
	openPlan(m)

	if b.planCalls != 2 {
		t.Fatalf("planCalls = %d, want 2: a plan must reflect the config as it is now", b.planCalls)
	}
}

func TestAIsIgnoredWhileAToggleIsBeingWritten(t *testing.T) {
	m, b := withPlan(PlanPreview{Text: "Plan", NeedsApply: true}, nil)
	writing, _ := m.Update(key("space")) // the write has not finished

	next, cmd := writing.(Model).Update(key("a"))

	if cmd != nil || next.(Model).screen != screenBrowser {
		t.Fatal("a plan must not be requested while the config is being written")
	}
	if b.planCalls != 0 {
		t.Fatalf("planCalls = %d, want 0", b.planCalls)
	}
}

func TestAIsTextWhileTypingTheFilter(t *testing.T) {
	m, b := withPlan(PlanPreview{Text: "Plan", NeedsApply: true}, nil)
	m = press(t, m, "/", "a")

	if m.screen != screenBrowser || m.filter != "a" || b.planCalls != 0 {
		t.Fatalf("screen=%v filter=%q planCalls=%d, want a typed into the filter", m.screen, m.filter, b.planCalls)
	}
}

func TestQuitKeysOnThePlanScreenLeaveWithoutApplying(t *testing.T) {
	for _, name := range []string{"q", "ctrl+c"} {
		m, _ := withPlan(PlanPreview{Text: "Plan", NeedsApply: true}, nil)
		m = openPlan(m)

		next, cmd := m.Update(key(name))

		if cmd == nil || next.(Model).applyRequested {
			t.Fatalf("%s: want the UI to close without requesting an apply", name)
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Fatalf("%s: the command did not quit", name)
		}
	}
}

func TestScrollingMovesTheWindowAndStopsAtBothEnds(t *testing.T) {
	m, _ := withPlan(PlanPreview{Text: planText(100), NeedsApply: true}, nil)
	m = openPlan(m) // 80x20: 18 body rows, 100 lines, so the last offset is 82

	if m.plan.offset != 0 {
		t.Fatalf("offset = %d, want to start at the top", m.plan.offset)
	}
	if up := press(t, m, "up", "k"); up.plan.offset != 0 {
		t.Fatalf("offset above the top = %d, want 0", up.plan.offset)
	}
	if down := press(t, m, "down", "j"); down.plan.offset != 2 {
		t.Fatalf("offset after down, j = %d, want 2", down.plan.offset)
	}
	end := press(t, m, "end")
	if end.plan.offset != 82 {
		t.Fatalf("offset at the end = %d, want 82", end.plan.offset)
	}
	if past := press(t, end, "down", "pgdown"); past.plan.offset != 82 {
		t.Fatalf("offset past the end = %d, want 82", past.plan.offset)
	}
	if page := press(t, m, "pgdown"); page.plan.offset != 18 {
		t.Fatalf("offset after one page = %d, want 18", page.plan.offset)
	}
	if home := press(t, end, "home"); home.plan.offset != 0 {
		t.Fatalf("offset after home = %d, want 0", home.plan.offset)
	}
	if g := press(t, end, "g"); g.plan.offset != 0 {
		t.Fatalf("offset after g = %d, want 0", g.plan.offset)
	}
	if bigG := press(t, m, "G"); bigG.plan.offset != 82 {
		t.Fatalf("offset after G = %d, want 82", bigG.plan.offset)
	}
}

func TestAPlanShorterThanTheScreenDoesNotScroll(t *testing.T) {
	m, _ := withPlan(PlanPreview{Text: planText(5), NeedsApply: true}, nil)
	m = openPlan(m)

	if down := press(t, m, "down", "pgdown", "end"); down.plan.offset != 0 {
		t.Fatalf("offset = %d, want 0 for a plan that fits", down.plan.offset)
	}
}

func TestPlanTextAndErrorsAreNeutralisedBeforeTheyAreShown(t *testing.T) {
	m, _ := withPlan(PlanPreview{Text: "install bad\x1b]0;pwned\x07\x1b[2J\tname\nsecond", NeedsApply: true}, nil)
	m = openPlan(m)
	for _, line := range m.plan.lines {
		if strings.ContainsAny(line, "\x1b\x07\t") {
			t.Fatalf("a plan line still holds a control character: %q", line)
		}
	}

	failed, _ := withPlan(PlanPreview{}, errors.New("bad\x1b[2J\nnews"))
	failed = openPlan(failed)
	if strings.ContainsAny(failed.plan.err, "\x1b\n") {
		t.Fatalf("the error still holds a control character: %q", failed.plan.err)
	}
}

func TestResizingKeepsThePlanScrollPositionInRange(t *testing.T) {
	m, _ := withPlan(PlanPreview{Text: planText(100), NeedsApply: true}, nil)
	m = press(t, openPlan(m), "end") // offset 82 at 20 rows

	m = sized(m, 80, 50) // 48 rows: the largest sensible offset is now 52

	if rows := m.planRows(); m.plan.offset > m.maxPlanOffset() {
		t.Fatalf("offset %d is past the end (%d) after growing to %d rows", m.plan.offset, m.maxPlanOffset(), rows)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -count=1`
Expected: FAIL to build, `undefined: screenPlan` (and the other new identifiers).

- [ ] **Step 3: Implement**

Create `internal/tui/plan.go`:

```go
package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// screen says which of the two screens is showing.
type screen int

const (
	screenBrowser screen = iota
	screenPlan
)

// nothingToApply ends the plan text when applying would change nothing.
const nothingToApply = "Nothing to apply: no module or package changes are planned."

// planState is the plan screen: waiting for the plan, the error that replaced
// it, or the plan itself, scrolled to offset.
type planState struct {
	loading    bool
	err        string   // the reason the plan could not be computed, cleaned
	lines      []string // the plan, one cleaned line per element
	needsApply bool
	offset     int // index of the first visible line
}

// canApply reports whether y may hand over to apply: the plan is there and
// applying would do something.
func (p planState) canApply() bool {
	return !p.loading && p.err == "" && p.needsApply
}

// planMsg carries a computed plan back to Update. seq says which request it
// answers, so a plan the user has already walked away from can be dropped.
type planMsg struct {
	seq     int
	preview PlanPreview
	err     error
}

// planCmd computes the plan in the background; Update itself does no I/O.
func planCmd(b Backend, seq int) tea.Cmd {
	return func() tea.Msg {
		preview, err := b.Plan()
		return planMsg{seq: seq, preview: preview, err: err}
	}
}

// openPlan switches to the plan screen and starts computing the plan. It does
// nothing while a toggle is being written: the plan would describe a config
// that is about to change.
func (m Model) openPlan() (tea.Model, tea.Cmd) {
	if m.pending {
		return m, nil
	}
	m.screen = screenPlan
	m.planSeq++
	m.plan = planState{loading: true}
	return m, planCmd(m.backend, m.planSeq)
}

// applyPlan stores a computed plan, unless the user has left the plan screen
// (or asked for a newer plan) since it was requested.
func (m Model) applyPlan(msg planMsg) Model {
	if m.screen != screenPlan || msg.seq != m.planSeq {
		return m
	}
	if msg.err != nil {
		m.plan = planState{err: sanitize(msg.err.Error())}
		return m
	}
	text := strings.TrimRight(msg.preview.Text, "\n")
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = sanitize(line)
	}
	if !msg.preview.NeedsApply {
		lines = append(lines, "", nothingToApply)
	}
	m.plan = planState{lines: lines, needsApply: msg.preview.NeedsApply}
	return m
}

// updatePlan handles a key press on the plan screen. Only y and enter lead on
// to apply, and only when canApply says so; everything else scrolls or leaves.
func (m Model) updatePlan(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "esc", "n", "b":
		m.screen = screenBrowser
		m.planSeq++ // whatever is still being computed is no longer wanted
		m.plan = planState{}
	case "y", "enter":
		if m.plan.canApply() {
			m.applyRequested = true
			return m, tea.Quit
		}
	case "up", "k":
		m.scrollPlan(-1)
	case "down", "j":
		m.scrollPlan(1)
	case "pgup":
		m.scrollPlan(-m.planRows())
	case "pgdown":
		m.scrollPlan(m.planRows())
	case "home", "g":
		m.plan.offset = 0
	case "end", "G":
		m.plan.offset = m.maxPlanOffset()
	}
	return m, nil
}

// planRows is how many plan lines fit on the screen.
func (m Model) planRows() int {
	return max(m.height-headerLines-footerLines, 1)
}

func (m Model) maxPlanOffset() int {
	return max(len(m.plan.lines)-m.planRows(), 0)
}

// scrollPlan moves the visible window by delta lines, stopping at both ends.
func (m *Model) scrollPlan(delta int) {
	m.plan.offset = min(max(m.plan.offset+delta, 0), m.maxPlanOffset())
}
```

Replace `internal/tui/model.go` (adds the screen, plan and request-counter fields, the `a` key, the plan message, and the clamp of the scroll position on resize):

```go
package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/JtheGunner/omnishell/internal/modedit"
)

// The smallest terminal the layout is designed for.
const (
	minWidth  = 80
	minHeight = 20
)

// Model is the Bubble Tea model of the module browser. All state changes go
// through Update, which returns a new Model.
type Model struct {
	backend        Backend
	views          []modedit.ModuleView
	visible        []int // indexes into views that match the filter, in order
	cursor         int   // position within visible
	filter         string
	filtering      bool                      // true while the user is typing into the filter
	status         string                    // the last error to show, cleared by the next key press
	pending        bool                      // true from pressing space until the write has finished
	initial        map[string]modedit.Status // each module's state when the browser started
	screen         screen
	plan           planState
	planSeq        int  // counts plan requests, so a stale answer can be told from the current one
	applyRequested bool // set when the user confirmed on the plan screen
	width          int  // 0 until the first tea.WindowSizeMsg
	height         int
}

// New returns a browser over views, which must already be sorted for display,
// that changes modules through b. Module text is cleaned of control characters
// first (see sanitize.go).
func New(b Backend, views []modedit.ModuleView) Model {
	m := Model{backend: b, views: sanitizeViews(views)}
	m.initial = make(map[string]modedit.Status, len(m.views))
	for _, v := range m.views {
		m.initial[v.ID] = v.Status
	}
	m.applyFilter()
	return m
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd { return nil }

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		// A taller terminal shows more lines, so the old position may now be
		// past the last useful one.
		m.plan.offset = min(m.plan.offset, m.maxPlanOffset())
	case toggledMsg:
		return m.applyToggled(msg), nil
	case planMsg:
		return m.applyPlan(msg), nil
	case tea.KeyPressMsg:
		m.status = ""
		if m.screen == screenPlan {
			return m.updatePlan(msg)
		}
		if m.filtering {
			return m.updateFiltering(msg)
		}
		return m.updateBrowsing(msg)
	}
	return m, nil
}

func (m Model) updateBrowsing(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "up", "k":
		m.move(-1)
	case "down", "j":
		m.move(1)
	case "space":
		return m.toggle()
	case "a":
		return m.openPlan()
	case "/":
		m.filtering = true
	case "esc":
		if m.filter != "" {
			m.filter = ""
			m.applyFilter()
		}
	}
	return m, nil
}

func (m Model) updateFiltering(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "enter":
		m.filtering = false
	case "esc":
		m.filtering = false
		m.filter = ""
		m.applyFilter()
	case "backspace":
		if runes := []rune(m.filter); len(runes) > 0 {
			m.filter = string(runes[:len(runes)-1])
			m.applyFilter()
		}
	default:
		if msg.Text != "" {
			m.filter += msg.Text
			m.applyFilter()
		}
	}
	return m, nil
}

// move shifts the cursor by delta, staying put at either end of the list.
func (m *Model) move(delta int) {
	next := m.cursor + delta
	if next < 0 || next >= len(m.visible) {
		return
	}
	m.cursor = next
}

// applyFilter recomputes visible from the filter (a case-insensitive substring
// of the module id or description) and moves the cursor back to the top.
func (m *Model) applyFilter() { m.applyFilterKeeping("") }

// applyFilterKeeping is applyFilter, but leaves the cursor on the module with
// the given id when it is still visible. An empty id means the top.
func (m *Model) applyFilterKeeping(id string) {
	query := strings.ToLower(m.filter)
	m.visible = make([]int, 0, len(m.views))
	for i, v := range m.views {
		if query == "" ||
			strings.Contains(strings.ToLower(v.ID), query) ||
			strings.Contains(strings.ToLower(v.Description), query) {
			m.visible = append(m.visible, i)
		}
	}
	m.cursor = 0
	for pos, idx := range m.visible {
		if id != "" && m.views[idx].ID == id {
			m.cursor = pos
		}
	}
}

// selected returns the module under the cursor, if the filtered list is not
// empty.
func (m Model) selected() (modedit.ModuleView, bool) {
	if len(m.visible) == 0 {
		return modedit.ModuleView{}, false
	}
	return m.views[m.visible[m.cursor]], true
}

// changes counts the modules whose enabled state differs from the state they
// had when the browser started, so toggling a module back counts as no change.
func (m Model) changes() int {
	n := 0
	for _, v := range m.views {
		if initial, ok := m.initial[v.ID]; ok && initial != v.Status {
			n++
		}
	}
	return n
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `gofmt -l internal/tui && go vet ./internal/tui/ && go test ./internal/tui/ -race -count=1`
Expected: `gofmt -l` prints nothing; PASS, including the unchanged golden tests (the view does not draw the plan screen yet).

- [ ] **Step 5: Commit**

```bash
git add internal/tui
git commit -m "feat: add the plan screen to the tui model"
```

---

### Task 4: Draw the plan screen

**Files:**
- Create: `internal/tui/planview.go`
- Modify: `internal/tui/view.go`
- Test: `internal/tui/planview_test.go`; golden files in `internal/tui/testdata/` (generated)

**Interfaces:**
- Consumes: `planState`, `Model.planRows()`, `planState.canApply()` (Task 3); `titleStyle`, `dimStyle`, `headerLines`, `footerLines` (existing in `view.go`).
- Produces: `func (m Model) renderPlan() string` and the unexported helpers it uses.

- [ ] **Step 1: Write the failing tests**

`internal/tui/planview_test.go`:

```go
package tui

import (
	"errors"
	"strings"
	"testing"
)

const samplePlan = `Plan (package manager: apt, shells: bash)

  install  completion          snippet: bash
  install  fzf                 snippet: bash   packages: fzf
  skip     macosonly           not supported on linux

2 to install, 0 to update, 1 skipped`

// planScreens returns the plan screen in each of its states at 80x20.
func planScreens(t *testing.T) map[string]Model {
	t.Helper()
	waiting, _ := withPlan(PlanPreview{Text: samplePlan, NeedsApply: true}, nil)
	next, _ := waiting.Update(key("a"))

	ready, _ := withPlan(PlanPreview{Text: samplePlan, NeedsApply: true}, nil)
	long, _ := withPlan(PlanPreview{Text: planText(100), NeedsApply: true}, nil)
	nothing, _ := withPlan(PlanPreview{Text: "Plan (package manager: apt, shells: bash)\n\n0 modules", NeedsApply: false}, nil)
	failed, _ := withPlan(PlanPreview{}, errors.New(`module "fzf": option ctrl_r: "yes please" is not a bool`))

	return map[string]Model{
		"plan-loading":  next.(Model),
		"plan-ready":    openPlan(ready),
		"plan-scrolled": press(t, openPlan(long), "pgdown", "down"),
		"plan-nothing":  openPlan(nothing),
		"plan-error":    openPlan(failed),
	}
}

func TestPlanScreenFillsTheTerminalExactlyInEveryState(t *testing.T) {
	for name, m := range planScreens(t) {
		assertFits(t, plain(m), 80, 20)
		big := sized(m, 120, 40)
		assertFits(t, plain(big), 120, 40)
		if t.Failed() {
			t.Fatalf("state %s did not fit", name)
		}
	}
}

func TestPlanScreenShowsTheWaitingNoteAndOnlyBack(t *testing.T) {
	out := plain(planScreens(t)["plan-loading"])

	if !strings.Contains(out, "plan preview") || !strings.Contains(out, "computing plan…") {
		t.Fatalf("waiting screen:\n%s", out)
	}
	if strings.Contains(out, "continue to apply") {
		t.Fatalf("the way forward must not be offered before the plan is shown:\n%s", out)
	}
}

func TestPlanScreenShowsThePlanAndOffersTheWayForward(t *testing.T) {
	out := plain(planScreens(t)["plan-ready"])

	for _, want := range []string{
		"plan preview", "install  fzf                 snippet: bash   packages: fzf",
		"skip     macosonly", "y/enter continue to apply", "esc back",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "lines ") {
		t.Fatalf("a plan that fits needs no line counter:\n%s", out)
	}
}

func TestPlanScreenCountsLinesWhileScrolling(t *testing.T) {
	long := planScreens(t)["plan-scrolled"] // one page and one line down: offset 19

	out := plain(long)

	if !strings.Contains(out, "lines 20-37 of 100") {
		t.Fatalf("header should show the window:\n%s", out)
	}
	if !strings.Contains(out, "line 020") || strings.Contains(out, "line 019") || strings.Contains(out, "line 038") {
		t.Fatalf("body should hold exactly lines 20 to 37:\n%s", out)
	}
}

func TestPlanScreenWithNothingToApplyExplainsAndOffersOnlyBack(t *testing.T) {
	out := plain(planScreens(t)["plan-nothing"])

	if !strings.Contains(out, nothingToApply) {
		t.Fatalf("missing the note:\n%s", out)
	}
	if strings.Contains(out, "continue to apply") {
		t.Fatalf("there is nothing to continue to:\n%s", out)
	}
}

func TestPlanScreenShowsTheErrorAndOffersOnlyBack(t *testing.T) {
	out := plain(planScreens(t)["plan-error"])

	if !strings.Contains(out, `Could not compute the plan: module "fzf"`) {
		t.Fatalf("missing the error:\n%s", out)
	}
	if strings.Contains(out, "continue to apply") {
		t.Fatalf("a failed plan must not offer apply:\n%s", out)
	}
}

func TestPlanScreenKeepsLongLinesAndLongErrorsInsideTheTerminal(t *testing.T) {
	long, _ := withPlan(PlanPreview{Text: "install " + strings.Repeat("x", 400), NeedsApply: true}, nil)
	assertFits(t, plain(openPlan(long)), 80, 20)

	failed, _ := withPlan(PlanPreview{}, errors.New(strings.Repeat("a very long reason ", 60)))
	out := plain(openPlan(failed))
	assertFits(t, out, 80, 20)
	if !strings.Contains(out, "Could not compute the plan: a very long reason") {
		t.Fatalf("the start of the error must stay readable:\n%s", out)
	}
}

func TestPlanScreenGivesWayToTheTooSmallMessage(t *testing.T) {
	m := sized(planScreens(t)["plan-ready"], 60, 10)

	if out := plain(m); !strings.Contains(out, "Terminal too small") || strings.Contains(out, "plan preview") {
		t.Fatalf("a small terminal must show the too-small message on this screen too:\n%s", out)
	}
}

func TestBrowserHelpMentionsThePlanKey(t *testing.T) {
	if out := plain(sized(newTestModel(sampleViews()), 80, 20)); !strings.Contains(out, "a plan") {
		t.Fatalf("the browser footer should list the plan key:\n%s", out)
	}
}

func TestPlanScreenGoldenFiles(t *testing.T) {
	for name, m := range planScreens(t) {
		assertGolden(t, name, plain(m))
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -count=1 -skip Golden`
Expected: FAIL for these seven: `TestPlanScreenShowsTheWaitingNoteAndOnlyBack`, `TestPlanScreenShowsThePlanAndOffersTheWayForward`, `TestPlanScreenCountsLinesWhileScrolling`, `TestPlanScreenWithNothingToApplyExplainsAndOffersOnlyBack`, `TestPlanScreenShowsTheErrorAndOffersOnlyBack`, `TestPlanScreenKeepsLongLinesAndLongErrorsInsideTheTerminal`, `TestBrowserHelpMentionsThePlanKey`. (The two golden tests fail too until Step 5. `TestPlanScreenFillsTheTerminalExactlyInEveryState` and `TestPlanScreenGivesWayToTheTooSmallMessage` already pass: the old view fills the terminal, and the too-small rule is checked before any screen; they are guards for what follows.)

- [ ] **Step 3: Implement**

Create `internal/tui/planview.go`:

```go
package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// renderPlan draws the plan screen: a header, exactly planRows lines of body
// and a footer, so it fills the terminal the way the browser does.
func (m Model) renderPlan() string {
	rows := m.planRows()
	body := m.planBody(rows)
	for len(body) < rows {
		body = append(body, "")
	}
	for i, line := range body {
		body[i] = ansi.Truncate(line, m.width, "…")
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		m.renderPlanHeader(rows),
		strings.Join(body, "\n"),
		m.renderPlanFooter(),
	)
}

// planBody returns at most rows lines: a waiting note, the error, or the
// visible window of the plan.
func (m Model) planBody(rows int) []string {
	switch {
	case m.plan.loading:
		return []string{dimStyle.Render("computing plan…")}
	case m.plan.err != "":
		wrapped := lipgloss.NewStyle().Width(m.width).Render("Could not compute the plan: " + m.plan.err)
		return clip(strings.Split(wrapped, "\n"), rows)
	}
	end := min(m.plan.offset+rows, len(m.plan.lines))
	return append([]string(nil), m.plan.lines[m.plan.offset:end]...)
}

func clip(lines []string, n int) []string {
	if len(lines) > n {
		return lines[:n]
	}
	return lines
}

func (m Model) renderPlanHeader(rows int) string {
	line := titleStyle.Render("omnishell") + dimStyle.Render("  plan preview")
	if len(m.plan.lines) > rows {
		last := min(m.plan.offset+rows, len(m.plan.lines))
		line += dimStyle.Render(fmt.Sprintf("  lines %d-%d of %d", m.plan.offset+1, last, len(m.plan.lines)))
	}
	return lipgloss.NewStyle().Inline(true).MaxWidth(m.width).Render(line)
}

func (m Model) renderPlanFooter() string {
	help := "↑/↓ scroll · esc back · q quit"
	switch {
	case m.plan.loading:
		help = "esc back · q quit"
	case m.plan.err != "":
		help = "esc back · q quit"
	case m.plan.canApply():
		help = "↑/↓ scroll · y/enter continue to apply · esc back · q quit"
	}
	return dimStyle.Inline(true).MaxWidth(m.width).Render(help)
}
```

In `internal/tui/view.go`, in `render()`, add the plan branch just before the browser layout. Replace:

```go
	bodyHeight := m.height - headerLines - footerLines
	rows := bodyHeight - boxChromeV
```

with:

```go
	if m.screen == screenPlan {
		return m.renderPlan()
	}

	bodyHeight := m.height - headerLines - footerLines
	rows := bodyHeight - boxChromeV
```

and in `renderFooter()`, replace the browser help string:

```go
	help := "↑/↓ move · space toggle · / filter · esc clear filter · q quit"
```

with:

```go
	help := "↑/↓ move · space toggle · a plan · / filter · esc clear filter · q quit"
```

- [ ] **Step 4: Run the structural tests to verify they pass**

Run: `gofmt -l internal/tui && go vet ./internal/tui/ && go test ./internal/tui/ -count=1 -skip Golden`
Expected: `gofmt -l` prints nothing; PASS for everything except the skipped golden tests.

- [ ] **Step 5: Regenerate the golden files and review the diff by eye**

Run: `go test ./internal/tui/ -run Golden -update -count=1 && git status --short internal/tui/testdata`
Expected: six existing goldens modified (`browser-80x20`, `browser-filtered`, `browser-no-modules`, `browser-second-row`, `browser-toggled`, `browser-unavailable`: only the footer help gained `a plan`), three unchanged (`browser-rejected` shows the status line instead of the help, `browser-too-small`, `browser-typing` has its own help), and five new files: `plan-error`, `plan-loading`, `plan-nothing`, `plan-ready`, `plan-scrolled`. Run `git diff internal/tui/testdata` and check that the six modified files differ from before only in that one footer line. `plan-ready.golden` must read exactly (lines are padded with trailing spaces):

```text
omnishell  plan preview                                     
Plan (package manager: apt, shells: bash)                   
                                                            
  install  completion          snippet: bash                
  install  fzf                 snippet: bash   packages: fzf
  skip     macosonly           not supported on linux       
                                                            
2 to install, 0 to update, 1 skipped                        
                                                            
                                                            
                                                            
                                                            
                                                            
                                                            
                                                            
                                                            
                                                            
                                                            
                                                            
↑/↓ scroll · y/enter continue to apply · esc back · q quit  
```

`plan-scrolled.golden` starts with `omnishell  plan preview  lines 20-37 of 100`; `plan-nothing.golden` ends its text with `Nothing to apply: no module or package changes are planned.` and has the footer `↑/↓ scroll · esc back · q quit`; `plan-error.golden` and `plan-loading.golden` have the footer `esc back · q quit`.

- [ ] **Step 6: Run the package with the race detector**

Run: `go test ./internal/tui/ -race -count=1`
Expected: PASS without `-update`.

- [ ] **Step 7: Commit**

```bash
git add internal/tui
git commit -m "feat: draw the plan screen in the tui"
```

---

### Task 5: Hand over to `apply`

**Files:**
- Modify (replace): `internal/tui/run.go`, `internal/cli/tui.go`
- Test: `internal/tui/run_test.go` (replace), `internal/cli/tui_test.go`

**Interfaces:**
- Consumes: `Model.applyRequested` (Task 3); `newApplyCmd`, `runApply`, `SetPromptForTest`, `zshPresentLookPath`, `setupModuleCLITest` (existing in `internal/cli`).
- Produces:
  ```go
  type Result struct{ ApplyRequested bool }                                   // package tui
  func Run(b Backend, in io.Reader, out io.Writer) (Result, error)            // was: returns only error
  func resultOf(final tea.Model) Result
  // package cli:
  func SetTUIForTest(isTerminal func(io.Reader, io.Writer) bool,
      run func(tui.Backend, io.Reader, io.Writer) (tui.Result, error))        // run now returns a Result
  func handOffToApply(cmd *cobra.Command) error
  ```

- [ ] **Step 1: Write the failing tests**

Replace `internal/tui/run_test.go` (`Run` now returns a `Result`; two new assertions and one new test):

```go
package tui

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRunReturnsTheBackendErrorWithoutTouchingTheTerminal(t *testing.T) {
	boom := errors.New("boom")
	var out bytes.Buffer

	_, err := Run(&fakeBackend{modulesErr: boom}, strings.NewReader(""), &out)

	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
	if out.Len() != 0 {
		t.Fatalf("nothing may be written when loading fails, got %q", out.String())
	}
}

// A real program loop, fed a q on its input, must start and quit cleanly.
func TestRunQuitsWhenTheUserPressesQ(t *testing.T) {
	type outcome struct {
		result Result
		err    error
	}
	done := make(chan outcome, 1)
	var out bytes.Buffer
	go func() {
		result, err := Run(&fakeBackend{views: sampleViews()}, strings.NewReader("q"), &out)
		done <- outcome{result, err}
	}()

	select {
	case got := <-done:
		if got.err != nil {
			t.Fatalf("Run: %v", got.err)
		}
		if got.result.ApplyRequested {
			t.Fatal("quitting with q must not request an apply")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after q")
	}
}

func TestResultOfReadsTheUsersDecisionOffTheFinalModel(t *testing.T) {
	if (resultOf(Model{applyRequested: true})) != (Result{ApplyRequested: true}) {
		t.Fatal("a confirmed plan screen must request an apply")
	}
	if resultOf(Model{}) != (Result{}) {
		t.Fatal("a model that never confirmed must not request an apply")
	}
	if resultOf(nil) != (Result{}) {
		t.Fatal("a missing model must not request an apply")
	}
}
```

In `internal/cli/tui_test.go`, make these edits.

Replace the import block with:

```go
import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/JtheGunner/omnishell/internal/cli"
	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/JtheGunner/omnishell/internal/modedit"
	"github.com/JtheGunner/omnishell/internal/pkgmgr"
	"github.com/JtheGunner/omnishell/internal/tui"
)
```

In `tuiSpy`, replace the UI runner literal

```go
		func(b tui.Backend, _ io.Reader, _ io.Writer) error {
			got = &b
			return nil
		},
```

with

```go
		func(b tui.Backend, _ io.Reader, _ io.Writer) (tui.Result, error) {
			got = &b
			return tui.Result{}, nil
		},
```

In `TestTUIRefusesWithoutAnInteractiveTerminal`, replace

```go
	cli.SetTUIForTest(nil, func(tui.Backend, io.Reader, io.Writer) error { started = true; return nil })
```

with

```go
	cli.SetTUIForTest(nil, func(tui.Backend, io.Reader, io.Writer) (tui.Result, error) { started = true; return tui.Result{}, nil })
```

and append these tests and helpers at the end of the file:

```go
// handoffRun makes the fake UI report the given result, as the real one does
// when the user leaves the plan screen.
func handoffRun(t *testing.T, result tui.Result, runErr error) {
	t.Helper()
	cli.SetTUIForTest(
		func(io.Reader, io.Writer) bool { return true },
		func(tui.Backend, io.Reader, io.Writer) (tui.Result, error) { return result, runErr },
	)
	t.Cleanup(func() { cli.SetTUIForTest(nil, nil) })
}

// askedQuestions replaces the confirmation prompt with one that records the
// questions and answers with answer.
func askedQuestions(t *testing.T, answer bool) *[]string {
	t.Helper()
	var asked []string
	cli.SetPromptForTest(func(question string) bool {
		asked = append(asked, question)
		return answer
	})
	t.Cleanup(func() { cli.SetPromptForTest(nil) })
	return &asked
}

// zshHostWithCompletion is a host with zsh, initialised and with the completion
// module enabled, ready for a first apply. It returns the init file's path.
func zshHostWithCompletion(t *testing.T) string {
	t.Helper()
	home, _ := setupModuleCLITest(t)
	cli.SetLookPathForTest(zshPresentLookPath)
	t.Cleanup(func() { cli.SetLookPathForTest(nil) })
	var out, errb bytes.Buffer
	for _, args := range [][]string{{"init"}, {"enable", "completion"}} {
		if code := cli.Execute(args, &out, &errb); code != 0 {
			t.Fatalf("%v exit %d: %s", args, code, errb.String())
		}
	}
	return filepath.Join(home, ".config", "omnishell", "init.zsh")
}

func TestTUIDoesNotApplyUnlessTheUserConfirmedThePlan(t *testing.T) {
	initFile := zshHostWithCompletion(t)
	handoffRun(t, tui.Result{}, nil)
	asked := askedQuestions(t, true)

	var out, errb bytes.Buffer
	if code := cli.Execute([]string{"tui"}, &out, &errb); code != 0 {
		t.Fatalf("exit = %d (stderr: %s)", code, errb.String())
	}

	if len(*asked) != 0 {
		t.Fatalf("apply must not run, but it asked %v", *asked)
	}
	if _, err := os.Stat(initFile); !os.IsNotExist(err) {
		t.Fatalf("init.zsh must not exist (err=%v)", err)
	}
}

// The plan screen is a preview. The prompt of the real apply stays the gate: a
// "no" there must change nothing, and the exit code is apply's own.
func TestTUIHandsOverToApplyWhichStillAsksBeforeChangingAnything(t *testing.T) {
	initFile := zshHostWithCompletion(t)
	handoffRun(t, tui.Result{ApplyRequested: true}, nil)
	asked := askedQuestions(t, false)

	var out, errb bytes.Buffer
	code := cli.Execute([]string{"tui"}, &out, &errb)

	if code != 1 {
		t.Fatalf("exit = %d, want 1 (apply was declined); stderr: %s", code, errb.String())
	}
	if !reflect.DeepEqual(*asked, []string{"Proceed?"}) {
		t.Fatalf("apply must ask once before it changes anything, asked %v", *asked)
	}
	if !strings.Contains(errb.String(), "aborted") {
		t.Fatalf("stderr = %q, want apply's own 'aborted'", errb.String())
	}
	if !strings.Contains(out.String(), "Plan (") {
		t.Fatalf("apply shows its plan before it asks; stdout:\n%s", out.String())
	}
	if _, err := os.Stat(initFile); !os.IsNotExist(err) {
		t.Fatalf("a declined apply must write nothing (err=%v)", err)
	}
}

func TestTUIHandoffRunsTheRealApplyWhenTheUserSaysYes(t *testing.T) {
	initFile := zshHostWithCompletion(t)
	handoffRun(t, tui.Result{ApplyRequested: true}, nil)
	askedQuestions(t, true)

	var out, errb bytes.Buffer
	if code := cli.Execute([]string{"tui"}, &out, &errb); code != 0 {
		t.Fatalf("exit = %d (stderr: %s)", code, errb.String())
	}

	body, err := os.ReadFile(initFile)
	if err != nil || !strings.Contains(string(body), "# >>> omnishell:completion") {
		t.Fatalf("apply did not write the completion section (err=%v):\n%s", err, body)
	}
}

func TestTUIHandoffStreamsInstallOutputLikeApplyDoes(t *testing.T) {
	zshHostWithCompletion(t)
	handoffRun(t, tui.Result{ApplyRequested: true}, nil)
	askedQuestions(t, false)
	cli.SetRunnerForTest(nil)
	var runnerWriters []io.Writer
	cli.SetRunnerFactoryForTest(func(stdout, _ io.Writer) pkgmgr.Runner {
		runnerWriters = append(runnerWriters, stdout)
		return &pkgmgr.MockRunner{}
	})
	t.Cleanup(func() { cli.SetRunnerFactoryForTest(nil) })

	var out, errb bytes.Buffer
	cli.Execute([]string{"tui"}, &out, &errb)

	if len(runnerWriters) < 2 {
		t.Fatalf("want an engine for the UI and one for apply, got %d", len(runnerWriters))
	}
	if runnerWriters[0] != io.Discard {
		t.Fatal("the UI's engine must stay quiet")
	}
	if last := runnerWriters[len(runnerWriters)-1]; last == io.Discard {
		t.Fatal("the apply that follows the UI must stream installs and hooks to the terminal")
	}
}

func TestTUIDoesNotApplyWhenTheUIFailed(t *testing.T) {
	zshHostWithCompletion(t)
	handoffRun(t, tui.Result{ApplyRequested: true}, errors.New("terminal went away"))
	asked := askedQuestions(t, true)

	var out, errb bytes.Buffer
	code := cli.Execute([]string{"tui"}, &out, &errb)

	if code != 1 || !strings.Contains(errb.String(), "terminal went away") {
		t.Fatalf("exit=%d stderr=%q, want the UI's error", code, errb.String())
	}
	if len(*asked) != 0 {
		t.Fatalf("apply must not run after a failed UI, asked %v", *asked)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go vet ./internal/tui/ ./internal/cli/`
Expected: FAIL to build. `internal/tui/run_test.go` reports `assignment mismatch: 2 variables but Run returns 1 value`; `internal/cli/tui_test.go` reports `undefined: tui.Result`.

- [ ] **Step 3: Implement**

Replace `internal/tui/run.go`:

```go
package tui

import (
	"fmt"
	"io"

	tea "charm.land/bubbletea/v2"
)

// Result is what the user decided before the UI closed.
type Result struct {
	// ApplyRequested is true when the user confirmed the plan screen. The UI
	// does not apply anything itself: the caller runs apply once the terminal
	// is back to normal.
	ApplyRequested bool
}

// Run loads the modules from b and runs the browser on in and out until the
// user quits. The modules are loaded before the terminal is touched, so a
// failure reaches the caller as a plain error and never leaves a half-drawn
// screen behind.
func Run(b Backend, in io.Reader, out io.Writer) (Result, error) {
	views, err := b.Modules()
	if err != nil {
		return Result{}, err
	}
	final, err := tea.NewProgram(New(b, views), tea.WithInput(in), tea.WithOutput(out)).Run()
	if err != nil {
		return Result{}, fmt.Errorf("run terminal UI: %w", err)
	}
	return resultOf(final), nil
}

// resultOf reads the user's decision off the model the program ended with.
func resultOf(final tea.Model) Result {
	m, ok := final.(Model)
	return Result{ApplyRequested: ok && m.applyRequested}
}
```

Replace `internal/cli/tui.go` (the runner returns a `Result`; after the UI has closed, a confirmed plan runs the real apply command; the help text mentions the plan preview):

```go
package cli

import (
	"errors"
	"io"
	"os"

	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/JtheGunner/omnishell/internal/engine"
	"github.com/JtheGunner/omnishell/internal/modedit"
	"github.com/JtheGunner/omnishell/internal/tui"
)

// Seams for tests: whether the process is attached to a terminal, and the
// terminal UI itself. Tests reassign them via SetTUIForTest.
var (
	tuiIsTerminal = defaultTUIIsTerminal
	tuiRun        = tui.Run
)

// SetTUIForTest swaps the terminal check and the UI runner. Passing nil for
// either restores the real implementation.
func SetTUIForTest(isTerminal func(in io.Reader, out io.Writer) bool, run func(b tui.Backend, in io.Reader, out io.Writer) (tui.Result, error)) {
	tuiIsTerminal = defaultTUIIsTerminal
	if isTerminal != nil {
		tuiIsTerminal = isTerminal
	}
	tuiRun = tui.Run
	if run != nil {
		tuiRun = run
	}
}

// defaultTUIIsTerminal reports whether both in and out are terminals.
func defaultTUIIsTerminal(in io.Reader, out io.Writer) bool {
	inFile, ok := in.(*os.File)
	if !ok {
		return false
	}
	outFile, ok := out.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(inFile.Fd()) && term.IsTerminal(outFile.Fd())
}

// tuiBackend adapts a modedit.Editor, and the engine inside it, to the
// tui.Backend the UI consumes.
type tuiBackend struct {
	editor   modedit.Editor
	cfgPath  string
	lockPath string
}

func (b tuiBackend) Modules() ([]modedit.ModuleView, error) { return b.editor.Views() }

func (b tuiBackend) Statuses() (map[string]modedit.Status, error) { return b.editor.Statuses() }

func (b tuiBackend) Enable(id string) error  { return userMessage(b.editor.Enable(id)) }
func (b tuiBackend) Disable(id string) error { return userMessage(b.editor.Disable(id)) }

// Plan shows what `omnishell apply` would do for the config as it is now. It
// reads config.toml again, so toggles made in the UI are included.
func (b tuiBackend) Plan() (tui.PlanPreview, error) {
	cfg, err := config.Load(b.cfgPath)
	if err != nil {
		return tui.PlanPreview{}, userMessage(err)
	}
	preview, err := b.editor.Engine.Preview(cfg, b.lockPath)
	if err != nil {
		return tui.PlanPreview{}, userMessage(err)
	}
	return tui.PlanPreview{Text: preview.Text, NeedsApply: preview.NeedsApply}, nil
}

// userMessage reduces a config.Error to its message. The TUI shows it on a
// one-line status bar, where the config path that prefixes every config.Error
// would push the actual reason off the screen.
func userMessage(err error) error {
	var cfgErr config.Error
	if errors.As(err, &cfgErr) {
		return errors.New(cfgErr.Msg)
	}
	return err
}

func newTUICmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Browse modules in a full-screen terminal UI",
		Long: `Browse modules in a full-screen terminal UI.

Shows every known module with its description, homepage, package status,
platforms and shells. Move with the arrow keys, press space to enable or disable
the selected module, type / to filter, q to quit. Space writes config.toml at
once, exactly like 'omnishell enable' and 'disable'; it never touches your
shells. Press a to preview the plan; confirming it closes the UI and runs
'omnishell apply', which still asks before it changes anything.

Needs an interactive terminal; in scripts use 'omnishell list'.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			in, out := cmd.InOrStdin(), cmd.OutOrStdout()
			if !tuiIsTerminal(in, out) {
				return engine.ConfigError{Err: errors.New(
					"omnishell tui needs an interactive terminal; use 'omnishell list', 'enable' and 'apply' in scripts")}
			}

			// The engine's runner copies a package manager's output to the writers
			// it is given, and the UI owns the terminal: send it nowhere.
			e, cfgPath, lockPath, err := buildEngine(io.Discard, io.Discard)
			if err != nil {
				return err
			}
			// A missing or malformed config.toml must stop us before the screen
			// is taken over, so the message lands in the normal terminal.
			if _, err := config.Load(cfgPath); err != nil {
				return hintIfUninitialised(cmd, err)
			}

			backend := tuiBackend{
				editor:   modedit.Editor{Engine: e, CfgPath: cfgPath},
				cfgPath:  cfgPath,
				lockPath: lockPath,
			}
			result, err := tuiRun(backend, in, out)
			if err != nil || !result.ApplyRequested {
				return err
			}
			return handOffToApply(cmd)
		},
	}
}

// handOffToApply runs `omnishell apply` the way the user would have typed it,
// now that the UI has given the terminal back: its plan, its confirmation
// prompt, sudo, hooks and exit codes are exactly those of the real command.
// The UI's plan screen is a preview, not a confirmation.
func handOffToApply(cmd *cobra.Command) error {
	apply := newApplyCmd() // every flag at its default
	apply.SetIn(cmd.InOrStdin())
	apply.SetOut(cmd.OutOrStdout())
	apply.SetErr(cmd.ErrOrStderr())
	return runApply(apply, false)
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `gofmt -l internal && go build ./... && go vet ./... && go test ./internal/tui/ ./internal/cli/ -race -count=1`
Expected: `gofmt -l` prints nothing; build and vet clean; both packages PASS (86 tests in `tui`, 18 `TUI` tests in `cli`).

- [ ] **Step 5: Commit**

```bash
git add internal/tui internal/cli
git commit -m "feat: hand over to apply when the plan is confirmed in the tui"
```

---

### Task 6: Documentation and verification

**Files:**
- Modify: `README.md`, `CHANGELOG.md`, `CLAUDE.md`

**Interfaces:**
- Consumes: everything above.
- Produces: nothing code-level.

- [ ] **Step 1: Update the README row**

In `README.md`, replace the Commands-table row for `omnishell tui` (find it with `grep -n 'omnishell tui' README.md`) with:

```markdown
| `omnishell tui`                        | Browse and toggle modules in a full-screen terminal UI: a module list with description, homepage, package status, platforms and shells, a `/` filter, and Space to enable or disable the selected module (written to `config.toml` at once, like `enable` / `disable`). `a` previews the plan; confirming it closes the UI and runs `omnishell apply`, which still asks before it changes anything. Modules that cannot run on this host are dimmed. Needs an interactive terminal (exit 2 otherwise).                                                                                          | —                                                                                                                                                                                                                                                                                                               |
```

- [ ] **Step 2: Update the changelog entry**

In `CHANGELOG.md`, in the `omnishell tui` bullet under `## [Unreleased]` → `### Added`, replace the sentences from `Space enables` to `dimmed with the reason shown.` so the whole bullet reads:

```markdown
- `omnishell tui`: a full-screen terminal UI to browse modules (description,
  homepage, package status, platforms, shells) with a `/` filter. Space enables
  or disables the selected module in `config.toml`, like `enable` / `disable`.
  `a` previews the plan; confirming it closes the UI and runs `omnishell apply`,
  which still asks before it changes anything. Modules that cannot run on this
  host are dimmed with the reason shown. It needs an interactive terminal. It
  brings the first runtime dependencies, Bubble Tea and Lip Gloss, so the
  binary grows by about 1.5 MiB.
```

- [ ] **Step 3: Replace the `CLAUDE.md` entry for `internal/tui`**

In `CLAUDE.md`, replace item 11 (from `11. **\`internal/tui\`**` up to, not including, the `### Key invariants` heading; it is also re-wrapped, which fixes its ragged lines) with:

```markdown
11. **`internal/tui`** — the Bubble Tea v2 module browser behind
   `omnishell tui` (`charm.land/bubbletea/v2`, `charm.land/lipgloss/v2`). It
   imports neither `cli` nor `engine`: everything comes through its `Backend`
   interface, which `internal/cli/tui.go` implements over `modedit` and the
   engine. The command refuses a non-terminal and a missing or malformed config
   before the screen is taken over.
   `Update` stays free of I/O: toggling returns a `tea.Cmd` that writes through
   `modedit.Enable`/`Disable` and then re-reads only the module statuses
   (`Backend.Statuses`; `Modules` probes the package manager and takes seconds),
   one write at a time. The plan screen (`a`) asks `Backend.Plan`, which is
   `Engine.Preview`: the plan text plus apply's own "nothing to do" rule, so the
   screen never promises a change apply would not make. `y` only sets
   `Result.ApplyRequested`; once the UI has closed, `internal/cli/tui.go` runs
   the real apply command (`handOffToApply`), so its prompt, sudo and exit codes
   stay the gate. The UI's own engine is built with `io.Discard` writers,
   because the runner copies package-manager output to them and the UI owns the
   terminal.
   Any text that comes from a backend or a manifest (module fields, plan text,
   error messages) must go through `sanitize` before it is drawn. Rendering
   tests compare `View().Content` (styling stripped) with golden files in
   `internal/tui/testdata`; regenerate them with
   `go test ./internal/tui -run Golden -update` and review the diff by eye.

```

- [ ] **Step 4: Run the full verification**

Run: `make vet && make test`
Expected: both clean; every package PASS.

Run: `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2 run ./...`
Expected: `0 issues.`

Run the release-target builds:
```bash
for t in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do GOOS=${t%/*} GOARCH=${t#*/} CGO_ENABLED=0 go build -o /dev/null ./cmd/omnishell && echo "$t ok"; done
for v in 6 7; do GOOS=linux GOARCH=arm GOARM=$v CGO_ENABLED=0 go build -o /dev/null ./cmd/omnishell && echo "linux/armv$v ok"; done
```
Expected: six `ok` lines.

Run: `git diff origin/main --stat -- go.mod go.sum`
Expected: no output (no dependency changes).

- [ ] **Step 5: Check the handoff with the real binary in a pseudo-terminal**

The riskiest step is that `apply`'s prompt can read your answer after Bubble Tea has released the terminal. The unit tests fake the UI, so check it once with the real binary. This only ever uses a throwaway `HOME`; never point it at a real config. Save this driver outside the repository (for example in the scratch directory) as `pty_drive.py`:

```python
import os, pty, sys, time, select, fcntl, termios, struct, re, signal

BIN, HOME, LOG = sys.argv[1], sys.argv[2], sys.argv[3]
steps = sys.argv[4].split("|")
signal.alarm(60)                     # hard stop for the whole run
logf = open(LOG, "w", buffering=1)
def log(*a):
    logf.write(" ".join(str(x) for x in a) + "\n")

env = dict(os.environ, HOME=HOME, XDG_CONFIG_HOME=HOME + "/.config", TERM="xterm-256color", SHELL="/bin/zsh")
pid, fd = pty.fork()
if pid == 0:
    os.execve(BIN, [BIN, "tui"], env)
fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", 30, 100, 0, 0))
ANSI = re.compile(rb"\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(\x07|\x1b\\)|\x1b[=>]")
seen = b""

def pump(timeout):
    global seen
    r, _, _ = select.select([fd], [], [], timeout)
    if not r:
        return True
    try:
        chunk = os.read(fd, 65536)
    except OSError:
        return False
    if not chunk:
        return False
    seen += chunk
    return True

def wait_for(marker, timeout=15):
    global seen
    end = time.time() + timeout
    while time.time() < end:
        if marker.encode() in ANSI.sub(b"", seen):
            seen = b""
            return True
        if not pump(0.2):
            return marker.encode() in ANSI.sub(b"", seen)
    return False

for step in steps:
    kind, _, val = step.partition(":")
    if kind == "wait":
        log("waiting for", repr(val)); log("  ->", wait_for(val))
    elif kind == "send":
        data = val.encode().decode("unicode_escape").encode()
        log("sending", repr(data)); os.write(fd, data); time.sleep(0.3)
log("steps done; waiting for the child to exit")
code = None
end = time.time() + 15
while time.time() < end:
    r, st = os.waitpid(pid, os.WNOHANG)
    if r:
        code = os.WEXITSTATUS(st) if os.WIFEXITED(st) else "signal %d" % os.WTERMSIG(st)
        break
    pump(0.2)
if code is None:
    log("CHILD DID NOT EXIT; killing"); os.kill(pid, signal.SIGKILL); os.waitpid(pid, 0); code = "killed"
log("exit code:", code)
for _ in range(20):
    if not pump(0.1): break
tail = ANSI.sub(b"", seen).decode("utf-8", "replace").replace("\r", "")
log("last output:", " | ".join(l.strip() for l in tail.split("\n") if l.strip())[-500:])
```

Then (every command that needs the throwaway `HOME` gets it through the `E` function and `env`; nothing is exported, and a function, unlike a variable holding a command, works the same in bash and zsh):

```bash
T=$(mktemp -d) && go build -o $T/omnishell ./cmd/omnishell && mkdir -p $T/home
E() { env HOME=$T/home XDG_CONFIG_HOME=$T/home/.config "$@"; }
E $T/omnishell init >/dev/null && E $T/omnishell enable completion
# 1. plan, confirm, then decline apply's own prompt
python3 pty_drive.py $T/omnishell $T/home $T/run1.log "wait:modules|send:a|wait:continue to apply|send:y|wait:Proceed?|send:n\\n"; cat $T/run1.log
ls $T/home/.config/omnishell/
# 2. plan, go back, quit: nothing is applied
python3 pty_drive.py $T/omnishell $T/home $T/run2.log "wait:modules|send:a|wait:continue to apply|send:\\x1b|wait:modules|send:q"; cat $T/run2.log
# 3. plan, confirm, accept apply's prompt
python3 pty_drive.py $T/omnishell $T/home $T/run3.log "wait:modules|send:a|wait:continue to apply|send:y|wait:Proceed?|send:y\\n"; cat $T/run3.log
ls $T/home/.config/omnishell/ && grep -c 'omnishell:completion' $T/home/.config/omnishell/init.zsh
rm -rf $T
```

Expected:
- Run 1: the log shows every `wait` answered `True`, ends with `exit code: 1` and `last output:` containing `aborted`; the directory listing has no `init.zsh`.
- Run 2: `exit code: 0`.
- Run 3: `exit code: 0` and `last output:` containing `applied  completion`; the listing now has `init.zsh`, `init.bash` and `state.lock.json`, and the `grep -c` prints at least `1`.

(The driver starts the binary itself and kills it after a hard 60-second limit, so a hang shows up as a failed step in the log, not as a stuck session.)

- [ ] **Step 6: Commit**

```bash
git add README.md CHANGELOG.md CLAUDE.md
git commit -m "docs: describe the plan preview in the omnishell tui"
```
