# modedit Extraction Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move the enable/disable/set rules and the `list` view builder out of `internal/cli` into a new `internal/modedit` package, with no change in observable behaviour.

**Architecture:** `modedit.Editor` bundles an `engine.Engine` and the `config.toml` path and exposes `Enable`, `Disable`, `SetOption` and `Views`. The CLI commands `enable`, `disable`, `set` and `list` become thin callers. This is sub-task 1 of the TUI epic; the TUI itself consumes `modedit` in later sub-tasks.

**Tech Stack:** Go 1.26, existing dependencies only (`BurntSushi/toml`, `spf13/cobra`). No new dependencies.

**Spec:** `docs/superpowers/specs/2026-10-07-interactive-tui-design.md` (sections "Architecture" and "Sub-tasks", item 1).

## Global Constraints

- No new dependencies in `go.mod`.
- `enable`, `disable`, `set` and `list` behave exactly as before: same stdout and stderr text, same exit codes (config and schema errors exit `2`), same JSON shape and field order for `list --json`.
- Errors stay `config.Error` (or wrap `config.ErrNotFound`), so `ClassifyError` is untouched.
- `config.toml` is written only through `config.SetEnabled` and `config.SetOption` (atomic, comment-preserving). `modedit` never writes any other file.
- `internal/modedit` must not import `internal/cli`.
- The existing tests in `internal/cli` (`enable_test.go`, `set_test.go`, `list_test.go`) are not edited or deleted; they are the regression net.
- All identifiers, comments and commit messages are in English; commit subjects use `<type>: <description>` with no attribution trailers.

## Review Focus

- `config.toml` missing: `Enable`, `Disable` and `SetOption` return an error wrapping `config.ErrNotFound` and create no file (Tasks 1 and 2).
- Disabling a module that cannot run on any managed shell must still work; only enabling is shell-checked (Task 1).
- A rejected `SetOption` (pattern mismatch, bad enum) leaves `config.toml` byte-identical (Task 2).
- Malformed `config.toml`: `Views` returns an error rather than an empty or all-disabled list (Task 3).
- No package manager detected, or an empty registry: `Views` reports `n/a` packages and an empty non-nil slice instead of failing (Task 3).

---

### Task 1: `modedit.Editor` with `Enable` and `Disable`

**Files:**
- Create: `internal/modedit/editor.go`
- Create: `internal/modedit/helpers_test.go`
- Create: `internal/modedit/editor_test.go`
- Create: `internal/modedit/testdata/modules/fzf/manifest.toml`
- Create: `internal/modedit/testdata/modules/plain/manifest.toml`
- Create: `internal/modedit/testdata/modules/zshonly/manifest.toml`

**Interfaces:**
- Consumes: `config.Load`, `config.SetEnabled`, `config.Error`, `engine.ManagedShells`, `module.Registry.Get`.
- Produces:
  ```go
  type Editor struct {
      Engine  engine.Engine
      CfgPath string
  }
  func (ed Editor) Enable(id string) error
  func (ed Editor) Disable(id string) error
  ```

- [ ] **Step 1: Create the fixture modules**

`internal/modedit/testdata/modules/fzf/manifest.toml`:

```toml
platforms = ["macos", "linux"]
shells    = ["zsh", "bash"]
requires  = []
after     = []

[module]
id          = "fzf"
name        = "FZF Fuzzy Finder"
description = "Ctrl+R history search"
homepage    = "https://github.com/junegunn/fzf"
version     = "1.0.0"
schema      = 1

[packages]
apt  = ["fzf"]
brew = ["fzf"]

[options.ctrl_r]
type    = "bool"
default = true
help    = "Bind Ctrl+R to the fzf history widget"

[options.theme]
type    = "enum"
default = "dark"
values  = ["dark", "light"]
help    = "Colour theme"

[options.prefix]
type    = "string"
default = "abc"
pattern = "^[a-z]+$"
help    = "Key prefix"
```

`internal/modedit/testdata/modules/plain/manifest.toml`:

```toml
platforms = ["macos", "linux"]
shells    = ["zsh", "bash"]
requires  = []
after     = []

[module]
id          = "plain"
name        = "Plain"
description = "A module that needs no external package"
version     = "1.0.0"
schema      = 1
```

`internal/modedit/testdata/modules/zshonly/manifest.toml`:

```toml
platforms = ["macos", "linux"]
shells    = ["zsh"]
requires  = []
after     = []

[module]
id          = "zshonly"
name        = "Zsh-only fixture"
description = "A module that only supports zsh"
version     = "1.0.0"
schema      = 1
```

- [ ] **Step 2: Write the test helper**

`internal/modedit/helpers_test.go`:

```go
package modedit_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/JtheGunner/omnishell/internal/engine"
	"github.com/JtheGunner/omnishell/internal/modedit"
	"github.com/JtheGunner/omnishell/internal/module"
	"github.com/JtheGunner/omnishell/internal/pkgmgr"
	"github.com/JtheGunner/omnishell/internal/platform"
)

// newEditor builds an Editor over the fixture modules in testdata/modules and a
// config.toml in a temp dir. withConfig writes the default config; otherwise
// the file does not exist. presentShells names the shells reported as present
// on the fake host. It returns the editor and the config path.
func newEditor(t *testing.T, withConfig bool, presentShells ...string) (modedit.Editor, string) {
	t.Helper()
	reg, err := module.LoadRegistry(nil, "testdata/modules")
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	present := map[string]bool{}
	for _, s := range presentShells {
		present[s] = true
	}
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	if withConfig {
		if err := os.WriteFile(cfgPath, config.RenderDefault(), 0o644); err != nil {
			t.Fatalf("write config: %v", err)
		}
	}
	ed := modedit.Editor{
		Engine: engine.Engine{
			Platform: platform.Info{
				OS:        platform.Linux,
				HomeDir:   t.TempDir(),
				ConfigDir: t.TempDir(),
				Shells: []platform.ShellInfo{
					{Name: "zsh", RCPath: "/x/.zshrc", Present: present["zsh"]},
					{Name: "bash", RCPath: "/x/.bashrc", Present: present["bash"]},
				},
			},
			Registry:  reg,
			Manager:   &pkgmgr.MockManager{NameV: "apt", DetectV: true, Installed: map[string]bool{}},
			ManagerOK: true,
			Runner:    &pkgmgr.MockRunner{},
		},
		CfgPath: cfgPath,
	}
	return ed, cfgPath
}
```

- [ ] **Step 3: Write the failing tests**

`internal/modedit/editor_test.go`:

```go
package modedit_test

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/JtheGunner/omnishell/internal/config"
)

func TestEnableThenDisableUpdatesConfig(t *testing.T) {
	ed, cfgPath := newEditor(t, true, "bash")

	if err := ed.Enable("fzf"); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	c, err := config.Load(cfgPath)
	if err != nil || !c.Modules["fzf"].Enabled {
		t.Fatalf("fzf should be enabled: %+v err=%v", c.Modules, err)
	}

	if err := ed.Disable("fzf"); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	c, err = config.Load(cfgPath)
	if err != nil || c.Modules["fzf"].Enabled {
		t.Fatalf("fzf should be disabled: %+v err=%v", c.Modules, err)
	}
}

func TestEnablePreservesComments(t *testing.T) {
	ed, cfgPath := newEditor(t, true, "bash")

	if err := ed.Enable("fzf"); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	src, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(src), "# Edit this file by hand") {
		t.Fatalf("header comment lost:\n%s", src)
	}
}

func TestEnableUnknownModuleIsConfigError(t *testing.T) {
	ed, _ := newEditor(t, true, "bash")

	err := ed.Enable("nope")
	var cfgErr config.Error
	if !errors.As(err, &cfgErr) || !strings.Contains(cfgErr.Msg, `unknown module "nope"`) {
		t.Fatalf("err = %v, want config.Error naming the unknown module", err)
	}
}

func TestEnableRejectsModuleIncompatibleWithManagedShells(t *testing.T) {
	ed, cfgPath := newEditor(t, true, "bash") // only bash is present

	err := ed.Enable("zshonly")
	var cfgErr config.Error
	if !errors.As(err, &cfgErr) {
		t.Fatalf("err = %v, want config.Error", err)
	}
	for _, want := range []string{"zshonly", "zsh", "none of your managed shells"} {
		if !strings.Contains(cfgErr.Msg, want) {
			t.Fatalf("message %q should contain %q", cfgErr.Msg, want)
		}
	}
	c, err := config.Load(cfgPath)
	if err != nil || c.Modules["zshonly"].Enabled {
		t.Fatalf("zshonly must not be enabled: %+v err=%v", c.Modules, err)
	}
}

func TestEnableAllowsModuleWhenItsShellIsManaged(t *testing.T) {
	ed, cfgPath := newEditor(t, true, "zsh")

	if err := ed.Enable("zshonly"); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	c, err := config.Load(cfgPath)
	if err != nil || !c.Modules["zshonly"].Enabled {
		t.Fatalf("zshonly should be enabled: %+v err=%v", c.Modules, err)
	}
}

// Only enabling is shell-checked: a user must always be able to switch an
// incompatible module off.
func TestDisableSkipsShellCompatibilityCheck(t *testing.T) {
	ed, cfgPath := newEditor(t, true, "bash")

	if err := ed.Disable("zshonly"); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	c, err := config.Load(cfgPath)
	if err != nil || c.Modules["zshonly"].Enabled {
		t.Fatalf("zshonly should be disabled: %+v err=%v", c.Modules, err)
	}
}

func TestEnableAndDisableWithoutConfigReturnNotFoundAndCreateNothing(t *testing.T) {
	ed, cfgPath := newEditor(t, false, "bash")

	for name, fn := range map[string]func(string) error{"Enable": ed.Enable, "Disable": ed.Disable} {
		if err := fn("fzf"); !errors.Is(err, config.ErrNotFound) {
			t.Fatalf("%s err = %v, want ErrNotFound", name, err)
		}
	}
	if _, err := os.Stat(cfgPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("config.toml must not be created, stat err = %v", err)
	}
}
```

- [ ] **Step 4: Run the tests to verify they fail**

Run: `go test ./internal/modedit/... -count=1`
Expected: FAIL to compile, with `undefined: modedit.Editor` (and `package ... modedit` not found for non-test files).

- [ ] **Step 5: Implement `Editor`, `Enable`, `Disable`**

`internal/modedit/editor.go`:

```go
// Package modedit holds the rules for changing which modules are enabled and
// how they are configured, plus the per-module view the `list` command and the
// TUI show. Both front ends call it, so "same rules as enable/set" is a
// property of the code, not a convention.
//
// modedit only ever writes config.toml, and only through the comment-preserving
// atomic edits in internal/config.
package modedit

import (
	"fmt"
	"sort"
	"strings"

	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/JtheGunner/omnishell/internal/engine"
	"github.com/JtheGunner/omnishell/internal/module"
)

// Editor edits the config.toml at CfgPath, validating against the modules in
// Engine.Registry and the shells Engine.Platform reports.
type Editor struct {
	Engine  engine.Engine
	CfgPath string
}

// Enable sets modules.<id>.enabled = true. It refuses a module that none of the
// currently managed shells can run.
func (ed Editor) Enable(id string) error { return ed.setEnabled(id, true) }

// Disable sets modules.<id>.enabled = false. It does not check shell
// compatibility, so an incompatible module can always be switched off.
func (ed Editor) Disable(id string) error { return ed.setEnabled(id, false) }

// setEnabled requires an existing config.toml (a missing one wraps
// config.ErrNotFound), validates the module id, and flips modules.<id>.enabled.
func (ed Editor) setEnabled(id string, enabled bool) error {
	cfg, err := config.Load(ed.CfgPath)
	if err != nil {
		return err
	}
	mod, ok := ed.Engine.Registry.Get(id)
	if !ok {
		return config.Error{Path: ed.CfgPath, Msg: fmt.Sprintf("unknown module %q", id)}
	}
	if enabled {
		if err := ed.checkShellCompatible(cfg, mod); err != nil {
			return err
		}
	}
	return config.SetEnabled(ed.CfgPath, id, enabled)
}

// checkShellCompatible refuses to enable a module that none of the shells
// omnishell currently manages on this host could ever run — e.g. a zsh-only
// module (autosuggestions, syntax-highlighting) on a bash-only system.
// Enabling it anyway would leave it permanently degraded ("no snippet for
// any managed shell") with no way to notice besides `doctor`/`list`.
func (ed Editor) checkShellCompatible(cfg config.Config, mod module.Module) error {
	managed := engine.ManagedShells(cfg, ed.Engine.Platform)
	for _, sh := range managed {
		if contains(mod.Manifest.Shells, sh) {
			return nil
		}
	}
	return config.Error{Path: ed.CfgPath, Msg: fmt.Sprintf(
		"module %q only supports %s, but none of your managed shells (%s) do — install one of those shells first",
		mod.Manifest.Module.ID, strings.Join(mod.Manifest.Shells, ", "), joinOrNone(managed),
	)}
}

func contains(ss []string, v string) bool {
	for _, s := range ss {
		if s == v {
			return true
		}
	}
	return false
}

func joinOrNone(ss []string) string {
	if len(ss) == 0 {
		return "none"
	}
	return strings.Join(ss, ", ")
}

// optionKeys returns the option keys of a manifest, sorted, for error messages.
func optionKeys(opts map[string]module.OptionSchema) []string {
	keys := make([]string, 0, len(opts))
	for k := range opts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
```

`optionKeys` is unused until Task 2; golangci-lint's `unused` check would flag it if you lint between tasks, so if you lint here, move it to Task 2's file edit instead. Tests and `go vet` do not care.

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/modedit/... -race -count=1 -v`
Expected: PASS for all six tests.

- [ ] **Step 7: Commit**

```bash
git add internal/modedit
git commit -m "refactor: add modedit with the enable/disable rules"
```

---

### Task 2: `Editor.SetOption`

**Files:**
- Modify: `internal/modedit/editor.go`
- Modify: `internal/modedit/editor_test.go`

**Interfaces:**
- Consumes: `Editor`, `optionKeys` (Task 1); `module.OptionSchema.ParseValue`, `config.SetOption`.
- Produces: `func (ed Editor) SetOption(id, key, raw string) error`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/modedit/editor_test.go` (add `"bytes"` to the import block):

```go
func TestSetOptionWritesTypedValues(t *testing.T) {
	ed, cfgPath := newEditor(t, true, "bash")

	if err := ed.SetOption("fzf", "ctrl_r", "false"); err != nil {
		t.Fatalf("SetOption bool: %v", err)
	}
	if err := ed.SetOption("fzf", "theme", "light"); err != nil {
		t.Fatalf("SetOption enum: %v", err)
	}
	c, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	opts := c.Modules["fzf"].Options
	if opts["ctrl_r"] != false || opts["theme"] != "light" {
		t.Fatalf("options = %#v", opts)
	}
	if c.Modules["fzf"].Enabled {
		t.Fatal("SetOption must never change enablement")
	}
}

func TestSetOptionUnknownKeyListsValidKeysSorted(t *testing.T) {
	ed, _ := newEditor(t, true, "bash")

	err := ed.SetOption("fzf", "bogus", "x")
	var cfgErr config.Error
	if !errors.As(err, &cfgErr) {
		t.Fatalf("err = %v, want config.Error", err)
	}
	want := `unknown option "bogus" for module "fzf" (valid: ctrl_r, prefix, theme)`
	if cfgErr.Msg != want {
		t.Fatalf("message = %q, want %q", cfgErr.Msg, want)
	}
}

func TestSetOptionUnknownModuleIsConfigError(t *testing.T) {
	ed, _ := newEditor(t, true, "bash")

	err := ed.SetOption("nope", "ctrl_r", "true")
	var cfgErr config.Error
	if !errors.As(err, &cfgErr) || !strings.Contains(cfgErr.Msg, `unknown module "nope"`) {
		t.Fatalf("err = %v, want config.Error naming the unknown module", err)
	}
}

// A rejected value must not touch the file at all.
func TestSetOptionRejectedValueLeavesConfigByteIdentical(t *testing.T) {
	ed, cfgPath := newEditor(t, true, "bash")
	before, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}

	cases := []struct{ key, raw, wantMsg string }{
		{"theme", "purple", "invalid value for fzf.theme"},
		{"prefix", "ABC", "invalid value for fzf.prefix"},
		{"ctrl_r", "maybe", "invalid value for fzf.ctrl_r"},
	}
	for _, tc := range cases {
		err := ed.SetOption("fzf", tc.key, tc.raw)
		var cfgErr config.Error
		if !errors.As(err, &cfgErr) || !strings.Contains(cfgErr.Msg, tc.wantMsg) {
			t.Fatalf("%s=%s: err = %v, want config.Error containing %q", tc.key, tc.raw, err, tc.wantMsg)
		}
	}

	after, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("config changed after rejected values:\n%s", after)
	}
}

func TestSetOptionWithoutConfigReturnsNotFoundAndCreatesNothing(t *testing.T) {
	ed, cfgPath := newEditor(t, false, "bash")

	if err := ed.SetOption("fzf", "ctrl_r", "true"); !errors.Is(err, config.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if _, err := os.Stat(cfgPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("config.toml must not be created, stat err = %v", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/modedit/... -count=1`
Expected: FAIL to compile, `ed.SetOption undefined`.

- [ ] **Step 3: Implement `SetOption`**

Add to `internal/modedit/editor.go`, after `Disable`:

```go
// SetOption sets modules.<id>.options.<key> from the raw string form a user
// types. It requires an existing config.toml, validates the module and key,
// and parses raw against the option's schema before anything is written, so a
// rejected value leaves the file untouched. It never changes enablement.
func (ed Editor) SetOption(id, key, raw string) error {
	if _, err := config.Load(ed.CfgPath); err != nil {
		return err
	}
	mod, ok := ed.Engine.Registry.Get(id)
	if !ok {
		return config.Error{Path: ed.CfgPath, Msg: fmt.Sprintf("unknown module %q", id)}
	}
	schema, ok := mod.Manifest.Options[key]
	if !ok {
		return config.Error{
			Path: ed.CfgPath,
			Msg: fmt.Sprintf("unknown option %q for module %q (valid: %s)",
				key, id, strings.Join(optionKeys(mod.Manifest.Options), ", ")),
		}
	}
	typed, err := schema.ParseValue(raw)
	if err != nil {
		return config.Error{
			Path: ed.CfgPath,
			Msg:  fmt.Sprintf("invalid value for %s.%s: %v", id, key, err),
		}
	}
	return config.SetOption(ed.CfgPath, id, key, typed)
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/modedit/... -race -count=1 -v`
Expected: PASS for all tests (Task 1 and Task 2).

- [ ] **Step 5: Commit**

```bash
git add internal/modedit
git commit -m "refactor: add the set-option rules to modedit"
```

---

### Task 3: `Editor.Views`

**Files:**
- Create: `internal/modedit/views.go`
- Create: `internal/modedit/views_test.go`

**Interfaces:**
- Consumes: `Editor` (Task 1); `config.Load`, `config.Default`, `module.Registry.All/Overrides`, `module.SourceUser`, `pkgmgr.Manager.IsInstalled`.
- Produces:
  ```go
  type Status string       // StatusEnabled "enabled", StatusDisabled "disabled", StatusUnknown "—"
  type PackageState string // PackagesOK "ok", PackagesMissing "missing", PackagesNA "n/a"
  type Origin string       // OriginBuiltin "builtin", OriginUser "user", OriginUserOverride "user*"
  type ModuleView struct {
      ID, Name, Description, Homepage string
      Status      Status
      Packages    PackageState
      Platforms   []string
      Shells      []string
      Origin      Origin
      OptionCount int
  }
  func (ed Editor) Views() ([]ModuleView, error) // sorted by ID, never nil on success
  ```

- [ ] **Step 1: Write the failing tests**

`internal/modedit/views_test.go`:

```go
package modedit_test

import (
	"errors"
	"os"
	"reflect"
	"testing"
	"testing/fstest"

	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/JtheGunner/omnishell/internal/modedit"
	"github.com/JtheGunner/omnishell/internal/module"
	"github.com/JtheGunner/omnishell/internal/pkgmgr"
)

func viewByID(t *testing.T, views []modedit.ModuleView, id string) modedit.ModuleView {
	t.Helper()
	for _, v := range views {
		if v.ID == id {
			return v
		}
	}
	t.Fatalf("no view for %q in %+v", id, views)
	return modedit.ModuleView{}
}

func TestViewsAreSortedAndCarryManifestFields(t *testing.T) {
	ed, _ := newEditor(t, true, "bash")

	views, err := ed.Views()
	if err != nil {
		t.Fatalf("Views: %v", err)
	}
	ids := make([]string, len(views))
	for i, v := range views {
		ids[i] = v.ID
	}
	if want := []string{"fzf", "plain", "zshonly"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("ids = %v, want %v", ids, want)
	}

	fzf := viewByID(t, views, "fzf")
	if fzf.Name != "FZF Fuzzy Finder" || fzf.Description != "Ctrl+R history search" ||
		fzf.Homepage != "https://github.com/junegunn/fzf" {
		t.Fatalf("fzf text fields = %+v", fzf)
	}
	if !reflect.DeepEqual(fzf.Platforms, []string{"macos", "linux"}) ||
		!reflect.DeepEqual(fzf.Shells, []string{"zsh", "bash"}) {
		t.Fatalf("fzf platforms/shells = %v / %v", fzf.Platforms, fzf.Shells)
	}
	if fzf.OptionCount != 3 {
		t.Fatalf("fzf OptionCount = %d, want 3", fzf.OptionCount)
	}
	if fzf.Origin != modedit.OriginUser {
		t.Fatalf("fzf Origin = %q, want %q", fzf.Origin, modedit.OriginUser)
	}
}

func TestViewsStatusFollowsConfig(t *testing.T) {
	ed, _ := newEditor(t, true, "bash")
	if err := ed.Enable("fzf"); err != nil {
		t.Fatalf("Enable: %v", err)
	}

	views, err := ed.Views()
	if err != nil {
		t.Fatalf("Views: %v", err)
	}
	if got := viewByID(t, views, "fzf").Status; got != modedit.StatusEnabled {
		t.Fatalf("fzf status = %q, want enabled", got)
	}
	if got := viewByID(t, views, "plain").Status; got != modedit.StatusDisabled {
		t.Fatalf("plain status = %q, want disabled", got)
	}
}

// `list` works before `init`: a missing config is "no config", not an error.
func TestViewsWithoutConfigReportUnknownStatus(t *testing.T) {
	ed, _ := newEditor(t, false, "bash")

	views, err := ed.Views()
	if err != nil {
		t.Fatalf("Views: %v", err)
	}
	if len(views) == 0 {
		t.Fatal("expected the fixture modules")
	}
	for _, v := range views {
		if v.Status != modedit.StatusUnknown {
			t.Fatalf("%s status = %q, want %q", v.ID, v.Status, modedit.StatusUnknown)
		}
	}
}

// A broken config must surface as an error, never as an all-disabled list.
func TestViewsMalformedConfigIsAnError(t *testing.T) {
	ed, cfgPath := newEditor(t, true, "bash")
	if err := os.WriteFile(cfgPath, []byte("not = [toml"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	views, err := ed.Views()
	if err == nil || errors.Is(err, config.ErrNotFound) {
		t.Fatalf("err = %v, want a parse error that is not ErrNotFound", err)
	}
	if views != nil {
		t.Fatalf("views = %+v, want nil on error", views)
	}
}

func TestViewsPackageState(t *testing.T) {
	ed, _ := newEditor(t, true, "bash")
	mgr := ed.Engine.Manager.(*pkgmgr.MockManager)

	views, err := ed.Views()
	if err != nil {
		t.Fatalf("Views: %v", err)
	}
	if got := viewByID(t, views, "fzf").Packages; got != modedit.PackagesMissing {
		t.Fatalf("fzf packages = %q, want missing", got)
	}
	if got := viewByID(t, views, "plain").Packages; got != modedit.PackagesNA {
		t.Fatalf("plain packages = %q, want n/a", got)
	}

	mgr.Installed["fzf"] = true
	views, err = ed.Views()
	if err != nil {
		t.Fatalf("Views: %v", err)
	}
	if got := viewByID(t, views, "fzf").Packages; got != modedit.PackagesOK {
		t.Fatalf("fzf packages = %q, want ok", got)
	}
}

func TestViewsPackagesAreNAWithoutPackageManager(t *testing.T) {
	ed, _ := newEditor(t, true, "bash")
	ed.Engine.ManagerOK = false

	views, err := ed.Views()
	if err != nil {
		t.Fatalf("Views: %v", err)
	}
	if got := viewByID(t, views, "fzf").Packages; got != modedit.PackagesNA {
		t.Fatalf("fzf packages = %q, want n/a", got)
	}
}

func TestViewsMarksBuiltinOverriddenByUserModule(t *testing.T) {
	manifest, err := os.ReadFile("testdata/modules/fzf/manifest.toml")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	builtin := fstest.MapFS{"fzf/manifest.toml": {Data: manifest}, "zzz/manifest.toml": {Data: []byte(`
platforms = ["macos", "linux"]
shells    = ["zsh", "bash"]
requires  = []
after     = []

[module]
id          = "zzz"
name        = "Built-in only"
description = "Only in the built-in set"
version     = "1.0.0"
schema      = 1
`)}}
	reg, err := module.LoadRegistry(builtin, "testdata/modules")
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	ed, _ := newEditor(t, true, "bash")
	ed.Engine.Registry = reg

	views, err := ed.Views()
	if err != nil {
		t.Fatalf("Views: %v", err)
	}
	if got := viewByID(t, views, "fzf").Origin; got != modedit.OriginUserOverride {
		t.Fatalf("fzf origin = %q, want %q", got, modedit.OriginUserOverride)
	}
	if got := viewByID(t, views, "zzz").Origin; got != modedit.OriginBuiltin {
		t.Fatalf("zzz origin = %q, want %q", got, modedit.OriginBuiltin)
	}
	if got := viewByID(t, views, "plain").Origin; got != modedit.OriginUser {
		t.Fatalf("plain origin = %q, want %q", got, modedit.OriginUser)
	}
}

func TestViewsEmptyRegistryIsEmptyNotNil(t *testing.T) {
	reg, err := module.LoadRegistry(nil, "")
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	ed, _ := newEditor(t, true, "bash")
	ed.Engine.Registry = reg

	views, err := ed.Views()
	if err != nil {
		t.Fatalf("Views: %v", err)
	}
	if views == nil || len(views) != 0 {
		t.Fatalf("views = %#v, want empty non-nil slice", views)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/modedit/... -count=1`
Expected: FAIL to compile, `undefined: modedit.ModuleView` (and the related constants).

- [ ] **Step 3: Implement `Views`**

`internal/modedit/views.go`:

```go
package modedit

import (
	"errors"
	"sort"

	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/JtheGunner/omnishell/internal/module"
)

// Status is a module's state in config.toml.
type Status string

const (
	StatusEnabled  Status = "enabled"
	StatusDisabled Status = "disabled"
	// StatusUnknown means there is no config.toml yet, so nothing is enabled or
	// disabled.
	StatusUnknown Status = "—"
)

// PackageState reports whether a module's packages are installed.
type PackageState string

const (
	// PackagesOK: every package for the detected manager is installed.
	PackagesOK PackageState = "ok"
	// PackagesMissing: at least one is not installed (or could not be checked).
	PackagesMissing PackageState = "missing"
	// PackagesNA: the module needs no package, or none could be checked
	// (no package manager detected, or fallback-only).
	PackagesNA PackageState = "n/a"
)

// Origin says where a module comes from.
type Origin string

const (
	OriginBuiltin Origin = "builtin"
	OriginUser    Origin = "user"
	// OriginUserOverride is a user module that replaces a built-in of the same id.
	OriginUserOverride Origin = "user*"
)

// ModuleView is everything a front end shows about one module.
type ModuleView struct {
	ID          string
	Name        string
	Description string
	Homepage    string
	Status      Status
	Packages    PackageState
	Platforms   []string
	Shells      []string
	Origin      Origin
	OptionCount int
}

// Views returns one ModuleView per registry module, sorted by ID. A missing
// config.toml is not an error: every Status is then StatusUnknown. Any other
// config error is returned and no views are.
func (ed Editor) Views() ([]ModuleView, error) {
	cfg, cfgMissing, err := ed.loadConfigAllowMissing()
	if err != nil {
		return nil, err
	}

	overrides := map[string]bool{}
	for _, id := range ed.Engine.Registry.Overrides() {
		overrides[id] = true
	}

	views := make([]ModuleView, 0)
	for _, m := range ed.Engine.Registry.All() {
		mf := m.Manifest
		id := mf.Module.ID

		status := StatusUnknown
		if !cfgMissing {
			status = StatusDisabled
			if mc, ok := cfg.Modules[id]; ok && mc.Enabled {
				status = StatusEnabled
			}
		}

		origin := OriginBuiltin
		if m.Source == module.SourceUser {
			origin = OriginUser
		}
		if overrides[id] {
			origin = OriginUserOverride
		}

		views = append(views, ModuleView{
			ID:          id,
			Name:        mf.Module.Name,
			Description: mf.Module.Description,
			Homepage:    mf.Module.Homepage,
			Status:      status,
			Packages:    ed.packageState(mf),
			Platforms:   mf.Platforms,
			Shells:      mf.Shells,
			Origin:      origin,
			OptionCount: len(mf.Options),
		})
	}
	sort.Slice(views, func(i, j int) bool { return views[i].ID < views[j].ID })
	return views, nil
}

// loadConfigAllowMissing loads config.toml, treating a missing file as "no
// config" rather than an error.
func (ed Editor) loadConfigAllowMissing() (config.Config, bool, error) {
	cfg, err := config.Load(ed.CfgPath)
	if err == nil {
		return cfg, false, nil
	}
	if errors.Is(err, config.ErrNotFound) {
		return config.Default(), true, nil
	}
	return config.Config{}, false, err
}

// packageState reports ok/missing/n/a for a module's packages under the
// detected manager. With no manager detected, or when the module declares no
// packages for that manager, it is n/a; fallback-only modules also report n/a
// here (fallback satisfaction needs the apply-time vendor context).
func (ed Editor) packageState(mf module.Manifest) PackageState {
	e := ed.Engine
	var pkgs []string
	if e.ManagerOK {
		pkgs = mf.Packages.ForManager(e.Manager.Name())
	}
	if !e.ManagerOK || len(pkgs) == 0 {
		return PackagesNA
	}
	for _, p := range pkgs {
		installed, err := e.Manager.IsInstalled(p)
		if err != nil || !installed {
			return PackagesMissing
		}
	}
	return PackagesOK
}
```

The original `packageStatus` had a redundant first branch (`len(pkgs) == 0 && !hasFallback`) that returned the same `"n/a"` as the second; the version above is the equivalent single check. The `Platforms` and `Shells` slices alias the manifest's slices; callers treat them as read-only.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/modedit/... -race -count=1 -v`
Expected: PASS for every test in the package.

- [ ] **Step 5: Commit**

```bash
git add internal/modedit
git commit -m "refactor: add the module view builder to modedit"
```

---

### Task 4: Route `enable`, `disable` and `set` through `modedit`

**Files:**
- Modify: `internal/cli/enable.go`
- Modify: `internal/cli/set.go`

**Interfaces:**
- Consumes: `modedit.Editor`, `Enable`, `Disable`, `SetOption` (Tasks 1 and 2).
- Produces: `func hintIfUninitialised(cmd *cobra.Command, err error) error` in `internal/cli/enable.go`, replacing `loadConfigOrHint`.

No new test is written: the existing `enable_test.go` and `set_test.go` cases are the safety net for this behaviour-preserving change.

- [ ] **Step 1: Run the CLI tests as a baseline**

Run: `go test ./internal/cli/... -race -count=1`
Expected: PASS.

- [ ] **Step 2: Replace `internal/cli/enable.go`**

```go
package cli

import (
	"errors"
	"fmt"

	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/JtheGunner/omnishell/internal/modedit"
	"github.com/spf13/cobra"
)

func newEnableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "enable <module>",
		Short: "Enable a module in the config",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return setModuleEnabled(cmd, args[0], true)
		},
	}
}

func newDisableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "disable <module>",
		Short: "Disable a module in the config",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return setModuleEnabled(cmd, args[0], false)
		},
	}
}

// setModuleEnabled is the shared body of `enable` and `disable`: it builds the
// engine, hands the change to modedit (which owns the rules), and prints a
// confirmation. Every error path classifies to exit code 2 and prints nothing
// to stdout.
func setModuleEnabled(cmd *cobra.Command, id string, enabled bool) error {
	e, cfgPath, _, err := buildEngine(cmd.OutOrStdout(), cmd.ErrOrStderr())
	if err != nil {
		return err
	}
	editor := modedit.Editor{Engine: e, CfgPath: cfgPath}
	if enabled {
		err = editor.Enable(id)
	} else {
		err = editor.Disable(id)
	}
	if err != nil {
		return hintIfUninitialised(cmd, err)
	}

	verb := "enabled"
	if !enabled {
		verb = "disabled"
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s %s — run 'omnishell apply' to apply\n", verb, id)
	return nil
}

// hintIfUninitialised writes the `run 'omnishell init' first` hint to stderr
// when err says config.toml is missing, and returns err unchanged (ErrNotFound
// classifies to exit code 2).
func hintIfUninitialised(cmd *cobra.Command, err error) error {
	if errors.Is(err, config.ErrNotFound) {
		_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "run 'omnishell init' first")
	}
	return err
}
```

- [ ] **Step 3: Replace `internal/cli/set.go`**

```go
package cli

import (
	"fmt"
	"strings"

	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/JtheGunner/omnishell/internal/modedit"
	"github.com/spf13/cobra"
)

func newSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <module>.<key> <value>",
		Short: "Set a module option in the config",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, cfgPath, _, err := buildEngine(cmd.OutOrStdout(), cmd.ErrOrStderr())
			if err != nil {
				return err
			}

			dot := strings.LastIndex(args[0], ".")
			if dot < 0 {
				return config.Error{Path: cfgPath, Msg: "expected <module>.<key>"}
			}
			id, key := args[0][:dot], args[0][dot+1:]

			editor := modedit.Editor{Engine: e, CfgPath: cfgPath}
			if err := editor.SetOption(id, key, args[1]); err != nil {
				return hintIfUninitialised(cmd, err)
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "set %s.%s = %s\n", id, key, args[1])
			return nil
		},
	}
}
```

- [ ] **Step 4: Build, vet and run the CLI tests**

Run: `go build ./... && go vet ./internal/cli/... ./internal/modedit/... && go test ./internal/cli/... ./internal/modedit/... -race -count=1`
Expected: build and vet clean, tests PASS. If `go vet` or the build reports `contains`, `joinOrNone`, `optionKeys` or `loadConfigOrHint` as still referenced from another `internal/cli` file, that file needs the same import swap; the grep in the next step shows none do.

- [ ] **Step 5: Confirm nothing else referenced the removed helpers**

Run: `grep -rn "loadConfigOrHint\|checkShellCompatible\|joinOrNone\|optionKeys" internal/cli/`
Expected: no output.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/enable.go internal/cli/set.go
git commit -m "refactor: route enable, disable and set through modedit"
```

---

### Task 5: Route `list` through `modedit.Views`

**Files:**
- Modify: `internal/cli/list.go`

**Interfaces:**
- Consumes: `modedit.Editor.Views`, `modedit.ModuleView` (Task 3).
- Produces: nothing new; `listRow` and the table and JSON output stay identical.

- [ ] **Step 1: Run the list tests as a baseline**

Run: `go test ./internal/cli/... -run List -race -count=1 -v`
Expected: PASS.

- [ ] **Step 2: Replace the `RunE` body, the import block, and drop the moved helpers in `internal/cli/list.go`**

The import block becomes:

```go
import (
	"encoding/json"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/JtheGunner/omnishell/internal/modedit"
	"github.com/spf13/cobra"
)
```

`listRow` and `newListCmd`'s `Use`/`Short`/`Long`/`Args` and the `--json` flag registration stay exactly as they are. The `RunE` body becomes:

```go
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			e, cfgPath, _, err := buildEngine(out, cmd.ErrOrStderr())
			if err != nil {
				return err
			}

			views, err := modedit.Editor{Engine: e, CfgPath: cfgPath}.Views()
			if err != nil {
				return err
			}

			rows := make([]listRow, 0, len(views))
			for _, v := range views {
				rows = append(rows, listRow{
					Module:      v.ID,
					Status:      string(v.Status),
					Description: v.Description,
					Homepage:    v.Homepage,
					Packages:    string(v.Packages),
					Platforms:   strings.Join(v.Platforms, ","),
					Shells:      strings.Join(v.Shells, ","),
					Src:         string(v.Origin),
				})
			}

			if asJSON {
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(rows)
			}

			tw := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
			_, _ = fmt.Fprintln(tw, "MODULE\tSTATUS\tPACKAGES\tPLATFORMS\tSHELLS\tSRC\tDESCRIPTION")
			for _, r := range rows {
				_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
					r.Module, r.Status, r.Packages, r.Platforms, r.Shells, r.Src, r.Description)
			}
			return tw.Flush()
		},
```

Delete `loadConfigForList` and `packageStatus` from `list.go`; both now live in `modedit/views.go`. Keep the `PACKAGES is one of:` text in the command's `Long` help unchanged.

- [ ] **Step 3: Build, vet and run the full package tests**

Run: `go build ./... && go vet ./... && go test ./internal/cli/... ./internal/modedit/... -race -count=1`
Expected: clean, PASS. `list --json` for an empty registry still prints `[]` because `rows` is created with `make([]listRow, 0, len(views))`.

- [ ] **Step 4: Confirm the moved helpers are gone**

Run: `grep -rn "loadConfigForList\|packageStatus" internal/`
Expected: no output.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/list.go
git commit -m "refactor: route list through modedit.Views"
```

---

### Task 6: Architecture note and full verification

**Files:**
- Modify: `CLAUDE.md` (architecture list)

**Interfaces:**
- Consumes: everything above.
- Produces: nothing code-level.

- [ ] **Step 1: Add `modedit` to the architecture list in `CLAUDE.md`**

In the `**internal/config**` entry (item 5), append this sentence at the end of its paragraph:

```text
   The rules shared by `enable`/`disable`/`set`/`list` (unknown-id, shell
   compatibility, option validation, the per-module view) live in
   `internal/modedit`; the CLI commands are thin callers of it.
```

This is a delta the file did not carry before, so it belongs in `CLAUDE.md` (agent-facing); the README is user-facing and unchanged because no behaviour changes, and no `CHANGELOG.md` entry is needed for an internal refactor.

- [ ] **Step 2: Run the full verification**

Run: `make vet && make test`
Expected: both clean; every package PASS.

Run: `make lint`
Expected: no findings. If `golangci-lint` is not installed locally, say so in the hand-off instead of skipping silently; CI runs it.

- [ ] **Step 3: Confirm the diff is behaviour-preserving**

Run: `git diff main --stat -- go.mod go.sum internal/cli/*_test.go`
Expected: no output (no dependency changes, no edits to existing CLI tests).

Run: `git diff main --stat`
Expected: only `internal/modedit/**` added, `internal/cli/{enable,set,list}.go` and `CLAUDE.md` modified, plus the spec and plan docs.

`test/e2e/run.sh` is not run here: it installs real packages and is only for a disposable environment; CI runs it on every push.

- [ ] **Step 4: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: note internal/modedit in the architecture overview"
```
