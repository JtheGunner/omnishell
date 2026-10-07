# Options Editor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** In `omnishell tui`, the key `o` opens an options screen for the selected module: bool options switch with Space, enum options cycle with ←/→, string and int options are typed and saved with Enter, and every value is validated before it is written.

**Architecture:** A new `modedit.Options(id)` returns a module's options with their current values (config.toml and the manifest only, no package manager). The TUI's `Backend` gains `Options` and `SetOption`; the model loads and writes in `tea.Cmd`s, so `Update` stays free of I/O. A rejected value goes to the status line and keeps the typed text for correction.

**Tech Stack:** Go 1.26 and the dependencies already on `main` (Bubble Tea v2, Lip Gloss v2, x/ansi, x/term). No new dependencies.

**Spec:** `docs/superpowers/specs/2026-10-07-interactive-tui-design.md` (Screens → Options editor, Failure behavior, Testing; Sub-tasks item 4). YouTrack: OMNIS-33.

## Global Constraints

- `go.mod` and `go.sum` do not change.
- `config.toml` is written only through `modedit.SetOption` (atomic, comment-preserving); the TUI never writes a file itself and never writes a value the schema rejected.
- `Update` does no I/O: options are read and written inside returned `tea.Cmd`s.
- Every piece of text that reaches the screen from a backend (option names, help, values, defaults, patterns, error messages) goes through `sanitize`; text typed by the user is cleaned too.
- One write runs at a time; while one is pending, keys on the options screen are ignored (except Ctrl-C).
- `omnishell set`, `enable`, `disable`, `list`, `apply` behave exactly as before.
- Existing tests are not deleted; the only edits to them are the ones this plan lists (the `fakeBackend` gains fields and methods, the browser's footer help text changes, six golden files change by that one footer line).
- Every commit builds and passes `go test ./...`. Before the PR: `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2 run ./...` reports 0 issues (CI runs exactly this version).
- Write invisible or format Unicode characters in Go sources as `\uXXXX` escapes, never as raw characters.
- All identifiers, comments and commit messages are in English; commit subjects use `<type>: <description>` with no attribution trailers.

## Rulings carried into this plan

- **`modedit.Options` is new.** The spec's `Backend` lists `SetOption` but nothing that returns the *current* values, which the screen must show. `Options` reads config.toml and the manifest only (`module.ValidateOptions` supplies the defaults and the canonical values), so it is cheap enough to call after every write; a test pins that it never touches the package manager.
- **Keys:** `o` opens the screen from the browser; Space switches a bool and ←/→ cycle an enum (wrapping), each writing at once; Enter starts typing a string or int, pre-filled with the current value, and Enter saves; Esc cancels typing or leaves the screen. The spec's "Enter validates through SetOption" applies to the typed types; one-key changes need no confirmation.
- **List options are shown but not editable** (`list<string>`, `list<enum>`; out of scope in the issue). Their rows are dimmed and the detail says to use `omnishell set`.
- **A value that config.toml holds but the schema rejects (a hand edit) is shown as written and marked `(invalid)`**, so the user can see it and overwrite it; it does not break the screen.
- **The header's change count includes options:** the number of options whose value differs from the first value the screen showed, so setting one back counts as no change. Module toggles are counted as before.
- **The module need not be enabled**, matching `omnishell set`; the detail pane notes when it is not, because the options then apply only once it is.
- **The browser footer drops "filter" from `esc clear filter`** (`esc clear`) to fit the new `o options` entry into 80 columns.
- **A rejected string or int keeps the typed text** and stays in typing mode, so a typo is corrected rather than retyped; the reason is on the status line.
- **A failed re-read after a successful write** shows the error and leaves the old values on screen (the write happened once); this mirrors the toggle behaviour and is pinned by a test.

## Review Focus

- A rejected value (pattern mismatch, bad int, bad bool) is never written, is explained on the status line, and keeps the typed text (Task 3 tests, the CLI adapter test in Task 2, and the pseudo-terminal check in Task 5).
- Option names, help, values and typed text with control characters cannot reach the terminal or break the layout (Task 3 and Task 4 tests).
- Keys pressed while a write is pending are ignored, so a double press cannot write twice or write a stale value (Task 3 test).
- The change count is net across several options and combines with module toggles (Task 3 test).
- An invalid value in config.toml, an enum whose stored value is not allowed, a list option, a long value or help text, and a tall or short terminal never break the screen (Tasks 1, 3 and 4 tests).

---

### Task 1: `modedit.Options`

**Files:**
- Create: `internal/modedit/options.go`
- Create: `internal/modedit/testdata/modules-options/tuned/manifest.toml`, `internal/modedit/testdata/modules-options/bare/manifest.toml`
- Test: `internal/modedit/options_test.go`

**Interfaces:**
- Consumes: `config.Load`, `module.ValidateOptions`, `Editor`, and the `forbiddenManager` test double in `internal/modedit/views_test.go`.
- Produces:
  ```go
  type OptionView struct {
      Key, Type, Help string
      Values          []string // allowed values of an enum
      Pattern         string
      Default, Value  string   // as text; Value is the configured value, else the default
      Set, Invalid    bool     // set in config.toml; set but rejected by the schema
      Editable        bool     // false for list options
  }
  func (ed Editor) Options(id string) ([]OptionView, error) // sorted by key; never nil on success
  ```

- [ ] **Step 1: Add the fixture modules**

`internal/modedit/testdata/modules-options/tuned/manifest.toml`:

```toml
platforms = ["macos", "linux"]
shells    = ["zsh", "bash"]
requires  = []
after     = []

[module]
id          = "tuned"
name        = "Tuned"
description = "A module with one option of every editable type and a list option"
version     = "1.0.0"
schema      = 1

[options.verbose]
type    = "bool"
default = false
help    = "Print more"

[options.theme]
type    = "enum"
default = "dark"
values  = ["dark", "light", "solarized"]
help    = "Colour theme"

[options.retries]
type    = "int"
default = 3
help    = "How often to retry"

[options.label]
type    = "string"
default = "work"
pattern = "^[a-z]+$"
help    = "Short lower-case label"

[options.extras]
type    = "list<string>"
default = ["a", "b"]
help    = "Extra items"
```

`internal/modedit/testdata/modules-options/bare/manifest.toml`:

```toml
platforms = ["macos", "linux"]
shells    = ["zsh", "bash"]
requires  = []
after     = []

[module]
id          = "bare"
name        = "Bare"
description = "A module without options"
version     = "1.0.0"
schema      = 1
```

- [ ] **Step 2: Write the failing tests**

`internal/modedit/options_test.go`:

```go
package modedit_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/JtheGunner/omnishell/internal/engine"
	"github.com/JtheGunner/omnishell/internal/modedit"
	"github.com/JtheGunner/omnishell/internal/module"
	"github.com/JtheGunner/omnishell/internal/platform"
)

// newOptionsEditor builds an Editor over the modules in testdata/modules-options
// with cfgBody as config.toml (no file at all when cfgBody is empty).
func newOptionsEditor(t *testing.T, cfgBody string) (modedit.Editor, string) {
	t.Helper()
	reg, err := module.LoadRegistry(nil, "testdata/modules-options")
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	if cfgBody != "" {
		if err := os.WriteFile(cfgPath, []byte(cfgBody), 0o644); err != nil {
			t.Fatalf("write config: %v", err)
		}
	}
	return modedit.Editor{
		Engine: engine.Engine{
			Platform: platform.Info{OS: platform.Linux},
			Registry: reg,
			Manager:  forbiddenManager{},
		},
		CfgPath: cfgPath,
	}, cfgPath
}

func optionByKey(t *testing.T, opts []modedit.OptionView, key string) modedit.OptionView {
	t.Helper()
	for _, o := range opts {
		if o.Key == key {
			return o
		}
	}
	t.Fatalf("no option %q in %+v", key, opts)
	return modedit.OptionView{}
}

const baseConfig = "[omnishell]\nversion = 1\n"

func TestOptionsAreSortedAndShowTheirDefaultsWhenNothingIsSet(t *testing.T) {
	ed, _ := newOptionsEditor(t, baseConfig)

	opts, err := ed.Options("tuned")
	if err != nil {
		t.Fatalf("Options: %v", err)
	}

	keys := make([]string, len(opts))
	for i, o := range opts {
		keys[i] = o.Key
	}
	if want := []string{"extras", "label", "retries", "theme", "verbose"}; !reflect.DeepEqual(keys, want) {
		t.Fatalf("keys = %v, want %v", keys, want)
	}
	for _, o := range opts {
		if o.Set || o.Invalid || o.Value != o.Default {
			t.Fatalf("%s: set=%v invalid=%v value=%q default=%q, want the default and nothing set", o.Key, o.Set, o.Invalid, o.Value, o.Default)
		}
	}
	want := map[string]string{"extras": "a,b", "label": "work", "retries": "3", "theme": "dark", "verbose": "false"}
	for key, def := range want {
		if got := optionByKey(t, opts, key).Default; got != def {
			t.Fatalf("%s default = %q, want %q", key, got, def)
		}
	}
}

func TestOptionsCarryTheirSchema(t *testing.T) {
	ed, _ := newOptionsEditor(t, baseConfig)

	opts, err := ed.Options("tuned")
	if err != nil {
		t.Fatalf("Options: %v", err)
	}

	theme := optionByKey(t, opts, "theme")
	if theme.Type != "enum" || !reflect.DeepEqual(theme.Values, []string{"dark", "light", "solarized"}) || theme.Help != "Colour theme" || !theme.Editable {
		t.Fatalf("theme = %+v", theme)
	}
	label := optionByKey(t, opts, "label")
	if label.Type != "string" || label.Pattern != "^[a-z]+$" {
		t.Fatalf("label = %+v", label)
	}
	if optionByKey(t, opts, "extras").Editable {
		t.Fatal("a list option must not be editable as one text")
	}
	for _, key := range []string{"verbose", "retries", "label", "theme"} {
		if !optionByKey(t, opts, key).Editable {
			t.Fatalf("%s must be editable", key)
		}
	}
}

func TestOptionsShowTheValuesConfigTomlSets(t *testing.T) {
	ed, _ := newOptionsEditor(t, baseConfig+`
[modules.tuned.options]
verbose = true
retries = 7
theme   = "light"
label   = "home"
extras  = ["x", "y", "z"]
`)

	opts, err := ed.Options("tuned")
	if err != nil {
		t.Fatalf("Options: %v", err)
	}

	want := map[string]string{"verbose": "true", "retries": "7", "theme": "light", "label": "home", "extras": "x,y,z"}
	for key, value := range want {
		o := optionByKey(t, opts, key)
		if o.Value != value || !o.Set || o.Invalid {
			t.Fatalf("%s = %+v, want value %q, set, valid", key, o, value)
		}
	}
}

func TestOptionsFollowSetOption(t *testing.T) {
	ed, _ := newOptionsEditor(t, baseConfig)

	if err := ed.SetOption("tuned", "retries", "9"); err != nil {
		t.Fatalf("SetOption: %v", err)
	}
	opts, err := ed.Options("tuned")
	if err != nil {
		t.Fatalf("Options: %v", err)
	}

	if o := optionByKey(t, opts, "retries"); o.Value != "9" || !o.Set {
		t.Fatalf("retries = %+v, want 9 and set", o)
	}
	if o := optionByKey(t, opts, "verbose"); o.Set {
		t.Fatal("an option nobody touched must stay unset")
	}
}

// A value the schema rejects (a hand edit) must still be shown, as written and
// flagged, so the user can see and fix it; the other options stay usable.
func TestOptionsFlagAValueTheSchemaRejects(t *testing.T) {
	ed, _ := newOptionsEditor(t, baseConfig+"[modules.tuned.options]\nretries = \"many\"\ntheme = \"light\"\n")

	opts, err := ed.Options("tuned")
	if err != nil {
		t.Fatalf("Options: %v", err)
	}

	if o := optionByKey(t, opts, "retries"); !o.Invalid || o.Value != "many" || !o.Set {
		t.Fatalf("retries = %+v, want it flagged invalid and shown as written", o)
	}
	if o := optionByKey(t, opts, "theme"); o.Invalid || o.Value != "light" {
		t.Fatalf("theme = %+v, want the valid value untouched", o)
	}
}

func TestOptionsIgnoreKeysTheSchemaDoesNotKnow(t *testing.T) {
	ed, _ := newOptionsEditor(t, baseConfig+"[modules.tuned.options]\nbogus = 1\n")

	opts, err := ed.Options("tuned")
	if err != nil || len(opts) != 5 {
		t.Fatalf("err=%v len=%d, want the five declared options", err, len(opts))
	}
}

func TestOptionsOfAModuleWithoutOptionsIsEmptyNotNil(t *testing.T) {
	ed, _ := newOptionsEditor(t, baseConfig)

	opts, err := ed.Options("bare")

	if err != nil || opts == nil || len(opts) != 0 {
		t.Fatalf("opts=%#v err=%v, want an empty, non-nil slice", opts, err)
	}
}

func TestOptionsErrors(t *testing.T) {
	ed, _ := newOptionsEditor(t, baseConfig)
	var cfgErr config.Error
	if _, err := ed.Options("nope"); !errors.As(err, &cfgErr) {
		t.Fatalf("unknown module: err = %v, want a config.Error", err)
	}

	missing, _ := newOptionsEditor(t, "")
	if _, err := missing.Options("tuned"); !errors.Is(err, config.ErrNotFound) {
		t.Fatalf("missing config: err = %v, want ErrNotFound", err)
	}

	broken, _ := newOptionsEditor(t, "not = [toml")
	if _, err := broken.Options("tuned"); err == nil || errors.Is(err, config.ErrNotFound) {
		t.Fatalf("malformed config: err = %v, want a parse error", err)
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/modedit/ -count=1 -run Options`
Expected: FAIL to build, `ed.Options undefined (type modedit.Editor has no field or method Options)`.

- [ ] **Step 4: Implement**

`internal/modedit/options.go`:

```go
package modedit

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/JtheGunner/omnishell/internal/module"
)

// OptionView is one option of a module as a front end shows and edits it.
type OptionView struct {
	Key  string
	Type string // bool, int, string, enum, list<string> or list<enum>
	Help string
	// Values are the allowed values of an enum option, in manifest order.
	Values []string
	// Pattern is the regexp a string option must match; empty means any.
	Pattern string
	// Default is the manifest's default, as text.
	Default string
	// Value is what the option is right now, as text: the value in config.toml
	// when it sets one, otherwise the default.
	Value string
	// Set is true when config.toml sets the option explicitly.
	Set bool
	// Invalid is true when config.toml holds a value the schema rejects; Value
	// then shows it as written.
	Invalid bool
	// Editable is false for list options, which SetOption cannot take as one
	// raw string a user can sensibly type here.
	Editable bool
}

// Options returns the options of module id, sorted by key, with their current
// values. It reads only config.toml and the module's manifest, so it is cheap.
// A missing config.toml wraps config.ErrNotFound; an unknown module is a
// config.Error, as for Enable and SetOption.
func (ed Editor) Options(id string) ([]OptionView, error) {
	cfg, err := config.Load(ed.CfgPath)
	if err != nil {
		return nil, err
	}
	mod, ok := ed.Engine.Registry.Get(id)
	if !ok {
		return nil, config.Error{Path: ed.CfgPath, Msg: fmt.Sprintf("unknown module %q", id)}
	}
	schema := mod.Manifest.Options
	defaults, err := module.ValidateOptions(schema, nil)
	if err != nil {
		return nil, config.Error{Path: ed.CfgPath, Msg: fmt.Sprintf("module %q: %v", id, err)}
	}

	keys := make([]string, 0, len(schema))
	for key := range schema {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	configured := cfg.Modules[id].Options
	views := make([]OptionView, 0, len(keys))
	for _, key := range keys {
		spec := schema[key]
		v := OptionView{
			Key:      key,
			Type:     spec.Type,
			Help:     spec.Help,
			Values:   slices.Clone(spec.Values),
			Pattern:  spec.Pattern,
			Default:  optionText(defaults[key]),
			Editable: !strings.HasPrefix(spec.Type, "list<"),
		}
		v.Value = v.Default
		if raw, set := configured[key]; set {
			v.Set = true
			checked, verr := module.ValidateOptions(map[string]module.OptionSchema{key: spec}, map[string]any{key: raw})
			if verr != nil {
				v.Invalid = true
				v.Value = optionText(raw)
			} else {
				v.Value = optionText(checked[key])
			}
		}
		views = append(views, v)
	}
	return views, nil
}

// optionText renders an option value the way a user would type it.
func optionText(v any) string {
	switch x := v.(type) {
	case bool:
		return strconv.FormatBool(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case int:
		return strconv.Itoa(x)
	case string:
		return x
	case []string:
		return strings.Join(x, ",")
	case []any:
		parts := make([]string, len(x))
		for i, item := range x {
			parts[i] = fmt.Sprint(item)
		}
		return strings.Join(parts, ",")
	default:
		return fmt.Sprint(v)
	}
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `gofmt -l internal/modedit && go vet ./internal/modedit/ && go test ./internal/modedit/ -race -count=1 -v -run Options`
Expected: `gofmt -l` prints nothing; PASS for the eight `TestOptions…` tests.

Run: `go test ./internal/modedit/ ./internal/cli/ -race -count=1`
Expected: PASS (nothing existing changed).

- [ ] **Step 6: Commit**

```bash
git add internal/modedit
git commit -m "feat: list a module's options with their current values in modedit"
```

---

### Task 2: `Backend.Options`, `Backend.SetOption` and the CLI adapter

**Files:**
- Modify: `internal/tui/backend.go`, `internal/cli/tui.go`
- Test: `internal/tui/helpers_test.go` (the fake backend), `internal/cli/tui_test.go`

**Interfaces:**
- Consumes: `Editor.Options`, `Editor.SetOption` (Task 1 and earlier); `userMessage` (existing).
- Produces:
  ```go
  // Backend gains:
  Options(id string) ([]modedit.OptionView, error)
  SetOption(id, key, raw string) error
  // test double in package tui gains: optionRows, optionsErr, setOptionErr, optionCalls, optionsRead,
  // and the helpers sampleOptions(), withOptions(), openFzfOptions(t, m)
  ```

- [ ] **Step 1: Write the failing tests**

In `internal/tui/helpers_test.go`, replace

```go
	planErr     error // returned by Plan
	planCalls   int   // how often Plan was called
}
```

with

```go
	planErr     error // returned by Plan
	planCalls   int   // how often Plan was called

	optionRows   map[string][]modedit.OptionView // the options Options returns, by module id
	optionsErr   error                           // returned by Options
	setOptionErr error                           // returned by SetOption, before anything changes
	optionCalls  []string                        // "set fzf theme light", in order
	optionsRead  int                             // how often Options was called
}

func (f *fakeBackend) Options(id string) ([]modedit.OptionView, error) {
	f.optionsRead++
	if f.optionsErr != nil {
		return nil, f.optionsErr
	}
	return slices.Clone(f.optionRows[id]), nil
}

func (f *fakeBackend) SetOption(id, key, raw string) error {
	f.optionCalls = append(f.optionCalls, "set "+id+" "+key+" "+raw)
	if f.setOptionErr != nil {
		return f.setOptionErr
	}
	for i := range f.optionRows[id] {
		if f.optionRows[id][i].Key == key {
			f.optionRows[id][i].Value, f.optionRows[id][i].Set = raw, true
		}
	}
	return nil
}

// sampleOptions is the options of fzf in the tests: one of each editable type,
// one list, and one with an allowed-values list and a pattern.
func sampleOptions() []modedit.OptionView {
	return []modedit.OptionView{
		{Key: "ctrl_r", Type: "bool", Help: "Bind Ctrl+R to the fzf history widget", Default: "true", Value: "true", Editable: true},
		{Key: "extras", Type: "list<string>", Help: "Extra items", Default: "a,b", Value: "a,b"},
		{Key: "prefix", Type: "string", Help: "Key prefix", Pattern: "^[a-z]+$", Default: "abc", Value: "abc", Editable: true},
		{Key: "retries", Type: "int", Help: "How often to retry", Default: "3", Value: "3", Editable: true},
		{Key: "theme", Type: "enum", Help: "Colour theme", Values: []string{"dark", "light", "solarized"}, Default: "dark", Value: "dark", Editable: true},
	}
}

// withOptions returns a sized model over sampleViews whose backend knows the
// options of fzf, plus the backend.
func withOptions() (Model, *fakeBackend) {
	m, b := newBackedModel(sampleViews())
	b.optionRows = map[string][]modedit.OptionView{"fzf": sampleOptions()}
	return sized(m, 80, 20), b
}

// openFzfOptions presses o on fzf and lets the options load.
func openFzfOptions(t *testing.T, m Model) Model {
	t.Helper()
	m = press(t, m, "down") // fzf is the second module
	next, cmd := m.Update(key("o"))
	return settle(next.(Model), cmd)
}
```

(This keeps the `fakeBackend` struct, adds its option fields and methods, and the test helpers; `func (f *fakeBackend) Plan()` still follows.)

Append to `internal/cli/tui_test.go`:

```go
func TestTUIBackendOptionsAndSetOptionRoundTripThroughConfigToml(t *testing.T) {
	cfgPath := setTestInit(t)
	b := backendFor(t)

	before, err := b.Options("fzf")
	if err != nil {
		t.Fatalf("Options: %v", err)
	}
	if len(before) != 1 || before[0].Key != "ctrl_r" || before[0].Value != "true" || before[0].Set || !before[0].Editable {
		t.Fatalf("options = %+v, want ctrl_r true, unset and editable", before)
	}

	if err := b.SetOption("fzf", "ctrl_r", "false"); err != nil {
		t.Fatalf("SetOption: %v", err)
	}
	after, err := b.Options("fzf")
	if err != nil {
		t.Fatalf("Options after: %v", err)
	}
	if after[0].Value != "false" || !after[0].Set {
		t.Fatalf("ctrl_r = %+v, want false and set", after[0])
	}
	src, err := os.ReadFile(cfgPath)
	if err != nil || !strings.Contains(string(src), "ctrl_r = false") || !strings.Contains(string(src), "# Edit this file by hand") {
		t.Fatalf("config.toml must hold the option and keep its comments (err=%v):\n%s", err, src)
	}
}

func TestTUIBackendRejectedOptionIsAShortMessageAndWritesNothing(t *testing.T) {
	cfgPath := setTestInit(t)
	b := backendFor(t)
	before, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}

	err = b.SetOption("fzf", "ctrl_r", "maybe")

	if err == nil || !strings.Contains(err.Error(), "invalid value for fzf.ctrl_r") {
		t.Fatalf("err = %v, want the validation message", err)
	}
	if strings.Contains(err.Error(), cfgPath) {
		t.Fatalf("message %q must not start with the config path", err)
	}
	after, err := os.ReadFile(cfgPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("config.toml must be untouched (err=%v)", err)
	}
}

func TestTUIBackendOptionsMarksListOptionsAsNotEditable(t *testing.T) {
	setTestInit(t)
	b := backendFor(t)

	opts, err := b.Options("modern-aliases")
	if err != nil {
		t.Fatalf("Options: %v", err)
	}

	if len(opts) != 1 || opts[0].Type != "list<enum>" || opts[0].Editable {
		t.Fatalf("options = %+v, want one list option that is not editable", opts)
	}
}

func TestTUIBackendOptionsOfAnUnknownModuleIsAnError(t *testing.T) {
	setTestInit(t)
	b := backendFor(t)

	if _, err := b.Options("no-such-module"); err == nil || !strings.Contains(err.Error(), "unknown module") {
		t.Fatalf("err = %v, want unknown module", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go vet ./internal/tui/ ./internal/cli/`
Expected: `internal/tui` compiles (the fake simply has extra methods); `internal/cli` FAILS to build with `b.Options undefined (type tui.Backend has no field or method Options)`.

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
	// Options returns the options of one module with their current values. It
	// reads only config.toml and the manifest, so it is cheap.
	Options(id string) ([]modedit.OptionView, error)
	// SetOption writes one option of one module, validated against the
	// module's schema; nothing is written for a rejected value. The error text
	// is shown to the user as it is.
	SetOption(id, key, raw string) error
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

In `internal/cli/tui.go`, insert this directly above the comment `// Plan shows what \`omnishell apply\` would do`:

```go
// Options lists a module's options with their current values; SetOption writes
// one. Both reduce a config.Error to its message, like Enable and Disable.
func (b tuiBackend) Options(id string) ([]modedit.OptionView, error) {
	opts, err := b.editor.Options(id)
	return opts, userMessage(err)
}

func (b tuiBackend) SetOption(id, key, raw string) error {
	return userMessage(b.editor.SetOption(id, key, raw))
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `gofmt -l internal && go build ./... && go vet ./... && go test ./internal/tui/ ./internal/cli/ ./internal/modedit/ -race -count=1`
Expected: `gofmt -l` prints nothing; build and vet clean; all three packages PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui internal/cli
git commit -m "feat: let the tui backend read and write module options"
```

---

### Task 3: The options screen in the model

**Files:**
- Create: `internal/tui/options.go`
- Modify (replace): `internal/tui/model.go`
- Modify: `internal/tui/plan.go`, `internal/tui/view.go`
- Test: `internal/tui/options_test.go`

**Interfaces:**
- Consumes: `Backend.Options`, `Backend.SetOption` (Task 2); `sanitize`, `sanitizeAll` (existing); the helpers `withOptions`, `openFzfOptions`, `newBackedModel`, `sized`, `press`, `settle`, `space`, `key`.
- Produces:
  ```go
  const screenOptions // third value of the screen type
  type optionsState struct { id string; rows []modedit.OptionView; cursor int; editing bool; input string }
  type optionsMsg struct { id string; rows []modedit.OptionView; err error }
  type optionWrittenMsg struct { id string; rows []modedit.OptionView; err error }
  func optionsCmd(b Backend, id string) tea.Cmd
  func optionWriteCmd(b Backend, id, key, raw string) tea.Cmd
  func (m Model) openOptions() (tea.Model, tea.Cmd)
  func (m Model) updateOptions(msg tea.KeyPressMsg) (tea.Model, tea.Cmd)
  func (m Model) changedOptions() int
  func (m Model) bodyRows() int
  // Model gains: options optionsState; optionBase, optionNow map[string]map[string]string
  ```
  Test helpers produced here and used by Task 4: `optionRow`, `onOption`, `settleKey`.

- [ ] **Step 1: Write the failing tests**

`internal/tui/options_test.go`:

```go
package tui

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/JtheGunner/omnishell/internal/modedit"
)

// optionRow returns the option with the given key from the open options screen.
func optionRow(t *testing.T, m Model, key string) modedit.OptionView {
	t.Helper()
	for _, r := range m.options.rows {
		if r.Key == key {
			return r
		}
	}
	t.Fatalf("no option %q on the screen: %+v", key, m.options.rows)
	return modedit.OptionView{}
}

// onOption moves the cursor to the option with the given key.
func onOption(t *testing.T, m Model, key string) Model {
	t.Helper()
	for i, r := range m.options.rows {
		if r.Key == key {
			m.options.cursor = i
			return m
		}
	}
	t.Fatalf("no option %q", key)
	return m
}

// settleKey presses a key and lets any resulting write finish.
func settleKey(t *testing.T, m Model, name string) Model {
	t.Helper()
	next, cmd := m.Update(key(name))
	return settle(next.(Model), cmd)
}

func TestOOpensTheOptionsOfTheSelectedModuleInACommand(t *testing.T) {
	m, b := withOptions()
	m = press(t, m, "down") // fzf

	next, cmd := m.Update(key("o"))

	if cmd == nil || !next.(Model).pending {
		t.Fatal("o must start loading the options in a command")
	}
	if b.optionsRead != 0 {
		t.Fatalf("Update read the options itself (%d reads); it must do no I/O", b.optionsRead)
	}
	opened := settle(next.(Model), cmd)
	if opened.screen != screenOptions || opened.options.id != "fzf" || len(opened.options.rows) != 5 || opened.pending {
		t.Fatalf("screen=%v id=%q rows=%d pending=%v, want the options of fzf", opened.screen, opened.options.id, len(opened.options.rows), opened.pending)
	}
}

func TestOOnAModuleWithoutOptionsSaysSo(t *testing.T) {
	m, b := withOptions() // completion is first and has none

	next, cmd := m.Update(key("o"))

	if cmd != nil || next.(Model).screen != screenBrowser {
		t.Fatal("a module without options must not open the options screen")
	}
	if got := next.(Model).status; got != "completion has no options" {
		t.Fatalf("status = %q", got)
	}
	if b.optionsRead != 0 {
		t.Fatal("nothing should be read")
	}
}

func TestOIsIgnoredWhileAWriteIsPendingAndTypedIntoAFilter(t *testing.T) {
	m, _ := withOptions()
	m = press(t, m, "down")
	writing, _ := m.Update(key("space")) // a toggle is being written
	if next, cmd := writing.(Model).Update(key("o")); cmd != nil || next.(Model).screen != screenBrowser {
		t.Fatal("o must be ignored while a write is pending")
	}

	typing := press(t, m, "/", "o")
	if typing.screen != screenBrowser || typing.filter != "o" {
		t.Fatalf("screen=%v filter=%q, want o typed into the filter", typing.screen, typing.filter)
	}
}

func TestAnOptionsLoadFailureShowsTheMessageAndStaysInTheBrowser(t *testing.T) {
	m, b := withOptions()
	b.optionsErr = errors.New("cannot read config.toml")
	m = press(t, m, "down")

	next, cmd := m.Update(key("o"))
	failed := settle(next.(Model), cmd)

	if failed.screen != screenBrowser || failed.status != "cannot read config.toml" || failed.pending {
		t.Fatalf("screen=%v status=%q pending=%v", failed.screen, failed.status, failed.pending)
	}
}

func TestSpaceTogglesABoolOptionThroughTheBackend(t *testing.T) {
	m, b := withOptions()
	m = openFzfOptions(t, m)

	next, cmd := m.Update(key("space"))
	if len(b.optionCalls) != 0 {
		t.Fatal("Update wrote the option itself; it must do no I/O")
	}
	m = settle(next.(Model), cmd)

	if want := []string{"set fzf ctrl_r false"}; !reflect.DeepEqual(b.optionCalls, want) {
		t.Fatalf("backend calls = %v, want %v", b.optionCalls, want)
	}
	if r := optionRow(t, m, "ctrl_r"); r.Value != "false" || !r.Set {
		t.Fatalf("ctrl_r = %+v, want false and set", r)
	}
	if again := settleKey(t, m, "space"); optionRow(t, again, "ctrl_r").Value != "true" {
		t.Fatal("a second space must switch it back on")
	}
}

func TestSpaceDoesNothingOnOtherOptionTypes(t *testing.T) {
	m, b := withOptions()
	m = openFzfOptions(t, m)
	for _, key := range []string{"extras", "prefix", "retries", "theme"} {
		on := onOption(t, m, key)
		if _, cmd := on.Update(keyMsg("space")); cmd != nil {
			t.Fatalf("space on %s must not write", key)
		}
	}
	if len(b.optionCalls) != 0 {
		t.Fatalf("backend calls = %v, want none", b.optionCalls)
	}
}

func keyMsg(name string) tea.KeyPressMsg { return key(name) }

func TestLeftAndRightCycleAnEnumAndWrapAround(t *testing.T) {
	m, b := withOptions()
	m = onOption(t, openFzfOptions(t, m), "theme") // dark of dark, light, solarized

	m = settleKey(t, m, "right")
	m = settleKey(t, m, "right")
	m = settleKey(t, m, "right") // wraps to dark
	m = settleKey(t, m, "left")  // wraps back to solarized

	want := []string{"set fzf theme light", "set fzf theme solarized", "set fzf theme dark", "set fzf theme solarized"}
	if !reflect.DeepEqual(b.optionCalls, want) {
		t.Fatalf("backend calls = %v, want %v", b.optionCalls, want)
	}
	if got := optionRow(t, m, "theme").Value; got != "solarized" {
		t.Fatalf("theme = %q, want solarized", got)
	}
}

func TestAnEnumWithAnUnknownValueStartsAtTheEdgeInTheDirectionPressed(t *testing.T) {
	m, b := withOptions()
	b.optionRows["fzf"][4].Value = "weird"
	m = onOption(t, openFzfOptions(t, m), "theme")

	right := settleKey(t, m, "right")
	left := settleKey(t, m, "left")

	if got := optionRow(t, right, "theme").Value; got != "dark" {
		t.Fatalf("right from an unknown value = %q, want the first (dark)", got)
	}
	if got := optionRow(t, left, "theme").Value; got != "solarized" {
		t.Fatalf("left from an unknown value = %q, want the last (solarized)", got)
	}
}

func TestLeftAndRightDoNothingOnOtherOptionTypes(t *testing.T) {
	m, _ := withOptions()
	m = openFzfOptions(t, m)
	for _, k := range []string{"ctrl_r", "extras", "prefix", "retries"} {
		on := onOption(t, m, k)
		for _, dir := range []string{"left", "right"} {
			if _, cmd := on.Update(key(dir)); cmd != nil {
				t.Fatalf("%s on %s must not write", dir, k)
			}
		}
	}
}

func TestEnterEditsAStringStartingFromItsValueAndSavesOnEnter(t *testing.T) {
	m, b := withOptions()
	m = onOption(t, openFzfOptions(t, m), "prefix")

	m = press(t, m, "enter")
	if !m.options.editing || m.options.input != "abc" {
		t.Fatalf("editing=%v input=%q, want editing with the current value", m.options.editing, m.options.input)
	}
	m = press(t, m, "backspace", "x", "y")
	if m.options.input != "abxy" {
		t.Fatalf("input = %q, want abxy", m.options.input)
	}
	if len(b.optionCalls) != 0 {
		t.Fatal("typing must not write")
	}

	m = settleKey(t, m, "enter")

	if want := []string{"set fzf prefix abxy"}; !reflect.DeepEqual(b.optionCalls, want) {
		t.Fatalf("backend calls = %v, want %v", b.optionCalls, want)
	}
	if m.options.editing || optionRow(t, m, "prefix").Value != "abxy" {
		t.Fatalf("editing=%v value=%q, want the saved value shown and editing over", m.options.editing, optionRow(t, m, "prefix").Value)
	}
}

func TestAnIntIsEditedLikeAString(t *testing.T) {
	m, b := withOptions()
	m = onOption(t, openFzfOptions(t, m), "retries")

	m = press(t, m, "enter", "backspace", "7")
	settleKey(t, m, "enter")

	if want := []string{"set fzf retries 7"}; !reflect.DeepEqual(b.optionCalls, want) {
		t.Fatalf("backend calls = %v, want %v", b.optionCalls, want)
	}
}

// A rejected value must not be written, must be explained, and must leave the
// typed text in place so it can be corrected rather than retyped.
func TestARejectedValueKeepsTheTextAndShowsTheMessage(t *testing.T) {
	m, b := withOptions()
	b.setOptionErr = errors.New(`invalid value for fzf.prefix: "ABC" does not match required pattern ^[a-z]+$`)
	m = onOption(t, openFzfOptions(t, m), "prefix")
	m = press(t, m, "enter", "backspace", "backspace", "backspace", "A", "B", "C")

	m = settleKey(t, m, "enter")

	if !m.options.editing || m.options.input != "ABC" {
		t.Fatalf("editing=%v input=%q, want the text kept for correction", m.options.editing, m.options.input)
	}
	if !strings.Contains(m.status, "does not match required pattern") {
		t.Fatalf("status = %q, want the reason", m.status)
	}
	if got := optionRow(t, m, "prefix").Value; got != "abc" {
		t.Fatalf("prefix = %q, want the old value untouched", got)
	}

	b.setOptionErr = nil
	m = press(t, m, "backspace", "backspace", "backspace", "x", "y", "z")
	m = settleKey(t, m, "enter")
	if m.options.editing || optionRow(t, m, "prefix").Value != "xyz" || m.status != "" {
		t.Fatalf("editing=%v value=%q status=%q, want the corrected value saved", m.options.editing, optionRow(t, m, "prefix").Value, m.status)
	}
}

func TestEscCancelsEditingWithoutWriting(t *testing.T) {
	m, b := withOptions()
	m = onOption(t, openFzfOptions(t, m), "prefix")

	m = press(t, m, "enter", "x", "esc")

	if m.options.editing || m.options.input != "" || m.screen != screenOptions {
		t.Fatalf("editing=%v input=%q screen=%v, want back on the option list", m.options.editing, m.options.input, m.screen)
	}
	if len(b.optionCalls) != 0 {
		t.Fatalf("backend calls = %v, want none", b.optionCalls)
	}
}

func TestWhileEditingQAndSpaceAreTextButCtrlCStillQuits(t *testing.T) {
	m, _ := withOptions()
	m = onOption(t, openFzfOptions(t, m), "prefix")
	m = press(t, m, "enter", "backspace", "backspace", "backspace", "q", "space", "j")

	if m.options.input != "q j" || m.screen != screenOptions {
		t.Fatalf("input=%q screen=%v, want q, space and j typed", m.options.input, m.screen)
	}
	_, cmd := m.Update(key("ctrl+c"))
	if cmd == nil {
		t.Fatal("ctrl+c must quit even while typing")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("ctrl+c did not quit")
	}
}

func TestBackspaceRemovesAWholeMultiByteCharacterFromTheInput(t *testing.T) {
	m, _ := withOptions()
	m = onOption(t, openFzfOptions(t, m), "prefix")

	m = press(t, m, "enter", "backspace", "backspace", "backspace", "ä")
	if m.options.input != "ä" {
		t.Fatalf("input = %q, want ä", m.options.input)
	}
	if m = press(t, m, "backspace"); m.options.input != "" {
		t.Fatalf("input = %q, want empty (not a broken byte)", m.options.input)
	}
}

func TestEnterDoesNotStartEditingABoolEnumOrListOption(t *testing.T) {
	m, _ := withOptions()
	m = openFzfOptions(t, m)
	for _, k := range []string{"ctrl_r", "extras", "theme"} {
		if on := press(t, onOption(t, m, k), "enter"); on.options.editing {
			t.Fatalf("enter on %s must not start text editing", k)
		}
	}
}

func TestEscLeavesTheOptionsAndKeepsTheBrowserSelectionAndFilter(t *testing.T) {
	m, _ := withOptions()
	m = press(t, m, "/", "z", "enter", "up") // filter z: fzf, zshonly; cursor on fzf
	next, cmd := m.Update(key("o"))
	m = settle(next.(Model), cmd)

	back := press(t, m, "esc")

	if back.screen != screenBrowser || back.filter != "z" {
		t.Fatalf("screen=%v filter=%q", back.screen, back.filter)
	}
	if v, _ := back.selected(); v.ID != "fzf" {
		t.Fatalf("selected = %q, want fzf", v.ID)
	}
}

func TestQuitsFromTheOptionsList(t *testing.T) {
	m, _ := withOptions()
	m = openFzfOptions(t, m)
	_, cmd := m.Update(key("q"))
	if cmd == nil {
		t.Fatal("q must quit from the option list")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("q did not quit")
	}
}

func TestTheOptionCursorMovesAndStopsAtBothEnds(t *testing.T) {
	m, _ := withOptions()
	m = openFzfOptions(t, m)

	if up := press(t, m, "up", "k"); up.options.cursor != 0 {
		t.Fatalf("cursor above the top = %d", up.options.cursor)
	}
	if down := press(t, m, "down", "j"); down.options.cursor != 2 {
		t.Fatalf("cursor after down, j = %d, want 2", down.options.cursor)
	}
	if end := press(t, m, "down", "down", "down", "down", "down", "down"); end.options.cursor != 4 {
		t.Fatalf("cursor past the bottom = %d, want 4", end.options.cursor)
	}
}

func TestKeysAreIgnoredWhileAnOptionIsBeingWritten(t *testing.T) {
	m, b := withOptions()
	m = openFzfOptions(t, m)
	writing, cmd := m.Update(key("space"))
	if cmd == nil {
		t.Fatal("setup: expected a write")
	}

	for _, k := range []string{"space", "down", "esc", "enter", "q"} {
		next, again := writing.(Model).Update(key(k))
		if again != nil || next.(Model).screen != screenOptions || next.(Model).options.cursor != 0 {
			t.Fatalf("%s while writing must be ignored", k)
		}
	}
	if _, quit := writing.(Model).Update(key("ctrl+c")); quit == nil {
		t.Fatal("ctrl+c must still quit")
	}
	settle(writing.(Model), cmd)
	if len(b.optionCalls) != 1 {
		t.Fatalf("backend calls = %v, want exactly the one write", b.optionCalls)
	}
}

func TestOptionTextIsNeutralisedBeforeItIsShownOrTyped(t *testing.T) {
	m, b := withOptions()
	b.optionRows["fzf"][2].Help = "evil\x1b]0;pwned\x07\x1b[2J\nhelp"
	b.optionRows["fzf"][2].Value = "v\x1b[31m"
	m = onOption(t, openFzfOptions(t, m), "prefix")
	for _, r := range m.options.rows {
		for _, s := range []string{r.Key, r.Help, r.Value, r.Default} {
			if strings.ContainsAny(s, "\x1b\x07\n") {
				t.Fatalf("option text still holds a control character: %q", s)
			}
		}
	}

	m = press(t, m, "enter")
	typed, _ := m.Update(tea.KeyPressMsg{Code: 'x', Text: "x\x1b[2J"})
	if strings.ContainsAny(typed.(Model).options.input, "\x1b") {
		t.Fatalf("typed text still holds an escape: %q", typed.(Model).options.input)
	}
}

func TestTheChangeCounterCountsOptionChangesNetAndWithModuleChanges(t *testing.T) {
	m, _ := withOptions()
	m = openFzfOptions(t, m)
	if m.changes() != 0 {
		t.Fatalf("changes = %d before touching anything, want 0", m.changes())
	}

	m = settleKey(t, m, "space")
	if m.changes() != 1 {
		t.Fatalf("changes = %d after one option, want 1", m.changes())
	}
	m = settleKey(t, onOption(t, m, "theme"), "right")
	if m.changes() != 2 {
		t.Fatalf("changes = %d after two options, want 2", m.changes())
	}
	m = settleKey(t, onOption(t, m, "ctrl_r"), "space") // back to the first value
	if m.changes() != 1 {
		t.Fatalf("changes = %d after setting one back, want 1", m.changes())
	}

	m = press(t, m, "esc") // browser: fzf is selected; toggle it on
	m = space(m)
	if m.changes() != 2 {
		t.Fatalf("changes = %d with one option and one module change, want 2", m.changes())
	}
}

func TestAFailedRereadAfterASuccessfulWriteShowsTheErrorAndKeepsTheScreen(t *testing.T) {
	m, b := withOptions()
	m = openFzfOptions(t, m)
	b.optionsErr = errors.New("cannot re-read")

	m = settleKey(t, m, "space")

	if m.screen != screenOptions || !strings.Contains(m.status, "cannot re-read") || m.pending {
		t.Fatalf("screen=%v status=%q pending=%v", m.screen, m.status, m.pending)
	}
	if len(b.optionCalls) != 1 {
		t.Fatalf("the write itself must have happened once: %v", b.optionCalls)
	}
}

func TestTheStatusMessageClearsOnTheNextKeyOnTheOptionsScreen(t *testing.T) {
	m, b := withOptions()
	b.setOptionErr = errors.New("nope")
	m = openFzfOptions(t, m)
	m = settleKey(t, m, "space")
	if m.status == "" {
		t.Fatal("setup: expected a message")
	}

	if m = press(t, m, "down"); m.status != "" {
		t.Fatalf("status = %q, want it cleared by the key press", m.status)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -count=1`
Expected: FAIL to build, `undefined: screenOptions` (and the other new identifiers).

- [ ] **Step 3: Implement**

Create `internal/tui/options.go`:

```go
package tui

import (
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/JtheGunner/omnishell/internal/modedit"
)

// optionsState is the options screen: the options of one module, the one under
// the cursor, and the text being typed while a string or int option is edited.
type optionsState struct {
	id      string // the module whose options are shown
	rows    []modedit.OptionView
	cursor  int
	editing bool   // true while the user types a new value
	input   string // the value typed so far
}

// optionsMsg carries the options of a module back to Update after `o`.
type optionsMsg struct {
	id   string
	rows []modedit.OptionView
	err  error
}

// optionWrittenMsg reports the outcome of writing one option: the error that
// stopped it, or the module's options re-read after the write.
type optionWrittenMsg struct {
	id   string
	rows []modedit.OptionView
	err  error
}

// optionsCmd reads the options of a module in the background.
func optionsCmd(b Backend, id string) tea.Cmd {
	return func() tea.Msg {
		rows, err := b.Options(id)
		return optionsMsg{id: id, rows: rows, err: err}
	}
}

// optionWriteCmd writes one option and then re-reads the module's options, so
// the screen shows what config.toml now holds.
func optionWriteCmd(b Backend, id, key, raw string) tea.Cmd {
	return func() tea.Msg {
		if err := b.SetOption(id, key, raw); err != nil {
			return optionWrittenMsg{id: id, err: err}
		}
		rows, err := b.Options(id)
		return optionWrittenMsg{id: id, rows: rows, err: err}
	}
}

// openOptions asks for the options of the selected module. It does nothing
// while another write is pending, and says so when the module has no options.
func (m Model) openOptions() (tea.Model, tea.Cmd) {
	v, ok := m.selected()
	if !ok || m.pending {
		return m, nil
	}
	if v.OptionCount == 0 {
		m.status = v.ID + " has no options"
		return m, nil
	}
	m.pending = true
	return m, optionsCmd(m.backend, v.ID)
}

// applyOptions shows the options screen, or the reason it cannot be shown.
func (m Model) applyOptions(msg optionsMsg) Model {
	m.pending = false
	if msg.err != nil {
		m.status = sanitize(msg.err.Error())
		return m
	}
	rows := sanitizeOptions(msg.rows)
	m.options = optionsState{id: msg.id, rows: rows}
	m.screen = screenOptions
	m.rememberOptions(msg.id, rows)
	return m
}

// applyOptionWritten shows a rejected value on the status line and keeps the
// text being typed, so it can be corrected; or swaps in the re-read options and
// leaves the editing mode.
func (m Model) applyOptionWritten(msg optionWrittenMsg) Model {
	m.pending = false
	if msg.err != nil {
		m.status = sanitize(msg.err.Error())
		return m
	}
	rows := sanitizeOptions(msg.rows)
	m.options.rows = rows
	m.options.cursor = min(m.options.cursor, max(len(rows)-1, 0))
	m.options.editing = false
	m.options.input = ""
	m.rememberOptions(msg.id, rows)
	return m
}

// rememberOptions records the current values of a module's options, and their
// values the first time they were seen, so changes() can tell what changed.
func (m *Model) rememberOptions(id string, rows []modedit.OptionView) {
	now := make(map[string]string, len(rows))
	for _, r := range rows {
		now[r.Key] = r.Value
	}
	if m.optionBase == nil {
		m.optionBase = map[string]map[string]string{}
	}
	if _, seen := m.optionBase[id]; !seen {
		m.optionBase[id] = now
	}
	if m.optionNow == nil {
		m.optionNow = map[string]map[string]string{}
	}
	m.optionNow[id] = now
}

// changedOptions counts the options whose value differs from the first value
// the options screen showed, so setting one back counts as no change.
func (m Model) changedOptions() int {
	n := 0
	for id, now := range m.optionNow {
		for key, value := range now {
			if m.optionBase[id][key] != value {
				n++
			}
		}
	}
	return n
}

// sanitizeOptions returns a cleaned copy of rows; see sanitize.go.
func sanitizeOptions(rows []modedit.OptionView) []modedit.OptionView {
	out := make([]modedit.OptionView, len(rows))
	for i, r := range rows {
		r.Key = sanitize(r.Key)
		r.Type = sanitize(r.Type)
		r.Help = sanitize(r.Help)
		r.Pattern = sanitize(r.Pattern)
		r.Default = sanitize(r.Default)
		r.Value = sanitize(r.Value)
		r.Values = sanitizeAll(r.Values)
		out[i] = r
	}
	return out
}

// selectedOption returns the option under the cursor, if there is one.
func (m Model) selectedOption() (modedit.OptionView, bool) {
	if m.options.cursor < 0 || m.options.cursor >= len(m.options.rows) {
		return modedit.OptionView{}, false
	}
	return m.options.rows[m.options.cursor], true
}

// updateOptions handles a key press on the options screen. While a write is
// pending only ctrl+c works: the keys would act on values about to change.
func (m Model) updateOptions(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.pending {
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		return m, nil
	}
	if m.options.editing {
		return m.updateOptionInput(msg)
	}
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "esc", "b":
		m.screen = screenBrowser
		m.options = optionsState{}
	case "up", "k":
		m.options.cursor = max(m.options.cursor-1, 0)
	case "down", "j":
		m.options.cursor = min(m.options.cursor+1, max(len(m.options.rows)-1, 0))
	case "space":
		if row, ok := m.selectedOption(); ok && row.Editable && row.Type == "bool" {
			next := "true"
			if row.Value == "true" {
				next = "false"
			}
			return m.writeOption(row.Key, next)
		}
	case "left":
		return m.cycleEnum(-1)
	case "right":
		return m.cycleEnum(1)
	case "enter":
		if row, ok := m.selectedOption(); ok && row.Editable && (row.Type == "string" || row.Type == "int") {
			m.options.editing = true
			m.options.input = row.Value
		}
	}
	return m, nil
}

// updateOptionInput handles a key press while a string or int value is typed.
func (m Model) updateOptionInput(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.options.editing = false
		m.options.input = ""
	case "enter":
		if row, ok := m.selectedOption(); ok {
			return m.writeOption(row.Key, m.options.input)
		}
	case "backspace":
		if runes := []rune(m.options.input); len(runes) > 0 {
			m.options.input = string(runes[:len(runes)-1])
		}
	default:
		if msg.Text != "" {
			m.options.input += sanitize(msg.Text)
		}
	}
	return m, nil
}

// cycleEnum moves an enum option to the next (delta 1) or previous (delta -1)
// allowed value, wrapping around, and writes it.
func (m Model) cycleEnum(delta int) (tea.Model, tea.Cmd) {
	row, ok := m.selectedOption()
	if !ok || !row.Editable || row.Type != "enum" || len(row.Values) == 0 {
		return m, nil
	}
	n := len(row.Values)
	idx := slices.Index(row.Values, row.Value)
	var next int
	switch {
	case idx < 0 && delta > 0:
		next = 0
	case idx < 0:
		next = n - 1
	default:
		next = (idx + delta + n) % n
	}
	return m.writeOption(row.Key, row.Values[next])
}

// writeOption starts writing one option; the write itself runs in a command.
func (m Model) writeOption(key, raw string) (tea.Model, tea.Cmd) {
	m.pending = true
	return m, optionWriteCmd(m.backend, m.options.id, key, raw)
}
```

Replace `internal/tui/model.go` (adds the screen, the option state and baselines, the two messages, the `o` key, and the option part of the change count):

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
	backend   Backend
	views     []modedit.ModuleView
	visible   []int // indexes into views that match the filter, in order
	cursor    int   // position within visible
	filter    string
	filtering bool                      // true while the user is typing into the filter
	status    string                    // the last error to show, cleared by the next key press
	pending   bool                      // true from pressing space until the write has finished
	initial   map[string]modedit.Status // each module's state when the browser started
	screen    screen
	options   optionsState
	// optionBase holds each option's value the first time its module's options
	// were shown, optionNow the latest; their difference is what changed.
	optionBase     map[string]map[string]string
	optionNow      map[string]map[string]string
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
	case optionsMsg:
		return m.applyOptions(msg), nil
	case optionWrittenMsg:
		return m.applyOptionWritten(msg), nil
	case tea.KeyPressMsg:
		m.status = ""
		switch m.screen {
		case screenPlan:
			return m.updatePlan(msg)
		case screenOptions:
			return m.updateOptions(msg)
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
	case "o":
		return m.openOptions()
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

// changes counts the modules whose enabled state, and the options whose value,
// differ from what they were when the browser started, so setting something
// back counts as no change.
func (m Model) changes() int {
	n := m.changedOptions()
	for _, v := range m.views {
		if initial, ok := m.initial[v.ID]; ok && initial != v.Status {
			n++
		}
	}
	return n
}
```

In `internal/tui/plan.go`, add the third screen. Replace:

```go
const (
	screenBrowser screen = iota
	screenPlan
)
```

with:

```go
const (
	screenBrowser screen = iota
	screenPlan
	screenOptions
)
```

and make the plan share the body height. Replace:

```go
// planRows is how many plan lines fit on the screen.
func (m Model) planRows() int {
	return max(m.height-headerLines-footerLines, 1)
}
```

with:

```go
// planRows is how many plan lines fit on the screen.
func (m Model) planRows() int { return m.bodyRows() }
```

In `internal/tui/view.go`, add `bodyRows` directly above the comment `// changesText says how many`:

```go
// bodyRows is how many lines fit between the header and the footer.
func (m Model) bodyRows() int {
	return max(m.height-headerLines-footerLines, 1)
}

// changesText says how many
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `gofmt -l internal/tui && go vet ./internal/tui/ && go test ./internal/tui/ -race -count=1`
Expected: `gofmt -l` prints nothing; PASS, including the unchanged golden tests (the view does not draw the options screen yet, and the browser footer is unchanged so far).

- [ ] **Step 5: Commit**

```bash
git add internal/tui
git commit -m "feat: add the options screen to the tui model"
```

---

### Task 4: Draw the options screen

**Files:**
- Create: `internal/tui/optionsview.go`
- Modify: `internal/tui/view.go`
- Test: `internal/tui/optionsview_test.go`; golden files in `internal/tui/testdata/` (generated)

**Interfaces:**
- Consumes: everything from Task 3, plus `titleStyle`, `dimStyle`, `statusStyle`, `changesText`, `clip` (existing in `view.go` and `planview.go`).
- Produces: `func (m Model) renderOptions() string`, `optionTable(n int) []string` and the helpers it uses.

- [ ] **Step 1: Write the failing tests**

`internal/tui/optionsview_test.go`:

```go
package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/JtheGunner/omnishell/internal/modedit"
)

// optionScreens returns the options screen of fzf in the states worth a
// snapshot, at 80x20.
func optionScreens(t *testing.T) map[string]Model {
	t.Helper()
	open := func() (Model, *fakeBackend) {
		m, b := withOptions()
		return openFzfOptions(t, m), b
	}

	bool_, _ := open()
	enum, _ := open()
	editing, _ := open()
	list, _ := open()
	failing, failingBackend := open()
	failingBackend.setOptionErr = errors.New(`invalid value for fzf.prefix: "ABC" does not match required pattern ^[a-z]+$`)

	return map[string]Model{
		"options-bool":    bool_,
		"options-enum":    onOption(t, enum, "theme"),
		"options-editing": press(t, onOption(t, editing, "prefix"), "enter", "x"),
		"options-list":    onOption(t, list, "extras"),
		"options-error": settleKey(t, press(t, onOption(t, failing, "prefix"), "enter", "backspace", "backspace", "backspace", "A", "B", "C"),
			"enter"),
	}
}

func TestOptionsScreenFillsTheTerminalExactlyInEveryState(t *testing.T) {
	for name, m := range optionScreens(t) {
		assertFits(t, plain(m), 80, 20)
		assertFits(t, plain(sized(m, 120, 40)), 120, 40)
		if t.Failed() {
			t.Fatalf("state %s did not fit", name)
		}
	}
}

func TestOptionsScreenHeaderNamesTheModuleAndCountsChanges(t *testing.T) {
	m := optionScreens(t)["options-bool"]
	if out := plain(m); !strings.Contains(out, "options · fzf · 0 changes since start") {
		t.Fatalf("header before a change:\n%s", out)
	}

	m = settleKey(t, m, "space")
	if out := plain(m); !strings.Contains(out, "options · fzf · 1 change since start") {
		t.Fatalf("header after a change:\n%s", out)
	}
}

func TestOptionsTableShowsKeysValuesTypesAndWhereAValueComesFrom(t *testing.T) {
	m, b := withOptions()
	b.optionRows["fzf"][3].Value, b.optionRows["fzf"][3].Set = "9", true         // retries: set
	b.optionRows["fzf"][4].Value, b.optionRows["fzf"][4].Invalid = "weird", true // theme: invalid
	b.optionRows["fzf"][4].Set = true
	out := plain(openFzfOptions(t, m))

	for _, want := range []string{
		"▸ ctrl_r", "true (default)", "bool",
		"  extras", "a,b (default)", "list<string>",
		"  retries", " 9 ", "int",
		"weird (invalid)", "enum",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "9 (default)") {
		t.Fatalf("an option that is set must not be labelled default:\n%s", out)
	}
}

func TestOptionsDetailExplainsTheSelectedOptionByType(t *testing.T) {
	m := openFzfOptions(t, mustWithOptions())
	cases := []struct {
		key  string
		want []string
	}{
		{"ctrl_r", []string{"Bind Ctrl+R to the fzf history widget", "Type:    bool · space toggles", "Default: true"}},
		{"theme", []string{"Colour theme", "Type:    enum · ←/→ changes", "Allowed: dark, light, solarized"}},
		{"prefix", []string{"Key prefix", "Type:    string · enter edits", "Pattern: ^[a-z]+$"}},
		{"retries", []string{"Type:    int · enter edits", "Default: 3"}},
		{"extras", []string{"Type:    list<string> · a list; set it with omnishell set"}},
	}
	for _, c := range cases {
		out := plain(onOption(t, m, c.key))
		for _, want := range c.want {
			if !strings.Contains(out, want) {
				t.Fatalf("%s: missing %q:\n%s", c.key, want, out)
			}
		}
	}
}

func mustWithOptions() Model {
	m, _ := withOptions()
	return m
}

func TestOptionsDetailWarnsWhenTheModuleIsNotEnabledAndStopsOnceItIs(t *testing.T) {
	m, _ := withOptions()
	disabled := plain(openFzfOptions(t, m))
	if !strings.Contains(disabled, "this module is not enabled") {
		t.Fatalf("a disabled module needs the note:\n%s", disabled)
	}

	m, _ = withOptions()
	m = space(press(t, m, "down")) // enable fzf in the browser first
	next, cmd := m.Update(key("o"))
	enabled := plain(settle(next.(Model), cmd))
	if strings.Contains(enabled, "not enabled") {
		t.Fatalf("an enabled module must not get the note:\n%s", enabled)
	}
}

func TestOptionsEditShowsTheTypedTextInPlaceOfTheValue(t *testing.T) {
	out := plain(optionScreens(t)["options-editing"])

	if !strings.Contains(out, "[abcx_]") {
		t.Fatalf("the input cell is missing:\n%s", out)
	}
	if !strings.Contains(out, "type a value · enter save · esc cancel") {
		t.Fatalf("the editing footer is missing:\n%s", out)
	}
}

func TestOptionsErrorReplacesTheFooterAndKeepsTheInput(t *testing.T) {
	out := plain(optionScreens(t)["options-error"])

	if !strings.Contains(out, `! invalid value for fzf.prefix: "ABC" does not match required pattern`) {
		t.Fatalf("the status line is missing:\n%s", out)
	}
	if !strings.Contains(out, "[ABC_]") {
		t.Fatalf("the typed text must stay visible for correction:\n%s", out)
	}
}

func TestOptionsFooterShowsSavingWhileAWriteIsPending(t *testing.T) {
	m := optionScreens(t)["options-bool"]
	writing, cmd := m.Update(key("space"))

	pending := plain(writing.(Model))
	if !strings.Contains(pending, "saving…") {
		t.Fatalf("a pending write must be visible:\n%s", pending)
	}
	assertFits(t, pending, 80, 20)
	if done := plain(settle(writing.(Model), cmd)); strings.Contains(done, "saving…") {
		t.Fatalf("the marker must disappear once the write is done:\n%s", done)
	}
}

func TestOptionsListOptionsAreDimmedBecauseTheyCannotBeEditedHere(t *testing.T) {
	m := optionScreens(t)["options-bool"]
	rows := m.optionTable(10)

	const faint = "\x1b[2m"
	if strings.Contains(rows[0], faint) { // ctrl_r is editable (and selected: reverse, not faint)
		t.Fatalf("an editable option must not be dimmed: %q", rows[0])
	}
	if !strings.Contains(rows[1], faint) { // extras is a list
		t.Fatalf("a list option must be dimmed: %q", rows[1])
	}
}

func TestOptionsScreenScrollsTheTableToKeepTheCursorVisible(t *testing.T) {
	m, b := withOptions()
	many := make([]modedit.OptionView, 30)
	for i := range many {
		many[i] = modedit.OptionView{Key: fmt.Sprintf("opt%02d", i), Type: "bool", Default: "false", Value: "false", Editable: true}
	}
	b.optionRows["fzf"] = many
	m = openFzfOptions(t, m)
	for range 29 {
		m = press(t, m, "down")
	}

	out := plain(m)
	if !strings.Contains(out, "▸ opt29") || strings.Contains(out, "opt00") {
		t.Fatalf("the cursor row opt29 must be visible and opt00 scrolled out:\n%s", out)
	}
	assertFits(t, out, 80, 20)
}

func TestOptionsScreenKeepsLongValuesAndHelpInsideTheTerminal(t *testing.T) {
	m, b := withOptions()
	b.optionRows["fzf"][2].Value = strings.Repeat("v", 300)
	b.optionRows["fzf"][2].Key = strings.Repeat("k", 100)
	b.optionRows["fzf"][2].Help = strings.Repeat("a very long help text ", 40)
	m = onOption(t, openFzfOptions(t, m), strings.Repeat("k", 100))

	assertFits(t, plain(m), 80, 20)
	assertFits(t, plain(press(t, m, "enter")), 80, 20)
}

func TestOptionsScreenGivesWayToTheTooSmallMessage(t *testing.T) {
	m := sized(optionScreens(t)["options-bool"], 60, 10)

	if out := plain(m); !strings.Contains(out, "Terminal too small") || strings.Contains(out, "options ·") {
		t.Fatalf("a small terminal must show the too-small message:\n%s", out)
	}
}

func TestBrowserHelpMentionsTheOptionsKey(t *testing.T) {
	if out := plain(sized(newTestModel(sampleViews()), 80, 20)); !strings.Contains(out, "o options") {
		t.Fatalf("the browser footer should list the options key:\n%s", out)
	}
}

func TestOptionsScreenGoldenFiles(t *testing.T) {
	for name, m := range optionScreens(t) {
		assertGolden(t, name, plain(m))
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go vet ./internal/tui/`
Expected: FAIL to build, `m.optionTable undefined (type Model has no field or method optionTable)`.

- [ ] **Step 3: Implement**

Create `internal/tui/optionsview.go`:

```go
package tui

import (
	"fmt"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/JtheGunner/omnishell/internal/modedit"
)

const (
	// optionDetailLines is how many lines under the option table describe the
	// option under the cursor.
	optionDetailLines = 8
	keyColumn         = 22 // width of the option name column
	valueColumn       = 26 // width of the value column
)

// renderOptions draws the options screen: a header, exactly bodyRows lines of
// body (the option table, then the selected option's description) and a footer.
func (m Model) renderOptions() string {
	rows := m.bodyRows()
	tableRows := max(rows-optionDetailLines, 1)

	// The description follows the table directly, however few options there are.
	body := m.optionTable(tableRows)
	body = append(body, "")
	body = append(body, m.optionDetail(max(rows-len(body), 0))...)
	for len(body) < rows {
		body = append(body, "")
	}
	for i, line := range body[:rows] {
		body[i] = ansi.Truncate(line, m.width, "…")
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		m.renderOptionsHeader(),
		strings.Join(body[:rows], "\n"),
		m.renderOptionsFooter(),
	)
}

func (m Model) renderOptionsHeader() string {
	line := titleStyle.Render("omnishell") +
		dimStyle.Render(fmt.Sprintf("  options · %s · %s", m.options.id, changesText(m.changes())))
	return lipgloss.NewStyle().Inline(true).MaxWidth(m.width).Render(line)
}

func (m Model) renderOptionsFooter() string {
	switch {
	case m.status != "":
		return statusStyle.Render(ansi.Truncate("! "+m.status, m.width, "…"))
	case m.pending:
		return dimStyle.Inline(true).MaxWidth(m.width).Render("saving…")
	case m.options.editing:
		return dimStyle.Inline(true).MaxWidth(m.width).Render("type a value · enter save · esc cancel · ctrl+c quit")
	}
	return dimStyle.Inline(true).MaxWidth(m.width).
		Render("↑/↓ move · space toggle · ←/→ change · enter edit · esc back · q quit")
}

// optionTable draws at most n option lines, scrolled so the cursor is visible.
func (m Model) optionTable(n int) []string {
	rows := m.options.rows
	if len(rows) == 0 {
		return []string{dimStyle.Render("No options")}
	}
	start := 0
	if m.options.cursor >= n {
		start = m.options.cursor - n + 1
	}
	end := min(start+n, len(rows))

	lines := make([]string, 0, end-start)
	for pos := start; pos < end; pos++ {
		row := rows[pos]
		marker := "  "
		if pos == m.options.cursor {
			marker = "▸ "
		}
		line := marker + pad(row.Key, keyColumn) + pad(m.optionValueCell(pos, row), valueColumn) + row.Type

		style := lipgloss.NewStyle()
		if !row.Editable {
			style = dimStyle
		}
		if pos == m.options.cursor {
			style = style.Reverse(true)
		}
		lines = append(lines, style.Render(ansi.Truncate(line, m.width, "…")))
	}
	return lines
}

// optionValueCell is what the value column shows for one option: the text being
// typed for the row under edit, otherwise the value with a note where it is not
// a plain, explicitly set value.
func (m Model) optionValueCell(pos int, row modedit.OptionView) string {
	switch {
	case m.options.editing && pos == m.options.cursor:
		return "[" + m.options.input + "_]"
	case row.Invalid:
		return row.Value + " (invalid)"
	case !row.Set:
		return row.Value + " (default)"
	}
	return row.Value
}

// pad cuts s to width cells (with an ellipsis) or pads it with spaces.
func pad(s string, width int) string {
	s = ansi.Truncate(s, width-1, "…")
	return s + strings.Repeat(" ", max(width-ansi.StringWidth(s), 0))
}

// optionDetail describes the option under the cursor in at most n lines.
func (m Model) optionDetail(n int) []string {
	row, ok := m.selectedOption()
	if !ok {
		return nil
	}

	var lines []string
	if row.Help != "" {
		wrapped := lipgloss.NewStyle().Width(max(m.width-2, 1)).Render(row.Help)
		lines = append(lines, clip(strings.Split(wrapped, "\n"), 3)...)
	}
	lines = append(lines, "Type:    "+row.Type+" · "+optionHint(row))
	lines = append(lines, "Default: "+row.Default)
	if len(row.Values) > 0 {
		lines = append(lines, "Allowed: "+strings.Join(row.Values, ", "))
	}
	if row.Pattern != "" {
		lines = append(lines, "Pattern: "+row.Pattern)
	}
	if !m.moduleEnabled(m.options.id) {
		lines = append(lines, "Note:    this module is not enabled; its options apply once it is")
	}
	return clip(lines, n)
}

// optionHint says how the option under the cursor is changed.
func optionHint(row modedit.OptionView) string {
	switch {
	case !row.Editable:
		return "a list; set it with omnishell set"
	case row.Type == "bool":
		return "space toggles"
	case row.Type == "enum":
		return "←/→ changes"
	}
	return "enter edits"
}

// moduleEnabled reports whether the module with the given id is enabled.
func (m Model) moduleEnabled(id string) bool {
	i := slices.IndexFunc(m.views, func(v modedit.ModuleView) bool { return v.ID == id })
	return i >= 0 && m.views[i].Status == modedit.StatusEnabled
}
```

In `internal/tui/view.go`, in `render()`, route the new screen. Replace:

```go
	if m.screen == screenPlan {
		return m.renderPlan()
	}
```

with:

```go
	switch m.screen {
	case screenPlan:
		return m.renderPlan()
	case screenOptions:
		return m.renderOptions()
	}
```

and in `renderFooter()`, replace the browser help line

```go
help := "↑/↓ move · space toggle · a plan · / filter · esc clear filter · q quit"
```

with

```go
help := "↑/↓ move · space toggle · o options · a plan · / filter · esc clear · q quit"
```

- [ ] **Step 4: Run the structural tests to verify they pass**

Run: `gofmt -l internal/tui && go vet ./internal/tui/ && go test ./internal/tui/ -count=1 -skip Golden`
Expected: `gofmt -l` prints nothing; PASS for everything except the skipped golden tests.

- [ ] **Step 5: Regenerate the golden files and review the diff by eye**

Run: `go test ./internal/tui/ -run Golden -update -count=1 && git status --short internal/tui/testdata`
Expected: six existing goldens modified (`browser-80x20`, `browser-filtered`, `browser-no-modules`, `browser-second-row`, `browser-toggled`, `browser-unavailable`: only the footer help line changed), eight unchanged (`browser-rejected`, `browser-too-small`, `browser-typing` and the five `plan-*` files) and five new files: `options-bool`, `options-editing`, `options-enum`, `options-error`, `options-list`. Run `git diff internal/tui/testdata` and check that each modified file differs only in that one footer line. `options-enum.golden` must read exactly (lines are padded with trailing spaces):

```text
omnishell  options · fzf · 0 changes since start                              
  ctrl_r                true (default)            bool                        
  extras                a,b (default)             list<string>                
  prefix                abc (default)             string                      
  retries               3 (default)               int                         
▸ theme                 dark (default)            enum                        
                                                                              
Colour theme                                                                  
Type:    enum · ←/→ changes                                                   
Default: dark                                                                 
Allowed: dark, light, solarized                                               
Note:    this module is not enabled; its options apply once it is             
                                                                              
                                                                              
                                                                              
                                                                              
                                                                              
                                                                              
                                                                              
↑/↓ move · space toggle · ←/→ change · enter edit · esc back · q quit         
```

`options-editing.golden` shows `[abcx_]` in the value column and the footer `type a value · enter save · esc cancel · ctrl+c quit`; `options-error.golden` ends with the status line `! invalid value for fzf.prefix: "ABC" does not match required pattern ^[a-z]+$` and still shows `[ABC_]`; `options-list.golden` selects `extras` and its `Type:` line ends with `a list; set it with omnishell set`.

- [ ] **Step 6: Run the package with the race detector**

Run: `go test ./internal/tui/ -race -count=1`
Expected: PASS without `-update`.

- [ ] **Step 7: Commit**

```bash
git add internal/tui
git commit -m "feat: draw the options screen in the tui"
```

---

### Task 5: Documentation and verification

**Files:**
- Modify: `README.md`, `CHANGELOG.md`, `CLAUDE.md`, `internal/cli/tui.go` (help text)

**Interfaces:**
- Consumes: everything above.
- Produces: nothing code-level.

- [ ] **Step 1: Mention the options key in `omnishell tui --help`**

In `internal/cli/tui.go`, in the `Long` text of `newTUICmd`, replace:

```text
the selected module, type / to filter, q to quit.
```

with:

```text
the selected module, press o to edit its options, type / to filter, q to quit.
```

(If the line is re-wrapped by the editor, keep the text; the sentence just gains `press o to edit its options,`.)

- [ ] **Step 2: Update the README row**

In `README.md`, replace the Commands-table row for `omnishell tui` (find it with `grep -n 'omnishell tui' README.md`) with:

```markdown
| `omnishell tui`                        | Browse and toggle modules in a full-screen terminal UI: a module list with description, homepage, package status, platforms and shells, a `/` filter, and Space to enable or disable the selected module (written to `config.toml` at once, like `enable` / `disable`). `o` edits the module's options (bool, enum, string and int; each change is written at once and validated like `omnishell set`). `a` previews the plan; confirming it closes the UI and runs `omnishell apply`, which still asks before it changes anything. Modules that cannot run on this host are dimmed. Needs an interactive terminal (exit 2 otherwise).                                                                                          | —                                                                                                                                                                                                                                                                                                               |
```

- [ ] **Step 3: Update the changelog entry**

In `CHANGELOG.md`, in the `omnishell tui` bullet under `## [Unreleased]` → `### Added`, replace:

```markdown
or disables the selected module in `config.toml`, like `enable` / `disable`.
  `a` previews the plan;
```

with:

```markdown
or disables the selected module in `config.toml`, like `enable` / `disable`.
  `o` edits the module's options (bool, enum, string and int), validated like
  `omnishell set`. `a` previews the plan;
```

- [ ] **Step 4: Extend the `CLAUDE.md` entry for `internal/tui`**

In `CLAUDE.md`, in item 11, replace:

```markdown
   one write at a time. The plan screen (`a`) asks `Backend.Plan`
```

with:

```markdown
   one write at a time. The options screen (`o`) reads `Backend.Options`
   (config.toml and the manifest only, so it is cheap) and writes through
   `Backend.SetOption`, which is `modedit.SetOption`: bool and enum options
   change on one key press, string and int options are typed and validated on
   enter (a rejected value keeps the typed text), list options are shown but not
   editable. The header's change count includes options whose value differs from
   the first value the screen showed. The plan screen (`a`) asks `Backend.Plan`
```

- [ ] **Step 5: Run the full verification**

Run: `make vet && make test`
Expected: both clean; every package PASS (124 tests in `internal/tui`, 22 `TUI` tests in `internal/cli`, the 8 `Options` tests in `internal/modedit`).

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

- [ ] **Step 6: Check the options screen with the real binary in a pseudo-terminal**

The unit tests fake the backend, so check the real thing once: a real `config.toml`, real validation. This only ever uses a throwaway `HOME`; never point it at a real config. Save this driver outside the repository (for example in the scratch directory) as `pty_drive.py`:

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

Then (every command that needs the throwaway `HOME` gets it through the `E` function and `env`; nothing is exported):

```bash
T=$(mktemp -d) && go build -o $T/omnishell ./cmd/omnishell && mkdir -p $T/home
E() { env HOME=$T/home XDG_CONFIG_HOME=$T/home/.config "$@"; }
E $T/omnishell init >/dev/null
# A. switch the bool option tmux.auto_attach with Space
python3 pty_drive.py $T/omnishell $T/home $T/runA.log "wait:modules|send:/|send:tmux|send:\\r|send:o|wait:options · tmux|send: |send:\\x1b|wait:modules|send:q"; grep -E "exit code|-> " $T/runA.log | tr '\n' ' '; echo
grep -c 'auto_attach = false' $T/home/.config/omnishell/config.toml
# B. type a value the pattern rejects (tmux.session must match ^[A-Za-z_][A-Za-z0-9_-]*$): "default!"
python3 pty_drive.py $T/omnishell $T/home $T/runB.log "wait:modules|send:/|send:tmux|send:\\r|send:o|wait:options · tmux|send:j|send:\\r|send:!|send:\\r|wait:does not match required pattern|send:\\x1b|send:\\x1b|wait:modules|send:q"; grep -E "exit code|-> " $T/runB.log | tr '\n' ' '; echo
grep -c 'session' $T/home/.config/omnishell/config.toml
# C. type a valid value: "defaultx"
python3 pty_drive.py $T/omnishell $T/home $T/runC.log "wait:modules|send:/|send:tmux|send:\\r|send:o|wait:options · tmux|send:j|send:\\r|send:x|send:\\r|send:\\x1b|wait:modules|send:q"; grep "exit code" $T/runC.log
grep -A3 'modules.tmux' $T/home/.config/omnishell/config.toml
rm -rf $T
```

Expected:
- Run A: every `wait` answered `True`, `exit code: 0`; the `grep -c` prints `1`.
- Run B: every `wait` answered `True` (including `does not match required pattern`, the status line), `exit code: 0`; the `grep -c` prints `0`: the rejected value was not written.
- Run C: `exit code: 0`; the last command prints a `[modules.tmux.options]` table that holds `session = "defaultx"` next to `auto_attach = false`.

(The driver starts the binary itself and kills it after a hard 60-second limit, so a hang shows up as a failed step in the log, not as a stuck session. Do not add `wait:` markers for text that only changes in place, such as the change count: the terminal renderer redraws only the cells that changed, so such a phrase never appears contiguously in the output.)

- [ ] **Step 7: Commit**

```bash
git add README.md CHANGELOG.md CLAUDE.md internal/cli/tui.go
git commit -m "docs: describe the options editor in the omnishell tui"
```
