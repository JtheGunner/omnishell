# Toggle Modules in the TUI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** In `omnishell tui`, Space enables or disables the selected module in `config.toml`; modules that cannot run on this host are dimmed with the reason shown; the header counts the changes since start; a rejected toggle shows its reason on a status line and leaves the checkbox alone.

**Architecture:** `modedit.ModuleView` gains an `Unavailable` reason computed by the same rules as `Enable`. The TUI's `Backend` gains `Enable` and `Disable`. `Update` stays free of I/O: Space returns a `tea.Cmd` that writes through the backend and re-reads the modules, and the result comes back as a message that either swaps in the fresh list or sets the status line.

**Tech Stack:** Go 1.26, the dependencies already on `main` (Bubble Tea v2, Lip Gloss v2, x/ansi, x/term). No new dependencies.

**Spec:** `docs/superpowers/specs/2026-10-07-interactive-tui-design.md` (Screens → Browser, Failure behavior, Testing; Sub-tasks item 3). YouTrack: OMNIS-32.

## Global Constraints

- `go.mod` and `go.sum` do not change.
- `internal/tui` imports neither `internal/cli` nor `internal/engine`; it writes nothing itself. `config.toml` is written only through `modedit.Enable` / `Disable` (atomic, comment-preserving).
- `Update` does no I/O; the write runs inside a returned `tea.Cmd`.
- Every piece of backend text that reaches the screen (module fields, error messages) goes through `sanitize` first.
- `omnishell enable` / `disable` / `list` behave exactly as before (same output, exit codes, JSON).
- Existing tests are not deleted; the only edits to them are the mechanical `New(` → `newTestModel(` rename and the `fakeBackend` type moving to `helpers_test.go`.
- Every commit builds and passes `go test ./...`. Before the PR: `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2 run ./...` reports 0 issues (CI runs exactly this version; `golangci-lint` is not installed locally).
- Write invisible or format Unicode characters in Go sources as `\uXXXX` escapes, never as raw characters.
- All identifiers, comments and commit messages are in English; commit subjects use `<type>: <description>` with no attribution trailers.

## Rulings carried into this plan

- **"Cannot run on this host" has two parts:** the OS check (as `apply` does: it skips a module that does not support the OS) and the managed-shell check (as `Enable` does). Only the shell check rejects an enable today; `omnishell enable` does not reject an OS-mismatched module. This plan does not change `enable`: a module dimmed for its OS can still be toggled, and the status line shows whatever `modedit` decides. Making `enable` stricter would change CLI behaviour and is out of scope.
- **The change counter is net:** it counts modules whose state differs from the state at start, so toggling a module back counts as no change. This is what tells the user whether `apply` has anything to do.
- **Space only**, as the issue says; Enter keeps its filter meaning.
- **`tui.New` takes the backend:** `New(b Backend, views)`; the model needs it to build the toggle command.
- **Errors are shortened in the CLI adapter**, not in `tui`: a `config.Error` becomes its message, because the config path that prefixes every `config.Error` would push the reason off a one-line status bar.
- This closes the OMNIS-31 ruling "dim modules that cannot run on this host". The other deferred OMNIS-31 minors stay deferred.

## Review Focus

- A rejected enable (for example a zsh-only module on a bash-only host) shows the reason, leaves the checkbox and `config.toml` unchanged, and the next key press clears the message (Tasks 2, 3 and 4 tests).
- The dimming decision never disagrees with what `Enable` accepts (Task 1 test `TestViewsUnavailableMatchesWhatEnableRejectsForShells`).
- Error text with control characters or newlines cannot reach the terminal or break the layout (Task 4 test).
- Toggling while a filter is active keeps the cursor on the same module and keeps the filter (Task 3 test).
- Space while typing a filter, on an empty list, or with no match never toggles anything (Task 3 tests).

---

### Task 1: `ModuleView.Unavailable` in `modedit`

**Files:**
- Create: `internal/modedit/testdata/modules/macosonly/manifest.toml`
- Modify: `internal/modedit/editor.go`, `internal/modedit/views.go`
- Test: `internal/modedit/views_test.go`

**Interfaces:**
- Consumes: `engine.ManagedShells`, `joinOrNone`, `contains` (existing).
- Produces:
  ```go
  // new field on modedit.ModuleView
  Unavailable string // "" when the module can run on this host
  // unexported
  func sharesAny(a, b []string) bool
  func (ed Editor) unavailableReason(cfg config.Config, mf module.Manifest) string
  ```
  Reasons: `not supported on <os> (module supports <platforms, comma-separated>)` and `needs <shells joined with " or ">, but your managed shells are <list or none>`.

- [ ] **Step 1: Add the fixture module**

`internal/modedit/testdata/modules/macosonly/manifest.toml`:

```toml
platforms = ["macos"]
shells    = ["zsh", "bash"]
requires  = []
after     = []

[module]
id          = "macosonly"
name        = "macOS-only fixture"
description = "A module that only supports macOS"
version     = "1.0.0"
schema      = 1
```

- [ ] **Step 2: Write the failing tests**

Replace `internal/modedit/views_test.go` with:

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
	if want := []string{"fzf", "macosonly", "plain", "zshonly"}; !reflect.DeepEqual(ids, want) {
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

func TestViewsAvailableModulesHaveNoReason(t *testing.T) {
	ed, _ := newEditor(t, true, "bash")

	views, err := ed.Views()
	if err != nil {
		t.Fatalf("Views: %v", err)
	}
	for _, id := range []string{"fzf", "plain"} {
		if got := viewByID(t, views, id).Unavailable; got != "" {
			t.Fatalf("%s: Unavailable = %q, want empty", id, got)
		}
	}
}

func TestViewsExplainAModuleThatNeedsAnotherOS(t *testing.T) {
	ed, _ := newEditor(t, true, "bash") // the fake host is Linux

	views, err := ed.Views()
	if err != nil {
		t.Fatalf("Views: %v", err)
	}
	want := "not supported on linux (module supports macos)"
	if got := viewByID(t, views, "macosonly").Unavailable; got != want {
		t.Fatalf("Unavailable = %q, want %q", got, want)
	}
}

func TestViewsExplainAModuleThatNeedsAShellTheHostDoesNotManage(t *testing.T) {
	ed, _ := newEditor(t, true, "bash") // zsh is not present

	views, err := ed.Views()
	if err != nil {
		t.Fatalf("Views: %v", err)
	}
	want := "needs zsh, but your managed shells are bash"
	if got := viewByID(t, views, "zshonly").Unavailable; got != want {
		t.Fatalf("Unavailable = %q, want %q", got, want)
	}
}

func TestViewsReportNoManagedShellsAsNone(t *testing.T) {
	ed, _ := newEditor(t, true) // neither zsh nor bash present

	views, err := ed.Views()
	if err != nil {
		t.Fatalf("Views: %v", err)
	}
	want := "needs zsh, but your managed shells are none"
	if got := viewByID(t, views, "zshonly").Unavailable; got != want {
		t.Fatalf("Unavailable = %q, want %q", got, want)
	}
}

func TestViewsBecomeAvailableOnceTheShellIsPresent(t *testing.T) {
	ed, _ := newEditor(t, true, "zsh")

	views, err := ed.Views()
	if err != nil {
		t.Fatalf("Views: %v", err)
	}
	if got := viewByID(t, views, "zshonly").Unavailable; got != "" {
		t.Fatalf("Unavailable = %q, want empty when zsh is managed", got)
	}
}

// The reason must agree with what Enable does, so the TUI never dims a module
// that Enable would accept, or the other way round.
func TestViewsUnavailableMatchesWhatEnableRejectsForShells(t *testing.T) {
	for _, shells := range [][]string{{"bash"}, {"zsh"}, {"zsh", "bash"}, {}} {
		ed, _ := newEditor(t, true, shells...)
		views, err := ed.Views()
		if err != nil {
			t.Fatalf("shells %v: Views: %v", shells, err)
		}
		dimmed := viewByID(t, views, "zshonly").Unavailable != ""
		rejected := ed.Enable("zshonly") != nil
		if dimmed != rejected {
			t.Fatalf("shells %v: dimmed=%v but Enable rejected=%v", shells, dimmed, rejected)
		}
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/modedit/ -count=1`
Expected: FAIL to build, `viewByID(...).Unavailable undefined (type modedit.ModuleView has no field or method Unavailable)`.

- [ ] **Step 4: Implement**

Replace `internal/modedit/editor.go` with (the shell test of `checkShellCompatible` moves into the shared `sharesAny`; messages are unchanged):

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
	if sharesAny(mod.Manifest.Shells, managed) {
		return nil
	}
	return config.Error{Path: ed.CfgPath, Msg: fmt.Sprintf(
		"module %q only supports %s, but none of your managed shells (%s) do — install one of those shells first",
		mod.Manifest.Module.ID, strings.Join(mod.Manifest.Shells, ", "), joinOrNone(managed),
	)}
}

// sharesAny reports whether a and b have at least one element in common.
func sharesAny(a, b []string) bool {
	for _, v := range b {
		if contains(a, v) {
			return true
		}
	}
	return false
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

Replace `internal/modedit/views.go` with:

```go
package modedit

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/JtheGunner/omnishell/internal/engine"
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
	// Unavailable says why the module cannot run on this host (an unsupported
	// OS, or none of the managed shells can run it). Empty means it can.
	Unavailable string
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
			Unavailable: ed.unavailableReason(cfg, mf),
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

// unavailableReason explains why a module cannot run on this host, or returns
// "" when it can. The OS is checked first, as apply does (it skips a module
// that does not support the OS), then the managed shells, as Enable does.
func (ed Editor) unavailableReason(cfg config.Config, mf module.Manifest) string {
	osName := string(ed.Engine.Platform.OS)
	if !contains(mf.Platforms, osName) {
		return fmt.Sprintf("not supported on %s (module supports %s)", osName, strings.Join(mf.Platforms, ", "))
	}
	managed := engine.ManagedShells(cfg, ed.Engine.Platform)
	if !sharesAny(mf.Shells, managed) {
		return fmt.Sprintf("needs %s, but your managed shells are %s",
			strings.Join(mf.Shells, " or "), joinOrNone(managed))
	}
	return ""
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `gofmt -l internal/modedit && go vet ./internal/modedit/ && go test ./internal/modedit/ ./internal/cli/ -race -count=1`
Expected: `gofmt -l` prints nothing; both packages PASS (26 tests in `modedit`; the CLI tests prove `enable` / `list` did not change).

- [ ] **Step 6: Commit**

```bash
git add internal/modedit
git commit -m "feat: report why a module cannot run on this host in modedit views"
```

---

### Task 2: `Backend` gains `Enable` and `Disable`, and the CLI adapter implements them

**Files:**
- Modify: `internal/tui/backend.go`, `internal/cli/tui.go`
- Test: `internal/tui/helpers_test.go` (adds `fakeBackend`), `internal/tui/run_test.go`, `internal/cli/tui_test.go`

**Interfaces:**
- Consumes: `modedit.Editor.Enable` / `Disable` / `Views` (Task 1 and earlier).
- Produces:
  ```go
  type Backend interface {
      Modules() ([]modedit.ModuleView, error)
      Enable(id string) error
      Disable(id string) error
  }
  // test double in package tui:
  type fakeBackend struct { views []modedit.ModuleView; modulesErr, toggleErr error; calls []string }
  ```

- [ ] **Step 1: Write the failing tests**

In `internal/tui/helpers_test.go`, add the `fakeBackend` type directly above the `// key builds the key press` comment:

```go
// fakeBackend is an in-memory Backend that records every write. Enable and
// Disable flip the stored status, as the real config.toml would.
type fakeBackend struct {
	views      []modedit.ModuleView
	modulesErr error    // returned by Modules
	toggleErr  error    // returned by Enable and Disable, before anything changes
	calls      []string // "enable fzf", "disable fzf", in order
}

func (f *fakeBackend) Modules() ([]modedit.ModuleView, error) { return f.views, f.modulesErr }
func (f *fakeBackend) Enable(id string) error                 { return f.write(id, "enable", modedit.StatusEnabled) }
func (f *fakeBackend) Disable(id string) error                { return f.write(id, "disable", modedit.StatusDisabled) }

func (f *fakeBackend) write(id, verb string, status modedit.Status) error {
	f.calls = append(f.calls, verb+" "+id)
	if f.toggleErr != nil {
		return f.toggleErr
	}
	for i := range f.views {
		if f.views[i].ID == id {
			f.views[i].Status = status
		}
	}
	return nil
}
```

Replace `internal/tui/run_test.go` (the fake moved to the helpers; it is now used by pointer and its error field is `modulesErr`):

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

	err := Run(&fakeBackend{modulesErr: boom}, strings.NewReader(""), &out)

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
	go func() { done <- Run(&fakeBackend{views: sampleViews()}, strings.NewReader("q"), &out) }()

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

Replace `internal/cli/tui_test.go` (the first five tests are unchanged; four tests for the backend's writes are new, and the imports gain `config`):

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
	"github.com/JtheGunner/omnishell/internal/config"
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

// backendFor starts `omnishell tui` against a fake UI and returns the backend
// the command handed over, with the module registry already built.
func backendFor(t *testing.T) tui.Backend {
	t.Helper()
	got := tuiSpy(t, true)
	var out, errb bytes.Buffer
	if code := cli.Execute([]string{"tui"}, &out, &errb); code != 0 {
		t.Fatalf("exit = %d (stderr: %s)", code, errb.String())
	}
	if *got == nil {
		t.Fatal("the UI was not started")
	}
	return **got
}

func TestTUIBackendTogglesWriteConfigTomlAndKeepItsComments(t *testing.T) {
	cfgPath := setTestInit(t)
	cli.SetLookPathForTest(bashPresentLookPath)
	t.Cleanup(func() { cli.SetLookPathForTest(nil) })
	b := backendFor(t)

	if err := b.Enable("fzf"); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	c, err := config.Load(cfgPath)
	if err != nil || !c.Modules["fzf"].Enabled {
		t.Fatalf("fzf should be enabled: %+v err=%v", c.Modules, err)
	}

	if err := b.Disable("fzf"); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	c, err = config.Load(cfgPath)
	if err != nil || c.Modules["fzf"].Enabled {
		t.Fatalf("fzf should be disabled: %+v err=%v", c.Modules, err)
	}

	src, err := os.ReadFile(cfgPath)
	if err != nil || !strings.Contains(string(src), "# Edit this file by hand") {
		t.Fatalf("the header comment must survive toggling (err=%v):\n%s", err, src)
	}
}

func TestTUIBackendRejectionIsAShortMessageWithoutTheConfigPath(t *testing.T) {
	cfgPath := setTestInit(t)
	cli.SetLookPathForTest(bashPresentLookPath) // zsh is absent, so zshonly cannot run
	t.Cleanup(func() { cli.SetLookPathForTest(nil) })
	b := backendFor(t)
	before, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}

	err = b.Enable("zshonly")

	if err == nil {
		t.Fatal("enabling a zsh-only module on a bash-only host must be rejected")
	}
	if !strings.Contains(err.Error(), `module "zshonly" only supports zsh`) {
		t.Fatalf("message = %q, want the reason", err)
	}
	if strings.Contains(err.Error(), cfgPath) {
		t.Fatalf("message %q must not start with the config path", err)
	}
	after, err := os.ReadFile(cfgPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("config.toml must be untouched after a rejection (err=%v)", err)
	}
}

func TestTUIBackendReportsModulesThatCannotRunOnThisHost(t *testing.T) {
	setTestInit(t)
	cli.SetLookPathForTest(bashPresentLookPath)
	t.Cleanup(func() { cli.SetLookPathForTest(nil) })
	b := backendFor(t)

	views, err := b.Modules()
	if err != nil {
		t.Fatalf("Modules: %v", err)
	}
	reasons := map[string]string{}
	for _, v := range views {
		reasons[v.ID] = v.Unavailable
	}
	if !strings.Contains(reasons["zshonly"], "needs zsh") {
		t.Fatalf("zshonly reason = %q, want it to explain the missing zsh", reasons["zshonly"])
	}
	if reasons["fzf"] != "" {
		t.Fatalf("fzf reason = %q, want none", reasons["fzf"])
	}
}

func TestTUIBackendUnknownModuleIsAnError(t *testing.T) {
	setTestInit(t)
	b := backendFor(t)

	if err := b.Enable("no-such-module"); err == nil || !strings.Contains(err.Error(), "unknown module") {
		t.Fatalf("err = %v, want unknown module", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ ./internal/cli/ -count=1`
Expected: FAIL to build. `internal/tui` reports `*fakeBackend does not implement Backend` is not yet the problem (the interface still has one method); `internal/cli` reports `b.Enable undefined (type tui.Backend has no field or method Enable)`.

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
}
```

Replace `internal/cli/tui.go` (adds `Enable`, `Disable`, `userMessage`, and the longer help text):

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

func (b tuiBackend) Enable(id string) error  { return userMessage(b.editor.Enable(id)) }
func (b tuiBackend) Disable(id string) error { return userMessage(b.editor.Disable(id)) }

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

- [ ] **Step 4: Run the tests to verify they pass**

Run: `gofmt -l internal && go build ./... && go vet ./... && go test ./internal/tui/ ./internal/cli/ ./internal/modedit/ -race -count=1`
Expected: build and vet clean; all three packages PASS (33 tests in `tui`, 9 `TUI` tests in `cli`).

- [ ] **Step 5: Commit**

```bash
git add internal/tui internal/cli
git commit -m "feat: let the tui backend enable and disable modules"
```

---

### Task 3: The model toggles modules

**Files:**
- Create: `internal/tui/toggle.go`
- Modify: `internal/tui/model.go`, `internal/tui/run.go`
- Test: `internal/tui/helpers_test.go` (replace), `internal/tui/toggle_test.go` (new), `internal/tui/model_test.go`, `internal/tui/view_test.go`, `internal/tui/sanitize_test.go` (mechanical rename)

**Interfaces:**
- Consumes: `Backend.Enable` / `Disable` / `Modules` (Task 2); `sanitize`, `sanitizeViews` (existing).
- Produces:
  ```go
  func New(b Backend, views []modedit.ModuleView) Model            // was New(views)
  func (m Model) changes() int                                     // modules whose status differs from the start
  type toggledMsg struct { id string; views []modedit.ModuleView; err error }
  func toggleCmd(b Backend, id string, enable bool) tea.Cmd
  func (m Model) toggle() (tea.Model, tea.Cmd)
  func (m Model) applyToggled(msg toggledMsg) Model
  func (m *Model) applyFilterKeeping(id string)
  // Model gains: backend Backend; status string; initial map[string]modedit.Status
  ```
  Test helpers produced here and used by Task 4: `newBackedModel(views) (Model, *fakeBackend)`, `newTestModel(views) Model`, `settle(m, cmd) Model`, `space(m) Model`, and `key("space")`; `sampleViews()` now marks `zshonly` as unavailable.

- [ ] **Step 1: Write the failing tests**

Replace `internal/tui/helpers_test.go` with the full helper set:

```go
package tui

import (
	"flag"
	"os"
	"path/filepath"
	"slices"
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
			Origin:      modedit.OriginUser,
			Unavailable: "needs zsh, but your managed shells are bash",
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

// fakeBackend is an in-memory Backend that records every write. Enable and
// Disable flip the stored status, as the real config.toml would.
type fakeBackend struct {
	views      []modedit.ModuleView
	modulesErr error    // returned by Modules
	toggleErr  error    // returned by Enable and Disable, before anything changes
	calls      []string // "enable fzf", "disable fzf", in order
}

func (f *fakeBackend) Modules() ([]modedit.ModuleView, error) { return f.views, f.modulesErr }
func (f *fakeBackend) Enable(id string) error                 { return f.write(id, "enable", modedit.StatusEnabled) }
func (f *fakeBackend) Disable(id string) error                { return f.write(id, "disable", modedit.StatusDisabled) }

func (f *fakeBackend) write(id, verb string, status modedit.Status) error {
	f.calls = append(f.calls, verb+" "+id)
	if f.toggleErr != nil {
		return f.toggleErr
	}
	for i := range f.views {
		if f.views[i].ID == id {
			f.views[i].Status = status
		}
	}
	return nil
}

// newBackedModel returns a model over views together with the fake backend it
// writes through; the backend gets its own copy of views.
func newBackedModel(views []modedit.ModuleView) (Model, *fakeBackend) {
	b := &fakeBackend{views: slices.Clone(views)}
	return New(b, views), b
}

// newTestModel is newBackedModel for tests that never look at the backend.
func newTestModel(views []modedit.ModuleView) Model {
	m, _ := newBackedModel(views)
	return m
}

// settle runs cmd the way the Bubble Tea runtime would and feeds the message it
// produces back into m.
func settle(m Model, cmd tea.Cmd) Model {
	if cmd == nil {
		return m
	}
	next, _ := m.Update(cmd())
	return next.(Model)
}

// space presses the space bar and lets the resulting write finish.
func space(m Model) Model {
	next, cmd := m.Update(key("space"))
	return settle(next.(Model), cmd)
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
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
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

Rename the constructor in the three existing test files (every test builds its model through the new helper; `perl` is used because BSD `sed` lacks `\b`):

```bash
perl -pi -e 's/\bNew\(/newTestModel(/g' internal/tui/model_test.go internal/tui/view_test.go internal/tui/sanitize_test.go
```

Create `internal/tui/toggle_test.go`:

```go
package tui

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/JtheGunner/omnishell/internal/modedit"
)

func selectedStatus(t *testing.T, m Model) modedit.Status {
	t.Helper()
	v, ok := m.selected()
	if !ok {
		t.Fatal("nothing is selected")
	}
	return v.Status
}

func TestSpaceEnablesADisabledModule(t *testing.T) {
	m, b := newBackedModel(sampleViews())
	m = press(t, m, "down") // fzf, disabled

	m = space(m)

	if want := []string{"enable fzf"}; !reflect.DeepEqual(b.calls, want) {
		t.Fatalf("backend calls = %v, want %v", b.calls, want)
	}
	if got := selectedStatus(t, m); got != modedit.StatusEnabled {
		t.Fatalf("fzf status = %q, want enabled", got)
	}
	if m.changes() != 1 {
		t.Fatalf("changes = %d, want 1", m.changes())
	}
}

func TestSpaceDisablesAnEnabledModule(t *testing.T) {
	m, b := newBackedModel(sampleViews()) // completion is first and enabled

	m = space(m)

	if want := []string{"disable completion"}; !reflect.DeepEqual(b.calls, want) {
		t.Fatalf("backend calls = %v, want %v", b.calls, want)
	}
	if got := selectedStatus(t, m); got != modedit.StatusDisabled {
		t.Fatalf("completion status = %q, want disabled", got)
	}
}

// Update must not do I/O itself: the write belongs to the command it returns.
func TestUpdateOnlyReturnsTheWriteAsACommand(t *testing.T) {
	m, b := newBackedModel(sampleViews())

	_, cmd := m.Update(key("space"))

	if cmd == nil {
		t.Fatal("space on a module must return a command")
	}
	if len(b.calls) != 0 {
		t.Fatalf("Update wrote %v before the command ran", b.calls)
	}
}

func TestTogglingBackLeavesNoChanges(t *testing.T) {
	m := space(space(newTestModel(sampleViews())))

	if m.changes() != 0 {
		t.Fatalf("changes = %d, want 0 after toggling the same module twice", m.changes())
	}
}

func TestChangesCountEveryModuleThatDiffersFromTheStart(t *testing.T) {
	m := newTestModel(sampleViews())
	m = space(m)                   // completion off
	m = space(press(t, m, "down")) // fzf on
	m = space(press(t, m, "down")) // zshonly on

	if m.changes() != 3 {
		t.Fatalf("changes = %d, want 3", m.changes())
	}
}

func TestRejectedToggleKeepsTheCheckboxAndShowsTheMessage(t *testing.T) {
	m, b := newBackedModel(sampleViews())
	b.toggleErr = errors.New(`module "zshonly" only supports zsh, but none of your managed shells (bash) do`)
	m = press(t, m, "down", "down") // zshonly, disabled

	m = space(m)

	if got := selectedStatus(t, m); got != modedit.StatusDisabled {
		t.Fatalf("zshonly status = %q, want it unchanged (disabled)", got)
	}
	if m.changes() != 0 {
		t.Fatalf("changes = %d, want 0 after a rejected toggle", m.changes())
	}
	if !strings.Contains(m.status, `module "zshonly" only supports zsh`) {
		t.Fatalf("status = %q, want the rejection message", m.status)
	}
}

func TestTheNextKeyPressClearsTheStatusMessage(t *testing.T) {
	m, b := newBackedModel(sampleViews())
	b.toggleErr = errors.New("nope")
	m = space(m)
	if m.status == "" {
		t.Fatal("setup: expected a status message")
	}

	m = press(t, m, "down")

	if m.status != "" {
		t.Fatalf("status = %q, want it cleared by the key press", m.status)
	}
}

func TestToggleKeepsTheCursorOnTheSameModuleUnderAFilter(t *testing.T) {
	m := press(t, newTestModel(sampleViews()), "/", "z", "enter") // fzf, zshonly
	m = press(t, m, "down")                                       // zshonly

	m = space(m)

	if v, _ := m.selected(); v.ID != "zshonly" {
		t.Fatalf("selected = %q, want zshonly to stay selected", v.ID)
	}
	if m.filter != "z" || len(m.visible) != 2 {
		t.Fatalf("filter=%q visible=%v, want the filter kept", m.filter, visibleIDs(m))
	}
}

func TestSpaceOnAnEmptyOrUnmatchedListDoesNothing(t *testing.T) {
	empty := newTestModel(nil)
	if _, cmd := empty.Update(key("space")); cmd != nil {
		t.Fatal("space on an empty list must not return a command")
	}

	m, b := newBackedModel(sampleViews())
	m = press(t, m, "/", "n", "o", "p", "e", "enter")
	if _, cmd := m.Update(key("space")); cmd != nil {
		t.Fatal("space with no match must not return a command")
	}
	if len(b.calls) != 0 {
		t.Fatalf("backend calls = %v, want none", b.calls)
	}
}

func TestSpaceWhileTypingTheFilterIsText(t *testing.T) {
	m, b := newBackedModel(sampleViews())
	m = press(t, m, "/", "a")

	next, cmd := m.Update(key("space"))

	if cmd != nil || len(b.calls) != 0 {
		t.Fatalf("space in the filter must not toggle: cmd=%v calls=%v", cmd != nil, b.calls)
	}
	if got := next.(Model).filter; got != "a " {
		t.Fatalf("filter = %q, want %q", got, "a ")
	}
}

func TestFailedRefreshAfterASuccessfulWriteShowsTheError(t *testing.T) {
	m, b := newBackedModel(sampleViews())
	b.modulesErr = errors.New("cannot re-read config")

	m = space(m)

	if !strings.Contains(m.status, "cannot re-read config") {
		t.Fatalf("status = %q, want the refresh error", m.status)
	}
	if want := []string{"disable completion"}; !reflect.DeepEqual(b.calls, want) {
		t.Fatalf("the write itself must still have happened once: %v", b.calls)
	}
}

func TestToggleOfAModuleThatIsUnavailableOnThisHostIsStillAttempted(t *testing.T) {
	// Dimming is advice, not a lock: modedit.Enable is the authority on what
	// is allowed, and its error reaches the status line.
	m, b := newBackedModel(sampleViews())
	m = press(t, m, "down", "down") // zshonly, Unavailable

	space(m)

	if want := []string{"enable zshonly"}; !reflect.DeepEqual(b.calls, want) {
		t.Fatalf("backend calls = %v, want %v", b.calls, want)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -count=1`
Expected: FAIL to build: `too many arguments in call to New` (the helpers call `New(b, views)`).

- [ ] **Step 3: Implement**

Replace `internal/tui/model.go`:

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
	initial   map[string]modedit.Status // each module's state when the browser started
	width     int                       // 0 until the first tea.WindowSizeMsg
	height    int
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
	case toggledMsg:
		return m.applyToggled(msg), nil
	case tea.KeyPressMsg:
		m.status = ""
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

Create `internal/tui/toggle.go`:

```go
package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/JtheGunner/omnishell/internal/modedit"
)

// toggledMsg reports the outcome of flipping one module: either the error that
// stopped it, or the module list re-read after the write.
type toggledMsg struct {
	id    string
	views []modedit.ModuleView
	err   error
}

// toggle flips the selected module. The write happens in a command so Update
// itself stays free of I/O.
func (m Model) toggle() (tea.Model, tea.Cmd) {
	v, ok := m.selected()
	if !ok {
		return m, nil
	}
	return m, toggleCmd(m.backend, v.ID, v.Status != modedit.StatusEnabled)
}

// toggleCmd enables or disables id, then re-reads the modules so the screen
// shows what is now in config.toml rather than what it expects to be there.
func toggleCmd(b Backend, id string, enable bool) tea.Cmd {
	return func() tea.Msg {
		var err error
		if enable {
			err = b.Enable(id)
		} else {
			err = b.Disable(id)
		}
		if err != nil {
			return toggledMsg{id: id, err: err}
		}
		views, err := b.Modules()
		return toggledMsg{id: id, views: views, err: err}
	}
}

// applyToggled shows the error on the status line and leaves the list as it
// was, or swaps in the re-read modules and keeps the cursor on the module.
func (m Model) applyToggled(msg toggledMsg) Model {
	if msg.err != nil {
		m.status = sanitize(msg.err.Error())
		return m
	}
	m.views = sanitizeViews(msg.views)
	m.applyFilterKeeping(msg.id)
	return m
}
```

In `internal/tui/run.go`, pass the backend to the model:

```go
	if _, err := tea.NewProgram(New(b, views), tea.WithInput(in), tea.WithOutput(out)).Run(); err != nil {
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `gofmt -l internal/tui && go vet ./internal/tui/ && go test ./internal/tui/ -race -count=1 -v`
Expected: `gofmt -l` prints nothing; PASS for all 45 tests, including the unchanged golden test (the view does not use the new data yet).

- [ ] **Step 5: Commit**

```bash
git add internal/tui
git commit -m "feat: toggle the selected module with space in the tui model"
```

---

### Task 4: Show the counter, the status line and the dimmed modules

**Files:**
- Modify: `internal/tui/view.go`
- Modify (replace): `internal/tui/view_test.go`
- Create/modify: `internal/tui/testdata/*.golden` (generated)

**Interfaces:**
- Consumes: `Model.changes()`, `Model.status`, `ModuleView.Unavailable` (Tasks 1 and 3).
- Produces: the final `View()`; unexported `changesText(n int) string`.

- [ ] **Step 1: Write the failing tests**

Replace `internal/tui/view_test.go` with the full set (the existing tests keep their meaning; new tests cover the counter, the status line, the dimming and the host line, and three new goldens are added):

```go
package tui

import (
	"errors"
	"strings"
	"testing"
)

func TestViewIsEmptyUntilTheTerminalSizeIsKnown(t *testing.T) {
	if got := newTestModel(sampleViews()).View().Content; got != "" {
		t.Fatalf("view before the first size message = %q, want empty", got)
	}
}

func TestViewShowsATooSmallMessageBelowTheMinimumSize(t *testing.T) {
	cases := []struct{ w, h int }{{79, 20}, {80, 19}, {10, 5}}
	for _, c := range cases {
		out := plain(sized(newTestModel(sampleViews()), c.w, c.h))
		if !strings.Contains(out, "Terminal too small: need at least 80x20") {
			t.Fatalf("%dx%d: view = %q, want the too-small message", c.w, c.h, out)
		}
		if strings.Contains(out, "fzf") {
			t.Fatalf("%dx%d: the module list must not render when too small", c.w, c.h)
		}
	}
}

func TestViewAtTheMinimumSizeRendersTheBrowser(t *testing.T) {
	out := plain(sized(newTestModel(sampleViews()), 80, 20))

	if strings.Contains(out, "too small") {
		t.Fatalf("80x20 must be big enough:\n%s", out)
	}
	assertFits(t, out, 80, 20)
}

func TestViewFitsExactlyAtSeveralSizes(t *testing.T) {
	for _, c := range []struct{ w, h int }{{80, 20}, {100, 30}, {200, 50}} {
		assertFits(t, plain(sized(newTestModel(sampleViews()), c.w, c.h)), c.w, c.h)
	}
}

func TestViewListsModulesWithStatusBoxesAndMarksTheCursor(t *testing.T) {
	out := plain(sized(newTestModel(sampleViews()), 80, 20))

	for _, want := range []string{"▸ [x] completion", "  [ ] fzf", "  [ ] zshonly"} {
		if !strings.Contains(out, want) {
			t.Fatalf("view is missing %q:\n%s", want, out)
		}
	}
}

func TestViewDetailFollowsTheCursor(t *testing.T) {
	m := sized(newTestModel(sampleViews()), 100, 30)
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
	out := plain(sized(newTestModel(sampleViews()), 100, 30)) // completion has none

	if !strings.Contains(out, "Homepage:  —") {
		t.Fatalf("want a dash for the missing homepage:\n%s", out)
	}
}

func TestViewScrollsTheListToKeepTheCursorVisible(t *testing.T) {
	m := sized(newTestModel(manyViews(30)), 80, 20)
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

	assertFits(t, plain(sized(newTestModel(views), 80, 20)), 80, 20)
}

func TestViewTruncatesALongModuleIDInTheList(t *testing.T) {
	views := sampleViews()
	views[0].ID = strings.Repeat("long-module-id-", 10)

	assertFits(t, plain(sized(newTestModel(views), 80, 20)), 80, 20)
}

func TestViewSaysSoWhenThereAreNoModulesOrNoMatches(t *testing.T) {
	if out := plain(sized(newTestModel(nil), 80, 20)); !strings.Contains(out, "No modules") {
		t.Fatalf("empty registry view:\n%s", out)
	}
	assertFits(t, plain(sized(newTestModel(nil), 80, 20)), 80, 20)

	m := press(t, sized(newTestModel(sampleViews()), 80, 20), "/", "n", "o", "p", "e")
	out := plain(m)
	if !strings.Contains(out, "No matches") {
		t.Fatalf("unmatched filter view:\n%s", out)
	}
	assertFits(t, out, 80, 20)
}

func TestViewHeaderAndFooterReflectTheFilterState(t *testing.T) {
	m := sized(newTestModel(sampleViews()), 80, 20)

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
	m := sized(newTestModel(sampleViews()), 100, 30)
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

func TestViewHeaderCountsChangesSinceStart(t *testing.T) {
	m := sized(newTestModel(sampleViews()), 100, 30)
	if out := plain(m); !strings.Contains(out, "3 modules · 0 changes since start") {
		t.Fatalf("header before any toggle:\n%s", out)
	}

	m = space(m)
	if out := plain(m); !strings.Contains(out, "3 modules · 1 change since start") {
		t.Fatalf("header after one toggle:\n%s", out)
	}

	m = space(press(t, m, "down"))
	if out := plain(m); !strings.Contains(out, "3 modules · 2 changes since start") {
		t.Fatalf("header after two toggles:\n%s", out)
	}
}

func TestViewFooterShowsTheStatusMessageInsteadOfTheHelp(t *testing.T) {
	m, b := newBackedModel(sampleViews())
	b.toggleErr = errors.New("module \"zshonly\" only supports zsh")
	m = space(sized(m, 100, 30))

	out := plain(m)
	if !strings.Contains(out, `! module "zshonly" only supports zsh`) {
		t.Fatalf("status line missing:\n%s", out)
	}
	if strings.Contains(out, "esc clear filter") {
		t.Fatalf("the help must give way to the status line:\n%s", out)
	}
	assertFits(t, out, 100, 30)

	if out := plain(press(t, m, "down")); !strings.Contains(out, "space toggle") {
		t.Fatalf("the help must come back after the next key:\n%s", out)
	}
}

// Error text from a backend is untrusted like manifest text: it must be shown,
// but never as a terminal command or as extra lines.
func TestViewShowsAStatusMessageButNeutralisesControlSequences(t *testing.T) {
	m, b := newBackedModel(sampleViews())
	b.toggleErr = errors.New("bad\x1b]0;pwned\x07\x1b[2J\nsecond line")
	m = space(sized(m, 100, 30))

	out := m.View().Content
	for _, bad := range []string{"\x1b]", "\x1b[2J", "\x07"} {
		if strings.Contains(out, bad) {
			t.Fatalf("view contains %q from the error text:\n%q", bad, out)
		}
	}
	if !strings.Contains(plain(m), "! bad ]0;pwned") {
		t.Fatalf("the message must still be readable:\n%s", plain(m))
	}
	assertFits(t, plain(m), 100, 30)
}

func TestViewTruncatesALongStatusMessageToTheWidth(t *testing.T) {
	m, b := newBackedModel(sampleViews())
	b.toggleErr = errors.New(strings.Repeat("a very long message ", 20))
	m = space(sized(m, 80, 20))

	out := plain(m)
	if !strings.Contains(out, "! a very long message") || !strings.Contains(out, "…") {
		t.Fatalf("expected a visibly truncated status line:\n%s", out)
	}
	assertFits(t, out, 80, 20)
}

func TestViewHelpMentionsTheSpaceKey(t *testing.T) {
	if out := plain(sized(newTestModel(sampleViews()), 80, 20)); !strings.Contains(out, "space toggle") {
		t.Fatalf("footer help should list the space key:\n%s", out)
	}
}

func TestViewDimsModulesThatCannotRunOnThisHost(t *testing.T) {
	m := sized(newTestModel(sampleViews()), 80, 20)
	rows := strings.Split(m.renderList(10, 26), "\n")

	const faint = "\x1b[2m"
	if strings.Contains(rows[1], faint) { // fzf runs here
		t.Fatalf("fzf must not be dimmed: %q", rows[1])
	}
	if !strings.Contains(rows[2], faint) { // zshonly does not
		t.Fatalf("zshonly must be dimmed: %q", rows[2])
	}
}

func TestViewDetailExplainsWhyAModuleCannotRunHere(t *testing.T) {
	m := press(t, sized(newTestModel(sampleViews()), 100, 30), "down", "down") // zshonly

	if out := plain(m); !strings.Contains(out, "Host:      needs zsh, but your managed shells are bash") {
		t.Fatalf("detail is missing the host line:\n%s", out)
	}
}

func TestViewDetailHasNoHostLineForAModuleThatRunsHere(t *testing.T) {
	m := press(t, sized(newTestModel(sampleViews()), 100, 30), "down") // fzf

	if out := plain(m); strings.Contains(out, "Host:") {
		t.Fatalf("a usable module must not get a host line:\n%s", out)
	}
}

func TestViewGoldenFiles(t *testing.T) {
	base := sized(newTestModel(sampleViews()), 80, 20)
	rejectedModel, rejectedBackend := newBackedModel(sampleViews())
	rejectedBackend.toggleErr = errors.New(`module "zshonly" only supports zsh, but none of your managed shells (bash) do`)
	rejected := space(press(t, sized(rejectedModel, 80, 20), "down", "down"))
	cases := map[string]Model{
		"browser-80x20":       base,
		"browser-second-row":  press(t, base, "down"),
		"browser-filtered":    press(t, base, "/", "f", "z", "enter"),
		"browser-typing":      press(t, base, "/", "z"),
		"browser-no-modules":  sized(newTestModel(nil), 80, 20),
		"browser-too-small":   sized(newTestModel(sampleViews()), 60, 10),
		"browser-toggled":     space(press(t, base, "down")),
		"browser-unavailable": press(t, base, "down", "down"),
		"browser-rejected":    rejected,
	}
	for name, m := range cases {
		assertGolden(t, name, plain(m))
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -count=1`
Expected: FAIL for exactly these eight tests: `TestViewHeaderCountsChangesSinceStart`, `TestViewFooterShowsTheStatusMessageInsteadOfTheHelp`, `TestViewShowsAStatusMessageButNeutralisesControlSequences`, `TestViewTruncatesALongStatusMessageToTheWidth`, `TestViewHelpMentionsTheSpaceKey`, `TestViewDimsModulesThatCannotRunOnThisHost`, `TestViewDetailExplainsWhyAModuleCannotRunHere` and `TestViewGoldenFiles` (the goldens still describe the old header and footer).

- [ ] **Step 3: Implement**

Replace `internal/tui/view.go`:

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
	boxStyle    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	titleStyle  = lipgloss.NewStyle().Bold(true)
	dimStyle    = lipgloss.NewStyle().Faint(true)
	statusStyle = lipgloss.NewStyle().Bold(true).Inline(true)
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
	line := titleStyle.Render("omnishell") +
		dimStyle.Render(fmt.Sprintf("  %d modules · %s", len(m.views), changesText(m.changes())))
	switch {
	case m.filtering:
		line += "  filter: " + m.filter + "_"
	case m.filter != "":
		line += fmt.Sprintf("  filter: %s (%d shown)", m.filter, len(m.visible))
	}
	return lipgloss.NewStyle().Inline(true).MaxWidth(m.width).Render(line)
}

func (m Model) renderFooter() string {
	if m.status != "" {
		return statusStyle.Render(ansi.Truncate("! "+m.status, m.width, "…"))
	}
	help := "↑/↓ move · space toggle · / filter · esc clear filter · q quit"
	if m.filtering {
		help = "type to filter · enter keep · esc cancel · ctrl+c quit"
	}
	return dimStyle.Inline(true).MaxWidth(m.width).Render(help)
}

// changesText says how many modules differ from the state the browser started
// with: "0 changes since start", "1 change since start".
func changesText(n int) string {
	if n == 1 {
		return "1 change since start"
	}
	return fmt.Sprintf("%d changes since start", n)
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
		style := lipgloss.NewStyle()
		if v.Unavailable != "" {
			style = dimStyle
		}
		if pos == m.cursor {
			style = style.Reverse(true)
		}
		lines = append(lines, style.Render(ansi.Truncate(marker+statusBox(v.Status)+" "+v.ID, width, "…")))
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
	lines := []string{
		titleStyle.Render(v.Name),
		dimStyle.Render(fmt.Sprintf("%s · %s", v.ID, v.Origin)),
		"",
		v.Description,
		"",
		field("Status", string(v.Status)),
		field("Packages", string(v.Packages)),
		field("Platforms", strings.Join(v.Platforms, ", ")),
		field("Shells", strings.Join(v.Shells, ", ")),
	}
	if v.Unavailable != "" {
		lines = append(lines, field("Host", v.Unavailable))
	}
	lines = append(lines,
		field("Options", fmt.Sprint(v.OptionCount)),
		field("Homepage", homepage),
	)
	text := strings.Join(lines, "\n")

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

- [ ] **Step 5: Regenerate the golden files and review the diff by eye**

Run: `go test ./internal/tui/ -run Golden -update -count=1 && git status --short internal/tui/testdata`
Expected: all six existing goldens modified (the header now reads `N modules · 0 changes since start` and the footer lists `space toggle`) and three new files: `browser-toggled`, `browser-unavailable`, `browser-rejected`. `browser-rejected.golden` must read exactly:

```text
omnishell  3 modules · 0 changes since start                                    
╭────────────────────────────╮╭────────────────────────────────────────────────╮
│   [x] completion           ││ Zsh only                                       │
│   [ ] fzf                  ││ zshonly · user                                 │
│ ▸ [ ] zshonly              ││                                                │
│                            ││ A module for zsh alone                         │
│                            ││                                                │
│                            ││ Status:    disabled                            │
│                            ││ Packages:  n/a                                 │
│                            ││ Platforms: linux                               │
│                            ││ Shells:    zsh                                 │
│                            ││ Host:      needs zsh, but your managed shells  │
│                            ││ are bash                                       │
│                            ││ Options:   0                                   │
│                            ││ Homepage:  —                                   │
│                            ││                                                │
│                            ││                                                │
│                            ││                                                │
╰────────────────────────────╯╰────────────────────────────────────────────────╯
! module "zshonly" only supports zsh, but none of your managed shells (bash) do 
```

`browser-toggled.golden` shows `1 change since start`, `fzf` as `[x]` and `Status:    enabled`; `browser-unavailable.golden` is the same screen as the rejected one but with the normal help line in the footer. Run `git diff internal/tui/testdata` and check that nothing else changed.

- [ ] **Step 6: Run the package with the race detector**

Run: `go test ./internal/tui/ -race -count=1`
Expected: PASS (53 tests) without `-update`.

- [ ] **Step 7: Commit**

```bash
git add internal/tui
git commit -m "feat: show the change counter, status line and dimmed modules in the tui"
```

---

### Task 5: Documentation and verification

**Files:**
- Modify: `README.md`, `CHANGELOG.md`, `CLAUDE.md`

**Interfaces:**
- Consumes: everything above.
- Produces: nothing code-level.

- [ ] **Step 1: Update the README row**

In `README.md`, replace the Commands-table row for `omnishell tui` (find it with `grep -n 'omnishell tui' README.md`) with:

```markdown
| `omnishell tui`                        | Browse and toggle modules in a full-screen terminal UI: a module list with description, homepage, package status, platforms and shells, a `/` filter, and Space to enable or disable the selected module (written to `config.toml` at once, like `enable` / `disable`; run `apply` afterwards). Modules that cannot run on this host are dimmed. Needs an interactive terminal (exit 2 otherwise).                                                                                          | —                                                                                                                                                                                                                                                                                                               |
```

- [ ] **Step 2: Update the changelog entry**

In `CHANGELOG.md`, in the `omnishell tui` bullet under `## [Unreleased]`, replace the sentence `Read-only for now; it needs an interactive terminal.` (it wraps across two lines in the file) so the whole bullet reads:

```markdown
- `omnishell tui`: a full-screen terminal UI to browse modules (description,
  homepage, package status, platforms, shells) with a `/` filter. Space enables
  or disables the selected module in `config.toml`, like `enable` / `disable`
  (run `apply` afterwards), and modules that cannot run on this host are
  dimmed with the reason shown. It needs an interactive terminal. It brings the
  first runtime dependencies, Bubble Tea and Lip Gloss, so the binary grows by
  about 1.5 MiB.
```

- [ ] **Step 3: Extend the `CLAUDE.md` entry**

In `CLAUDE.md`, in item 11 (`internal/tui`), after the sentence ending `... before the screen is taken over.` insert:

```markdown
   `Update` stays free of I/O: toggling returns a `tea.Cmd` that
   writes through `modedit.Enable`/`Disable` and then re-reads the modules. Any
   text that comes from a backend or a manifest (module fields, error messages)
   must go through `sanitize` before it is drawn.
```

- [ ] **Step 4: Run the full verification**

Run: `make vet && make test`
Expected: both clean; every package PASS.

Run: `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2 run ./...`
Expected: `0 issues.` (the first run downloads the linter's dependencies and takes about a minute.)

Run the release-target builds:
```bash
for t in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do GOOS=${t%/*} GOARCH=${t#*/} CGO_ENABLED=0 go build -o /dev/null ./cmd/omnishell && echo "$t ok"; done
for v in 6 7; do GOOS=linux GOARCH=arm GOARM=$v CGO_ENABLED=0 go build -o /dev/null ./cmd/omnishell && echo "linux/armv$v ok"; done
```
Expected: six `ok` lines.

Run: `git diff origin/main --stat -- go.mod go.sum`
Expected: no output (no dependency changes).

Run: `go build -o /tmp/omnishell-toggle ./cmd/omnishell && /tmp/omnishell-toggle tui < /dev/null; echo "exit=$?"`
Expected: `error: omnishell tui needs an interactive terminal; ...` and `exit=2`. (Trying the toggle itself needs a real terminal: ask the user to run `! omnishell tui` against a scratch `HOME`, never against their real config from here.)

- [ ] **Step 5: Commit**

```bash
git add README.md CHANGELOG.md CLAUDE.md
git commit -m "docs: describe toggling in the omnishell tui"
```
