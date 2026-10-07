# TUI Skeleton and Module Browser Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `omnishell tui`, a full-screen, read-only module browser (list, detail pane, `/` filter) built on `modedit.Views`.

**Architecture:** A new `internal/tui` package holds a Bubble Tea v2 model whose `Update` is a pure function of `(model, msg)`. The package imports neither `cli` nor `engine`; it reads modules through a one-method `Backend` interface. `internal/cli/tui.go` implements that interface over `modedit.Editor`, guards the entry (terminal, config), and runs the program.

**Tech Stack:** Go 1.26, `charm.land/bubbletea/v2` v2.0.10, `charm.land/lipgloss/v2` v2.0.6, `github.com/charmbracelet/x/ansi` v0.11.8 and `github.com/charmbracelet/x/term` v0.2.2 (both already required by lipgloss).

**Spec:** `docs/superpowers/specs/2026-10-07-interactive-tui-design.md` (sections Architecture, Screens → Browser, Startup guards, Dependencies, Testing; Sub-tasks item 2). YouTrack: OMNIS-31.

## Global Constraints

- `internal/tui` imports neither `internal/cli` nor `internal/engine`; its only link to the rest of omnishell is `modedit.ModuleView` and the `Backend` interface.
- The TUI never writes `config.toml` or any other file in this sub-task: no `Enable`, `Disable`, `SetOption`, plan preview or `apply` (out of scope; later sub-tasks).
- Startup guards: stdin and stdout must be TTYs, otherwise exit 2; a missing `config.toml` prints `run 'omnishell init' first` and exits 2; a malformed config exits 2 with the usual `config.Error`. All of it happens before the screen is taken over.
- Below 80×20 the browser is replaced by a single "terminal too small" line.
- New dependencies only under `internal/tui` and `internal/cli/tui.go`; the release targets (linux/amd64, linux/arm64, linux/armv6, linux/armv7, darwin/amd64, darwin/arm64) must still build with `CGO_ENABLED=0`.
- The binary-size delta is measured and recorded for the PR description (not in repo docs).
- Existing tests are not edited or deleted; no new test framework (plain `testing`).
- All identifiers, comments and commit messages are in English; commit subjects use `<type>: <description>` with no attribution trailers.

## Rulings carried into this plan

These differ from or sharpen the spec; the executor does not need to rule on them again.

- **Bubble Tea v2, not v1.** v2 is the current stable line (`charm.land/bubbletea/v2`); its `View()` returns `tea.View`, keys arrive as `tea.KeyPressMsg`, and the alternate screen is requested through `View.AltScreen`. Every API call in this plan was compiled and run against v2.0.10 / lipgloss v2.0.6.
- **No `teatest`.** `teatest` has no tagged release (pseudo-versions only) and would pull in another module tree. The spec allows direct `View()` snapshots ("teatest or direct snapshots"); this plan uses plain-text goldens of `View().Content` plus structural assertions, and one real-program smoke test (`Run` fed a `q` on a non-TTY input) for the program loop. If the PR must carry `teatest` after all, say so before Task 2.
- **`Backend` has one method here.** The spec's full interface (`Enable`, `Disable`, `SetOption`, `Plan`) arrives with the sub-tasks that need it; adding unused methods now would be dead code. `Modules()` returns `([]modedit.ModuleView, error)` because `Views()` can fail.
- **README, CHANGELOG and `CLAUDE.md` are updated here**, not deferred to sub-task 5: the command ships with this change, and the project rule is to keep docs in the same change that alters commands.
- **The `go` directive becomes `1.26.0`.** Bubble Tea v2.0.10 requires Go 1.26.0 and `go get` raises the line. CI uses `go-version-file: go.mod`, so it follows.

## Review Focus

- Missing or malformed `config.toml` must stop the command before the screen is taken over, with exit 2 and the normal message (Task 3 tests).
- A pipe, redirect or CI run (no TTY) must exit 2 immediately instead of hanging or spraying escape codes (Task 3 test).
- Shrinking the terminal below 80×20 and growing it again must keep the selection and the filter (Task 2 test).
- A very long description, homepage or module id must never push the layout past the terminal's width or height (Task 2 tests).
- An empty module list, a filter with no matches, and a multi-byte character in the filter must not panic or corrupt state (Tasks 1 and 2 tests).

---

### Task 1: Dependencies, `Backend` and the browser model

**Files:**
- Modify: `go.mod`, `go.sum`
- Create: `internal/tui/backend.go`
- Create: `internal/tui/model.go`
- Create: `internal/tui/view.go` (minimal; Task 2 replaces it)
- Test: `internal/tui/helpers_test.go`, `internal/tui/model_test.go`, `internal/tui/view_test.go` (first two view tests; Task 2 replaces the file)

**Interfaces:**
- Consumes: `modedit.ModuleView`, `modedit.Status*`, `modedit.Packages*`, `modedit.Origin*` (already on `main`).
- Produces:
  ```go
  type Backend interface{ Modules() ([]modedit.ModuleView, error) }
  type Model struct{ /* unexported */ }
  func New(views []modedit.ModuleView) Model
  func (m Model) Init() tea.Cmd
  func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd)
  func (m Model) View() tea.View
  // unexported, used by later tasks and tests:
  const minWidth, minHeight = 80, 20
  func (m Model) selected() (modedit.ModuleView, bool)
  func statusBox(s modedit.Status) string
  ```
  Test helpers produced here and used by Tasks 2 and 3: `sampleViews()`, `manyViews(n)`, `key(name)`, `press(t, m, names...)`, `sized(m, w, h)`, `plain(m)`, `visibleIDs(m)`, `assertFits(t, out, w, h)`, `assertGolden(t, name, got)` and the `-update` flag.

- [ ] **Step 1: Add the dependencies**

Run:
```bash
go get charm.land/bubbletea/v2@v2.0.10 charm.land/lipgloss/v2@v2.0.6 github.com/charmbracelet/x/ansi@v0.11.8
```
Expected: the three modules are added; `go.mod`'s `go` line reads `go 1.26.0`. (`go mod tidy` in Step 5 drops anything not yet imported, so `x/term` is added in Task 3, not here.)

- [ ] **Step 2: Write the failing tests**

`internal/tui/helpers_test.go`:

```go
package tui

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/JtheGunner/omnishell/internal/modedit"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata/")

func sampleViews() []modedit.ModuleView {
	return []modedit.ModuleView{
		{
			ID: "completion", Name: "Completion", Description: "Tab completion tuning",
			Status: modedit.StatusEnabled, Packages: modedit.PackagesNA,
			Platforms: []string{"macos", "linux"}, Shells: []string{"zsh", "bash"},
			Origin: modedit.OriginBuiltin,
		},
		{
			ID: "fzf", Name: "FZF Fuzzy Finder", Description: "Ctrl+R history search",
			Homepage: "https://github.com/junegunn/fzf",
			Status:   modedit.StatusDisabled, Packages: modedit.PackagesMissing,
			Platforms: []string{"macos", "linux"}, Shells: []string{"zsh", "bash"},
			Origin: modedit.OriginBuiltin, OptionCount: 3,
		},
		{
			ID: "zshonly", Name: "Zsh only", Description: "A module for zsh alone",
			Status: modedit.StatusDisabled, Packages: modedit.PackagesNA,
			Platforms: []string{"linux"}, Shells: []string{"zsh"},
			Origin: modedit.OriginUser,
		},
	}
}

// manyViews returns n modules named mod00, mod01, ... for scrolling tests.
func manyViews(n int) []modedit.ModuleView {
	views := make([]modedit.ModuleView, n)
	for i := range views {
		id := "mod" + string(rune('0'+i/10)) + string(rune('0'+i%10))
		views[i] = modedit.ModuleView{ID: id, Name: id, Description: "module " + id, Status: modedit.StatusDisabled, Packages: modedit.PackagesNA}
	}
	return views
}

// key builds the key press a terminal would deliver for a key name such as
// "up", "esc", "ctrl+c", or a single typed character.
func key(name string) tea.KeyPressMsg {
	switch name {
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "ctrl+c":
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	default:
		r := []rune(name)
		return tea.KeyPressMsg{Code: r[0], Text: name}
	}
}

// press feeds keys to m one by one and returns the resulting model.
func press(t *testing.T, m Model, names ...string) Model {
	t.Helper()
	for _, name := range names {
		next, _ := m.Update(key(name))
		m = next.(Model)
	}
	return m
}

// sized returns m after a terminal of w x h cells has been reported.
func sized(m Model, w, h int) Model {
	next, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return next.(Model)
}

// plain renders m without styling, so assertions see only the text.
func plain(m Model) string {
	return ansi.Strip(m.View().Content)
}

// visibleIDs returns the ids of the modules that pass the filter, in order.
func visibleIDs(m Model) []string {
	ids := make([]string, len(m.visible))
	for i, idx := range m.visible {
		ids[i] = m.views[idx].ID
	}
	return ids
}

// assertFits fails unless out is exactly h lines, none wider than w cells.
func assertFits(t *testing.T, out string, w, h int) {
	t.Helper()
	lines := strings.Split(out, "\n")
	if len(lines) != h {
		t.Fatalf("view has %d lines, want %d:\n%s", len(lines), h, out)
	}
	for i, line := range lines {
		if got := ansi.StringWidth(line); got > w {
			t.Fatalf("line %d is %d cells wide, want at most %d: %q", i+1, got, w, line)
		}
	}
}

// assertGolden compares got with testdata/<name>.golden; run the tests with
// -update to (re)write the file after reviewing the change by eye.
func assertGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatalf("create testdata: %v", err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden (create it with -update): %v", err)
	}
	if string(want) != got {
		t.Fatalf("view differs from %s (re-run with -update if intended)\n--- got ---\n%s\n--- want ---\n%s", path, got, want)
	}
}
```

`internal/tui/model_test.go`:

```go
package tui

import (
	"reflect"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestNewShowsEveryModuleWithCursorOnTheFirst(t *testing.T) {
	m := New(sampleViews())

	if got, want := visibleIDs(m), []string{"completion", "fzf", "zshonly"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("visible = %v, want %v", got, want)
	}
	if v, ok := m.selected(); !ok || v.ID != "completion" {
		t.Fatalf("selected = %q ok=%v, want completion", v.ID, ok)
	}
}

func TestCursorMovesWithArrowsAndVimKeysAndStopsAtBothEnds(t *testing.T) {
	m := New(sampleViews())

	m = press(t, m, "up")
	if m.cursor != 0 {
		t.Fatalf("cursor above the top = %d, want 0", m.cursor)
	}
	m = press(t, m, "down", "j")
	if m.cursor != 2 {
		t.Fatalf("cursor after down, j = %d, want 2", m.cursor)
	}
	m = press(t, m, "down")
	if m.cursor != 2 {
		t.Fatalf("cursor below the bottom = %d, want 2", m.cursor)
	}
	m = press(t, m, "k", "up")
	if m.cursor != 0 {
		t.Fatalf("cursor after k, up = %d, want 0", m.cursor)
	}
}

func TestQuitKeys(t *testing.T) {
	for _, name := range []string{"q", "ctrl+c"} {
		_, cmd := New(sampleViews()).Update(key(name))
		if cmd == nil {
			t.Fatalf("%s: no command returned", name)
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Fatalf("%s: command did not quit", name)
		}
	}
}

func TestFilterNarrowsByIDOrDescriptionIgnoringCase(t *testing.T) {
	m := New(sampleViews())

	m = press(t, m, "/", "F", "Z")
	if got, want := visibleIDs(m), []string{"fzf"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("filter FZ: visible = %v, want %v", got, want)
	}

	m = press(t, m, "backspace", "backspace", "a", "l", "o", "n", "e")
	if got, want := visibleIDs(m), []string{"zshonly"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("filter alone (description match): visible = %v, want %v", got, want)
	}
}

func TestFilterBackspaceWidensAndEmptyBackspaceIsHarmless(t *testing.T) {
	m := press(t, New(sampleViews()), "/", "f", "z")
	m = press(t, m, "backspace", "backspace", "backspace")

	if len(m.visible) != 3 || m.filter != "" || !m.filtering {
		t.Fatalf("visible=%d filter=%q filtering=%v, want 3, empty, still typing", len(m.visible), m.filter, m.filtering)
	}
}

func TestFilterBackspaceRemovesAWholeMultiByteCharacter(t *testing.T) {
	m := press(t, New(sampleViews()), "/", "ä")
	if m.filter != "ä" {
		t.Fatalf("filter = %q, want ä", m.filter)
	}

	m = press(t, m, "backspace")
	if m.filter != "" {
		t.Fatalf("filter after backspace = %q, want empty (not a broken byte)", m.filter)
	}
}

func TestQDoesNotQuitWhileTypingAFilter(t *testing.T) {
	m := New(sampleViews())
	m = press(t, m, "/")

	next, cmd := m.Update(key("q"))
	if cmd != nil {
		t.Fatal("q while filtering must be typed into the filter, not quit")
	}
	if got := next.(Model).filter; got != "q" {
		t.Fatalf("filter = %q, want q", got)
	}
}

func TestCtrlCQuitsWhileTypingAFilter(t *testing.T) {
	m := press(t, New(sampleViews()), "/", "f")

	_, cmd := m.Update(key("ctrl+c"))
	if cmd == nil {
		t.Fatal("ctrl+c while filtering returned no command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("ctrl+c while filtering did not quit")
	}
}

func TestEnterKeepsTheFilterAndLeavesTypingMode(t *testing.T) {
	m := press(t, New(sampleViews()), "/", "f", "z", "enter")

	if m.filtering || m.filter != "fz" || len(m.visible) != 1 {
		t.Fatalf("filtering=%v filter=%q visible=%d, want false, fz, 1", m.filtering, m.filter, len(m.visible))
	}
	// Back in browsing mode, q quits again.
	if _, cmd := m.Update(key("q")); cmd == nil {
		t.Fatal("q after enter should quit")
	}
}

func TestEscCancelsTypingAndEscClearsAKeptFilter(t *testing.T) {
	m := press(t, New(sampleViews()), "/", "f", "z", "esc")
	if m.filtering || m.filter != "" || len(m.visible) != 3 {
		t.Fatalf("esc while typing: filtering=%v filter=%q visible=%d, want false, empty, 3", m.filtering, m.filter, len(m.visible))
	}

	m = press(t, m, "/", "f", "z", "enter", "esc")
	if m.filter != "" || len(m.visible) != 3 {
		t.Fatalf("esc on a kept filter: filter=%q visible=%d, want empty, 3", m.filter, len(m.visible))
	}
}

func TestFilteringMovesTheCursorBackToTheTopOfTheNewList(t *testing.T) {
	m := press(t, New(sampleViews()), "down", "down")
	if m.cursor != 2 {
		t.Fatalf("setup: cursor = %d, want 2", m.cursor)
	}

	m = press(t, m, "/", "f")
	if v, ok := m.selected(); m.cursor != 0 || !ok || v.ID != "fzf" {
		t.Fatalf("cursor=%d selected=%q ok=%v, want 0 on fzf", m.cursor, v.ID, ok)
	}
}

func TestEmptyAndUnmatchedListsAreSafe(t *testing.T) {
	m := New(nil)
	m = press(t, m, "down", "up", "j", "k", "/", "x", "esc")
	if _, ok := m.selected(); ok {
		t.Fatal("an empty list has no selection")
	}

	m = press(t, New(sampleViews()), "/", "n", "o", "p", "e", "x")
	if len(m.visible) != 0 {
		t.Fatalf("visible = %v, want none", visibleIDs(m))
	}
	m = press(t, m, "down", "enter")
	if _, ok := m.selected(); ok {
		t.Fatal("no match means no selection")
	}
}

func TestWindowSizeIsRecorded(t *testing.T) {
	m := sized(New(sampleViews()), 100, 30)

	if m.width != 100 || m.height != 30 {
		t.Fatalf("size = %dx%d, want 100x30", m.width, m.height)
	}
}

func TestViewRequestsTheAlternateScreen(t *testing.T) {
	if !New(sampleViews()).View().AltScreen {
		t.Fatal("the browser must run on the alternate screen")
	}
}
```

`internal/tui/view_test.go` (only the two size tests for now):

```go
package tui

import (
	"strings"
	"testing"
)

func TestViewIsEmptyUntilTheTerminalSizeIsKnown(t *testing.T) {
	if got := New(sampleViews()).View().Content; got != "" {
		t.Fatalf("view before the first size message = %q, want empty", got)
	}
}

func TestViewShowsATooSmallMessageBelowTheMinimumSize(t *testing.T) {
	cases := []struct{ w, h int }{{79, 20}, {80, 19}, {10, 5}}
	for _, c := range cases {
		out := plain(sized(New(sampleViews()), c.w, c.h))
		if !strings.Contains(out, "Terminal too small: need at least 80x20") {
			t.Fatalf("%dx%d: view = %q, want the too-small message", c.w, c.h, out)
		}
		if strings.Contains(out, "fzf") {
			t.Fatalf("%dx%d: the module list must not render when too small", c.w, c.h)
		}
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -count=1`
Expected: FAIL to build, `no non-test Go files in .../internal/tui` (and `undefined: New` once a file exists).

- [ ] **Step 4: Write the implementation**

`internal/tui/backend.go`:

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
}
```

`internal/tui/model.go`:

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
	views     []modedit.ModuleView
	visible   []int // indexes into views that match the filter, in order
	cursor    int   // position within visible
	filter    string
	filtering bool // true while the user is typing into the filter
	width     int  // 0 until the first tea.WindowSizeMsg
	height    int
}

// New returns a browser over views, which must already be sorted for display.
func New(views []modedit.ModuleView) Model {
	m := Model{views: views}
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
	case tea.KeyPressMsg:
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
func (m *Model) applyFilter() {
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
}

// selected returns the module under the cursor, if the filtered list is not
// empty.
func (m Model) selected() (modedit.ModuleView, bool) {
	if len(m.visible) == 0 {
		return modedit.ModuleView{}, false
	}
	return m.views[m.visible[m.cursor]], true
}
```

`internal/tui/view.go` (minimal: unknown size, too small, a plain list; Task 2 replaces it with the real layout):

```go
package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/JtheGunner/omnishell/internal/modedit"
)

// View implements tea.Model.
func (m Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	return v
}

func (m Model) render() string {
	switch {
	case m.width == 0 || m.height == 0:
		return "" // the size is unknown until the first tea.WindowSizeMsg
	case m.width < minWidth || m.height < minHeight:
		return fmt.Sprintf("Terminal too small: need at least %dx%d, have %dx%d",
			minWidth, minHeight, m.width, m.height)
	}
	return m.renderList()
}

// renderList draws one line per visible module; the full layout replaces it.
func (m Model) renderList() string {
	lines := make([]string, 0, len(m.visible))
	for pos, idx := range m.visible {
		marker := "  "
		if pos == m.cursor {
			marker = "▸ "
		}
		lines = append(lines, marker+statusBox(m.views[idx].Status)+" "+m.views[idx].ID)
	}
	return strings.Join(lines, "\n")
}

func statusBox(s modedit.Status) string {
	switch s {
	case modedit.StatusEnabled:
		return "[x]"
	case modedit.StatusDisabled:
		return "[ ]"
	default:
		return "[?]"
	}
}
```

- [ ] **Step 5: Tidy and run the tests to verify they pass**

Run: `gofmt -l internal/tui && go mod tidy && go vet ./internal/tui/ && go test ./internal/tui/ -race -count=1 -v`
Expected: `gofmt -l` prints nothing; PASS for all 16 tests (14 model, 2 view). `go.mod` lists `charm.land/bubbletea/v2`, `charm.land/lipgloss/v2` and `github.com/charmbracelet/x/ansi` as direct requirements.

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum internal/tui
git commit -m "feat: add the module browser model to the tui package"
```

---

### Task 2: The browser layout and golden snapshots

**Files:**
- Modify (replace): `internal/tui/view.go`
- Modify (replace): `internal/tui/view_test.go`
- Create: `internal/tui/testdata/*.golden` (generated)

**Interfaces:**
- Consumes: everything Task 1 produced, including the test helpers.
- Produces: the final `View()`; `listWidth`, `boxChrome` and friends stay unexported.

- [ ] **Step 1: Replace `internal/tui/view_test.go` with the full rendering tests**

```go
package tui

import (
	"strings"
	"testing"
)

func TestViewIsEmptyUntilTheTerminalSizeIsKnown(t *testing.T) {
	if got := New(sampleViews()).View().Content; got != "" {
		t.Fatalf("view before the first size message = %q, want empty", got)
	}
}

func TestViewShowsATooSmallMessageBelowTheMinimumSize(t *testing.T) {
	cases := []struct{ w, h int }{{79, 20}, {80, 19}, {10, 5}}
	for _, c := range cases {
		out := plain(sized(New(sampleViews()), c.w, c.h))
		if !strings.Contains(out, "Terminal too small: need at least 80x20") {
			t.Fatalf("%dx%d: view = %q, want the too-small message", c.w, c.h, out)
		}
		if strings.Contains(out, "fzf") {
			t.Fatalf("%dx%d: the module list must not render when too small", c.w, c.h)
		}
	}
}

func TestViewAtTheMinimumSizeRendersTheBrowser(t *testing.T) {
	out := plain(sized(New(sampleViews()), 80, 20))

	if strings.Contains(out, "too small") {
		t.Fatalf("80x20 must be big enough:\n%s", out)
	}
	assertFits(t, out, 80, 20)
}

func TestViewFitsExactlyAtSeveralSizes(t *testing.T) {
	for _, c := range []struct{ w, h int }{{80, 20}, {100, 30}, {200, 50}} {
		assertFits(t, plain(sized(New(sampleViews()), c.w, c.h)), c.w, c.h)
	}
}

func TestViewListsModulesWithStatusBoxesAndMarksTheCursor(t *testing.T) {
	out := plain(sized(New(sampleViews()), 80, 20))

	for _, want := range []string{"▸ [x] completion", "  [ ] fzf", "  [ ] zshonly"} {
		if !strings.Contains(out, want) {
			t.Fatalf("view is missing %q:\n%s", want, out)
		}
	}
}

func TestViewDetailFollowsTheCursor(t *testing.T) {
	m := sized(New(sampleViews()), 100, 30)
	m = press(t, m, "down") // fzf

	out := plain(m)
	for _, want := range []string{
		"FZF Fuzzy Finder", "fzf · builtin", "Ctrl+R history search",
		"Status:    disabled", "Packages:  missing", "Platforms: macos, linux",
		"Shells:    zsh, bash", "Options:   3", "Homepage:  https://github.com/junegunn/fzf",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("detail is missing %q:\n%s", want, out)
		}
	}
}

func TestViewDetailShowsADashWhenThereIsNoHomepage(t *testing.T) {
	out := plain(sized(New(sampleViews()), 100, 30)) // completion has none

	if !strings.Contains(out, "Homepage:  —") {
		t.Fatalf("want a dash for the missing homepage:\n%s", out)
	}
}

func TestViewScrollsTheListToKeepTheCursorVisible(t *testing.T) {
	m := sized(New(manyViews(30)), 80, 20)
	for range 29 {
		m = press(t, m, "down")
	}

	out := plain(m)
	if !strings.Contains(out, "▸ [ ] mod29") {
		t.Fatalf("the cursor row mod29 must be visible:\n%s", out)
	}
	if strings.Contains(out, "mod00") {
		t.Fatalf("mod00 must have scrolled out of view:\n%s", out)
	}
	assertFits(t, out, 80, 20)
}

func TestViewKeepsTheLayoutWhenDescriptionAndHomepageAreVeryLong(t *testing.T) {
	views := sampleViews()
	views[0].Description = strings.Repeat("a very long description ", 40)
	views[0].Homepage = "https://example.com/" + strings.Repeat("x", 300)

	assertFits(t, plain(sized(New(views), 80, 20)), 80, 20)
}

func TestViewTruncatesALongModuleIDInTheList(t *testing.T) {
	views := sampleViews()
	views[0].ID = strings.Repeat("long-module-id-", 10)

	assertFits(t, plain(sized(New(views), 80, 20)), 80, 20)
}

func TestViewSaysSoWhenThereAreNoModulesOrNoMatches(t *testing.T) {
	if out := plain(sized(New(nil), 80, 20)); !strings.Contains(out, "No modules") {
		t.Fatalf("empty registry view:\n%s", out)
	}
	assertFits(t, plain(sized(New(nil), 80, 20)), 80, 20)

	m := press(t, sized(New(sampleViews()), 80, 20), "/", "n", "o", "p", "e")
	out := plain(m)
	if !strings.Contains(out, "No matches") {
		t.Fatalf("unmatched filter view:\n%s", out)
	}
	assertFits(t, out, 80, 20)
}

func TestViewHeaderAndFooterReflectTheFilterState(t *testing.T) {
	m := sized(New(sampleViews()), 80, 20)

	typing := plain(press(t, m, "/", "f"))
	if !strings.Contains(typing, "filter: f_") || !strings.Contains(typing, "enter keep") {
		t.Fatalf("typing view:\n%s", typing)
	}

	kept := plain(press(t, m, "/", "f", "z", "enter"))
	if !strings.Contains(kept, "filter: fz (1 shown)") || !strings.Contains(kept, "q quit") {
		t.Fatalf("kept-filter view:\n%s", kept)
	}
}

// Shrinking the terminal below the minimum hides the browser but must not
// lose the user's place: growing it again shows the same selection and filter.
func TestResizingBelowTheMinimumAndBackKeepsTheState(t *testing.T) {
	m := sized(New(sampleViews()), 100, 30)
	m = press(t, m, "/", "z", "enter")

	m = sized(m, 40, 10)
	if !strings.Contains(plain(m), "Terminal too small") {
		t.Fatal("expected the too-small message at 40x10")
	}

	m = sized(m, 100, 30)
	out := plain(m)
	if !strings.Contains(out, "▸ [ ] fzf") || !strings.Contains(out, "filter: z (2 shown)") {
		t.Fatalf("state lost after the resize round-trip:\n%s", out)
	}
	assertFits(t, out, 100, 30)
}

func TestViewGoldenFiles(t *testing.T) {
	base := sized(New(sampleViews()), 80, 20)
	cases := map[string]Model{
		"browser-80x20":      base,
		"browser-second-row": press(t, base, "down"),
		"browser-filtered":   press(t, base, "/", "f", "z", "enter"),
		"browser-typing":     press(t, base, "/", "z"),
		"browser-no-modules": sized(New(nil), 80, 20),
		"browser-too-small":  sized(New(sampleViews()), 60, 10),
	}
	for name, m := range cases {
		assertGolden(t, name, plain(m))
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -count=1`
Expected: FAIL. The structural tests fail against Task 1's plain list (for example `TestViewAtTheMinimumSizeRendersTheBrowser`: the view has 3 lines, want 20) and `TestViewGoldenFiles` fails with `read golden (create it with -update)`.

- [ ] **Step 3: Replace `internal/tui/view.go` with the real layout**

```go
package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/JtheGunner/omnishell/internal/modedit"
)

const (
	listWidth = 30 // outer width of the left box, borders included
	// boxChrome is the horizontal space a box spends on its border (2) and
	// padding (2); the vertical cost is the border alone (2).
	boxChrome   = 4
	boxChromeV  = 2
	headerLines = 1
	footerLines = 1
)

var (
	boxStyle      = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	titleStyle    = lipgloss.NewStyle().Bold(true)
	selectedStyle = lipgloss.NewStyle().Reverse(true)
	dimStyle      = lipgloss.NewStyle().Faint(true)
)

// View implements tea.Model.
func (m Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	return v
}

func (m Model) render() string {
	switch {
	case m.width == 0 || m.height == 0:
		return "" // the size is unknown until the first tea.WindowSizeMsg
	case m.width < minWidth || m.height < minHeight:
		return fmt.Sprintf("Terminal too small: need at least %dx%d, have %dx%d",
			minWidth, minHeight, m.width, m.height)
	}

	bodyHeight := m.height - headerLines - footerLines
	rows := bodyHeight - boxChromeV
	rightWidth := m.width - listWidth

	left := boxStyle.Width(listWidth).Height(bodyHeight).
		Render(m.renderList(rows, listWidth-boxChrome))
	right := boxStyle.Width(rightWidth).Height(bodyHeight).
		Render(m.renderDetail(rows, rightWidth-boxChrome))

	return lipgloss.JoinVertical(lipgloss.Left,
		m.renderHeader(),
		lipgloss.JoinHorizontal(lipgloss.Top, left, right),
		m.renderFooter(),
	)
}

func (m Model) renderHeader() string {
	line := titleStyle.Render("omnishell") + dimStyle.Render(fmt.Sprintf("  %d modules", len(m.views)))
	switch {
	case m.filtering:
		line += "  filter: " + m.filter + "_"
	case m.filter != "":
		line += fmt.Sprintf("  filter: %s (%d shown)", m.filter, len(m.visible))
	}
	return lipgloss.NewStyle().Inline(true).MaxWidth(m.width).Render(line)
}

func (m Model) renderFooter() string {
	help := "↑/↓ move · / filter · esc clear filter · q quit"
	if m.filtering {
		help = "type to filter · enter keep · esc cancel · ctrl+c quit"
	}
	return dimStyle.Inline(true).MaxWidth(m.width).Render(help)
}

// renderList draws at most rows module lines, scrolled so the cursor is
// always on screen.
func (m Model) renderList(rows, width int) string {
	if len(m.visible) == 0 {
		if len(m.views) == 0 {
			return dimStyle.Render("No modules")
		}
		return dimStyle.Render("No matches")
	}

	start := 0
	if m.cursor >= rows {
		start = m.cursor - rows + 1
	}
	end := min(start+rows, len(m.visible))

	lines := make([]string, 0, end-start)
	for pos := start; pos < end; pos++ {
		v := m.views[m.visible[pos]]
		marker := "  "
		if pos == m.cursor {
			marker = "▸ "
		}
		line := ansi.Truncate(marker+statusBox(v.Status)+" "+v.ID, width, "…")
		if pos == m.cursor {
			line = selectedStyle.Render(line)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// renderDetail describes the selected module, wrapped to width and clipped to
// rows lines so a long description can never push the layout out of shape.
func (m Model) renderDetail(rows, width int) string {
	v, ok := m.selected()
	if !ok {
		return ""
	}

	homepage := v.Homepage
	if homepage == "" {
		homepage = "—"
	}
	text := strings.Join([]string{
		titleStyle.Render(v.Name),
		dimStyle.Render(fmt.Sprintf("%s · %s", v.ID, v.Origin)),
		"",
		v.Description,
		"",
		field("Status", string(v.Status)),
		field("Packages", string(v.Packages)),
		field("Platforms", strings.Join(v.Platforms, ", ")),
		field("Shells", strings.Join(v.Shells, ", ")),
		field("Options", fmt.Sprint(v.OptionCount)),
		field("Homepage", homepage),
	}, "\n")

	return lipgloss.NewStyle().Width(width).MaxHeight(rows).Render(text)
}

// field renders one "Label:  value" line of the detail pane.
func field(label, value string) string {
	return fmt.Sprintf("%-10s %s", label+":", value)
}

func statusBox(s modedit.Status) string {
	switch s {
	case modedit.StatusEnabled:
		return "[x]"
	case modedit.StatusDisabled:
		return "[ ]"
	default:
		return "[?]"
	}
}
```

- [ ] **Step 4: Run the structural tests to verify they pass**

Run: `gofmt -l internal/tui && go vet ./internal/tui/ && go test ./internal/tui/ -count=1 -skip Golden`
Expected: PASS for every test except the skipped golden test.

- [ ] **Step 5: Generate the golden files and review them by eye**

Run: `go test ./internal/tui/ -run Golden -update -count=1 && wc -l internal/tui/testdata/*.golden`
Expected: six files; five of them 20 lines (`browser-80x20`, `browser-second-row`, `browser-filtered`, `browser-typing`, `browser-no-modules`) and `browser-too-small` 1 line. Open `internal/tui/testdata/browser-80x20.golden`: it must look like this (each line is padded to 80 columns, so trailing spaces are expected):

```text
omnishell  3 modules                                                            
╭────────────────────────────╮╭────────────────────────────────────────────────╮
│ ▸ [x] completion           ││ Completion                                     │
│   [ ] fzf                  ││ completion · builtin                           │
│   [ ] zshonly              ││                                                │
│                            ││ Tab completion tuning                          │
│                            ││                                                │
│                            ││ Status:    enabled                             │
│                            ││ Packages:  n/a                                 │
│                            ││ Platforms: macos, linux                        │
│                            ││ Shells:    zsh, bash                           │
│                            ││ Options:   0                                   │
│                            ││ Homepage:  —                                   │
│                            ││                                                │
│                            ││                                                │
│                            ││                                                │
│                            ││                                                │
│                            ││                                                │
╰────────────────────────────╯╰────────────────────────────────────────────────╯
↑/↓ move · / filter · esc clear filter · q quit                                 
```

Check that `browser-filtered.golden` shows `filter: fz (1 shown)` with only `fzf` listed, `browser-typing.golden` shows `filter: z_` and the "type to filter" footer, and `browser-too-small.golden` is the single line `Terminal too small: need at least 80x20, have 60x10`.

- [ ] **Step 6: Run the whole package with the race detector**

Run: `go test ./internal/tui/ -race -count=1`
Expected: PASS, including `TestViewGoldenFiles` without `-update`.

- [ ] **Step 7: Commit**

```bash
git add internal/tui
git commit -m "feat: render the module browser layout with golden snapshots"
```

---

### Task 3: `Run`, the `tui` command and its guards

**Files:**
- Create: `internal/tui/run.go`
- Create: `internal/cli/tui.go`
- Modify: `internal/cli/root.go` (register the command)
- Modify: `go.mod`, `go.sum`
- Test: `internal/tui/run_test.go`, `internal/cli/tui_test.go`

**Interfaces:**
- Consumes: `tui.New`, `tui.Backend` (Tasks 1 and 2); `modedit.Editor.Views`; `buildEngine`, `hintIfUninitialised` (existing in `internal/cli`); the test helpers `setTestInit` and `setupModuleCLITest` (existing in `internal/cli/*_test.go`).
- Produces:
  ```go
  func Run(b Backend, in io.Reader, out io.Writer) error // package tui
  func SetTUIForTest(isTerminal func(io.Reader, io.Writer) bool, run func(tui.Backend, io.Reader, io.Writer) error) // package cli; nil restores the real one
  func newTUICmd() *cobra.Command
  ```

- [ ] **Step 1: Add the terminal-detection dependency**

Run: `go get github.com/charmbracelet/x/term@v0.2.2`
Expected: `x/term` is added to `go.mod`.

- [ ] **Step 2: Write the failing tests**

`internal/tui/run_test.go`:

```go
package tui

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/JtheGunner/omnishell/internal/modedit"
)

type fakeBackend struct {
	views []modedit.ModuleView
	err   error
}

func (f fakeBackend) Modules() ([]modedit.ModuleView, error) { return f.views, f.err }

func TestRunReturnsTheBackendErrorWithoutTouchingTheTerminal(t *testing.T) {
	boom := errors.New("boom")
	var out bytes.Buffer

	err := Run(fakeBackend{err: boom}, strings.NewReader(""), &out)

	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
	if out.Len() != 0 {
		t.Fatalf("nothing may be written when loading fails, got %q", out.String())
	}
}

// A real program loop, fed a q on its input, must start and quit cleanly.
func TestRunQuitsWhenTheUserPressesQ(t *testing.T) {
	done := make(chan error, 1)
	var out bytes.Buffer
	go func() { done <- Run(fakeBackend{views: sampleViews()}, strings.NewReader("q"), &out) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after q")
	}
}
```

`internal/cli/tui_test.go`:

```go
package cli_test

import (
	"bytes"
	"io"
	"os"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/JtheGunner/omnishell/internal/cli"
	"github.com/JtheGunner/omnishell/internal/tui"
)

// tuiSpy swaps the terminal check and the UI runner for the test's duration.
// It returns a pointer to the backend the command handed to the UI (nil if the
// UI never started).
func tuiSpy(t *testing.T, isTerminal bool) **tui.Backend {
	t.Helper()
	var got *tui.Backend
	cli.SetTUIForTest(
		func(io.Reader, io.Writer) bool { return isTerminal },
		func(b tui.Backend, _ io.Reader, _ io.Writer) error {
			got = &b
			return nil
		},
	)
	t.Cleanup(func() { cli.SetTUIForTest(nil, nil) })
	return &got
}

func TestTUIRefusesWithoutAnInteractiveTerminal(t *testing.T) {
	setTestInit(t)
	var started bool
	cli.SetTUIForTest(nil, func(tui.Backend, io.Reader, io.Writer) error { started = true; return nil })
	t.Cleanup(func() { cli.SetTUIForTest(nil, nil) })

	var out, errb bytes.Buffer
	code := cli.Execute([]string{"tui"}, &out, &errb)

	if code != 2 {
		t.Fatalf("exit = %d, want 2 (stderr: %s)", code, errb.String())
	}
	if !strings.Contains(errb.String(), "needs an interactive terminal") {
		t.Fatalf("stderr should explain the terminal requirement, got: %s", errb.String())
	}
	if out.Len() != 0 || started {
		t.Fatalf("nothing may start or print: stdout=%q started=%v", out.String(), started)
	}
}

func TestTUIWithoutConfigHintsAtInitAndNeverStarts(t *testing.T) {
	setupModuleCLITest(t) // no init: config.toml does not exist
	got := tuiSpy(t, true)

	var out, errb bytes.Buffer
	code := cli.Execute([]string{"tui"}, &out, &errb)

	if code != 2 {
		t.Fatalf("exit = %d, want 2 (stderr: %s)", code, errb.String())
	}
	if !strings.Contains(errb.String(), "run 'omnishell init' first") {
		t.Fatalf("stderr should carry the init hint, got: %s", errb.String())
	}
	if *got != nil {
		t.Fatal("the UI must not start without a config")
	}
}

func TestTUIWithMalformedConfigFailsBeforeStarting(t *testing.T) {
	cfgPath := setTestInit(t)
	if err := os.WriteFile(cfgPath, []byte("not = [toml"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	got := tuiSpy(t, true)

	var out, errb bytes.Buffer
	code := cli.Execute([]string{"tui"}, &out, &errb)

	if code != 2 {
		t.Fatalf("exit = %d, want 2 (stderr: %s)", code, errb.String())
	}
	if *got != nil {
		t.Fatal("the UI must not start with a malformed config")
	}
}

func TestTUIHandsTheFixtureModulesToTheUI(t *testing.T) {
	setTestInit(t)
	got := tuiSpy(t, true)

	var out, errb bytes.Buffer
	if code := cli.Execute([]string{"tui"}, &out, &errb); code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, errb.String())
	}
	if *got == nil {
		t.Fatal("the UI was not started")
	}

	views, err := (**got).Modules()
	if err != nil {
		t.Fatalf("Modules: %v", err)
	}
	ids := make([]string, len(views))
	for i, v := range views {
		ids[i] = v.ID
	}
	if !sort.StringsAreSorted(ids) {
		t.Fatalf("module ids must be sorted for display, got %v", ids)
	}
	for _, want := range []string{"completion", "fzf", "modern-aliases", "zshonly"} {
		if !slices.Contains(ids, want) {
			t.Fatalf("module %q missing from %v", want, ids)
		}
	}
}

func TestTUIRejectsArguments(t *testing.T) {
	var out, errb bytes.Buffer
	if code := cli.Execute([]string{"tui", "extra"}, &out, &errb); code == 0 {
		t.Fatal("tui takes no arguments")
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/tui/ ./internal/cli/ -run 'Run|TUI' -count=1`
Expected: FAIL to build: `undefined: Run` in `internal/tui` and `undefined: cli.SetTUIForTest` in `internal/cli`.

- [ ] **Step 4: Write the implementation**

`internal/tui/run.go`:

```go
package tui

import (
	"fmt"
	"io"

	tea "charm.land/bubbletea/v2"
)

// Run loads the modules from b and runs the browser on in and out until the
// user quits. The modules are loaded before the terminal is touched, so a
// failure reaches the caller as a plain error and never leaves a half-drawn
// screen behind.
func Run(b Backend, in io.Reader, out io.Writer) error {
	views, err := b.Modules()
	if err != nil {
		return err
	}
	if _, err := tea.NewProgram(New(views), tea.WithInput(in), tea.WithOutput(out)).Run(); err != nil {
		return fmt.Errorf("run terminal UI: %w", err)
	}
	return nil
}
```

`internal/cli/tui.go`:

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

// tuiBackend adapts a modedit.Editor to the tui.Backend the UI consumes.
type tuiBackend struct{ editor modedit.Editor }

func (b tuiBackend) Modules() ([]modedit.ModuleView, error) { return b.editor.Views() }

func newTUICmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Browse modules in a full-screen terminal UI",
		Long: `Browse modules in a full-screen terminal UI.

Shows every known module with its description, homepage, package status,
platforms and shells. Type / to filter, q to quit. Read-only: it never changes
config.toml or your shells.

Needs an interactive terminal; in scripts use 'omnishell list'.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			in, out := cmd.InOrStdin(), cmd.OutOrStdout()
			if !tuiIsTerminal(in, out) {
				return engine.ConfigError{Err: errors.New(
					"omnishell tui needs an interactive terminal; use 'omnishell list', 'enable' and 'apply' in scripts")}
			}

			e, cfgPath, _, err := buildEngine(out, cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			// A missing or malformed config.toml must stop us before the screen
			// is taken over, so the message lands in the normal terminal.
			if _, err := config.Load(cfgPath); err != nil {
				return hintIfUninitialised(cmd, err)
			}

			return tuiRun(tuiBackend{editor: modedit.Editor{Engine: e, CfgPath: cfgPath}}, in, out)
		},
	}
}
```

In `internal/cli/root.go`, register the command right after the `list` command:

```go
	root.AddCommand(newListCmd())
	root.AddCommand(newTUICmd())
	root.AddCommand(newEnableCmd())
```

- [ ] **Step 5: Tidy, build and run the tests to verify they pass**

Run: `gofmt -l internal && go mod tidy && go build ./... && go vet ./... && go test ./internal/tui/ ./internal/cli/ -race -count=1`
Expected: `gofmt -l` prints nothing; build and vet clean; both packages PASS. `go.mod` lists `github.com/charmbracelet/x/term` as a direct requirement.

- [ ] **Step 6: Check the real binary refuses a non-terminal**

Run:
```bash
make build && ./omnishell tui < /dev/null; echo "exit=$?"
```
Expected: `error: omnishell tui needs an interactive terminal; use 'omnishell list', 'enable' and 'apply' in scripts` and `exit=2`. (Trying the UI itself needs a real terminal: ask the user to run `! ./omnishell tui` after `omnishell init`; do not start it from here.)

- [ ] **Step 7: Commit**

```bash
git add go.mod go.sum internal/tui internal/cli
git commit -m "feat: add the omnishell tui command with terminal and config guards"
```

---

### Task 4: Documentation and verification

**Files:**
- Modify: `README.md` (commands table)
- Modify: `CHANGELOG.md` (`[Unreleased]`)
- Modify: `CLAUDE.md` (architecture list)

**Interfaces:**
- Consumes: everything above.
- Produces: nothing code-level; the measured numbers go into the ledger for the PR description.

- [ ] **Step 1: Add the command to the README table**

In `README.md`, directly below the `omnishell list` row of the Commands table, insert:

```markdown
| `omnishell tui`                        | Browse modules in a full-screen terminal UI: a module list with description, homepage, package status, platforms and shells, plus a `/` filter. Read-only; needs an interactive terminal (exit 2 otherwise).                                                                                          | —                                                                                                                                                                                                                                                                                                               |
```

- [ ] **Step 2: Add the changelog entry**

In `CHANGELOG.md`, under `## [Unreleased]`, add:

```markdown
### Added
- `omnishell tui`: a full-screen terminal UI to browse modules (description,
  homepage, package status, platforms, shells) with a `/` filter. Read-only for
  now; it needs an interactive terminal. It brings the first runtime
  dependencies, Bubble Tea and Lip Gloss, so the binary grows by about 1.5 MiB.
```

- [ ] **Step 3: Describe the package in `CLAUDE.md`**

In `CLAUDE.md`, after the `**modules/builtin/<id>/**` entry (item 10) and before the "Key invariants" heading, add:

```markdown
11. **`internal/tui`** — the Bubble Tea v2 module browser behind
   `omnishell tui` (`charm.land/bubbletea/v2`, `charm.land/lipgloss/v2`). It
   imports neither `cli` nor `engine`: everything comes through its `Backend`
   interface, which `internal/cli/tui.go` implements over `modedit`. The command
   refuses a non-terminal and a missing or malformed config before the screen is
   taken over. Rendering tests compare `View().Content` (styling stripped) with
   golden files in `internal/tui/testdata`; regenerate them with
   `go test ./internal/tui -run Golden -update` and review the diff by eye.
```

- [ ] **Step 4: Run the full verification**

Run: `make vet && make test`
Expected: both clean; every package PASS.

Run the release-target builds:
```bash
for t in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do GOOS=${t%/*} GOARCH=${t#*/} CGO_ENABLED=0 go build -o /dev/null ./cmd/omnishell && echo "$t ok"; done
for v in 6 7; do GOOS=linux GOARCH=arm GOARM=$v CGO_ENABLED=0 go build -o /dev/null ./cmd/omnishell && echo "linux/armv$v ok"; done
```
Expected: six `ok` lines.

Run: `go run golang.org/x/vuln/cmd/govulncheck@latest ./...`
Expected: `No vulnerabilities found.` (CI runs this on every PR.)

Measure the size delta against `main` (stripped, like a release build):
```bash
go build -ldflags '-s -w' -o /tmp/omnishell-new ./cmd/omnishell
dir=$(mktemp -d) && git archive main | tar -x -C "$dir" && (cd "$dir" && go build -ldflags '-s -w' -o /tmp/omnishell-base ./cmd/omnishell)
stat -f "%z %N" /tmp/omnishell-base /tmp/omnishell-new
```
Expected: roughly 10.4 MB for the base and 12.0 MB for the new binary (about +1.5 MiB, +15%). Write the two exact byte counts into the ledger; they go into the PR description. If the delta is far larger, stop and investigate before continuing.

Run: `git diff main --stat -- go.mod` and `git diff main -- go.mod | grep '^[-+]go '`
Expected: the new requirements plus `-go 1.26` / `+go 1.26.0`.

`golangci-lint` is not installed in the local environment; if it still is not, say so in the hand-off. CI runs it.

- [ ] **Step 5: Commit**

```bash
git add README.md CHANGELOG.md CLAUDE.md
git commit -m "docs: document the omnishell tui command"
```
