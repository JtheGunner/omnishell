# omnishell Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build `omnishell`, a single-binary CLI that applies modular, declarative, reversible terminal configuration on macOS and Linux.

**Architecture:** A Go binary with a generic "manifest engine". Modules are data — a folder with `manifest.toml`, shell snippet templates, and optional hook scripts. Built-in modules are compiled in via `go:embed`; user modules load at runtime from `~/.config/omnishell/modules/`. `omnishell apply` resolves a dependency graph of enabled modules, installs their packages via the detected system package manager, renders each module's snippet into one tool-managed init file per shell (`~/.config/omnishell/init.zsh`, `init.bash`), sources that file from `~/.zshrc` / `~/.bashrc` via a single marker block, and records the result in `~/.config/omnishell/state.lock.json` for idempotency, drift detection, and clean removal.

**Tech Stack:** Go 1.23, `github.com/spf13/cobra` (CLI), `github.com/BurntSushi/toml` (TOML read), `go:embed` (built-in modules), stdlib `text/template`, `crypto/sha256`, `os/exec`. Test: stdlib `testing` + golden files. Release: GoReleaser, GitHub Actions.

**Spec:** `docs/superpowers/specs/2026-09-02-omnishell-design.md` — read it alongside this plan. Every task argues from that spec.

## Global Constraints

- **Language:** Go, module path `github.com/JtheGunner/omnishell`. Minimum `go 1.23`.
- **Package layout:** everything non-`main` lives under `internal/`. One package = one responsibility. Files focused; prefer many small files over large ones (target 200–400 lines, 800 max).
- **Immutability:** functions return new values; never mutate a caller's struct, slice, or map in place. Copy before modifying.
- **No `sudo` unless a Linux package manager requires it.** When it does, the `sudo` invocation must be visible in output and announced before running.
- **Nothing changes on the system without `omnishell apply` or `omnishell remove`.** `init`, `enable`, `disable`, `set` only touch `~/.config/omnishell/config.toml` (and `init` also inserts the rc marker block).
- **Every write to an rc file or init file is preceded by a timestamped backup** under `~/.config/omnishell/backups/<RFC3339-utc>/`.
- **All file writes are atomic:** write to a temp file in the same directory, `chmod`, then `rename`.
- **Idempotency:** `apply` run twice with no config change performs no writes and creates no backup.
- **Exit codes:** `0` success; `1` application error (e.g. package install failed, ≥1 module degraded); `2` config/schema error (nothing changed); `3` drift detected (`doctor` only).
- **Hashes** are lowercase hex SHA-256, prefixed `sha256:`.
- **Config dir** resolves to `$XDG_CONFIG_HOME/omnishell` if `XDG_CONFIG_HOME` is set, else `~/.config/omnishell`.
- **Determinism:** module ordering, rendered init files, and serialized JSON must be byte-stable across runs given identical inputs. Sort maps by key before iterating for output.
- **TDD:** write the failing test first, watch it fail, implement minimally, watch it pass, commit. Small commits, conventional-commit messages (`feat:`, `fix:`, `test:`, `chore:`, `docs:`, `refactor:`, `ci:`).
- **Commit trailer:** end every commit message body with `Claude-Session: https://claude.ai/code/session_012MEGbPB16gL53nKQwspocK`.
- **No `console`-style debug prints** left in code. User-facing output goes through an injected `io.Writer`, never a bare `fmt.Println` in library packages.

---

## File Structure

```
omnishell/
├── cmd/omnishell/main.go              # entrypoint: build cobra root, run, map error→exit code
├── internal/
│   ├── buildinfo/buildinfo.go         # Version/Commit/Date vars set via -ldflags
│   ├── atomicfile/atomicfile.go       # WriteFile(path, data, perm) atomic via temp+rename
│   ├── backup/backup.go               # Session{dir}; Save(path) copies file into backups/<ts>/
│   ├── platform/platform.go           # Detect() → Info{OS,Arch,HomeDir,ConfigDir,Shells}
│   ├── platform/shells.go             # shell name → rc path; PATH lookup
│   ├── config/config.go               # Config/ModuleConfig types; Load; Default
│   ├── config/edit.go                 # surgical toml edits: SetEnabled, SetOption, EnsureModuleTable
│   ├── module/manifest.go             # Manifest types; ParseManifest; ValidateManifest
│   ├── module/options.go              # OptionSchema; ParseValue; ValidateOptions; OptionsHash
│   ├── module/module.go               # Module{Manifest,FS,Dir,Source}; Template; HasHook; ReadHook
│   ├── module/registry.go             # LoadRegistry(builtinFS, userDir); All/Get/Overrides
│   ├── graph/graph.go                 # Order(active map[string]Manifest) ([]string, error); errors
│   ├── render/render.go               # Context; Render(tmpl, ctx); funcs (shellquote,pathjoin,has)
│   ├── initfile/initfile.go           # Section; Build; ContentHash; ParseHeaderHash; DetectHandEdit
│   ├── rcfile/rcfile.go               # BlockPresent; EnsureBlock; RemoveBlock; SourceLine
│   ├── lockfile/lockfile.go           # Lock and nested types; Load; Write
│   ├── pkgmgr/pkgmgr.go               # Manager interface; Runner; DetectManager; ExecRunner
│   ├── pkgmgr/managers.go             # brew, apt, dnf, pacman, zypper, apk implementations
│   ├── pkgmgr/mock.go                 # MockManager + MockRunner for tests
│   ├── pkgmgr/gitfallback.go          # InstallGitFallback(fb, ctx, runner) → vendorPath
│   ├── engine/engine.go               # Engine struct + constructor wiring
│   ├── engine/plan.go                 # ComputePlan(...) → Plan; RenderPlan(Plan) string
│   ├── engine/apply.go                # Engine.Apply(cfg, lock, opts) → Result
│   ├── engine/remove.go               # Engine.Remove(cfg, lock, id, purge, opts) → Result
│   ├── engine/doctor.go               # Engine.Doctor(cfg, lock) → DoctorReport
│   ├── engine/uninstall.go            # Engine.Uninstall(cfg, lock, opts) → Result
│   └── cli/                           # one file per command; thin wrappers over engine
│       ├── root.go  version.go  init.go  list.go  enable.go  set.go
│       ├── apply.go  diff.go  doctor.go  remove.go  uninstall.go
│       └── exit.go                    # error classification → exit code
├── modules/                           # built-in modules, embedded via modules/embed.go
│   ├── embed.go                       # //go:embed all:completion all:history ... → fs.FS
│   ├── completion/  history/  autosuggestions/  syntax-highlighting/
│   ├── fzf/  zoxide/  modern-aliases/
├── testdata/                          # fixture modules + fake HOME trees for tests
├── docs/
│   ├── superpowers/specs/2026-09-02-omnishell-design.md
│   ├── superpowers/plans/2026-09-02-omnishell.md
│   └── writing-a-module.md
├── .github/workflows/ci.yml           # test + vet + lint on push/PR
├── .github/workflows/release.yml      # goreleaser on tag
├── .github/workflows/e2e.yml          # opt-in per-distro container tests
├── .goreleaser.yaml
├── install.sh
├── Makefile
├── go.mod
└── README.md
```

---

## Phases

- **Phase 0 — Bootstrap** (Task 1): repo compiles, `omnishell version` works, CI runs `go test`.
- **Phase 1 — Shared primitives** (Tasks 2–3): `atomicfile`, `backup`, `platform` (`buildinfo` is in Task 1).
- **Phase 2 — Config** (Tasks 4–5): read + surgical edit of `config.toml`.
- **Phase 3 — Module system** (Tasks 6–8): manifest, option schema, module accessor + registry.
- **Phase 4 — Graph & render** (Tasks 9–10): dependency ordering, template rendering.
- **Phase 5 — File managers** (Tasks 11–13): init file, rc file, lockfile.
- **Phase 6 — Package managers** (Tasks 14–16): interface + detection + mock, 6 managers, git fallback.
- **Phase 7 — Engine** (Tasks 17–22): plan, plan rendering, apply, remove, doctor, uninstall.
- **Phase 8 — CLI** (Tasks 23–26): wire cobra commands to the engine.
- **Phase 9 — Built-in modules** (Tasks 27–30): the 7 v1 modules with golden tests.
- **Phase 10 — Integration, distribution, docs** (Tasks 31–34): user-journey tests, GoReleaser + install.sh, README + module guide, opt-in per-distro E2E.

Natural stop points: after Phase 8 the binary is fully functional with user-supplied modules; after Phase 9 it ships the 7 built-ins; Phase 10 is release polish.

---

## Task 1: Project bootstrap

**Files:**
- Create: `go.mod`, `cmd/omnishell/main.go`, `internal/buildinfo/buildinfo.go`, `internal/cli/root.go`, `internal/cli/version.go`, `internal/cli/exit.go`, `Makefile`, `.gitignore` (already exists — verify), `.github/workflows/ci.yml`
- Test: `internal/cli/version_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `buildinfo.Version string` (default `"dev"`), `buildinfo.Commit string`, `buildinfo.Date string`.
  - `cli.NewRootCmd(stdout, stderr io.Writer) *cobra.Command` — builds the root command with all subcommands attached.
  - `cli.Execute(args []string, stdout, stderr io.Writer) int` — runs the root command, returns an exit code.
  - `cli.ClassifyError(err error) int` — maps an error to an exit code (see Task 24 for full mapping; for now: `nil`→0, else→1).

- [ ] **Step 1: Initialise the module and add cobra**

Run:
```bash
cd /Users/jeffry/Projects/omnishell
go mod init github.com/JtheGunner/omnishell
go get github.com/spf13/cobra@latest
go get github.com/BurntSushi/toml@latest
```
Edit `go.mod` so the first lines are:
```
module github.com/JtheGunner/omnishell

go 1.23
```

- [ ] **Step 2: Write `internal/buildinfo/buildinfo.go`**

```go
// Package buildinfo exposes version metadata injected at build time via -ldflags.
package buildinfo

var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

// String returns a single human-readable version line.
func String() string {
	return Version + " (commit " + Commit + ", built " + Date + ")"
}
```

- [ ] **Step 3: Write the failing test `internal/cli/version_test.go`**

```go
package cli_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/JtheGunner/omnishell/internal/cli"
)

func TestVersionCommandPrintsVersion(t *testing.T) {
	var out, errb bytes.Buffer
	code := cli.Execute([]string{"version"}, &out, &errb)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, errb.String())
	}
	if !strings.Contains(out.String(), "dev") {
		t.Fatalf("version output = %q, want it to contain %q", out.String(), "dev")
	}
}
```

- [ ] **Step 4: Run the test, expect failure**

Run: `go test ./internal/cli/ -run TestVersionCommand -v`
Expected: build failure — `cli.Execute` undefined.

- [ ] **Step 5: Implement `internal/cli/exit.go`**

```go
package cli

// ClassifyError maps an error to a process exit code.
// Extended in later tasks for config (2) and drift (3) errors.
func ClassifyError(err error) int {
	if err == nil {
		return 0
	}
	return 1
}
```

- [ ] **Step 6: Implement `internal/cli/root.go`**

```go
package cli

import (
	"io"

	"github.com/spf13/cobra"
)

// NewRootCmd builds the omnishell root command with every subcommand attached.
func NewRootCmd(stdout, stderr io.Writer) *cobra.Command {
	root := &cobra.Command{
		Use:           "omnishell",
		Short:         "Modular, declarative terminal configuration for macOS and Linux",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.PersistentFlags().Bool("verbose", false, "verbose output")

	root.AddCommand(newVersionCmd(stdout))
	return root
}

// Execute runs omnishell with the given args and returns an exit code.
func Execute(args []string, stdout, stderr io.Writer) int {
	root := NewRootCmd(stdout, stderr)
	root.SetArgs(args)
	err := root.Execute()
	if err != nil {
		if _, werr := io.WriteString(stderr, "error: "+err.Error()+"\n"); werr != nil {
			_ = werr
		}
	}
	return ClassifyError(err)
}
```

- [ ] **Step 7: Implement `internal/cli/version.go`**

```go
package cli

import (
	"fmt"
	"io"

	"github.com/JtheGunner/omnishell/internal/buildinfo"
	"github.com/spf13/cobra"
)

func newVersionCmd(stdout io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the omnishell version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintln(stdout, buildinfo.String())
			return err
		},
	}
}
```

- [ ] **Step 8: Implement `cmd/omnishell/main.go`**

```go
package main

import (
	"os"

	"github.com/JtheGunner/omnishell/internal/cli"
)

func main() {
	os.Exit(cli.Execute(os.Args[1:], os.Stdout, os.Stderr))
}
```

- [ ] **Step 9: Run the test, expect pass**

Run: `go test ./... -v`
Expected: PASS. Also run `go vet ./...` — clean. Also run `go run ./cmd/omnishell version` — prints `dev (commit none, built unknown)`.

- [ ] **Step 10: Add `Makefile`**

```makefile
BIN := omnishell
LDFLAGS := -X github.com/JtheGunner/omnishell/internal/buildinfo.Version=$(shell git describe --tags --always --dirty 2>/dev/null || echo dev) \
           -X github.com/JtheGunner/omnishell/internal/buildinfo.Commit=$(shell git rev-parse --short HEAD 2>/dev/null || echo none) \
           -X github.com/JtheGunner/omnishell/internal/buildinfo.Date=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)

.PHONY: build test lint vet
build:
	go build -ldflags '$(LDFLAGS)' -o $(BIN) ./cmd/omnishell
test:
	go test ./... -race -count=1
vet:
	go vet ./...
lint:
	golangci-lint run
```

- [ ] **Step 11: Add `.github/workflows/ci.yml`**

```yaml
name: ci
on:
  push: { branches: [main] }
  pull_request:
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.23' }
      - run: go vet ./...
      - run: go test ./... -race -count=1
      - uses: golangci/golangci-lint-action@v6
        with: { version: v1.61 }
```

- [ ] **Step 12: Commit**

```bash
git add -A
git commit -m "chore: bootstrap Go module, cobra root, version command, CI

Claude-Session: https://claude.ai/code/session_012MEGbPB16gL53nKQwspocK"
```

---

## Task 2: `atomicfile` and `backup` packages

**Files:**
- Create: `internal/atomicfile/atomicfile.go`, `internal/backup/backup.go`
- Test: `internal/atomicfile/atomicfile_test.go`, `internal/backup/backup_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `atomicfile.WriteFile(path string, data []byte, perm os.FileMode) error` — writes via temp file in the same dir + `rename`; creates parent dirs with `0o755`.
  - `backup.Session` struct with `Dir string`.
  - `backup.NewSession(configDir string, now time.Time) (Session, error)` — `Dir = configDir + "/backups/" + now.UTC().Format("20060102T150405Z")`; created lazily.
  - `(Session) Save(path string) (savedTo string, err error)` — copies `path` into `Dir`, preserving base name; if `path` does not exist, returns `("", nil)` (nothing to back up); creates `Dir` on first real save.

- [ ] **Step 1: Write failing test `internal/atomicfile/atomicfile_test.go`**

```go
package atomicfile_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/JtheGunner/omnishell/internal/atomicfile"
)

func TestWriteFileCreatesParentsAndContent(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "nested", "deep", "file.txt")

	if err := atomicfile.WriteFile(target, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "hello" {
		t.Fatalf("content = %q, want %q", got, "hello")
	}
}

func TestWriteFileOverwritesAtomically(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := atomicfile.WriteFile(target, []byte("new"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, _ := os.ReadFile(target)
	if string(got) != "new" {
		t.Fatalf("content = %q, want %q", got, "new")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("dir has %d entries, want 1 (no temp file left)", len(entries))
	}
}
```

- [ ] **Step 2: Run, expect failure**

Run: `go test ./internal/atomicfile/ -v`
Expected: FAIL — package does not compile (`atomicfile` undefined).

- [ ] **Step 3: Implement `internal/atomicfile/atomicfile.go`**

```go
// Package atomicfile writes files atomically: a temp file in the destination
// directory is written, flushed, chmod-ed, then renamed over the target.
package atomicfile

import (
	"fmt"
	"os"
	"path/filepath"
)

// WriteFile atomically writes data to path with the given permissions,
// creating parent directories (0o755) as needed.
func WriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create dir %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op if the rename succeeded

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename temp file over %s: %w", path, err)
	}
	return nil
}
```

- [ ] **Step 4: Run, expect pass**

Run: `go test ./internal/atomicfile/ -v` → PASS.

- [ ] **Step 5: Write failing test `internal/backup/backup_test.go`**

```go
package backup_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/JtheGunner/omnishell/internal/backup"
)

func fixedTime() time.Time {
	return time.Date(2026, 9, 2, 22, 41, 3, 0, time.UTC)
}

func TestSaveCopiesFileIntoTimestampedDir(t *testing.T) {
	cfgDir := t.TempDir()
	src := filepath.Join(t.TempDir(), ".zshrc")
	if err := os.WriteFile(src, []byte("rc contents"), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := backup.NewSession(cfgDir, fixedTime())
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	saved, err := s.Save(src)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	want := filepath.Join(cfgDir, "backups", "20260902T224103Z", ".zshrc")
	if saved != want {
		t.Fatalf("saved to %q, want %q", saved, want)
	}
	got, _ := os.ReadFile(saved)
	if string(got) != "rc contents" {
		t.Fatalf("backup content = %q", got)
	}
}

func TestSaveMissingFileIsNoop(t *testing.T) {
	s, err := backup.NewSession(t.TempDir(), fixedTime())
	if err != nil {
		t.Fatal(err)
	}
	saved, err := s.Save(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatalf("Save of missing file: %v", err)
	}
	if saved != "" {
		t.Fatalf("saved = %q, want empty string", saved)
	}
}
```

- [ ] **Step 6: Run, expect failure**

Run: `go test ./internal/backup/ -v` → FAIL (undefined).

- [ ] **Step 7: Implement `internal/backup/backup.go`**

```go
// Package backup copies files into a per-run timestamped directory under
// <configDir>/backups/ before omnishell overwrites them.
package backup

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// Session is one backup directory for a single omnishell invocation.
type Session struct {
	Dir string
}

// NewSession computes (but does not yet create) the backup directory
// <configDir>/backups/<RFC3339-basic-utc>/.
func NewSession(configDir string, now time.Time) (Session, error) {
	stamp := now.UTC().Format("20060102T150405Z")
	return Session{Dir: filepath.Join(configDir, "backups", stamp)}, nil
}

// Save copies path into the session directory, keeping its base name.
// A missing source file is not an error and returns ("", nil).
func (s Session) Save(path string) (string, error) {
	in, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("open %s for backup: %w", path, err)
	}
	defer in.Close()

	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return "", fmt.Errorf("create backup dir %s: %w", s.Dir, err)
	}
	dst := filepath.Join(s.Dir, filepath.Base(path))
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return "", fmt.Errorf("create backup file %s: %w", dst, err)
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return "", fmt.Errorf("copy to backup %s: %w", dst, err)
	}
	return dst, nil
}
```

- [ ] **Step 8: Run, expect pass**

Run: `go test ./internal/atomicfile/ ./internal/backup/ -v` → PASS.

- [ ] **Step 9: Commit**

```bash
git add -A
git commit -m "feat: add atomicfile and backup packages

Claude-Session: https://claude.ai/code/session_012MEGbPB16gL53nKQwspocK"
```

---

## Task 3: `platform` package — OS, arch, config dir, shells

**Files:**
- Create: `internal/platform/platform.go`, `internal/platform/shells.go`
- Test: `internal/platform/platform_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `platform.OS` string type with constants `platform.MacOS = "macos"`, `platform.Linux = "linux"`.
  - `platform.ShellInfo struct { Name string; RCPath string; Present bool }` — `Name` is `"zsh"` or `"bash"`.
  - `platform.Info struct { OS OS; Arch string; HomeDir string; ConfigDir string; Shells []ShellInfo }`.
  - `platform.Env struct { GOOS string; GOARCH string; Getenv func(string) string; LookPath func(string) (string, error) }`.
  - `platform.Detect() (Info, error)` — uses the real runtime/env.
  - `platform.DetectWith(env Env) (Info, error)` — pure, for tests. `Shells` always lists zsh then bash, with `Present` from `LookPath` and `RCPath` from `HomeDir`.
  - `platform.ConfigDirFor(getenv func(string) string, home string) string` — `$XDG_CONFIG_HOME/omnishell` or `home/.config/omnishell`.

- [ ] **Step 1: Write failing test `internal/platform/platform_test.go`**

```go
package platform_test

import (
	"errors"
	"testing"

	"github.com/JtheGunner/omnishell/internal/platform"
)

func env(goos string, present map[string]bool, vars map[string]string) platform.Env {
	return platform.Env{
		GOOS:   goos,
		GOARCH: "arm64",
		Getenv: func(k string) string { return vars[k] },
		LookPath: func(bin string) (string, error) {
			if present[bin] {
				return "/usr/bin/" + bin, nil
			}
			return "", errors.New("not found")
		},
	}
}

func TestDetectWithMacOSDefaultConfigDir(t *testing.T) {
	info, err := platform.DetectWith(env("darwin",
		map[string]bool{"zsh": true, "bash": false},
		map[string]string{"HOME": "/Users/j"}))
	if err != nil {
		t.Fatal(err)
	}
	if info.OS != platform.MacOS {
		t.Fatalf("OS = %q, want macos", info.OS)
	}
	if info.ConfigDir != "/Users/j/.config/omnishell" {
		t.Fatalf("ConfigDir = %q", info.ConfigDir)
	}
	if len(info.Shells) != 2 || info.Shells[0].Name != "zsh" || info.Shells[1].Name != "bash" {
		t.Fatalf("Shells = %+v, want [zsh bash]", info.Shells)
	}
	if !info.Shells[0].Present || info.Shells[1].Present {
		t.Fatalf("presence wrong: %+v", info.Shells)
	}
	if info.Shells[0].RCPath != "/Users/j/.zshrc" || info.Shells[1].RCPath != "/Users/j/.bashrc" {
		t.Fatalf("rc paths wrong: %+v", info.Shells)
	}
}

func TestDetectWithLinuxXDGConfigDir(t *testing.T) {
	info, err := platform.DetectWith(env("linux",
		map[string]bool{"bash": true},
		map[string]string{"HOME": "/home/j", "XDG_CONFIG_HOME": "/home/j/xdg"}))
	if err != nil {
		t.Fatal(err)
	}
	if info.OS != platform.Linux {
		t.Fatalf("OS = %q, want linux", info.OS)
	}
	if info.ConfigDir != "/home/j/xdg/omnishell" {
		t.Fatalf("ConfigDir = %q", info.ConfigDir)
	}
}

func TestDetectWithUnsupportedOS(t *testing.T) {
	_, err := platform.DetectWith(env("windows", nil, map[string]string{"HOME": "C:\\Users\\j"}))
	if err == nil {
		t.Fatal("want error for unsupported OS, got nil")
	}
}

func TestDetectWithMissingHome(t *testing.T) {
	_, err := platform.DetectWith(env("linux", nil, map[string]string{}))
	if err == nil {
		t.Fatal("want error when HOME is unset")
	}
}
```

- [ ] **Step 2: Run, expect failure**

Run: `go test ./internal/platform/ -v` → FAIL (undefined).

- [ ] **Step 3: Implement `internal/platform/shells.go`**

```go
package platform

import "path/filepath"

// SupportedShells is the fixed, ordered list of shells omnishell can manage.
var SupportedShells = []string{"zsh", "bash"}

func rcPath(shell, home string) string {
	switch shell {
	case "zsh":
		return filepath.Join(home, ".zshrc")
	case "bash":
		return filepath.Join(home, ".bashrc")
	default:
		return ""
	}
}
```

- [ ] **Step 4: Implement `internal/platform/platform.go`**

```go
// Package platform detects the host OS, architecture, config directory,
// and the shells omnishell can manage.
package platform

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// OS is a normalized operating-system name.
type OS string

const (
	MacOS OS = "macos"
	Linux OS = "linux"
)

// ShellInfo describes one manageable shell on the host.
type ShellInfo struct {
	Name    string
	RCPath  string
	Present bool
}

// Info is the full host description omnishell needs.
type Info struct {
	OS        OS
	Arch      string
	HomeDir   string
	ConfigDir string
	Shells    []ShellInfo
}

// Env holds the runtime/environment dependencies of detection, injectable for tests.
type Env struct {
	GOOS     string
	GOARCH   string
	Getenv   func(string) string
	LookPath func(string) (string, error)
}

// Detect inspects the real host.
func Detect() (Info, error) {
	return DetectWith(Env{
		GOOS:     runtime.GOOS,
		GOARCH:   runtime.GOARCH,
		Getenv:   os.Getenv,
		LookPath: exec.LookPath,
	})
}

// DetectWith is the pure core of Detect.
func DetectWith(env Env) (Info, error) {
	var osName OS
	switch env.GOOS {
	case "darwin":
		osName = MacOS
	case "linux":
		osName = Linux
	default:
		return Info{}, errors.New("unsupported OS: " + env.GOOS + " (omnishell supports macOS and Linux)")
	}

	home := env.Getenv("HOME")
	if home == "" {
		return Info{}, errors.New("cannot determine home directory: HOME is not set")
	}

	shells := make([]ShellInfo, 0, len(SupportedShells))
	for _, name := range SupportedShells {
		_, err := env.LookPath(name)
		shells = append(shells, ShellInfo{
			Name:    name,
			RCPath:  rcPath(name, home),
			Present: err == nil,
		})
	}

	return Info{
		OS:        osName,
		Arch:      env.GOARCH,
		HomeDir:   home,
		ConfigDir: ConfigDirFor(env.Getenv, home),
		Shells:    shells,
	}, nil
}

// ConfigDirFor resolves the omnishell config directory.
func ConfigDirFor(getenv func(string) string, home string) string {
	if xdg := getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "omnishell")
	}
	return filepath.Join(home, ".config", "omnishell")
}
```

- [ ] **Step 5: Run, expect pass**

Run: `go test ./internal/platform/ -v` → PASS. Run `go vet ./...` → clean.

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "feat: add platform detection (os, arch, config dir, shells)

Claude-Session: https://claude.ai/code/session_012MEGbPB16gL53nKQwspocK"
```

---

## Task 4: `config` package — read `config.toml`

**Files:**
- Create: `internal/config/config.go`
- Test: `internal/config/config_test.go`, `internal/config/testdata/full.toml`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `config.ModuleConfig struct { Enabled bool; Options map[string]any }`.
  - `config.OmnishellSection struct { Version int; Shells []string }`.
  - `config.Config struct { Omnishell OmnishellSection; Modules map[string]ModuleConfig }`.
  - `config.SchemaVersion = 1` (int const).
  - `config.Default() Config` — `Version: 1`, `Shells: nil`, empty `Modules` map.
  - `config.Load(path string) (Config, error)` — missing file returns `Default(), nil` **only** when the caller expects that? No: `Load` returns an `fs.ErrNotExist`-wrapped error for a missing file; callers decide. Unknown top-level keys are rejected. `Version` other than `1` is an error. A non-`0` `Version` that is `1` is fine; `0`/absent is treated as `1`.
  - `config.ErrNotFound` — sentinel wrapped when the file does not exist.
  - Every error from `Load` is a `config.Error` (see below), so the CLI can map it to exit code 2.
  - `config.Error struct { Path string; Line int; Msg string }` implementing `error`; `Line` is `0` when unknown.

- [ ] **Step 1: Add fixture `internal/config/testdata/full.toml`**

```toml
[omnishell]
version = 1
shells = ["zsh", "bash"]

[modules.completion]
enabled = true

[modules.fzf]
enabled = true
[modules.fzf.options]
ctrl_r = true
default_opts = "--height 40% --reverse"

[modules.history]
enabled = false
[modules.history.options]
size = 50000
```

- [ ] **Step 2: Write failing test `internal/config/config_test.go`**

```go
package config_test

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/JtheGunner/omnishell/internal/config"
)

func TestLoadFullConfig(t *testing.T) {
	c, err := config.Load(filepath.Join("testdata", "full.toml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Omnishell.Version != 1 {
		t.Fatalf("version = %d", c.Omnishell.Version)
	}
	if len(c.Omnishell.Shells) != 2 {
		t.Fatalf("shells = %v", c.Omnishell.Shells)
	}
	if !c.Modules["completion"].Enabled {
		t.Fatal("completion should be enabled")
	}
	if c.Modules["history"].Enabled {
		t.Fatal("history should be disabled")
	}
	if got := c.Modules["fzf"].Options["ctrl_r"]; got != true {
		t.Fatalf("fzf.ctrl_r = %v (%T), want true", got, got)
	}
	if got := c.Modules["fzf"].Options["default_opts"]; got != "--height 40% --reverse" {
		t.Fatalf("fzf.default_opts = %v", got)
	}
}

func TestLoadMissingFileReturnsSentinel(t *testing.T) {
	_, err := config.Load(filepath.Join(t.TempDir(), "nope.toml"))
	if !errors.Is(err, config.ErrNotFound) {
		t.Fatalf("err = %v, want wrapping ErrNotFound", err)
	}
}

func TestLoadRejectsUnknownTopLevelKey(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "c.toml")
	if err := writeFile(p, "[omnishel]\nversion = 1\n"); err != nil {
		t.Fatal(err)
	}
	_, err := config.Load(p)
	var cerr config.Error
	if !errors.As(err, &cerr) {
		t.Fatalf("err = %v, want config.Error", err)
	}
}

func TestLoadRejectsUnknownSchemaVersion(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "c.toml")
	if err := writeFile(p, "[omnishell]\nversion = 99\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(p); err == nil {
		t.Fatal("want error for version 99")
	}
}

func TestDefault(t *testing.T) {
	d := config.Default()
	if d.Omnishell.Version != config.SchemaVersion {
		t.Fatalf("default version = %d", d.Omnishell.Version)
	}
	if d.Modules == nil {
		t.Fatal("default Modules map is nil")
	}
}
```

Add a small `helpers_test.go` in the same package:

```go
package config_test

import "os"

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}
```

- [ ] **Step 3: Run, expect failure**

Run: `go test ./internal/config/ -v` → FAIL (undefined).

- [ ] **Step 4: Implement `internal/config/config.go`**

```go
// Package config reads (and, via edit.go, surgically edits) the
// user-facing ~/.config/omnishell/config.toml file.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/BurntSushi/toml"
)

// SchemaVersion is the config schema version this build understands.
const SchemaVersion = 1

// ErrNotFound is wrapped by Load when the config file does not exist.
var ErrNotFound = errors.New("config file not found")

// Error is a structured config/parse error; the CLI maps it to exit code 2.
type Error struct {
	Path string
	Line int
	Msg  string
}

func (e Error) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("%s:%d: %s", e.Path, e.Line, e.Msg)
	}
	return fmt.Sprintf("%s: %s", e.Path, e.Msg)
}

// ModuleConfig is one [modules.<id>] table.
type ModuleConfig struct {
	Enabled bool
	Options map[string]any
}

// OmnishellSection is the [omnishell] table.
type OmnishellSection struct {
	Version int
	Shells  []string
}

// Config is the whole parsed file.
type Config struct {
	Omnishell OmnishellSection
	Modules   map[string]ModuleConfig
}

// Default is the config produced by `omnishell init`.
func Default() Config {
	return Config{
		Omnishell: OmnishellSection{Version: SchemaVersion},
		Modules:   map[string]ModuleConfig{},
	}
}

// raw mirrors the on-disk shape for decoding with strict key checking.
type raw struct {
	Omnishell struct {
		Version int      `toml:"version"`
		Shells  []string `toml:"shells"`
	} `toml:"omnishell"`
	Modules map[string]struct {
		Enabled bool           `toml:"enabled"`
		Options map[string]any `toml:"options"`
	} `toml:"modules"`
}

// Load parses the config file at path.
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Config{}, fmt.Errorf("%w: %s", ErrNotFound, path)
		}
		return Config{}, Error{Path: path, Msg: err.Error()}
	}

	var r raw
	md, derr := toml.Decode(string(data), &r)
	if derr != nil {
		return Config{}, Error{Path: path, Msg: derr.Error()}
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		return Config{}, Error{Path: path, Msg: "unknown key: " + undecoded[0].String()}
	}

	version := r.Omnishell.Version
	if version == 0 {
		version = SchemaVersion
	}
	if version != SchemaVersion {
		return Config{}, Error{
			Path: path,
			Msg:  fmt.Sprintf("unsupported config version %d (this omnishell understands version %d)", version, SchemaVersion),
		}
	}

	out := Config{
		Omnishell: OmnishellSection{Version: version, Shells: append([]string(nil), r.Omnishell.Shells...)},
		Modules:   make(map[string]ModuleConfig, len(r.Modules)),
	}
	for id, m := range r.Modules {
		opts := map[string]any{}
		for k, v := range m.Options {
			opts[k] = v
		}
		out.Modules[id] = ModuleConfig{Enabled: m.Enabled, Options: opts}
	}
	return out, nil
}
```

- [ ] **Step 5: Run, expect pass**

Run: `go test ./internal/config/ -v` → PASS.

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "feat: add config.toml reader

Claude-Session: https://claude.ai/code/session_012MEGbPB16gL53nKQwspocK"
```

---

## Task 5: `config` package — surgical edits (`enable`/`disable`/`set`)

**Files:**
- Modify: `internal/config` — add `internal/config/edit.go`
- Test: `internal/config/edit_test.go`

**Interfaces:**
- Consumes: `atomicfile.WriteFile`.
- Produces:
  - `config.SetEnabled(path string, moduleID string, enabled bool) error` — reads the file as text, ensures a `[modules.<id>]` table exists, sets/inserts `enabled = <bool>` as the first key line of that table, preserves all other lines and comments, writes atomically. Creates the file from `Default()` rendered to TOML if it does not exist.
  - `config.SetOption(path string, moduleID, key string, value any) error` — same, but sets `<key> = <toml-literal>` inside `[modules.<id>.options]`, creating that sub-table if needed. `value` is already a typed Go value (`bool`, `int64`, `string`, `[]string`); `edit.go` renders the TOML literal.
  - `config.RenderDefault() []byte` — the default config file as commented TOML (used by `omnishell init` and by the editors when the file is absent).
- **Design note:** editing is line-based, not decode-then-marshal, because `BurntSushi/toml` does not round-trip comments. The editor locates the `[modules.<id>]` header line, then operates within that table's line range (up to the next `[` header at column 0 or EOF).

- [ ] **Step 1: Write failing test `internal/config/edit_test.go`**

```go
package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JtheGunner/omnishell/internal/config"
)

func TestSetEnabledInExistingTablePreservesComments(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	initial := `# my omnishell config
[omnishell]
version = 1

[modules.fzf]
# keep this comment
enabled = false
[modules.fzf.options]
ctrl_r = true
`
	if err := os.WriteFile(p, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := config.SetEnabled(p, "fzf", true); err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}
	got, _ := os.ReadFile(p)
	s := string(got)
	if !strings.Contains(s, "# keep this comment") {
		t.Fatal("comment was lost")
	}
	if !strings.Contains(s, "enabled = true") {
		t.Fatalf("enabled not updated:\n%s", s)
	}
	if strings.Contains(s, "enabled = false") {
		t.Fatalf("old value still present:\n%s", s)
	}
	// Re-parse to prove it is still valid TOML.
	if _, err := config.Load(p); err != nil {
		t.Fatalf("result no longer parses: %v", err)
	}
}

func TestSetEnabledCreatesTableWhenMissing(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte("[omnishell]\nversion = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := config.SetEnabled(p, "zoxide", true); err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}
	c, err := config.Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !c.Modules["zoxide"].Enabled {
		t.Fatal("zoxide not enabled after SetEnabled")
	}
}

func TestSetEnabledCreatesFileFromDefault(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "config.toml")
	if err := config.SetEnabled(p, "history", true); err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}
	c, err := config.Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Omnishell.Version != config.SchemaVersion || !c.Modules["history"].Enabled {
		t.Fatalf("unexpected config: %+v", c)
	}
}

func TestSetOptionRendersLiterals(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte("[omnishell]\nversion = 1\n[modules.fzf]\nenabled = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := config.SetOption(p, "fzf", "default_opts", "--height 40%"); err != nil {
		t.Fatalf("SetOption string: %v", err)
	}
	if err := config.SetOption(p, "fzf", "ctrl_r", true); err != nil {
		t.Fatalf("SetOption bool: %v", err)
	}
	if err := config.SetOption(p, "history", "size", int64(50000)); err != nil {
		t.Fatalf("SetOption int: %v", err)
	}
	if err := config.SetOption(p, "modern-aliases", "replace", []string{"ls", "cat"}); err != nil {
		t.Fatalf("SetOption list: %v", err)
	}
	c, err := config.Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Modules["fzf"].Options["default_opts"] != "--height 40%" {
		t.Fatalf("default_opts = %v", c.Modules["fzf"].Options["default_opts"])
	}
	if c.Modules["fzf"].Options["ctrl_r"] != true {
		t.Fatalf("ctrl_r = %v", c.Modules["fzf"].Options["ctrl_r"])
	}
}
```

- [ ] **Step 2: Run, expect failure**

Run: `go test ./internal/config/ -run TestSet -v` → FAIL (undefined).

- [ ] **Step 3: Implement `internal/config/edit.go`**

```go
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/JtheGunner/omnishell/internal/atomicfile"
)

// RenderDefault returns the commented default config file.
func RenderDefault() []byte {
	return []byte(`# omnishell configuration — https://github.com/JtheGunner/omnishell
# Edit this file by hand, or use: omnishell enable/disable/set
# Apply changes with: omnishell apply

[omnishell]
version = 1
# shells = ["zsh", "bash"]   # omit to auto-detect the login shell
`)
}

// SetEnabled sets modules.<id>.enabled in the file at path.
func SetEnabled(path, moduleID string, enabled bool) error {
	return editTable(path, moduleID, "", "enabled", strconv.FormatBool(enabled))
}

// SetOption sets modules.<id>.options.<key> in the file at path.
func SetOption(path, moduleID, key string, value any) error {
	lit, err := tomlLiteral(value)
	if err != nil {
		return err
	}
	return editTable(path, moduleID, "options", key, lit)
}

func tomlLiteral(value any) (string, error) {
	switch v := value.(type) {
	case bool:
		return strconv.FormatBool(v), nil
	case int:
		return strconv.Itoa(v), nil
	case int64:
		return strconv.FormatInt(v, 10), nil
	case string:
		return strconv.Quote(v), nil
	case []string:
		quoted := make([]string, len(v))
		for i, s := range v {
			quoted[i] = strconv.Quote(s)
		}
		return "[" + strings.Join(quoted, ", ") + "]", nil
	default:
		return "", fmt.Errorf("cannot render TOML literal for %T", value)
	}
}

// editTable ensures [modules.<id>] (and optionally a .<sub> child table) exists
// and that "<key> = <literal>" is present within it, replacing any existing
// assignment of <key>. All other lines and comments are preserved.
func editTable(path, moduleID, sub, key, literal string) error {
	src, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		src = RenderDefault()
	} else if err != nil {
		return Error{Path: path, Msg: err.Error()}
	}

	header := "[modules." + moduleID + "]"
	if sub != "" {
		header = "[modules." + moduleID + "." + sub + "]"
	}
	assignment := key + " = " + literal

	lines := strings.Split(strings.TrimRight(string(src), "\n"), "\n")
	hdrIdx := indexOfHeader(lines, header)

	if hdrIdx == -1 {
		// Append the table (and parent table if the sub-table's parent is absent).
		if sub != "" && indexOfHeader(lines, "[modules."+moduleID+"]") == -1 {
			lines = append(lines, "", "[modules."+moduleID+"]", "enabled = true")
		}
		lines = append(lines, "", header, assignment)
		return atomicfile.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
	}

	// Scan the table body until the next top-level "[" header or EOF.
	end := len(lines)
	for i := hdrIdx + 1; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "[") {
			end = i
			break
		}
	}
	for i := hdrIdx + 1; i < end; i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		name := strings.TrimSpace(strings.SplitN(trimmed, "=", 2)[0])
		if name == key {
			lines[i] = assignment
			return atomicfile.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
		}
	}
	// Not found: insert right after the header line.
	out := append([]string{}, lines[:hdrIdx+1]...)
	out = append(out, assignment)
	out = append(out, lines[hdrIdx+1:]...)
	return atomicfile.WriteFile(path, []byte(strings.Join(out, "\n")+"\n"), 0o644)
}

func indexOfHeader(lines []string, header string) int {
	for i, l := range lines {
		if strings.TrimSpace(l) == header {
			return i
		}
	}
	return -1
}
```

- [ ] **Step 4: Run, expect pass**

Run: `go test ./internal/config/ -v` → PASS.

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "feat: add comment-preserving config.toml editors

Claude-Session: https://claude.ai/code/session_012MEGbPB16gL53nKQwspocK"
```

---

## Task 6: `module` package — manifest types & parser

**Files:**
- Create: `internal/module/manifest.go`
- Test: `internal/module/manifest_test.go`, `internal/module/testdata/fzf-manifest.toml`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `module.ManifestSchemaVersion = 1` (int const).
  - `module.ModuleMeta struct { ID, Name, Description, Version string; Schema int }`.
  - `module.Fallback struct { Type, Repo, Dest, Run string }` (`Type` is `"git"` in v1).
  - `module.Packages struct { Brew, Apt, Dnf, Pacman, Zypper, Apk []string; Fallback []Fallback }`.
  - `module.OptionSchema struct { Type string; Default any; Help string; Values []string }`.
  - `module.Manifest struct { Module ModuleMeta; Platforms []string; Shells []string; Requires []string; After []string; Packages Packages; Options map[string]OptionSchema }`.
  - `module.ParseManifest(data []byte) (Manifest, error)` — strict TOML decode, rejects unknown keys.
  - `module.ValidateManifest(m Manifest) error` — enforces: non-empty `Module.ID` matching `^[a-z][a-z0-9-]*$`; `Module.Version` non-empty; `Module.Schema == ManifestSchemaVersion`; `Platforms ⊆ {macos,linux}` and non-empty; `Shells ⊆ {zsh,bash}` and non-empty; every `OptionSchema.Type` is one of `bool|string|int|enum|list<string>|list<enum>`; `enum`/`list<enum>` have non-empty `Values`; `Default` is assignable to `Type` (bool→bool, int→int64/int, string→string, enum→string in Values, list→[]). Returns a `module.ManifestError{ID, Field, Msg}`.
- **Packages field mapping:** TOML `[packages]` table with keys `brew`,`apt`,`dnf`,`pacman`,`zypper`,`apk` (each `[]string`) and `[[packages.fallback]]` array of tables.

- [ ] **Step 1: Add fixture `internal/module/testdata/fzf-manifest.toml`**

```toml
[module]
id          = "fzf"
name        = "FZF Fuzzy Finder"
description = "Ctrl+R history search as a searchable list"
version     = "1.0.0"
schema      = 1

platforms = ["macos", "linux"]
shells    = ["zsh", "bash"]
requires  = []
after     = ["completion"]

[packages]
brew   = ["fzf"]
apt    = ["fzf"]
dnf    = ["fzf"]
pacman = ["fzf"]
zypper = ["fzf"]
apk    = ["fzf"]

[[packages.fallback]]
type = "git"
repo = "https://github.com/junegunn/fzf.git"
dest = "{{.VendorDir}}/fzf"
run  = "{{.VendorDir}}/fzf/install --bin"

[options.ctrl_r]
type    = "bool"
default = true
help    = "Bind Ctrl+R to the fzf history widget"

[options.default_opts]
type    = "string"
default = "--height 40% --reverse"
help    = "FZF_DEFAULT_OPTS"

[options.replace]
type    = "list<enum>"
values  = ["ls", "cat", "find"]
default = []
```

- [ ] **Step 2: Write failing test `internal/module/manifest_test.go`**

```go
package module_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/JtheGunner/omnishell/internal/module"
)

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParseManifestFzf(t *testing.T) {
	m, err := module.ParseManifest(loadFixture(t, "fzf-manifest.toml"))
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	if m.Module.ID != "fzf" || m.Module.Version != "1.0.0" || m.Module.Schema != 1 {
		t.Fatalf("meta = %+v", m.Module)
	}
	if len(m.Platforms) != 2 || len(m.Shells) != 2 {
		t.Fatalf("platforms/shells = %v / %v", m.Platforms, m.Shells)
	}
	if len(m.After) != 1 || m.After[0] != "completion" {
		t.Fatalf("after = %v", m.After)
	}
	if len(m.Packages.Brew) != 1 || m.Packages.Brew[0] != "fzf" {
		t.Fatalf("brew packages = %v", m.Packages.Brew)
	}
	if len(m.Packages.Fallback) != 1 || m.Packages.Fallback[0].Type != "git" {
		t.Fatalf("fallback = %+v", m.Packages.Fallback)
	}
	if m.Options["ctrl_r"].Type != "bool" || m.Options["ctrl_r"].Default != true {
		t.Fatalf("ctrl_r schema = %+v", m.Options["ctrl_r"])
	}
	if m.Options["replace"].Type != "list<enum>" || len(m.Options["replace"].Values) != 3 {
		t.Fatalf("replace schema = %+v", m.Options["replace"])
	}
	if err := module.ValidateManifest(m); err != nil {
		t.Fatalf("ValidateManifest: %v", err)
	}
}

func TestValidateManifestRejectsBadID(t *testing.T) {
	m, _ := module.ParseManifest(loadFixture(t, "fzf-manifest.toml"))
	m.Module.ID = "Fzf_Bad"
	if err := module.ValidateManifest(m); err == nil {
		t.Fatal("want error for bad id")
	}
}

func TestValidateManifestRejectsUnknownOptionType(t *testing.T) {
	m, _ := module.ParseManifest(loadFixture(t, "fzf-manifest.toml"))
	opt := m.Options["ctrl_r"]
	opt.Type = "float"
	m.Options["ctrl_r"] = opt
	if err := module.ValidateManifest(m); err == nil {
		t.Fatal("want error for unknown option type")
	}
}

func TestValidateManifestRejectsWrongSchema(t *testing.T) {
	m, _ := module.ParseManifest(loadFixture(t, "fzf-manifest.toml"))
	m.Module.Schema = 2
	if err := module.ValidateManifest(m); err == nil {
		t.Fatal("want error for schema 2")
	}
}

func TestParseManifestRejectsUnknownKey(t *testing.T) {
	_, err := module.ParseManifest([]byte("[module]\nid=\"x\"\nversion=\"1\"\nschema=1\nbogus=true\n"))
	if err == nil {
		t.Fatal("want error for unknown key")
	}
}
```

- [ ] **Step 3: Run, expect failure**

Run: `go test ./internal/module/ -run Manifest -v` → FAIL (undefined).

- [ ] **Step 4: Implement `internal/module/manifest.go`**

```go
// Package module parses module manifests, validates option schemas, exposes
// module folders, and builds the registry of built-in + user modules.
package module

import (
	"fmt"
	"regexp"

	"github.com/BurntSushi/toml"
)

// ManifestSchemaVersion is the manifest schema version this build understands.
const ManifestSchemaVersion = 1

var idRe = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// ModuleMeta is the [module] table.
type ModuleMeta struct {
	ID          string `toml:"id"`
	Name        string `toml:"name"`
	Description string `toml:"description"`
	Version     string `toml:"version"`
	Schema      int    `toml:"schema"`
}

// Fallback is one [[packages.fallback]] entry.
type Fallback struct {
	Type string `toml:"type"`
	Repo string `toml:"repo"`
	Dest string `toml:"dest"`
	Run  string `toml:"run"`
}

// Packages is the [packages] table.
type Packages struct {
	Brew     []string   `toml:"brew"`
	Apt      []string   `toml:"apt"`
	Dnf      []string   `toml:"dnf"`
	Pacman   []string   `toml:"pacman"`
	Zypper   []string   `toml:"zypper"`
	Apk      []string   `toml:"apk"`
	Fallback []Fallback `toml:"fallback"`
}

// ForManager returns the package list for a manager name ("brew", "apt", ...).
func (p Packages) ForManager(name string) []string {
	switch name {
	case "brew":
		return p.Brew
	case "apt":
		return p.Apt
	case "dnf":
		return p.Dnf
	case "pacman":
		return p.Pacman
	case "zypper":
		return p.Zypper
	case "apk":
		return p.Apk
	default:
		return nil
	}
}

// OptionSchema describes one [options.<key>] entry.
type OptionSchema struct {
	Type    string   `toml:"type"`
	Default any      `toml:"default"`
	Help    string   `toml:"help"`
	Values  []string `toml:"values"`
}

// Manifest is a parsed manifest.toml.
type Manifest struct {
	Module    ModuleMeta              `toml:"module"`
	Platforms []string               `toml:"platforms"`
	Shells    []string               `toml:"shells"`
	Requires  []string               `toml:"requires"`
	After     []string               `toml:"after"`
	Packages  Packages               `toml:"packages"`
	Options   map[string]OptionSchema `toml:"options"`
}

// ManifestError is a structured manifest validation error.
type ManifestError struct {
	ID    string
	Field string
	Msg   string
}

func (e ManifestError) Error() string {
	return fmt.Sprintf("module %q: %s: %s", e.ID, e.Field, e.Msg)
}

// ParseManifest strictly decodes manifest TOML.
func ParseManifest(data []byte) (Manifest, error) {
	var m Manifest
	md, err := toml.Decode(string(data), &m)
	if err != nil {
		return Manifest{}, fmt.Errorf("parse manifest: %w", err)
	}
	if u := md.Undecoded(); len(u) > 0 {
		return Manifest{}, fmt.Errorf("parse manifest: unknown key %s", u[0].String())
	}
	if m.Options == nil {
		m.Options = map[string]OptionSchema{}
	}
	return m, nil
}

var validOptionTypes = map[string]bool{
	"bool": true, "string": true, "int": true,
	"enum": true, "list<string>": true, "list<enum>": true,
}

func subsetOf(vals []string, allowed map[string]bool) (string, bool) {
	for _, v := range vals {
		if !allowed[v] {
			return v, false
		}
	}
	return "", true
}

// ValidateManifest enforces the manifest rules from the spec.
func ValidateManifest(m Manifest) error {
	id := m.Module.ID
	e := func(field, msg string) error { return ManifestError{ID: id, Field: field, Msg: msg} }

	if !idRe.MatchString(id) {
		return e("module.id", "must match ^[a-z][a-z0-9-]*$")
	}
	if m.Module.Version == "" {
		return e("module.version", "must not be empty")
	}
	if m.Module.Schema != ManifestSchemaVersion {
		return e("module.schema", fmt.Sprintf("must be %d", ManifestSchemaVersion))
	}
	if len(m.Platforms) == 0 {
		return e("platforms", "must list at least one of macos, linux")
	}
	if bad, ok := subsetOf(m.Platforms, map[string]bool{"macos": true, "linux": true}); !ok {
		return e("platforms", "unknown platform "+bad)
	}
	if len(m.Shells) == 0 {
		return e("shells", "must list at least one of zsh, bash")
	}
	if bad, ok := subsetOf(m.Shells, map[string]bool{"zsh": true, "bash": true}); !ok {
		return e("shells", "unknown shell "+bad)
	}
	for key, opt := range m.Options {
		if !validOptionTypes[opt.Type] {
			return e("options."+key+".type", "unknown type "+opt.Type)
		}
		if (opt.Type == "enum" || opt.Type == "list<enum>") && len(opt.Values) == 0 {
			return e("options."+key+".values", "enum types require a non-empty values list")
		}
		if err := checkDefault(opt); err != nil {
			return e("options."+key+".default", err.Error())
		}
	}
	return nil
}

func checkDefault(opt OptionSchema) error {
	if opt.Default == nil {
		return nil
	}
	switch opt.Type {
	case "bool":
		if _, ok := opt.Default.(bool); !ok {
			return fmt.Errorf("must be a boolean")
		}
	case "int":
		switch opt.Default.(type) {
		case int, int64:
		default:
			return fmt.Errorf("must be an integer")
		}
	case "string":
		if _, ok := opt.Default.(string); !ok {
			return fmt.Errorf("must be a string")
		}
	case "enum":
		s, ok := opt.Default.(string)
		if !ok {
			return fmt.Errorf("must be a string in values")
		}
		if _, in := subsetOf([]string{s}, toSet(opt.Values)); !in {
			return fmt.Errorf("%q is not in values", s)
		}
	case "list<string>", "list<enum>":
		if _, ok := toStringSlice(opt.Default); !ok {
			return fmt.Errorf("must be a list")
		}
	}
	return nil
}

func toSet(vals []string) map[string]bool {
	s := make(map[string]bool, len(vals))
	for _, v := range vals {
		s[v] = true
	}
	return s
}

func toStringSlice(v any) ([]string, bool) {
	switch x := v.(type) {
	case []string:
		return x, true
	case []any:
		out := make([]string, 0, len(x))
		for _, e := range x {
			s, ok := e.(string)
			if !ok {
				return nil, false
			}
			out = append(out, s)
		}
		return out, true
	default:
		return nil, false
	}
}
```

- [ ] **Step 5: Run, expect pass**

Run: `go test ./internal/module/ -run Manifest -v` → PASS.

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "feat: add module manifest parser and validator

Claude-Session: https://claude.ai/code/session_012MEGbPB16gL53nKQwspocK"
```

---

## Task 7: `module` package — option schema (parse, validate, hash)

**Files:**
- Modify: `internal/module` — add `internal/module/options.go`
- Test: `internal/module/options_test.go`

**Interfaces:**
- Consumes: `module.OptionSchema` (Task 6).
- Produces:
  - `(OptionSchema) ParseValue(raw string) (any, error)` — turns a CLI string into a typed value: `bool`→`strconv.ParseBool`; `int`→`int64`; `string`→as-is; `enum`→string, must be in `Values`; `list<string>`/`list<enum>`→comma-split, trimmed, each element validated for `list<enum>`. Empty `raw` yields `[]string{}` for list types.
  - `module.ValidateOptions(schema map[string]OptionSchema, in map[string]any) (map[string]any, error)` — returns a **new** map containing every schema key: the caller's value coerced to the canonical Go type (`bool`, `int64`, `string`, `[]string`), or the schema default, or the zero value for the type. Rejects keys not in the schema (`module.OptionError{Key, Msg}`). Coercion accepts what a TOML decode produces (`int64`, `[]any` of strings, etc.).
  - `module.OptionsHash(normalized map[string]any) string` — deterministic `sha256:` hash: JSON-encode with sorted keys, hash the bytes.

- [ ] **Step 1: Write failing test `internal/module/options_test.go`**

```go
package module_test

import (
	"reflect"
	"testing"

	"github.com/JtheGunner/omnishell/internal/module"
)

func schema() map[string]module.OptionSchema {
	return map[string]module.OptionSchema{
		"ctrl_r":       {Type: "bool", Default: true},
		"default_opts": {Type: "string", Default: "--reverse"},
		"size":         {Type: "int", Default: int64(50000)},
		"replace":      {Type: "list<enum>", Values: []string{"ls", "cat", "find"}, Default: []any{}},
	}
}

func TestValidateOptionsFillsDefaults(t *testing.T) {
	got, err := module.ValidateOptions(schema(), map[string]any{"ctrl_r": false})
	if err != nil {
		t.Fatalf("ValidateOptions: %v", err)
	}
	want := map[string]any{
		"ctrl_r":       false,
		"default_opts": "--reverse",
		"size":         int64(50000),
		"replace":      []string{},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v\nwant %#v", got, want)
	}
}

func TestValidateOptionsCoercesTomlTypes(t *testing.T) {
	got, err := module.ValidateOptions(schema(), map[string]any{
		"size":    int64(10),
		"replace": []any{"ls", "cat"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got["size"] != int64(10) {
		t.Fatalf("size = %#v", got["size"])
	}
	if !reflect.DeepEqual(got["replace"], []string{"ls", "cat"}) {
		t.Fatalf("replace = %#v", got["replace"])
	}
}

func TestValidateOptionsRejectsUnknownKey(t *testing.T) {
	_, err := module.ValidateOptions(schema(), map[string]any{"bogus": 1})
	if err == nil {
		t.Fatal("want error for unknown key")
	}
}

func TestValidateOptionsRejectsEnumNotInValues(t *testing.T) {
	_, err := module.ValidateOptions(schema(), map[string]any{"replace": []any{"grep"}})
	if err == nil {
		t.Fatal("want error for value not in enum")
	}
}

func TestParseValue(t *testing.T) {
	s := schema()
	if v, _ := s["ctrl_r"].ParseValue("false"); v != false {
		t.Fatalf("bool parse = %#v", v)
	}
	if v, _ := s["size"].ParseValue("42"); v != int64(42) {
		t.Fatalf("int parse = %#v", v)
	}
	v, err := s["replace"].ParseValue("ls, find")
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := v.([]string); len(got) != 2 || got[0] != "ls" || got[1] != "find" {
		t.Fatalf("list parse = %#v", v)
	}
	if _, err := s["replace"].ParseValue("ls, grep"); err == nil {
		t.Fatal("want error for grep not in values")
	}
}

func TestOptionsHashDeterministicAndOrderIndependent(t *testing.T) {
	a, _ := module.ValidateOptions(schema(), map[string]any{"ctrl_r": true, "size": int64(1)})
	b, _ := module.ValidateOptions(schema(), map[string]any{"size": int64(1), "ctrl_r": true})
	if module.OptionsHash(a) != module.OptionsHash(b) {
		t.Fatal("hash depends on input order")
	}
	c, _ := module.ValidateOptions(schema(), map[string]any{"ctrl_r": false})
	if module.OptionsHash(a) == module.OptionsHash(c) {
		t.Fatal("different options hashed equal")
	}
}
```

- [ ] **Step 2: Run, expect failure**

Run: `go test ./internal/module/ -run Option -v` → FAIL (undefined).

- [ ] **Step 3: Implement `internal/module/options.go`**

```go
package module

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// OptionError is a structured option-validation error (CLI exit code 2).
type OptionError struct {
	Key string
	Msg string
}

func (e OptionError) Error() string { return fmt.Sprintf("option %q: %s", e.Key, e.Msg) }

// ParseValue converts a CLI string into the canonical typed value for the schema.
func (s OptionSchema) ParseValue(raw string) (any, error) {
	switch s.Type {
	case "bool":
		return strconv.ParseBool(strings.TrimSpace(raw))
	case "int":
		return strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	case "string":
		return raw, nil
	case "enum":
		v := strings.TrimSpace(raw)
		if !toSet(s.Values)[v] {
			return nil, fmt.Errorf("%q is not one of %s", v, strings.Join(s.Values, ", "))
		}
		return v, nil
	case "list<string>", "list<enum>":
		if strings.TrimSpace(raw) == "" {
			return []string{}, nil
		}
		parts := strings.Split(raw, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			v := strings.TrimSpace(p)
			if s.Type == "list<enum>" && !toSet(s.Values)[v] {
				return nil, fmt.Errorf("%q is not one of %s", v, strings.Join(s.Values, ", "))
			}
			out = append(out, v)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unknown option type %q", s.Type)
	}
}

// ValidateOptions returns a new fully-populated, canonically-typed option map.
func ValidateOptions(schema map[string]OptionSchema, in map[string]any) (map[string]any, error) {
	for k := range in {
		if _, ok := schema[k]; !ok {
			return nil, OptionError{Key: k, Msg: "not a known option for this module"}
		}
	}
	out := make(map[string]any, len(schema))
	for key, spec := range schema {
		raw, present := in[key]
		if !present {
			out[key] = defaultFor(spec)
			continue
		}
		v, err := coerce(spec, raw)
		if err != nil {
			return nil, OptionError{Key: key, Msg: err.Error()}
		}
		out[key] = v
	}
	return out, nil
}

func defaultFor(spec OptionSchema) any {
	if spec.Default != nil {
		v, err := coerce(spec, spec.Default)
		if err == nil {
			return v
		}
	}
	switch spec.Type {
	case "bool":
		return false
	case "int":
		return int64(0)
	case "string", "enum":
		return ""
	default:
		return []string{}
	}
}

func coerce(spec OptionSchema, raw any) (any, error) {
	switch spec.Type {
	case "bool":
		b, ok := raw.(bool)
		if !ok {
			return nil, fmt.Errorf("expected a boolean, got %T", raw)
		}
		return b, nil
	case "int":
		switch n := raw.(type) {
		case int64:
			return n, nil
		case int:
			return int64(n), nil
		default:
			return nil, fmt.Errorf("expected an integer, got %T", raw)
		}
	case "string":
		s, ok := raw.(string)
		if !ok {
			return nil, fmt.Errorf("expected a string, got %T", raw)
		}
		return s, nil
	case "enum":
		s, ok := raw.(string)
		if !ok {
			return nil, fmt.Errorf("expected a string, got %T", raw)
		}
		if !toSet(spec.Values)[s] {
			return nil, fmt.Errorf("%q is not one of %s", s, strings.Join(spec.Values, ", "))
		}
		return s, nil
	case "list<string>", "list<enum>":
		list, ok := toStringSlice(raw)
		if !ok {
			return nil, fmt.Errorf("expected a list of strings, got %T", raw)
		}
		if spec.Type == "list<enum>" {
			for _, v := range list {
				if !toSet(spec.Values)[v] {
					return nil, fmt.Errorf("%q is not one of %s", v, strings.Join(spec.Values, ", "))
				}
			}
		}
		return append([]string{}, list...), nil
	default:
		return nil, fmt.Errorf("unknown option type %q", spec.Type)
	}
}

// OptionsHash is a deterministic content hash of a normalized option map.
func OptionsHash(normalized map[string]any) string {
	keys := make([]string, 0, len(normalized))
	for k := range normalized {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	b.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		kb, _ := json.Marshal(k)
		vb, _ := json.Marshal(normalized[k])
		b.Write(kb)
		b.WriteByte(':')
		b.Write(vb)
	}
	b.WriteByte('}')

	sum := sha256.Sum256([]byte(b.String()))
	return "sha256:" + hex.EncodeToString(sum[:])
}
```

- [ ] **Step 4: Run, expect pass**

Run: `go test ./internal/module/ -v` → PASS.

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "feat: add module option schema parsing, validation, hashing

Claude-Session: https://claude.ai/code/session_012MEGbPB16gL53nKQwspocK"
```

---

## Task 8: `module` package — `Module` accessor & registry

**Files:**
- Modify: `internal/module` — add `internal/module/module.go`, `internal/module/registry.go`
- Test: `internal/module/registry_test.go`
- Create fixtures: `internal/module/testdata/builtin/completion/manifest.toml` (+ `zsh.tmpl`, `bash.tmpl`), `internal/module/testdata/builtin/fzf/manifest.toml` (+ templates + `hooks/check.sh`), `internal/module/testdata/user/fzf/manifest.toml` (override), `internal/module/testdata/user/mymod/manifest.toml` (+ `zsh.tmpl`)

**Interfaces:**
- Consumes: `module.ParseManifest`, `module.ValidateManifest` (Tasks 6).
- Produces:
  - `module.Source` string type: `module.SourceBuiltin = "builtin"`, `module.SourceUser = "user"`.
  - `module.Module struct { Manifest Manifest; FS fs.FS; Root string; Source Source }` — `FS` is rooted at the module folder; `Root` is a display path.
  - `(Module) Template(shell string) (string, bool, error)` — reads `<shell>.tmpl` from `FS`; missing file → `("", false, nil)`.
  - `(Module) HasHook(name string) bool` — true if `hooks/<name>.sh` exists in `FS` (`name` ∈ `check`,`install`,`remove`).
  - `(Module) HookPath(name string) string` — absolute path on disk to the hook (`Root/hooks/<name>.sh`); only valid for `SourceUser` and built-ins materialised to disk (see note).
  - `module.Registry struct` with unexported fields.
  - `module.LoadRegistry(builtin fs.FS, userDir string) (Registry, error)` — walks `builtin` top-level dirs and `userDir` top-level dirs; each must contain a valid `manifest.toml` whose `module.id` equals the folder name; user modules override built-ins of the same id. A malformed module folder is a hard error naming the folder.
  - `(Registry) All() []Module` — sorted by `Manifest.Module.ID`.
  - `(Registry) Get(id string) (Module, bool)`.
  - `(Registry) Overrides() []string` — ids where a user module shadows a built-in, sorted.
- **Hook execution note:** hooks in the embedded FS cannot be executed directly. `engine` (Task 21) materialises a needed hook to a temp file with `0o700` before running it. `module` only needs to expose the hook bytes: add `(Module) ReadHook(name string) ([]byte, error)`.

- [ ] **Step 1: Create fixtures**

`internal/module/testdata/builtin/completion/manifest.toml`:
```toml
[module]
id = "completion"
name = "Completion"
description = "compinit + case-insensitive matching"
version = "1.0.0"
schema = 1
platforms = ["macos", "linux"]
shells = ["zsh", "bash"]
```
`internal/module/testdata/builtin/completion/zsh.tmpl`:
```
autoload -Uz compinit && compinit
```
`internal/module/testdata/builtin/completion/bash.tmpl`:
```
# bash completion placeholder
```
`internal/module/testdata/builtin/fzf/manifest.toml`: copy `testdata/fzf-manifest.toml` from Task 6.
`internal/module/testdata/builtin/fzf/zsh.tmpl`:
```
# fzf zsh (builtin)
```
`internal/module/testdata/builtin/fzf/bash.tmpl`:
```
# fzf bash (builtin)
```
`internal/module/testdata/builtin/fzf/hooks/check.sh`:
```sh
#!/bin/sh
command -v fzf >/dev/null 2>&1
```
`internal/module/testdata/user/fzf/manifest.toml`: same as builtin fzf but `version = "9.9.9"`.
`internal/module/testdata/user/fzf/zsh.tmpl`:
```
# fzf zsh (user override)
```
`internal/module/testdata/user/fzf/bash.tmpl`:
```
# fzf bash (user override)
```
`internal/module/testdata/user/mymod/manifest.toml`:
```toml
[module]
id = "mymod"
name = "My Module"
description = "custom"
version = "0.1.0"
schema = 1
platforms = ["linux"]
shells = ["bash"]
```
`internal/module/testdata/user/mymod/bash.tmpl`:
```
export MYMOD=1
```

- [ ] **Step 2: Write failing test `internal/module/registry_test.go`**

```go
package module_test

import (
	"os"
	"testing"

	"github.com/JtheGunner/omnishell/internal/module"
)

func TestLoadRegistryMergesBuiltinAndUser(t *testing.T) {
	reg, err := module.LoadRegistry(os.DirFS("testdata/builtin"), "testdata/user")
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	all := reg.All()
	if len(all) != 3 {
		t.Fatalf("modules = %d, want 3 (completion, fzf, mymod)", len(all))
	}
	if all[0].Manifest.Module.ID != "completion" || all[1].Manifest.Module.ID != "fzf" || all[2].Manifest.Module.ID != "mymod" {
		t.Fatalf("not sorted by id: %v", ids(all))
	}

	fzf, ok := reg.Get("fzf")
	if !ok {
		t.Fatal("fzf missing")
	}
	if fzf.Source != module.SourceUser || fzf.Manifest.Module.Version != "9.9.9" {
		t.Fatalf("fzf not overridden by user module: %+v", fzf.Source)
	}
	body, has, err := fzf.Template("zsh")
	if err != nil || !has {
		t.Fatalf("fzf zsh template: has=%v err=%v", has, err)
	}
	if body != "# fzf zsh (user override)\n" {
		t.Fatalf("template body = %q", body)
	}

	comp, _ := reg.Get("completion")
	if comp.Source != module.SourceBuiltin {
		t.Fatalf("completion source = %v", comp.Source)
	}
	if _, has, _ := comp.Template("zsh"); !has {
		t.Fatal("completion should have a zsh template")
	}
	if comp.HasHook("check") {
		t.Fatal("completion should have no check hook")
	}

	if got := reg.Overrides(); len(got) != 1 || got[0] != "fzf" {
		t.Fatalf("overrides = %v, want [fzf]", got)
	}
}

func TestLoadRegistryRejectsFolderNameMismatch(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(dir+"/wrongname", 0o755)
	os.WriteFile(dir+"/wrongname/manifest.toml", []byte(
		"[module]\nid=\"other\"\nname=\"x\"\ndescription=\"x\"\nversion=\"1\"\nschema=1\nplatforms=[\"linux\"]\nshells=[\"bash\"]\n"), 0o644)
	if _, err := module.LoadRegistry(os.DirFS(t.TempDir()), dir); err == nil {
		t.Fatal("want error when folder name != module.id")
	}
}

func ids(ms []module.Module) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.Manifest.Module.ID
	}
	return out
}
```

- [ ] **Step 3: Run, expect failure**

Run: `go test ./internal/module/ -run Registry -v` → FAIL (undefined).

- [ ] **Step 4: Implement `internal/module/module.go`**

```go
package module

import (
	"io/fs"
	"path"
)

// Source says where a module came from.
type Source string

const (
	SourceBuiltin Source = "builtin"
	SourceUser    Source = "user"
)

// Module is a manifest plus access to its folder contents.
type Module struct {
	Manifest Manifest
	FS       fs.FS  // rooted at the module folder
	Root     string // display path
	Source   Source
}

// Template returns the body of <shell>.tmpl, or ("", false, nil) if absent.
func (m Module) Template(shell string) (string, bool, error) {
	data, err := fs.ReadFile(m.FS, shell+".tmpl")
	if err != nil {
		if _, isPathErr := err.(*fs.PathError); isPathErr || err == fs.ErrNotExist {
			return "", false, nil
		}
		return "", false, nil
	}
	return string(data), true, nil
}

// HasHook reports whether hooks/<name>.sh exists.
func (m Module) HasHook(name string) bool {
	_, err := fs.Stat(m.FS, path.Join("hooks", name+".sh"))
	return err == nil
}

// ReadHook returns the bytes of hooks/<name>.sh.
func (m Module) ReadHook(name string) ([]byte, error) {
	return fs.ReadFile(m.FS, path.Join("hooks", name+".sh"))
}
```

- [ ] **Step 5: Implement `internal/module/registry.go`**

```go
package module

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// Registry is the merged set of built-in and user modules.
type Registry struct {
	byID      map[string]Module
	overrides map[string]bool
}

// LoadRegistry walks the built-in FS and the user modules directory.
func LoadRegistry(builtin fs.FS, userDir string) (Registry, error) {
	r := Registry{byID: map[string]Module{}, overrides: map[string]bool{}}

	if builtin != nil {
		if err := loadFrom(builtin, ".", SourceBuiltin, "<builtin>", r.byID, nil); err != nil {
			return Registry{}, err
		}
	}
	if userDir != "" {
		if info, err := os.Stat(userDir); err == nil && info.IsDir() {
			existed := map[string]bool{}
			for id := range r.byID {
				existed[id] = true
			}
			if err := loadFrom(os.DirFS(userDir), ".", SourceUser, userDir, r.byID, func(id string) {
				if existed[id] {
					r.overrides[id] = true
				}
			}); err != nil {
				return Registry{}, err
			}
		}
	}
	return r, nil
}

func loadFrom(fsys fs.FS, root string, src Source, display string, into map[string]Module, onLoad func(string)) error {
	entries, err := fs.ReadDir(fsys, root)
	if err != nil {
		return fmt.Errorf("read modules dir: %w", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		folder := e.Name()
		sub, err := fs.Sub(fsys, folder)
		if err != nil {
			return err
		}
		data, err := fs.ReadFile(sub, "manifest.toml")
		if err != nil {
			return fmt.Errorf("module %q: missing manifest.toml", folder)
		}
		mf, err := ParseManifest(data)
		if err != nil {
			return fmt.Errorf("module %q: %w", folder, err)
		}
		if err := ValidateManifest(mf); err != nil {
			return err
		}
		if mf.Module.ID != folder {
			return fmt.Errorf("module folder %q declares id %q; they must match", folder, mf.Module.ID)
		}
		into[folder] = Module{
			Manifest: mf,
			FS:       sub,
			Root:     filepath.Join(display, folder),
			Source:   src,
		}
		if onLoad != nil {
			onLoad(folder)
		}
	}
	return nil
}

// All returns every module, sorted by id.
func (r Registry) All() []Module {
	out := make([]Module, 0, len(r.byID))
	for _, m := range r.byID {
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Manifest.Module.ID < out[j].Manifest.Module.ID
	})
	return out
}

// Get returns the module with the given id.
func (r Registry) Get(id string) (Module, bool) {
	m, ok := r.byID[id]
	return m, ok
}

// Overrides lists ids where a user module shadows a built-in.
func (r Registry) Overrides() []string {
	out := make([]string, 0, len(r.overrides))
	for id := range r.overrides {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
```

- [ ] **Step 6: Run, expect pass**

Run: `go test ./internal/module/ -v` → PASS.

- [ ] **Step 7: Commit**

```bash
git add -A
git commit -m "feat: add module accessor and built-in/user registry

Claude-Session: https://claude.ai/code/session_012MEGbPB16gL53nKQwspocK"
```

---

## Task 9: `graph` package — dependency ordering

**Files:**
- Create: `internal/graph/graph.go`
- Test: `internal/graph/graph_test.go`

**Interfaces:**
- Consumes: `module.Manifest` (for `Requires`/`After`).
- Produces:
  - `graph.MissingRequireError struct { Module, Requires string }` implementing `error`.
  - `graph.CycleError struct { Cycle []string }` implementing `error` (message joins the cycle with `" -> "`).
  - `graph.Order(active map[string]module.Manifest) ([]string, error)` — returns module ids in load order. Edges: for each `m`, every `r` in `m.Requires` must be a key of `active` (else `MissingRequireError`); every `d` in `m.Requires` ∪ (`m.After` ∩ keys(active)) becomes an edge `d → m`. Result is a stable topological sort: at each step pick the ready node with the smallest id. A cycle yields `CycleError` listing the involved ids in sorted order.

- [ ] **Step 1: Write failing test `internal/graph/graph_test.go`**

```go
package graph_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/JtheGunner/omnishell/internal/graph"
	"github.com/JtheGunner/omnishell/internal/module"
)

func mf(id string, requires, after []string) module.Manifest {
	return module.Manifest{
		Module:   module.ModuleMeta{ID: id, Version: "1", Schema: 1},
		Requires: requires,
		After:    after,
	}
}

func TestOrderStableTopoSort(t *testing.T) {
	active := map[string]module.Manifest{
		"syntax-highlighting": mf("syntax-highlighting", nil, []string{"completion", "fzf", "autosuggestions"}),
		"autosuggestions":     mf("autosuggestions", nil, []string{"completion", "fzf"}),
		"fzf":                 mf("fzf", []string{"completion"}, nil),
		"completion":          mf("completion", nil, nil),
		"history":             mf("history", nil, []string{"completion"}),
	}
	got, err := graph.Order(active)
	if err != nil {
		t.Fatalf("Order: %v", err)
	}
	want := []string{"completion", "fzf", "history", "autosuggestions", "syntax-highlighting"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("order = %v\nwant  %v", got, want)
	}
}

func TestOrderMissingRequire(t *testing.T) {
	active := map[string]module.Manifest{
		"fzf": mf("fzf", []string{"completion"}, nil),
	}
	_, err := graph.Order(active)
	var mre graph.MissingRequireError
	if !errors.As(err, &mre) || mre.Module != "fzf" || mre.Requires != "completion" {
		t.Fatalf("err = %v, want MissingRequireError{fzf, completion}", err)
	}
}

func TestOrderIgnoresInactiveAfter(t *testing.T) {
	active := map[string]module.Manifest{
		"fzf": mf("fzf", nil, []string{"completion"}), // completion not active
	}
	got, err := graph.Order(active)
	if err != nil || !reflect.DeepEqual(got, []string{"fzf"}) {
		t.Fatalf("got %v err %v", got, err)
	}
}

func TestOrderCycle(t *testing.T) {
	active := map[string]module.Manifest{
		"a": mf("a", []string{"b"}, nil),
		"b": mf("b", []string{"a"}, nil),
	}
	_, err := graph.Order(active)
	var ce graph.CycleError
	if !errors.As(err, &ce) {
		t.Fatalf("err = %v, want CycleError", err)
	}
}
```

- [ ] **Step 2: Run, expect failure**

Run: `go test ./internal/graph/ -v` → FAIL (undefined).

- [ ] **Step 3: Implement `internal/graph/graph.go`**

```go
// Package graph computes the load order of active modules from their
// requires (hard) and after (soft) edges.
package graph

import (
	"fmt"
	"sort"

	"github.com/JtheGunner/omnishell/internal/module"
)

// MissingRequireError is returned when a required module is not active.
type MissingRequireError struct {
	Module   string
	Requires string
}

func (e MissingRequireError) Error() string {
	return fmt.Sprintf("module %q requires %q, which is not enabled", e.Module, e.Requires)
}

// CycleError is returned when the dependency edges contain a cycle.
type CycleError struct {
	Cycle []string
}

func (e CycleError) Error() string {
	return "dependency cycle among modules: " + joinArrow(e.Cycle)
}

func joinArrow(ids []string) string {
	out := ""
	for i, id := range ids {
		if i > 0 {
			out += " -> "
		}
		out += id
	}
	return out
}

// Order returns the active module ids in load order.
func Order(active map[string]module.Manifest) ([]string, error) {
	deps := make(map[string]map[string]bool, len(active)) // node -> set of prerequisites
	for id := range active {
		deps[id] = map[string]bool{}
	}
	for id, mf := range active {
		for _, r := range mf.Requires {
			if _, ok := active[r]; !ok {
				return nil, MissingRequireError{Module: id, Requires: r}
			}
			deps[id][r] = true
		}
		for _, a := range mf.After {
			if _, ok := active[a]; ok {
				deps[id][a] = true
			}
		}
	}

	var order []string
	done := map[string]bool{}
	for len(order) < len(active) {
		ready := make([]string, 0)
		for id := range active {
			if done[id] {
				continue
			}
			ok := true
			for pre := range deps[id] {
				if !done[pre] {
					ok = false
					break
				}
			}
			if ok {
				ready = append(ready, id)
			}
		}
		if len(ready) == 0 {
			remaining := make([]string, 0)
			for id := range active {
				if !done[id] {
					remaining = append(remaining, id)
				}
			}
			sort.Strings(remaining)
			return nil, CycleError{Cycle: remaining}
		}
		sort.Strings(ready)
		order = append(order, ready[0])
		done[ready[0]] = true
	}
	return order, nil
}
```

- [ ] **Step 4: Run, expect pass**

Run: `go test ./internal/graph/ -v` → PASS.

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "feat: add module dependency graph ordering

Claude-Session: https://claude.ai/code/session_012MEGbPB16gL53nKQwspocK"
```

---

## Task 10: `render` package — template rendering

**Files:**
- Create: `internal/render/render.go`
- Test: `internal/render/render_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `render.Context struct { Options map[string]any; Platform string; Shell string; VendorDir string; ConfigDir string; Bin map[string]string; Active map[string]bool }`.
  - `render.Render(tmpl string, ctx Context) (string, error)` — executes `tmpl` as Go `text/template` with `Missingkey=error` and these funcs:
    - `shellquote <string>` → POSIX single-quoted (`it's` → `'it'\''s'`).
    - `pathjoin <parts...>` → `filepath.Join`.
    - `has <moduleID>` → `ctx.Active[moduleID]`.
  - The template is executed against `ctx` directly (so `.Options.ctrl_r`, `.Platform`, `.VendorDir`, etc. work).
  - Trailing whitespace on each line is trimmed and the result ends in exactly one `\n`.

- [ ] **Step 1: Write failing test `internal/render/render_test.go`**

```go
package render_test

import (
	"strings"
	"testing"

	"github.com/JtheGunner/omnishell/internal/render"
)

func baseCtx() render.Context {
	return render.Context{
		Options:   map[string]any{"ctrl_r": true, "default_opts": "--height 40%", "name": "it's fine"},
		Platform:  "macos",
		Shell:     "zsh",
		VendorDir: "/home/j/.config/omnishell/vendor",
		ConfigDir: "/home/j/.config/omnishell",
		Bin:       map[string]string{"fzf": "/opt/homebrew/bin/fzf"},
		Active:    map[string]bool{"completion": true},
	}
}

func TestRenderConditionalAndShellquote(t *testing.T) {
	tmpl := `{{ if .Options.ctrl_r }}bindkey ^R{{ end }}
export OPTS={{ .Options.default_opts | shellquote }}
name={{ .Options.name | shellquote }}
vendor={{ pathjoin .VendorDir "fzf" }}
{{ if has "completion" }}need-completion{{ end }}`
	got, err := render.Render(tmpl, baseCtx())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	want := "bindkey ^R\n" +
		"export OPTS='--height 40%'\n" +
		"name='it'\\''s fine'\n" +
		"vendor=/home/j/.config/omnishell/vendor/fzf\n" +
		"need-completion\n"
	if got != want {
		t.Fatalf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestRenderUnknownKeyIsError(t *testing.T) {
	_, err := render.Render(`{{ .Options.nope }}`, baseCtx())
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("err = %v, want a missing-key error mentioning nope", err)
	}
}
```

- [ ] **Step 2: Run, expect failure**

Run: `go test ./internal/render/ -v` → FAIL (undefined).

- [ ] **Step 3: Implement `internal/render/render.go`**

```go
// Package render executes a module's shell snippet template.
package render

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"text/template"
)

// Context is the data and helper set available to a module template.
type Context struct {
	Options   map[string]any
	Platform  string
	Shell     string
	VendorDir string
	ConfigDir string
	Bin       map[string]string
	Active    map[string]bool
}

// Render executes tmpl against ctx.
func Render(tmpl string, ctx Context) (string, error) {
	funcs := template.FuncMap{
		"shellquote": shellquote,
		"pathjoin":   func(parts ...string) string { return filepath.Join(parts...) },
		"has":        func(id string) bool { return ctx.Active[id] },
	}
	t, err := template.New("snippet").Option("missingkey=error").Funcs(funcs).Parse(tmpl)
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, ctx); err != nil {
		return "", fmt.Errorf("render template: %w", err)
	}
	return normalise(buf.String()), nil
}

func shellquote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func normalise(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
	}
	out := strings.Join(lines, "\n")
	out = strings.Trim(out, "\n")
	if out == "" {
		return ""
	}
	return out + "\n"
}
```

- [ ] **Step 4: Run, expect pass**

Run: `go test ./internal/render/ -v` → PASS.

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "feat: add module snippet template renderer

Claude-Session: https://claude.ai/code/session_012MEGbPB16gL53nKQwspocK"
```

---

## Task 11: `initfile` package — build, hash, detect hand-edits

**Files:**
- Create: `internal/initfile/initfile.go`
- Test: `internal/initfile/initfile_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `initfile.Section struct { ID string; Version string; Body string }` — `Body` is the rendered snippet (already ends in `\n`, or is empty).
  - `initfile.ContentHash(sections []Section) string` — `sha256:` hash over a canonical serialization: for each section in the given order, `">>>" + ID + "\x00" + Version + "\x00" + Body + "<<<\n"`, concatenated. Order-sensitive.
  - `initfile.Build(shell string, sections []Section, generatedAt time.Time) string` — the full file: header comment block (with `GENERATED BY omnishell`, source-of-truth path note, regenerate hint, `Generated:` RFC3339 timestamp, `Content hash: <ContentHash>`), then a blank line, then each non-empty section wrapped as:
    ```
    # >>> omnishell:<id> (v<version>) >>>
    <body>
    # <<< omnishell:<id> <<<
    ```
    separated by one blank line. Sections with an empty `Body` are skipped entirely.
  - `initfile.ParseHeaderHash(content string) (string, bool)` — extracts the `Content hash:` value from the header.
  - `initfile.DetectHandEdit(content string, sections []Section) (edited bool, err error)` — recomputes `ContentHash(sections)` and compares to the header hash; if the header hash is absent, returns `edited=true`.
- **Note:** `Build` never varies by anything except its inputs → deterministic. Timestamp lives only in the header, not in the hashed content.

- [ ] **Step 1: Write failing test `internal/initfile/initfile_test.go`**

```go
package initfile_test

import (
	"strings"
	"testing"
	"time"

	"github.com/JtheGunner/omnishell/internal/initfile"
)

func sections() []initfile.Section {
	return []initfile.Section{
		{ID: "completion", Version: "1.0.0", Body: "autoload -Uz compinit && compinit\n"},
		{ID: "fzf", Version: "1.2.0", Body: "source fzf.zsh\n"},
		{ID: "empty", Version: "1.0.0", Body: ""},
	}
}

func TestBuildStructure(t *testing.T) {
	ts := time.Date(2026, 9, 2, 22, 41, 3, 0, time.UTC)
	got := initfile.Build("zsh", sections(), ts)

	if !strings.Contains(got, "GENERATED BY omnishell") {
		t.Fatal("missing generated header")
	}
	if !strings.Contains(got, "# >>> omnishell:completion (v1.0.0) >>>") ||
		!strings.Contains(got, "# <<< omnishell:completion <<<") {
		t.Fatalf("missing completion markers:\n%s", got)
	}
	if strings.Contains(got, "omnishell:empty") {
		t.Fatal("empty section should be skipped")
	}
	if !strings.Contains(got, "Content hash: "+initfile.ContentHash(sections())) {
		t.Fatal("header hash mismatch")
	}
	if strings.Index(got, "omnishell:completion") > strings.Index(got, "omnishell:fzf") {
		t.Fatal("section order not preserved")
	}
}

func TestDetectHandEdit(t *testing.T) {
	ts := time.Date(2026, 9, 2, 22, 41, 3, 0, time.UTC)
	content := initfile.Build("zsh", sections(), ts)

	edited, err := initfile.DetectHandEdit(content, sections())
	if err != nil {
		t.Fatal(err)
	}
	if edited {
		t.Fatal("freshly built file reported as hand-edited")
	}

	tampered := strings.Replace(content, "autoload -Uz compinit && compinit", "rm -rf /", 1)
	edited, _ = initfile.DetectHandEdit(tampered, sections())
	if !edited {
		t.Fatal("tampered file not detected")
	}

	edited, _ = initfile.DetectHandEdit("no header here", sections())
	if !edited {
		t.Fatal("headerless content should count as edited")
	}
}

func TestContentHashOrderSensitive(t *testing.T) {
	s := sections()
	swapped := []initfile.Section{s[1], s[0], s[2]}
	if initfile.ContentHash(s) == initfile.ContentHash(swapped) {
		t.Fatal("hash should depend on section order")
	}
}
```

- [ ] **Step 2: Run, expect failure**

Run: `go test ./internal/initfile/ -v` → FAIL (undefined).

- [ ] **Step 3: Implement `internal/initfile/initfile.go`**

```go
// Package initfile assembles the tool-managed shell init file, hashes its
// module sections, and detects manual edits.
package initfile

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// Section is one module's rendered contribution to an init file.
type Section struct {
	ID      string
	Version string
	Body    string
}

// ContentHash is an order-sensitive hash of the module sections only.
func ContentHash(sections []Section) string {
	var b strings.Builder
	for _, s := range sections {
		b.WriteString(">>>")
		b.WriteString(s.ID)
		b.WriteByte(0)
		b.WriteString(s.Version)
		b.WriteByte(0)
		b.WriteString(s.Body)
		b.WriteString("<<<\n")
	}
	sum := sha256.Sum256([]byte(b.String()))
	return "sha256:" + hex.EncodeToString(sum[:])
}

const headerSentinel = "GENERATED BY omnishell"

// Build returns the full init file content for a shell.
func Build(shell string, sections []Section, generatedAt time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# ─────────────────────────────────────────────────────────────\n")
	fmt.Fprintf(&b, "#  %s — DO NOT EDIT\n", headerSentinel)
	fmt.Fprintf(&b, "#  Source of truth: ~/.config/omnishell/config.toml\n")
	fmt.Fprintf(&b, "#  Regenerate:      omnishell apply\n")
	fmt.Fprintf(&b, "#  Shell:           %s\n", shell)
	fmt.Fprintf(&b, "#  Generated:       %s\n", generatedAt.UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "#  Content hash:    %s\n", ContentHash(sections))
	fmt.Fprintf(&b, "# ─────────────────────────────────────────────────────────────\n")

	for _, s := range sections {
		if strings.TrimSpace(s.Body) == "" {
			continue
		}
		body := s.Body
		if !strings.HasSuffix(body, "\n") {
			body += "\n"
		}
		fmt.Fprintf(&b, "\n# >>> omnishell:%s (v%s) >>>\n", s.ID, s.Version)
		b.WriteString(body)
		fmt.Fprintf(&b, "# <<< omnishell:%s <<<\n", s.ID)
	}
	return b.String()
}

// ParseHeaderHash extracts the "Content hash:" value from the header.
func ParseHeaderHash(content string) (string, bool) {
	for _, line := range strings.Split(content, "\n") {
		if i := strings.Index(line, "Content hash:"); i >= 0 {
			return strings.TrimSpace(line[i+len("Content hash:"):]), true
		}
		if !strings.HasPrefix(strings.TrimSpace(line), "#") && strings.TrimSpace(line) != "" {
			break // past the header
		}
	}
	return "", false
}

// DetectHandEdit reports whether content diverges from the expected sections.
func DetectHandEdit(content string, sections []Section) (bool, error) {
	have, ok := ParseHeaderHash(content)
	if !ok {
		return true, nil
	}
	return have != ContentHash(sections), nil
}
```

- [ ] **Step 4: Run, expect pass**

Run: `go test ./internal/initfile/ -v` → PASS.

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "feat: add init file builder with content hashing

Claude-Session: https://claude.ai/code/session_012MEGbPB16gL53nKQwspocK"
```

---

## Task 12: `rcfile` package — the single source marker block

**Files:**
- Create: `internal/rcfile/rcfile.go`
- Test: `internal/rcfile/rcfile_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `rcfile.BlockStart = "# >>> omnishell >>>"`, `rcfile.BlockEnd = "# <<< omnishell <<<"` (consts).
  - `rcfile.SourceLine(shell, initPath string) string` — e.g. `[[ -f "$HOME/.config/omnishell/init.zsh" ]] && source "$HOME/.config/omnishell/init.zsh"`. `initPath` is passed already `$HOME`-relativised by the caller when possible; if it is absolute it is used verbatim. For `bash` the guard uses `[ -f ... ]` (single bracket) for POSIX-bash safety; for `zsh` it uses `[[ -f ... ]]`.
  - `rcfile.BlockPresent(content string) bool`.
  - `rcfile.EnsureBlock(content, shell, initPath string) (newContent string, changed bool)` — if a block exists, replace its inner line with the current `SourceLine` (returns `changed` only if the text actually differs); else append `\n<BlockStart>\n<sourceLine>\n<BlockEnd>\n` (ensuring exactly one blank line before it, and a trailing newline).
  - `rcfile.RemoveBlock(content string) (newContent string, changed bool)` — removes the block and any immediately preceding blank line; leaves the rest byte-identical.

- [ ] **Step 1: Write failing test `internal/rcfile/rcfile_test.go`**

```go
package rcfile_test

import (
	"strings"
	"testing"

	"github.com/JtheGunner/omnishell/internal/rcfile"
)

func TestEnsureBlockAppendsWhenAbsent(t *testing.T) {
	in := "export PATH=/x\nalias g=git\n"
	out, changed := rcfile.EnsureBlock(in, "zsh", "$HOME/.config/omnishell/init.zsh")
	if !changed {
		t.Fatal("changed = false, want true")
	}
	if !strings.HasPrefix(out, in) {
		t.Fatalf("existing content not preserved:\n%s", out)
	}
	if !rcfile.BlockPresent(out) {
		t.Fatal("block not present after EnsureBlock")
	}
	if !strings.Contains(out, `[[ -f "$HOME/.config/omnishell/init.zsh" ]] && source "$HOME/.config/omnishell/init.zsh"`) {
		t.Fatalf("source line wrong:\n%s", out)
	}
}

func TestEnsureBlockIdempotent(t *testing.T) {
	in := "code\n"
	once, _ := rcfile.EnsureBlock(in, "zsh", "$HOME/.config/omnishell/init.zsh")
	twice, changed := rcfile.EnsureBlock(once, "zsh", "$HOME/.config/omnishell/init.zsh")
	if changed || once != twice {
		t.Fatalf("second EnsureBlock changed the file:\n%s", twice)
	}
}

func TestEnsureBlockBashUsesSingleBracket(t *testing.T) {
	out, _ := rcfile.EnsureBlock("", "bash", "$HOME/.config/omnishell/init.bash")
	if !strings.Contains(out, `[ -f "$HOME/.config/omnishell/init.bash" ] && source`) {
		t.Fatalf("bash guard wrong:\n%s", out)
	}
}

func TestRemoveBlockRestoresOriginal(t *testing.T) {
	original := "export PATH=/x\nalias g=git\n"
	withBlock, _ := rcfile.EnsureBlock(original, "zsh", "$HOME/.config/omnishell/init.zsh")
	out, changed := rcfile.RemoveBlock(withBlock)
	if !changed {
		t.Fatal("changed = false, want true")
	}
	if out != original {
		t.Fatalf("RemoveBlock did not restore original:\n%q\nwant\n%q", out, original)
	}
	if _, changed := rcfile.RemoveBlock(out); changed {
		t.Fatal("RemoveBlock on clean file reported a change")
	}
}
```

- [ ] **Step 2: Run, expect failure**

Run: `go test ./internal/rcfile/ -v` → FAIL (undefined).

- [ ] **Step 3: Implement `internal/rcfile/rcfile.go`**

```go
// Package rcfile inserts, updates, and removes the single omnishell marker
// block in a shell rc file. The rest of the file is never touched.
package rcfile

import "strings"

const (
	BlockStart = "# >>> omnishell >>>"
	BlockEnd   = "# <<< omnishell <<<"
)

// SourceLine returns the one line that lives inside the marker block.
func SourceLine(shell, initPath string) string {
	if shell == "bash" {
		return `[ -f "` + initPath + `" ] && source "` + initPath + `"`
	}
	return `[[ -f "` + initPath + `" ]] && source "` + initPath + `"`
}

// BlockPresent reports whether the marker block exists.
func BlockPresent(content string) bool {
	return strings.Contains(content, BlockStart) && strings.Contains(content, BlockEnd)
}

func block(shell, initPath string) string {
	return BlockStart + "\n" + SourceLine(shell, initPath) + "\n" + BlockEnd + "\n"
}

// EnsureBlock guarantees the block exists with the current source line.
func EnsureBlock(content, shell, initPath string) (string, bool) {
	want := block(shell, initPath)
	start := strings.Index(content, BlockStart)
	if start >= 0 {
		endIdx := strings.Index(content[start:], BlockEnd)
		if endIdx >= 0 {
			end := start + endIdx + len(BlockEnd)
			if end < len(content) && content[end] == '\n' {
				end++
			}
			existing := content[start:end]
			if existing == want {
				return content, false
			}
			return content[:start] + want + content[end:], true
		}
	}
	trimmed := strings.TrimRight(content, "\n")
	if trimmed == "" {
		return want, true
	}
	return trimmed + "\n\n" + want, true
}

// RemoveBlock deletes the block and one preceding blank line if present.
func RemoveBlock(content string) (string, bool) {
	start := strings.Index(content, BlockStart)
	if start < 0 {
		return content, false
	}
	endIdx := strings.Index(content[start:], BlockEnd)
	if endIdx < 0 {
		return content, false
	}
	end := start + endIdx + len(BlockEnd)
	if end < len(content) && content[end] == '\n' {
		end++
	}
	pre := content[:start]
	pre = strings.TrimRight(pre, "\n")
	if pre != "" {
		pre += "\n"
	}
	return pre + content[end:], true
}
```

- [ ] **Step 4: Run, expect pass**

Run: `go test ./internal/rcfile/ -v` → PASS.

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "feat: add rc-file marker block management

Claude-Session: https://claude.ai/code/session_012MEGbPB16gL53nKQwspocK"
```

---

## Task 13: `lockfile` package — `state.lock.json`

**Files:**
- Create: `internal/lockfile/lockfile.go`
- Test: `internal/lockfile/lockfile_test.go`

**Interfaces:**
- Consumes: `atomicfile.WriteFile`.
- Produces (all JSON tags snake_case, all fields exported):
  - `lockfile.SchemaVersion = 1`.
  - `lockfile.PackageState struct { Name, Manager string; InstalledByOmnishell bool }`.
  - `lockfile.ModuleState struct { ModuleVersion string; Enabled bool; OptionsHash string; ShellsRendered []string; Packages []PackageState; VendorPaths []string; Status string }` — `Status` ∈ `"ok"`,`"degraded"`,`"disabled"`.
  - `lockfile.FileState struct { Path, ContentHash string }`.
  - `lockfile.RCState struct { Path string; BlockPresent bool }`.
  - `lockfile.Lock struct { Schema int; OmnishellVersion string; LastApply string; Platform string; PackageManager string; Modules map[string]ModuleState; InitFiles map[string]FileState; RCFiles map[string]RCState }`.
  - `lockfile.Load(path string) (Lock, bool, error)` — missing file returns `(Lock{Schema:SchemaVersion, Modules:map...}, false, nil)`; the bool is `exists`. A schema mismatch is an error.
  - `(Lock) Write(path string) error` — marshals with `MarshalIndent` (2 spaces), map keys sorted by Go's `encoding/json` (already sorted), trailing newline, atomic.
  - `(Lock) InstalledPackages(moduleID string) []PackageState` — helper returning only `InstalledByOmnishell` packages for a module.

- [ ] **Step 1: Write failing test `internal/lockfile/lockfile_test.go`**

```go
package lockfile_test

import (
	"path/filepath"
	"testing"

	"github.com/JtheGunner/omnishell/internal/lockfile"
)

func sample() lockfile.Lock {
	return lockfile.Lock{
		Schema:           lockfile.SchemaVersion,
		OmnishellVersion: "1.0.0",
		LastApply:        "2026-09-02T22:41:03Z",
		Platform:         "macos",
		PackageManager:   "brew",
		Modules: map[string]lockfile.ModuleState{
			"fzf": {
				ModuleVersion:  "1.0.0",
				Enabled:        true,
				OptionsHash:    "sha256:abc",
				ShellsRendered: []string{"zsh", "bash"},
				Packages: []lockfile.PackageState{
					{Name: "fzf", Manager: "brew", InstalledByOmnishell: true},
					{Name: "ncurses", Manager: "brew", InstalledByOmnishell: false},
				},
				Status: "ok",
			},
		},
		InitFiles: map[string]lockfile.FileState{
			"zsh": {Path: "~/.config/omnishell/init.zsh", ContentHash: "sha256:0f3a"},
		},
		RCFiles: map[string]lockfile.RCState{
			"zsh": {Path: "~/.zshrc", BlockPresent: true},
		},
	}
}

func TestWriteThenLoadRoundTrips(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.lock.json")
	if err := sample().Write(p); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, exists, err := lockfile.Load(p)
	if err != nil || !exists {
		t.Fatalf("Load: exists=%v err=%v", exists, err)
	}
	if got.Modules["fzf"].OptionsHash != "sha256:abc" || got.Modules["fzf"].Status != "ok" {
		t.Fatalf("round-trip mismatch: %+v", got.Modules["fzf"])
	}
}

func TestLoadMissingReturnsEmptyLock(t *testing.T) {
	got, exists, err := lockfile.Load(filepath.Join(t.TempDir(), "absent.json"))
	if err != nil {
		t.Fatalf("Load missing: %v", err)
	}
	if exists {
		t.Fatal("exists = true for missing file")
	}
	if got.Schema != lockfile.SchemaVersion || got.Modules == nil {
		t.Fatalf("empty lock not initialised: %+v", got)
	}
}

func TestInstalledPackagesFilters(t *testing.T) {
	got := sample().InstalledPackages("fzf")
	if len(got) != 1 || got[0].Name != "fzf" {
		t.Fatalf("InstalledPackages = %+v, want just fzf", got)
	}
}
```

- [ ] **Step 2: Run, expect failure**

Run: `go test ./internal/lockfile/ -v` → FAIL (undefined).

- [ ] **Step 3: Implement `internal/lockfile/lockfile.go`**

```go
// Package lockfile reads and writes ~/.config/omnishell/state.lock.json,
// the machine-managed record of the last apply.
package lockfile

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/JtheGunner/omnishell/internal/atomicfile"
)

// SchemaVersion is the lockfile schema this build writes and accepts.
const SchemaVersion = 1

// PackageState records one package a module depends on.
type PackageState struct {
	Name                 string `json:"name"`
	Manager              string `json:"manager"`
	InstalledByOmnishell bool   `json:"installed_by_omnishell"`
}

// ModuleState is the recorded state of one module after apply.
type ModuleState struct {
	ModuleVersion  string         `json:"module_version"`
	Enabled        bool           `json:"enabled"`
	OptionsHash    string         `json:"options_hash"`
	ShellsRendered []string       `json:"shells_rendered"`
	Packages       []PackageState `json:"packages"`
	VendorPaths    []string       `json:"vendor_paths"`
	Status         string         `json:"status"`
}

// FileState records an init file's path and content hash.
type FileState struct {
	Path        string `json:"path"`
	ContentHash string `json:"content_hash"`
}

// RCState records whether the marker block is present in an rc file.
type RCState struct {
	Path         string `json:"path"`
	BlockPresent bool   `json:"block_present"`
}

// Lock is the whole state.lock.json document.
type Lock struct {
	Schema           int                    `json:"schema"`
	OmnishellVersion string                 `json:"omnishell_version"`
	LastApply        string                 `json:"last_apply"`
	Platform         string                 `json:"platform"`
	PackageManager   string                 `json:"package_manager"`
	Modules          map[string]ModuleState `json:"modules"`
	InitFiles        map[string]FileState   `json:"init_files"`
	RCFiles          map[string]RCState     `json:"rc_files"`
}

func empty() Lock {
	return Lock{
		Schema:    SchemaVersion,
		Modules:   map[string]ModuleState{},
		InitFiles: map[string]FileState{},
		RCFiles:   map[string]RCState{},
	}
}

// Load reads the lockfile. A missing file yields an initialised empty Lock
// and exists=false.
func Load(path string) (Lock, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return empty(), false, nil
		}
		return Lock{}, false, fmt.Errorf("read lockfile: %w", err)
	}
	var l Lock
	if err := json.Unmarshal(data, &l); err != nil {
		return Lock{}, false, fmt.Errorf("parse lockfile %s: %w", path, err)
	}
	if l.Schema != SchemaVersion {
		return Lock{}, false, fmt.Errorf("lockfile %s has schema %d, expected %d", path, l.Schema, SchemaVersion)
	}
	if l.Modules == nil {
		l.Modules = map[string]ModuleState{}
	}
	if l.InitFiles == nil {
		l.InitFiles = map[string]FileState{}
	}
	if l.RCFiles == nil {
		l.RCFiles = map[string]RCState{}
	}
	return l, true, nil
}

// Write serialises the lock atomically.
func (l Lock) Write(path string) error {
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal lockfile: %w", err)
	}
	return atomicfile.WriteFile(path, append(data, '\n'), 0o644)
}

// InstalledPackages returns only the packages omnishell installed for a module.
func (l Lock) InstalledPackages(moduleID string) []PackageState {
	var out []PackageState
	for _, p := range l.Modules[moduleID].Packages {
		if p.InstalledByOmnishell {
			out = append(out, p)
		}
	}
	return out
}
```

- [ ] **Step 4: Run, expect pass**

Run: `go test ./internal/lockfile/ -v` → PASS.

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "feat: add state.lock.json read/write

Claude-Session: https://claude.ai/code/session_012MEGbPB16gL53nKQwspocK"
```

---

## Task 14: `pkgmgr` package — `Manager` interface, `Runner`, detection, mock

**Files:**
- Create: `internal/pkgmgr/pkgmgr.go`, `internal/pkgmgr/mock.go`
- Test: `internal/pkgmgr/pkgmgr_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `pkgmgr.Runner interface { Run(name string, args ...string) (stdout []byte, err error); Look(name string) (string, error) }` — `Look` wraps `exec.LookPath`.
  - `pkgmgr.ExecRunner struct { Stdout, Stderr io.Writer }` implementing `Runner` — streams child stdout/stderr to the writers **and** returns captured stdout; used for real installs so the user sees progress (and any `sudo` prompt).
  - `pkgmgr.Manager interface { Name() string; Detect() bool; IsInstalled(pkg string) (bool, error); Install(pkgs []string) error; NeedsSudo() bool }`.
  - `pkgmgr.DetectManager(goos string, r Runner) (Manager, bool)` — order: `darwin` → `[brew]`; `linux` → `[apt, dnf, pacman, zypper, apk]`. Returns the first whose `Detect()` is true.
  - `pkgmgr.MockManager struct { NameV string; DetectV bool; Installed map[string]bool; InstallCalls [][]string; SudoV bool; InstallErr error }` implementing `Manager` — `Install` appends to `InstallCalls` and marks pkgs installed unless `InstallErr` is set.
  - `pkgmgr.MockRunner struct { Responses map[string]MockResponse; Calls []string; LookOK map[string]bool }` implementing `Runner` — keyed by `"name arg1 arg2"`.
  - `pkgmgr.MockResponse struct { Out []byte; Err error }`.

- [ ] **Step 1: Write failing test `internal/pkgmgr/pkgmgr_test.go`**

```go
package pkgmgr_test

import (
	"testing"

	"github.com/JtheGunner/omnishell/internal/pkgmgr"
)

func TestDetectManagerMacOSPrefersBrew(t *testing.T) {
	r := &pkgmgr.MockRunner{LookOK: map[string]bool{"brew": true}}
	m, ok := pkgmgr.DetectManager("darwin", r)
	if !ok || m.Name() != "brew" {
		t.Fatalf("got %v ok=%v, want brew", m, ok)
	}
}

func TestDetectManagerLinuxOrder(t *testing.T) {
	r := &pkgmgr.MockRunner{LookOK: map[string]bool{"dnf": true, "pacman": true}}
	m, ok := pkgmgr.DetectManager("linux", r)
	if !ok || m.Name() != "dnf" {
		t.Fatalf("got %v ok=%v, want dnf (first in order that is present)", m, ok)
	}
}

func TestDetectManagerNoneFound(t *testing.T) {
	r := &pkgmgr.MockRunner{LookOK: map[string]bool{}}
	if _, ok := pkgmgr.DetectManager("linux", r); ok {
		t.Fatal("want ok=false when no manager present")
	}
}

func TestMockManagerInstallRecords(t *testing.T) {
	m := &pkgmgr.MockManager{NameV: "brew", DetectV: true, Installed: map[string]bool{}}
	if err := m.Install([]string{"fzf", "zoxide"}); err != nil {
		t.Fatal(err)
	}
	ok, _ := m.IsInstalled("fzf")
	if !ok || len(m.InstallCalls) != 1 {
		t.Fatalf("install not recorded: %+v", m.InstallCalls)
	}
}
```

- [ ] **Step 2: Run, expect failure**

Run: `go test ./internal/pkgmgr/ -v` → FAIL (undefined).

- [ ] **Step 3: Implement `internal/pkgmgr/pkgmgr.go`**

```go
// Package pkgmgr abstracts the system package manager (brew/apt/dnf/pacman/
// zypper/apk) plus a git-clone fallback, behind a small interface.
package pkgmgr

import (
	"bytes"
	"io"
	"os/exec"
)

// Runner executes external commands. Injectable for tests.
type Runner interface {
	Run(name string, args ...string) ([]byte, error)
	Look(name string) (string, error)
}

// ExecRunner runs real commands, streaming their output to the given writers
// while also capturing stdout for the caller.
type ExecRunner struct {
	Stdout io.Writer
	Stderr io.Writer
}

// Run executes name+args.
func (e ExecRunner) Run(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	var buf bytes.Buffer
	if e.Stdout != nil {
		cmd.Stdout = io.MultiWriter(&buf, e.Stdout)
	} else {
		cmd.Stdout = &buf
	}
	cmd.Stderr = e.Stderr
	err := cmd.Run()
	return buf.Bytes(), err
}

// Look resolves a binary in PATH.
func (e ExecRunner) Look(name string) (string, error) { return exec.LookPath(name) }

// Manager is one package manager.
type Manager interface {
	Name() string
	Detect() bool
	IsInstalled(pkg string) (bool, error)
	Install(pkgs []string) error
	NeedsSudo() bool
}

var linuxOrder = []string{"apt", "dnf", "pacman", "zypper", "apk"}

// DetectManager returns the first available manager for the OS.
func DetectManager(goos string, r Runner) (Manager, bool) {
	var names []string
	switch goos {
	case "darwin":
		names = []string{"brew"}
	case "linux":
		names = linuxOrder
	default:
		return nil, false
	}
	for _, n := range names {
		m := newManager(n, r)
		if m != nil && m.Detect() {
			return m, true
		}
	}
	return nil, false
}
```

- [ ] **Step 4: Implement `internal/pkgmgr/mock.go`**

```go
package pkgmgr

import (
	"errors"
	"strings"
)

// MockResponse is one canned command result.
type MockResponse struct {
	Out []byte
	Err error
}

// MockRunner is a Runner backed by a response table.
type MockRunner struct {
	Responses map[string]MockResponse
	Calls     []string
	LookOK    map[string]bool
}

// Run records the call and returns the canned response (empty if none).
func (m *MockRunner) Run(name string, args ...string) ([]byte, error) {
	key := strings.TrimSpace(name + " " + strings.Join(args, " "))
	m.Calls = append(m.Calls, key)
	if r, ok := m.Responses[key]; ok {
		return r.Out, r.Err
	}
	return nil, nil
}

// Look reports whether LookOK[name] is set.
func (m *MockRunner) Look(name string) (string, error) {
	if m.LookOK[name] {
		return "/usr/bin/" + name, nil
	}
	return "", errors.New("not found: " + name)
}

// MockManager is a Manager for tests.
type MockManager struct {
	NameV        string
	DetectV      bool
	Installed    map[string]bool
	InstallCalls [][]string
	SudoV        bool
	InstallErr   error
}

func (m *MockManager) Name() string   { return m.NameV }
func (m *MockManager) Detect() bool   { return m.DetectV }
func (m *MockManager) NeedsSudo() bool { return m.SudoV }

func (m *MockManager) IsInstalled(pkg string) (bool, error) {
	return m.Installed[pkg], nil
}

func (m *MockManager) Install(pkgs []string) error {
	m.InstallCalls = append(m.InstallCalls, append([]string{}, pkgs...))
	if m.InstallErr != nil {
		return m.InstallErr
	}
	for _, p := range pkgs {
		if m.Installed == nil {
			m.Installed = map[string]bool{}
		}
		m.Installed[p] = true
	}
	return nil
}
```

- [ ] **Step 5: Run, expect pass**

Run: `go test ./internal/pkgmgr/ -v` → FAIL — `newManager` undefined (implemented next task). Temporarily add a stub at the bottom of `pkgmgr.go`:

```go
func newManager(name string, r Runner) Manager { return nil } // replaced in Task 15
```
Re-run: the detection tests that expect a real manager will fail. **Instead**, split: move `TestDetectManager*` assertions to accept `newManager` from Task 15. For this task, keep only `TestMockManagerInstallRecords` and a `TestMockRunnerLook`. Commit this task with the mock + interface only; detection tests land in Task 15.

Revised Step 5: delete the three `TestDetectManager*` tests from this task's test file (they move to Task 15). Run `go test ./internal/pkgmgr/ -v` → PASS with the mock tests.

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "feat: add pkgmgr interface, exec runner, and test mocks

Claude-Session: https://claude.ai/code/session_012MEGbPB16gL53nKQwspocK"
```

---

## Task 15: `pkgmgr` package — the six concrete managers

**Files:**
- Create: `internal/pkgmgr/managers.go`
- Modify: `internal/pkgmgr/pkgmgr.go` — replace the `newManager` stub
- Test: `internal/pkgmgr/managers_test.go`

**Interfaces:**
- Consumes: `pkgmgr.Runner`, `pkgmgr.Manager` (Task 14).
- Produces:
  - `pkgmgr.newManager(name string, r Runner) Manager` — constructs the named manager (`brew`,`apt`,`dnf`,`pacman`,`zypper`,`apk`), or `nil` for an unknown name.
  - Each manager: `Detect()` = `r.Look(<bin>)` succeeds; `IsInstalled(pkg)` and `Install(pkgs)` run the commands in the table below via `r.Run`; a non-nil error from `Run` is wrapped with the command string.

| mgr | bin | NeedsSudo | IsInstalled(pkg) | Install(pkgs...) |
|-----|-----|-----------|------------------|------------------|
| brew | `brew` | false | `brew list --versions <pkg>` → installed iff exit 0 and output non-empty | `brew install <pkgs...>` |
| apt | `apt-get` | true | `dpkg-query -W -f=${Status} <pkg>` → output contains `install ok installed` | `sudo apt-get install -y <pkgs...>` |
| dnf | `dnf` | true | `rpm -q <pkg>` → exit 0 | `sudo dnf install -y <pkgs...>` |
| pacman | `pacman` | true | `pacman -Q <pkg>` → exit 0 | `sudo pacman -S --noconfirm <pkgs...>` |
| zypper | `zypper` | true | `rpm -q <pkg>` → exit 0 | `sudo zypper install -y <pkgs...>` |
| apk | `apk` | true | `apk info -e <pkg>` → output non-empty | `sudo apk add <pkgs...>` |

- **sudo announcement:** `Install` for a `NeedsSudo` manager writes a line to the runner's stderr-equivalent before running. Since the mock has no writer, the announcement is the caller's job (engine, Task 21) — managers just prepend `sudo` to the argv. Document this: the manager is responsible only for building the correct argv.

- [ ] **Step 1: Write failing test `internal/pkgmgr/managers_test.go`**

```go
package pkgmgr_test

import (
	"testing"

	"github.com/JtheGunner/omnishell/internal/pkgmgr"
)

func TestBrewIsInstalledAndInstall(t *testing.T) {
	r := &pkgmgr.MockRunner{
		LookOK: map[string]bool{"brew": true},
		Responses: map[string]pkgmgr.MockResponse{
			"brew list --versions fzf": {Out: []byte("fzf 0.54.0\n")},
			"brew list --versions ripgrep": {Out: []byte("")},
			"brew install fzf": {Out: []byte("installed")},
		},
	}
	m, ok := pkgmgr.DetectManager("darwin", r)
	if !ok {
		t.Fatal("brew not detected")
	}
	if got, _ := m.IsInstalled("fzf"); !got {
		t.Fatal("fzf should be installed")
	}
	if got, _ := m.IsInstalled("ripgrep"); got {
		t.Fatal("ripgrep should not be installed")
	}
	if err := m.Install([]string{"fzf"}); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if m.NeedsSudo() {
		t.Fatal("brew should not need sudo")
	}
}

func TestAptInstallUsesSudoArgv(t *testing.T) {
	r := &pkgmgr.MockRunner{LookOK: map[string]bool{"apt-get": true}}
	m, ok := pkgmgr.DetectManager("linux", r)
	if !ok || m.Name() != "apt" {
		t.Fatalf("got %v, want apt", m)
	}
	if !m.NeedsSudo() {
		t.Fatal("apt should need sudo")
	}
	if err := m.Install([]string{"fzf", "zoxide"}); err != nil {
		t.Fatalf("Install: %v", err)
	}
	last := r.Calls[len(r.Calls)-1]
	if last != "sudo apt-get install -y fzf zoxide" {
		t.Fatalf("apt install argv = %q", last)
	}
}

func TestPacmanIsInstalled(t *testing.T) {
	r := &pkgmgr.MockRunner{
		LookOK:    map[string]bool{"pacman": true},
		Responses: map[string]pkgmgr.MockResponse{"pacman -Q fzf": {Out: []byte("fzf 0.54.0-1\n")}},
	}
	m, _ := pkgmgr.DetectManager("linux", r)
	if got, _ := m.IsInstalled("fzf"); !got {
		t.Fatal("pacman -Q success should mean installed")
	}
}
```

Also re-add the `TestDetectManager*` tests removed from Task 14 here (they now pass because `newManager` is real).

- [ ] **Step 2: Run, expect failure**

Run: `go test ./internal/pkgmgr/ -v` → FAIL.

- [ ] **Step 3: Implement `internal/pkgmgr/managers.go`**

```go
package pkgmgr

import (
	"fmt"
	"strings"
)

type cmdManager struct {
	name       string
	bin        string
	sudo       bool
	runner     Runner
	isInstalled func(r Runner, pkg string) (bool, error)
	installArgv func(pkgs []string) []string
}

func (m cmdManager) Name() string    { return m.name }
func (m cmdManager) NeedsSudo() bool  { return m.sudo }
func (m cmdManager) Detect() bool     { _, err := m.runner.Look(m.bin); return err == nil }

func (m cmdManager) IsInstalled(pkg string) (bool, error) { return m.isInstalled(m.runner, pkg) }

func (m cmdManager) Install(pkgs []string) error {
	if len(pkgs) == 0 {
		return nil
	}
	argv := m.installArgv(pkgs)
	if _, err := m.runner.Run(argv[0], argv[1:]...); err != nil {
		return fmt.Errorf("%s: %s: %w", m.name, strings.Join(argv, " "), err)
	}
	return nil
}

func exitZero(r Runner, name string, args ...string) (bool, error) {
	_, err := r.Run(name, args...)
	return err == nil, nil
}

func outputNonEmpty(r Runner, name string, args ...string) (bool, error) {
	out, err := r.Run(name, args...)
	if err != nil {
		return false, nil
	}
	return strings.TrimSpace(string(out)) != "", nil
}

func newManager(name string, r Runner) Manager {
	switch name {
	case "brew":
		return cmdManager{
			name: "brew", bin: "brew", sudo: false, runner: r,
			isInstalled: func(r Runner, pkg string) (bool, error) {
				return outputNonEmpty(r, "brew", "list", "--versions", pkg)
			},
			installArgv: func(p []string) []string { return append([]string{"brew", "install"}, p...) },
		}
	case "apt":
		return cmdManager{
			name: "apt", bin: "apt-get", sudo: true, runner: r,
			isInstalled: func(r Runner, pkg string) (bool, error) {
				out, err := r.Run("dpkg-query", "-W", "-f=${Status}", pkg)
				if err != nil {
					return false, nil
				}
				return strings.Contains(string(out), "install ok installed"), nil
			},
			installArgv: func(p []string) []string {
				return append([]string{"sudo", "apt-get", "install", "-y"}, p...)
			},
		}
	case "dnf":
		return cmdManager{
			name: "dnf", bin: "dnf", sudo: true, runner: r,
			isInstalled: func(r Runner, pkg string) (bool, error) { return exitZero(r, "rpm", "-q", pkg) },
			installArgv: func(p []string) []string {
				return append([]string{"sudo", "dnf", "install", "-y"}, p...)
			},
		}
	case "pacman":
		return cmdManager{
			name: "pacman", bin: "pacman", sudo: true, runner: r,
			isInstalled: func(r Runner, pkg string) (bool, error) { return exitZero(r, "pacman", "-Q", pkg) },
			installArgv: func(p []string) []string {
				return append([]string{"sudo", "pacman", "-S", "--noconfirm"}, p...)
			},
		}
	case "zypper":
		return cmdManager{
			name: "zypper", bin: "zypper", sudo: true, runner: r,
			isInstalled: func(r Runner, pkg string) (bool, error) { return exitZero(r, "rpm", "-q", pkg) },
			installArgv: func(p []string) []string {
				return append([]string{"sudo", "zypper", "install", "-y"}, p...)
			},
		}
	case "apk":
		return cmdManager{
			name: "apk", bin: "apk", sudo: true, runner: r,
			isInstalled: func(r Runner, pkg string) (bool, error) {
				return outputNonEmpty(r, "apk", "info", "-e", pkg)
			},
			installArgv: func(p []string) []string { return append([]string{"sudo", "apk", "add"}, p...) },
		}
	default:
		return nil
	}
}
```

Delete the temporary `newManager` stub from `pkgmgr.go`.

- [ ] **Step 4: Run, expect pass**

Run: `go test ./internal/pkgmgr/ -v` → PASS.

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "feat: implement brew/apt/dnf/pacman/zypper/apk managers

Claude-Session: https://claude.ai/code/session_012MEGbPB16gL53nKQwspocK"
```

---

## Task 16: `pkgmgr` package — git-clone fallback

**Files:**
- Create: `internal/pkgmgr/gitfallback.go`
- Test: `internal/pkgmgr/gitfallback_test.go`

**Interfaces:**
- Consumes: `pkgmgr.Runner`, `module.Fallback` (Task 6), `render.Render` (Task 10).
- Produces:
  - `pkgmgr.FallbackContext struct { VendorDir string; Platform string; Shell string }`.
  - `pkgmgr.InstallGitFallback(fb module.Fallback, ctx FallbackContext, r Runner) (vendorPath string, err error)`:
    - Only handles `fb.Type == "git"`; other types → error `"unsupported fallback type %q"`.
    - `dest := render fb.Dest` with a template context exposing `.VendorDir`, `.Platform`, `.Shell` (reuse `render.Render` with a `render.Context` whose `VendorDir`/`Platform`/`Shell` are set and `Options` empty).
    - If `dest` already exists as a non-empty dir → treat as satisfied, return `(dest, nil)` without cloning.
    - Else run `git clone --depth 1 <fb.Repo> <dest>` via `r.Run`.
    - If `fb.Run` is non-empty → render it the same way, split on spaces (no shell), run via `r.Run`; a failure removes `dest` and returns the error.
    - Returns the resolved `dest` as `vendorPath`.
  - `pkgmgr.FallbackSatisfied(fb module.Fallback, ctx FallbackContext) (bool, string)` — returns whether `dest` exists (non-empty dir) and the resolved dest path, without side effects.

- [ ] **Step 1: Write failing test `internal/pkgmgr/gitfallback_test.go`**

```go
package pkgmgr_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/JtheGunner/omnishell/internal/module"
	"github.com/JtheGunner/omnishell/internal/pkgmgr"
)

func TestInstallGitFallbackClonesWhenAbsent(t *testing.T) {
	vendor := t.TempDir()
	r := &pkgmgr.MockRunner{}
	fb := module.Fallback{
		Type: "git",
		Repo: "https://example.com/fzf.git",
		Dest: "{{.VendorDir}}/fzf",
		Run:  "{{.VendorDir}}/fzf/install --bin",
	}
	got, err := pkgmgr.InstallGitFallback(fb, pkgmgr.FallbackContext{VendorDir: vendor, Platform: "linux", Shell: "bash"}, r)
	if err != nil {
		t.Fatalf("InstallGitFallback: %v", err)
	}
	want := filepath.Join(vendor, "fzf")
	if got != want {
		t.Fatalf("vendorPath = %q, want %q", got, want)
	}
	if len(r.Calls) != 2 {
		t.Fatalf("calls = %v, want clone then run", r.Calls)
	}
	if r.Calls[0] != "git clone --depth 1 https://example.com/fzf.git "+want {
		t.Fatalf("clone call = %q", r.Calls[0])
	}
	if r.Calls[1] != filepath.Join(vendor, "fzf", "install")+" --bin" {
		t.Fatalf("run call = %q", r.Calls[1])
	}
}

func TestInstallGitFallbackSkipsWhenDestPopulated(t *testing.T) {
	vendor := t.TempDir()
	dest := filepath.Join(vendor, "fzf")
	os.MkdirAll(dest, 0o755)
	os.WriteFile(filepath.Join(dest, "bin"), []byte("x"), 0o644)

	r := &pkgmgr.MockRunner{}
	fb := module.Fallback{Type: "git", Repo: "r", Dest: "{{.VendorDir}}/fzf"}
	got, err := pkgmgr.InstallGitFallback(fb, pkgmgr.FallbackContext{VendorDir: vendor}, r)
	if err != nil || got != dest {
		t.Fatalf("got %q err %v", got, err)
	}
	if len(r.Calls) != 0 {
		t.Fatalf("should not clone when dest populated; calls=%v", r.Calls)
	}
}

func TestInstallGitFallbackRejectsUnknownType(t *testing.T) {
	_, err := pkgmgr.InstallGitFallback(module.Fallback{Type: "tarball"}, pkgmgr.FallbackContext{VendorDir: t.TempDir()}, &pkgmgr.MockRunner{})
	if err == nil {
		t.Fatal("want error for unsupported fallback type")
	}
}
```

- [ ] **Step 2: Run, expect failure**

Run: `go test ./internal/pkgmgr/ -run Fallback -v` → FAIL (undefined).

- [ ] **Step 3: Implement `internal/pkgmgr/gitfallback.go`**

```go
package pkgmgr

import (
	"fmt"
	"os"
	"strings"

	"github.com/JtheGunner/omnishell/internal/module"
	"github.com/JtheGunner/omnishell/internal/render"
)

// FallbackContext is the render context for fallback path templates.
type FallbackContext struct {
	VendorDir string
	Platform  string
	Shell     string
}

func (c FallbackContext) renderCtx() render.Context {
	return render.Context{
		Options:   map[string]any{},
		Platform:  c.Platform,
		Shell:     c.Shell,
		VendorDir: c.VendorDir,
		Active:    map[string]bool{},
	}
}

func renderPath(tmpl string, c FallbackContext) (string, error) {
	out, err := render.Render(tmpl, c.renderCtx())
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func populatedDir(path string) bool {
	entries, err := os.ReadDir(path)
	return err == nil && len(entries) > 0
}

// FallbackSatisfied reports whether the fallback destination already exists.
func FallbackSatisfied(fb module.Fallback, ctx FallbackContext) (bool, string) {
	dest, err := renderPath(fb.Dest, ctx)
	if err != nil {
		return false, ""
	}
	return populatedDir(dest), dest
}

// InstallGitFallback clones fb.Repo into the rendered fb.Dest and runs fb.Run.
func InstallGitFallback(fb module.Fallback, ctx FallbackContext, r Runner) (string, error) {
	if fb.Type != "git" {
		return "", fmt.Errorf("unsupported fallback type %q", fb.Type)
	}
	dest, err := renderPath(fb.Dest, ctx)
	if err != nil {
		return "", fmt.Errorf("render fallback dest: %w", err)
	}
	if populatedDir(dest) {
		return dest, nil
	}
	if _, err := r.Run("git", "clone", "--depth", "1", fb.Repo, dest); err != nil {
		return "", fmt.Errorf("git clone %s: %w", fb.Repo, err)
	}
	if strings.TrimSpace(fb.Run) != "" {
		rendered, err := renderPath(fb.Run, ctx)
		if err != nil {
			return "", fmt.Errorf("render fallback run: %w", err)
		}
		parts := strings.Fields(rendered)
		if _, err := r.Run(parts[0], parts[1:]...); err != nil {
			os.RemoveAll(dest)
			return "", fmt.Errorf("fallback run %q: %w", rendered, err)
		}
	}
	return dest, nil
}
```

- [ ] **Step 4: Run, expect pass**

Run: `go test ./internal/pkgmgr/ -v` → PASS.

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "feat: add git-clone package fallback

Claude-Session: https://claude.ai/code/session_012MEGbPB16gL53nKQwspocK"
```

---

## Task 17: `engine` package — types & `ComputePlan`

**Files:**
- Create: `internal/engine/engine.go`, `internal/engine/plan.go`
- Test: `internal/engine/plan_test.go`

**Interfaces:**
- Consumes: `platform.Info`, `module.Registry`, `module.Manifest`, `config.Config`, `lockfile.Lock`, `graph.Order`, `module.ValidateOptions`, `module.OptionsHash`.
- Produces:
  - `engine.Engine struct { Platform platform.Info; Registry module.Registry; Manager pkgmgr.Manager; ManagerOK bool; Runner pkgmgr.Runner; Now func() time.Time; Stdout, Stderr io.Writer; Prompt func(question string) bool }`.
  - No constructor: callers build `engine.Engine{...}` as a struct literal. `(Engine) now()` returns `e.Now()` or `time.Now()` when `e.Now` is nil.
  - `engine.ModuleAction string`: `ActionInstall` (`"install"`), `ActionUpdate` (`"update"`), `ActionUnchanged` (`"unchanged"`), `ActionRemove` (`"remove"`), `ActionSkip` (`"skip"`) (skip = inactive on this platform / disabled and not in lock).
  - `engine.PackagePlan struct { Name, Manager string; AlreadyInstalled bool }`.
  - `engine.ModulePlan struct { ID string; Action ModuleAction; Reason string; Shells []string; Manifest module.Manifest; Options map[string]any; OptionsHash string; MissingPackages []PackagePlan; UsesFallback bool; UsesCheckHook bool; DegradedReason string }`.
  - `engine.Plan struct { Order []string; Modules map[string]ModulePlan; ManagedShells []string; PackageManager string; ManagerAvailable bool; HasChanges bool }`.
  - `engine.ComputePlan(e Engine, cfg config.Config, lock lockfile.Lock, noPackages bool) (Plan, error)`:
    1. Determine `ManagedShells`: `cfg.Omnishell.Shells` if non-empty, else the `Present` shells from `e.Platform.Shells`; always intersected with `platform.SupportedShells` and ordered `[zsh, bash]`.
    2. Active set: modules with `cfg.Modules[id].Enabled == true` **and** `e.Platform.OS` in `manifest.Platforms`. A disabled/removed module that appears in `lock.Modules` with a rendered section → `ModulePlan{Action: ActionRemove}`.
    3. Validate options for each active module (`module.ValidateOptions`); a validation error aborts `ComputePlan` with a `config`-category error (wrap as `engine.ConfigError`).
    4. `graph.Order` over the active manifests → `Plan.Order`; a `graph` error aborts as a `config`-category error.
    5. For each active module compute per-shell applicability: shells = `ManagedShells ∩ manifest.Shells` for which a template exists. If empty → `DegradedReason = "no snippet for any managed shell"`, still allowed (renders nothing) but recorded.
    6. Requirement/packages: if `noPackages` skip package planning. Else, for the detected manager, `pkgs := manifest.Packages.ForManager(e.Manager.Name())`; for each not `IsInstalled` → `MissingPackages`. If `len(pkgs)==0` and a `[[packages.fallback]]` exists → `UsesFallback=true`, satisfied iff `pkgmgr.FallbackSatisfied`. If no packages and no fallback but `HasHook("check")` → `UsesCheckHook=true`. If `!e.ManagerOK` and the module needs packages → `DegradedReason = "no package manager detected"`.
    7. Action: not in `lock` → `ActionInstall`; in `lock` but `OptionsHash` or `ModuleVersion` differs, or `MissingPackages` non-empty, or shells changed → `ActionUpdate`; else `ActionUnchanged`.
    8. `HasChanges` = any module action ∈ {install, update, remove} OR any lock init-file/rc drift (compare later in Apply; for plan purposes: also true if `lock` has zero modules but active set is non-empty).
  - `engine.ConfigError struct { Err error }` implementing `error` + `Unwrap`; the CLI maps it to exit 2.

- [ ] **Step 1: Write failing test `internal/engine/plan_test.go`**

```go
package engine_test

import (
	"testing"
	"time"

	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/JtheGunner/omnishell/internal/engine"
	"github.com/JtheGunner/omnishell/internal/lockfile"
	"github.com/JtheGunner/omnishell/internal/module"
	"github.com/JtheGunner/omnishell/internal/pkgmgr"
	"github.com/JtheGunner/omnishell/internal/platform"
)

// testRegistry builds a Registry from an in-repo fixture dir.
// Fixtures live in internal/engine/testdata/modules/{completion,fzf,history}.
func testEngine(t *testing.T, mgr *pkgmgr.MockManager) engine.Engine {
	t.Helper()
	reg, err := module.LoadRegistry(nil, "testdata/modules")
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	return engine.Engine{
		Platform: platform.Info{
			OS:        platform.Linux,
			HomeDir:   t.TempDir(),
			ConfigDir: t.TempDir(),
			Shells: []platform.ShellInfo{
				{Name: "zsh", RCPath: "/x/.zshrc", Present: false},
				{Name: "bash", RCPath: "/x/.bashrc", Present: true},
			},
		},
		Registry:  reg,
		Manager:   mgr,
		ManagerOK: true,
		Runner:    &pkgmgr.MockRunner{},
		Now:       func() time.Time { return time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC) },
	}
}

func TestComputePlanFreshInstall(t *testing.T) {
	mgr := &pkgmgr.MockManager{NameV: "apt", DetectV: true, Installed: map[string]bool{}}
	e := testEngine(t, mgr)
	cfg := config.Config{
		Omnishell: config.OmnishellSection{Version: 1, Shells: []string{"bash"}},
		Modules: map[string]config.ModuleConfig{
			"completion": {Enabled: true},
			"fzf":        {Enabled: true, Options: map[string]any{"ctrl_r": true}},
		},
	}
	p, err := engine.ComputePlan(e, cfg, lockfile.Lock{Modules: map[string]lockfile.ModuleState{}}, false)
	if err != nil {
		t.Fatalf("ComputePlan: %v", err)
	}
	if len(p.Order) != 2 || p.Order[0] != "completion" || p.Order[1] != "fzf" {
		t.Fatalf("order = %v", p.Order)
	}
	if p.Modules["fzf"].Action != engine.ActionInstall {
		t.Fatalf("fzf action = %v", p.Modules["fzf"].Action)
	}
	if len(p.Modules["fzf"].MissingPackages) != 1 || p.Modules["fzf"].MissingPackages[0].Name != "fzf" {
		t.Fatalf("fzf missing packages = %+v", p.Modules["fzf"].MissingPackages)
	}
	if !p.HasChanges {
		t.Fatal("HasChanges should be true for a fresh install")
	}
}

func TestComputePlanUnchangedWhenLockMatches(t *testing.T) {
	mgr := &pkgmgr.MockManager{NameV: "apt", DetectV: true, Installed: map[string]bool{"fzf": true}}
	e := testEngine(t, mgr)
	cfg := config.Config{
		Omnishell: config.OmnishellSection{Version: 1, Shells: []string{"bash"}},
		Modules:   map[string]config.ModuleConfig{"fzf": {Enabled: true, Options: map[string]any{"ctrl_r": true}}},
	}
	// Precompute the options hash the plan will expect.
	norm, _ := module.ValidateOptions(mustManifest(t, e, "fzf").Options, map[string]any{"ctrl_r": true})
	lock := lockfile.Lock{Modules: map[string]lockfile.ModuleState{
		"fzf": {ModuleVersion: mustManifest(t, e, "fzf").Module.Version, Enabled: true,
			OptionsHash: module.OptionsHash(norm), ShellsRendered: []string{"bash"}, Status: "ok"},
	}}
	p, err := engine.ComputePlan(e, cfg, lock, false)
	if err != nil {
		t.Fatal(err)
	}
	if p.Modules["fzf"].Action != engine.ActionUnchanged {
		t.Fatalf("fzf action = %v, want unchanged", p.Modules["fzf"].Action)
	}
	if p.HasChanges {
		t.Fatal("HasChanges should be false")
	}
}

func TestComputePlanRemovesDisabledModuleInLock(t *testing.T) {
	mgr := &pkgmgr.MockManager{NameV: "apt", DetectV: true, Installed: map[string]bool{}}
	e := testEngine(t, mgr)
	cfg := config.Config{Omnishell: config.OmnishellSection{Version: 1, Shells: []string{"bash"}}, Modules: map[string]config.ModuleConfig{}}
	lock := lockfile.Lock{Modules: map[string]lockfile.ModuleState{
		"completion": {ModuleVersion: "1.0.0", Enabled: true, ShellsRendered: []string{"bash"}, Status: "ok"},
	}}
	p, err := engine.ComputePlan(e, cfg, lock, false)
	if err != nil {
		t.Fatal(err)
	}
	if p.Modules["completion"].Action != engine.ActionRemove {
		t.Fatalf("completion action = %v, want remove", p.Modules["completion"].Action)
	}
	if !p.HasChanges {
		t.Fatal("HasChanges should be true (removal)")
	}
}

func TestComputePlanOptionValidationError(t *testing.T) {
	mgr := &pkgmgr.MockManager{NameV: "apt", DetectV: true}
	e := testEngine(t, mgr)
	cfg := config.Config{
		Omnishell: config.OmnishellSection{Version: 1, Shells: []string{"bash"}},
		Modules:   map[string]config.ModuleConfig{"fzf": {Enabled: true, Options: map[string]any{"bogus": 1}}},
	}
	_, err := engine.ComputePlan(e, cfg, lockfile.Lock{Modules: map[string]lockfile.ModuleState{}}, false)
	var ce engine.ConfigError
	if err == nil || !asConfigError(err, &ce) {
		t.Fatalf("err = %v, want engine.ConfigError", err)
	}
}
```

Add `internal/engine/helpers_test.go`:

```go
package engine_test

import (
	"errors"
	"testing"

	"github.com/JtheGunner/omnishell/internal/engine"
	"github.com/JtheGunner/omnishell/internal/module"
)

func asConfigError(err error, target *engine.ConfigError) bool { return errors.As(err, target) }

func mustManifest(t *testing.T, e engine.Engine, id string) module.Manifest {
	t.Helper()
	m, ok := e.Registry.Get(id)
	if !ok {
		t.Fatalf("module %q not in registry", id)
	}
	return m.Manifest
}
```

- [ ] **Step 2: Create engine test fixtures**

`internal/engine/testdata/modules/completion/manifest.toml`:
```toml
[module]
id = "completion"
name = "Completion"
description = "compinit"
version = "1.0.0"
schema = 1
platforms = ["macos", "linux"]
shells = ["zsh", "bash"]
```
`.../completion/zsh.tmpl` → `autoload -Uz compinit && compinit`
`.../completion/bash.tmpl` → `# bash completion`
`internal/engine/testdata/modules/fzf/manifest.toml`: the Task 6 fzf fixture, plus `after = ["completion"]`.
`.../fzf/zsh.tmpl` → `{{ if .Options.ctrl_r }}source fzf-key-bindings.zsh{{ end }}`
`.../fzf/bash.tmpl` → `{{ if .Options.ctrl_r }}source fzf-key-bindings.bash{{ end }}`
`internal/engine/testdata/modules/history/manifest.toml`: config-only, `platforms=["macos","linux"]`, `shells=["zsh","bash"]`, `after=["completion"]`, one `size` int option default `50000`, no `[packages]`.
`.../history/zsh.tmpl` → `HISTSIZE={{ .Options.size }}`
`.../history/bash.tmpl` → `HISTSIZE={{ .Options.size }}`

- [ ] **Step 3: Run, expect failure**

Run: `go test ./internal/engine/ -run ComputePlan -v` → FAIL (undefined).

- [ ] **Step 4: Implement `internal/engine/engine.go`**

```go
// Package engine orchestrates plan/apply/remove/doctor/uninstall over the
// config, module registry, package manager, and on-disk files.
package engine

import (
	"io"
	"time"

	"github.com/JtheGunner/omnishell/internal/module"
	"github.com/JtheGunner/omnishell/internal/pkgmgr"
	"github.com/JtheGunner/omnishell/internal/platform"
)

// Engine holds every dependency the operations need.
type Engine struct {
	Platform  platform.Info
	Registry  module.Registry
	Manager   pkgmgr.Manager
	ManagerOK bool
	Runner    pkgmgr.Runner
	Now       func() time.Time
	Stdout    io.Writer
	Stderr    io.Writer
	Prompt    func(question string) bool
}

func (e Engine) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

// ConfigError marks an error the CLI should map to exit code 2.
type ConfigError struct{ Err error }

func (e ConfigError) Error() string { return e.Err.Error() }
func (e ConfigError) Unwrap() error { return e.Err }
```

- [ ] **Step 5: Implement `internal/engine/plan.go`**

```go
package engine

import (
	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/JtheGunner/omnishell/internal/graph"
	"github.com/JtheGunner/omnishell/internal/lockfile"
	"github.com/JtheGunner/omnishell/internal/module"
	"github.com/JtheGunner/omnishell/internal/pkgmgr"
	"github.com/JtheGunner/omnishell/internal/platform"
)

// ModuleAction is what apply will do with a module.
type ModuleAction string

const (
	ActionInstall   ModuleAction = "install"
	ActionUpdate    ModuleAction = "update"
	ActionUnchanged ModuleAction = "unchanged"
	ActionRemove    ModuleAction = "remove"
	ActionSkip      ModuleAction = "skip"
)

// PackagePlan is one package the plan may install.
type PackagePlan struct {
	Name             string
	Manager          string
	AlreadyInstalled bool
}

// ModulePlan is the planned outcome for one module.
type ModulePlan struct {
	ID              string
	Action          ModuleAction
	Reason          string
	Shells          []string
	Manifest        module.Manifest
	Options         map[string]any
	OptionsHash     string
	MissingPackages []PackagePlan
	UsesFallback    bool
	UsesCheckHook   bool
	DegradedReason  string
}

// Plan is the full computed plan.
type Plan struct {
	Order            []string
	Modules          map[string]ModulePlan
	ManagedShells    []string
	PackageManager   string
	ManagerAvailable bool
	HasChanges       bool
}

func managedShells(cfg config.Config, info platform.Info) []string {
	want := cfg.Omnishell.Shells
	if len(want) == 0 {
		for _, s := range info.Shells {
			if s.Present {
				want = append(want, s.Name)
			}
		}
	}
	wantSet := map[string]bool{}
	for _, s := range want {
		wantSet[s] = true
	}
	var out []string
	for _, s := range platform.SupportedShells { // fixed [zsh, bash] order
		if wantSet[s] {
			out = append(out, s)
		}
	}
	return out
}

func contains(ss []string, v string) bool {
	for _, s := range ss {
		if s == v {
			return true
		}
	}
	return false
}

// ComputePlan builds the plan without touching the system.
func ComputePlan(e Engine, cfg config.Config, lock lockfile.Lock, noPackages bool) (Plan, error) {
	shells := managedShells(cfg, e.Platform)
	mgrName := ""
	if e.ManagerOK {
		mgrName = e.Manager.Name()
	}
	p := Plan{
		Modules:          map[string]ModulePlan{},
		ManagedShells:    shells,
		PackageManager:   mgrName,
		ManagerAvailable: e.ManagerOK,
	}

	active := map[string]module.Manifest{}
	for id, mc := range cfg.Modules {
		if !mc.Enabled {
			continue
		}
		mod, ok := e.Registry.Get(id)
		if !ok {
			continue // unknown module id: skipped with a warning by Apply
		}
		if !contains(mod.Manifest.Platforms, string(e.Platform.OS)) {
			continue
		}
		active[id] = mod.Manifest
	}

	// Validate options; abort as ConfigError on failure.
	optsByID := map[string]map[string]any{}
	for id, mf := range active {
		norm, err := module.ValidateOptions(mf.Options, cfg.Modules[id].Options)
		if err != nil {
			return Plan{}, ConfigError{Err: err}
		}
		optsByID[id] = norm
	}

	order, err := graph.Order(active)
	if err != nil {
		return Plan{}, ConfigError{Err: err}
	}
	p.Order = order

	for _, id := range order {
		mf := active[id]
		mod, _ := e.Registry.Get(id)
		norm := optsByID[id]
		hash := module.OptionsHash(norm)

		var shellsForModule []string
		for _, sh := range shells {
			if !contains(mf.Shells, sh) {
				continue
			}
			if _, has, _ := mod.Template(sh); has {
				shellsForModule = append(shellsForModule, sh)
			}
		}

		mp := ModulePlan{
			ID: id, Manifest: mf, Options: norm, OptionsHash: hash,
			Shells: shellsForModule,
		}
		if len(shellsForModule) == 0 {
			mp.DegradedReason = "no snippet for any managed shell"
		}

		if !noPackages {
			planPackages(&mp, e, mod, shells)
		}

		prev, inLock := lock.Modules[id]
		switch {
		case !inLock:
			mp.Action = ActionInstall
		case prev.OptionsHash != hash || prev.ModuleVersion != mf.Module.Version ||
			!equalStringSet(prev.ShellsRendered, shellsForModule) || len(mp.MissingPackages) > 0:
			mp.Action = ActionUpdate
		default:
			mp.Action = ActionUnchanged
		}
		p.Modules[id] = mp
	}

	// Modules present in the lock but no longer active → removal.
	for id, st := range lock.Modules {
		if _, stillActive := p.Modules[id]; stillActive {
			continue
		}
		if st.Status == "disabled" {
			continue
		}
		p.Modules[id] = ModulePlan{ID: id, Action: ActionRemove, Reason: "no longer enabled"}
	}

	for _, mp := range p.Modules {
		if mp.Action == ActionInstall || mp.Action == ActionUpdate || mp.Action == ActionRemove {
			p.HasChanges = true
			break
		}
	}
	return p, nil
}

func planPackages(mp *ModulePlan, e Engine, mod module.Module, shells []string) {
	mf := mp.Manifest
	if !e.ManagerOK {
		if len(mf.Packages.Brew)+len(mf.Packages.Apt)+len(mf.Packages.Dnf)+
			len(mf.Packages.Pacman)+len(mf.Packages.Zypper)+len(mf.Packages.Apk) > 0 {
			mp.DegradedReason = "no package manager detected"
		}
		return
	}
	pkgs := mf.Packages.ForManager(e.Manager.Name())
	if len(pkgs) > 0 {
		for _, name := range pkgs {
			installed, _ := e.Manager.IsInstalled(name)
			if !installed {
				mp.MissingPackages = append(mp.MissingPackages, PackagePlan{Name: name, Manager: e.Manager.Name()})
			}
		}
		return
	}
	if len(mf.Packages.Fallback) > 0 {
		mp.UsesFallback = true
		ok, _ := pkgmgr.FallbackSatisfied(mf.Packages.Fallback[0], pkgmgr.FallbackContext{
			VendorDir: e.Platform.ConfigDir + "/vendor",
			Platform:  string(e.Platform.OS),
		})
		if !ok {
			mp.MissingPackages = append(mp.MissingPackages, PackagePlan{Name: mf.Packages.Fallback[0].Repo, Manager: "git"})
		}
		return
	}
	if mod.HasHook("check") {
		mp.UsesCheckHook = true
	}
}

func equalStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[string]bool{}
	for _, s := range a {
		seen[s] = true
	}
	for _, s := range b {
		if !seen[s] {
			return false
		}
	}
	return true
}
```

- [ ] **Step 6: Run, expect pass**

Run: `go test ./internal/engine/ -run ComputePlan -v` → PASS.

- [ ] **Step 7: Commit**

```bash
git add -A
git commit -m "feat: add engine plan computation

Claude-Session: https://claude.ai/code/session_012MEGbPB16gL53nKQwspocK"
```

---

## Task 18: `engine` package — `RenderPlan` (human-readable plan output)

**Files:**
- Modify: `internal/engine/plan.go` — add `RenderPlan`
- Test: `internal/engine/renderplan_test.go`, `internal/engine/testdata/plan_fresh.golden`

**Interfaces:**
- Consumes: `engine.Plan` (Task 17).
- Produces:
  - `engine.RenderPlan(p Plan) string` — deterministic multi-line summary:
    ```
    Plan (package manager: apt, shells: bash)

      install  completion            snippet: bash
      install  fzf                   snippet: bash   packages: fzf (apt)
      remove   zoxide                was enabled previously

    2 to install, 0 to update, 1 to remove, 0 unchanged
    ```
    - Modules listed in `p.Order`, then removals sorted by id.
    - `unchanged` modules are listed only when they are the sole content (otherwise omitted for brevity) — actually: always omit `unchanged` lines but include them in the trailing count.
    - A `DegradedReason` renders as `  degraded <id>   <reason>` and counts as its planned action.
    - Column widths: action padded to 8, id padded to 20.
  - Golden test compares `RenderPlan` output for a fixed `Plan` value against `testdata/plan_fresh.golden`, with a `-update` flag (`var update = flag.Bool("update", false, "update golden files")`).

- [ ] **Step 1: Write failing test `internal/engine/renderplan_test.go`**

```go
package engine_test

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/JtheGunner/omnishell/internal/engine"
	"github.com/JtheGunner/omnishell/internal/module"
)

var update = flag.Bool("update", false, "update golden files")

func TestRenderPlanFresh(t *testing.T) {
	p := engine.Plan{
		Order:          []string{"completion", "fzf"},
		ManagedShells:  []string{"bash"},
		PackageManager: "apt",
		ManagerAvailable: true,
		HasChanges:     true,
		Modules: map[string]engine.ModulePlan{
			"completion": {ID: "completion", Action: engine.ActionInstall, Shells: []string{"bash"},
				Manifest: module.Manifest{Module: module.ModuleMeta{ID: "completion", Version: "1.0.0"}}},
			"fzf": {ID: "fzf", Action: engine.ActionInstall, Shells: []string{"bash"},
				MissingPackages: []engine.PackagePlan{{Name: "fzf", Manager: "apt"}},
				Manifest: module.Manifest{Module: module.ModuleMeta{ID: "fzf", Version: "1.0.0"}}},
			"zoxide": {ID: "zoxide", Action: engine.ActionRemove, Reason: "no longer enabled"},
		},
	}
	got := engine.RenderPlan(p)
	golden := filepath.Join("testdata", "plan_fresh.golden")
	if *update {
		os.WriteFile(golden, []byte(got), 0o644)
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("RenderPlan mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}
```

- [ ] **Step 2: Run, expect failure**

Run: `go test ./internal/engine/ -run RenderPlan -v` → FAIL (undefined; golden missing).

- [ ] **Step 3: Implement `RenderPlan` in `internal/engine/plan.go`**

```go
// RenderPlan formats a plan for the user.
func RenderPlan(p Plan) string {
	var b strings.Builder
	mgr := p.PackageManager
	if !p.ManagerAvailable {
		mgr = "none detected"
	}
	fmt.Fprintf(&b, "Plan (package manager: %s, shells: %s)\n\n", mgr, strings.Join(p.ManagedShells, ", "))

	var nInstall, nUpdate, nRemove, nUnchanged int
	line := func(mp ModulePlan) {
		if mp.DegradedReason != "" {
			fmt.Fprintf(&b, "  %-8s %-20s %s\n", "degraded", mp.ID, mp.DegradedReason)
		}
		switch mp.Action {
		case ActionInstall, ActionUpdate:
			extra := ""
			if len(mp.Shells) > 0 {
				extra = "snippet: " + strings.Join(mp.Shells, ",")
			}
			if len(mp.MissingPackages) > 0 {
				names := make([]string, len(mp.MissingPackages))
				for i, pp := range mp.MissingPackages {
					names[i] = pp.Name + " (" + pp.Manager + ")"
				}
				extra += "   packages: " + strings.Join(names, ", ")
			}
			fmt.Fprintf(&b, "  %-8s %-20s %s\n", string(mp.Action), mp.ID, strings.TrimSpace(extra))
		case ActionRemove:
			fmt.Fprintf(&b, "  %-8s %-20s %s\n", "remove", mp.ID, mp.Reason)
		}
	}

	for _, id := range p.Order {
		mp := p.Modules[id]
		switch mp.Action {
		case ActionInstall:
			nInstall++
		case ActionUpdate:
			nUpdate++
		case ActionUnchanged:
			nUnchanged++
		}
		if mp.Action != ActionUnchanged {
			line(mp)
		}
	}

	var removals []string
	for id, mp := range p.Modules {
		if mp.Action == ActionRemove {
			removals = append(removals, id)
		}
	}
	sort.Strings(removals)
	for _, id := range removals {
		nRemove++
		line(p.Modules[id])
	}

	fmt.Fprintf(&b, "\n%d to install, %d to update, %d to remove, %d unchanged\n",
		nInstall, nUpdate, nRemove, nUnchanged)
	return b.String()
}
```

Add `"fmt"`, `"sort"`, `"strings"` to the `plan.go` imports.

- [ ] **Step 4: Generate the golden file and verify**

Run: `go test ./internal/engine/ -run RenderPlan -update`
Then inspect `internal/engine/testdata/plan_fresh.golden` by eye — it must read cleanly (no trailing spaces beyond padding, counts correct). Then run without `-update`:
Run: `go test ./internal/engine/ -run RenderPlan -v` → PASS.

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "feat: add human-readable plan rendering

Claude-Session: https://claude.ai/code/session_012MEGbPB16gL53nKQwspocK"
```

---

## Task 19: `engine` package — `Apply`

**Files:**
- Create: `internal/engine/apply.go`
- Test: `internal/engine/apply_test.go`

**Interfaces:**
- Consumes: everything so far — `ComputePlan`, `RenderPlan`, `pkgmgr`, `render`, `initfile`, `rcfile`, `lockfile`, `backup`, `atomicfile`, `config`.
- Produces:
  - `engine.ApplyOptions struct { DryRun, Yes, NoPackages, Force, Verbose bool }`.
  - `engine.ModuleResult struct { ID string; Action ModuleAction; Status string; Note string }` — `Status` ∈ `"applied"`,`"degraded"`,`"skipped"`,`"removed"`,`"unchanged"`.
  - `engine.Result struct { DryRun bool; PlanText string; Modules []ModuleResult; InitFilesWritten []string; BackupDir string; Changed bool }`.
  - `engine.ErrAborted = errors.New("aborted by user")` — returned (not printed) when `Prompt` says no.
  - `engine.ErrHandEdited` — sentinel wrapped when an init file was hand-edited and `--force` was not given.
  - `(Engine) Apply(cfg config.Config, lock lockfile.Lock, opts ApplyOptions) (Result, lockfile.Lock, error)` — returns the updated lock so the caller can persist it (the CLI writes it; Apply also writes it itself on the real path — see step logic). **Refined:** Apply takes the lock **path** too. Final signature:
    - `(Engine) Apply(cfg config.Config, cfgPath, lockPath string, opts ApplyOptions) (Result, error)` — Apply loads the lock from `lockPath` itself, and writes it back. This keeps the CLI layer trivial.
  - Algorithm (mirrors spec §7):
    1. `lock, _, err := lockfile.Load(lockPath)`.
    2. `plan, err := ComputePlan(e, cfg, lock, opts.NoPackages)` — a `ConfigError` propagates unchanged.
    3. `res.PlanText = RenderPlan(plan)`. If `opts.DryRun`: set `res.DryRun=true`, return now (no writes).
    4. If `!plan.HasChanges` and no init-file/rc drift (compare `initfile.Build` hash for each managed shell against `lock.InitFiles`, and `rcfile.BlockPresent` against actual rc files): write nothing, `res.Changed=false`, return. (This is the idempotent no-op.)
    5. If `!opts.Yes` and `e.Prompt != nil`: print `res.PlanText` to `e.Stdout`, ask `e.Prompt("Proceed?")`; false → return `ErrAborted`.
    6. **Package phase** (skip if `opts.NoPackages`): for each module in `plan.Order` with `MissingPackages`:
       - real packages → announce (to `e.Stdout`) `"installing <names> via <mgr>"`; if `e.Manager.NeedsSudo()` also print `"(sudo may prompt for your password)"`; call `e.Manager.Install(names)`. On error → mark module `degraded`, note the error, continue.
       - fallback → `pkgmgr.InstallGitFallback(mf.Packages.Fallback[0], ctx, e.Runner)`; record vendor path; on error → `degraded`.
       - After install, re-check `IsInstalled`; still missing → `degraded`.
    7. **Check/install-hook phase**: for modules with `UsesCheckHook`, materialise `hooks/check.sh` to a `0o700` temp file (write `mod.ReadHook("check")` bytes, `os.CreateTemp`, `chmod 0o700`), run it via `e.Runner` with the hook env vars (`OMNISHELL_VENDOR_DIR`, `OMNISHELL_CONFIG_DIR`, `OMNISHELL_PLATFORM`, `OMNISHELL_SHELL`, `OMNISHELL_OPT_<UPPERCASE_KEY>`). Non-zero exit **and** `mod.HasHook("install")` → materialise and run `hooks/install.sh` the same way, then re-run `check.sh`. Still non-zero (or no `install.sh`) → `degraded` with note `"check hook failed"`. Delete the temp files afterwards. The same env-var set is passed to the `remove.sh` hook in Task 20.
    8. **Render phase**: for each managed shell, build `[]initfile.Section` from `plan.Order`, skipping modules that are `degraded` or have empty `Shells`. For each included module+shell: `render.Render(templateBody, render.Context{...})`; a render error → `degraded`, drop from all shells (rebuild sections without it). `Active` map for `has` = set of non-degraded active ids.
    9. **Hand-edit guard**: for each managed shell whose target init file exists, `initfile.DetectHandEdit(existing, newSections)`. If edited and `!opts.Force` → return `fmt.Errorf("%w: %s (run with --force to overwrite)", ErrHandEdited, path)` **before any write**.
    10. **Write phase** (only now touch disk):
        - `bk, _ := backup.NewSession(e.Platform.ConfigDir, e.now())`; `res.BackupDir = bk.Dir`.
        - For each managed shell: `bk.Save(initPath)`; `content := initfile.Build(shell, sections, e.now())`; `atomicfile.WriteFile(initPath, []byte(content), 0o644)`; record hash in the new lock `InitFiles[shell]`.
        - For each managed shell with a real rc path: read rc (missing = empty), `bk.Save(rcPath)`, `rcfile.EnsureBlock(content, shell, homeRelative(initPath))`; if changed → `atomicfile.WriteFile(rcPath, ...)`. Record `RCFiles[shell]`.
    11. **Lock update**: rebuild `lock.Modules` from the plan + install results: `Status` per module (`ok`/`degraded`/`disabled`), `OptionsHash`, `ModuleVersion`, `ShellsRendered`, `Packages` (merge previous `installed_by_omnishell` truth with newly-installed = true), `VendorPaths`. Removed modules: delete from `lock.Modules`. Set `lock.Schema`, `OmnishellVersion` (`buildinfo.Version`), `LastApply` (`e.now().UTC().Format(time.RFC3339)`), `Platform`, `PackageManager`. `lock.Write(lockPath)`.
    12. **Result**: fill `res.Modules`, `res.InitFilesWritten`, `res.Changed=true`. Return `res, nil` — unless ≥1 module is `degraded`, in which case return `res, ErrDegraded` (a sentinel the CLI maps to exit 1 but still prints the report).
  - `engine.ErrDegraded = errors.New("one or more modules are degraded")`.
- **`homeRelative(path)`** helper: if `path` is under `e.Platform.HomeDir`, return `"$HOME" + rest`; else return `path`.

- [ ] **Step 1: Write failing test `internal/engine/apply_test.go`**

```go
package engine_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/JtheGunner/omnishell/internal/engine"
	"github.com/JtheGunner/omnishell/internal/lockfile"
	"github.com/JtheGunner/omnishell/internal/module"
	"github.com/JtheGunner/omnishell/internal/pkgmgr"
	"github.com/JtheGunner/omnishell/internal/platform"
)

func applyEngine(t *testing.T, home string, mgr *pkgmgr.MockManager, out *bytes.Buffer) engine.Engine {
	t.Helper()
	reg, err := module.LoadRegistry(nil, "testdata/modules")
	if err != nil {
		t.Fatal(err)
	}
	return engine.Engine{
		Platform: platform.Info{
			OS:        platform.Linux,
			HomeDir:   home,
			ConfigDir: filepath.Join(home, ".config", "omnishell"),
			Shells: []platform.ShellInfo{
				{Name: "zsh", RCPath: filepath.Join(home, ".zshrc"), Present: false},
				{Name: "bash", RCPath: filepath.Join(home, ".bashrc"), Present: true},
			},
		},
		Registry:  reg,
		Manager:   mgr,
		ManagerOK: true,
		Runner:    &pkgmgr.MockRunner{},
		Now:       func() time.Time { return time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC) },
		Stdout:    out,
		Stderr:    out,
		Prompt:    func(string) bool { return true },
	}
}

func writeConfig(t *testing.T, path, body string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestApplyFreshInstallWritesInitAndRC(t *testing.T) {
	home := t.TempDir()
	var out bytes.Buffer
	mgr := &pkgmgr.MockManager{NameV: "apt", DetectV: true, Installed: map[string]bool{}}
	e := applyEngine(t, home, mgr, &out)

	cfgPath := filepath.Join(home, ".config", "omnishell", "config.toml")
	lockPath := filepath.Join(home, ".config", "omnishell", "state.lock.json")
	writeConfig(t, cfgPath, `
[omnishell]
version = 1
shells = ["bash"]
[modules.completion]
enabled = true
[modules.fzf]
enabled = true
[modules.fzf.options]
ctrl_r = true
`)
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	res, err := e.Apply(cfg, cfgPath, lockPath, engine.ApplyOptions{Yes: true})
	if err != nil {
		t.Fatalf("Apply: %v\noutput:\n%s", err, out.String())
	}
	if !res.Changed {
		t.Fatal("res.Changed = false")
	}

	initBash := filepath.Join(home, ".config", "omnishell", "init.bash")
	body, err := os.ReadFile(initBash)
	if err != nil {
		t.Fatalf("init.bash not written: %v", err)
	}
	if !strings.Contains(string(body), "# >>> omnishell:completion") ||
		!strings.Contains(string(body), "source fzf-key-bindings.bash") {
		t.Fatalf("init.bash content wrong:\n%s", body)
	}

	rc, _ := os.ReadFile(filepath.Join(home, ".bashrc"))
	if !strings.Contains(string(rc), `[ -f "$HOME/.config/omnishell/init.bash" ] && source`) {
		t.Fatalf(".bashrc missing source block:\n%s", rc)
	}
	if len(mgr.InstallCalls) != 1 || mgr.InstallCalls[0][0] != "fzf" {
		t.Fatalf("fzf not installed: %+v", mgr.InstallCalls)
	}

	lock, exists, err := lockfile.Load(lockPath)
	if err != nil || !exists {
		t.Fatalf("lock: exists=%v err=%v", exists, err)
	}
	if lock.Modules["fzf"].Status != "ok" || lock.Modules["fzf"].OptionsHash == "" {
		t.Fatalf("lock fzf state wrong: %+v", lock.Modules["fzf"])
	}
	if !lock.Modules["fzf"].Packages[0].InstalledByOmnishell {
		t.Fatal("fzf package not marked installed_by_omnishell")
	}
}

func TestApplyIsIdempotent(t *testing.T) {
	home := t.TempDir()
	var out bytes.Buffer
	mgr := &pkgmgr.MockManager{NameV: "apt", DetectV: true, Installed: map[string]bool{}}
	e := applyEngine(t, home, mgr, &out)
	cfgPath := filepath.Join(home, ".config", "omnishell", "config.toml")
	lockPath := filepath.Join(home, ".config", "omnishell", "state.lock.json")
	writeConfig(t, cfgPath, "[omnishell]\nversion=1\nshells=[\"bash\"]\n[modules.completion]\nenabled=true\n")
	cfg, _ := config.Load(cfgPath)

	if _, err := e.Apply(cfg, cfgPath, lockPath, engine.ApplyOptions{Yes: true}); err != nil {
		t.Fatal(err)
	}
	info1, _ := os.Stat(filepath.Join(home, ".config", "omnishell", "init.bash"))

	res, err := e.Apply(cfg, cfgPath, lockPath, engine.ApplyOptions{Yes: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("second apply reported a change")
	}
	info2, _ := os.Stat(filepath.Join(home, ".config", "omnishell", "init.bash"))
	if !info1.ModTime().Equal(info2.ModTime()) {
		t.Fatal("init.bash was rewritten on a no-op apply")
	}
	backups, _ := os.ReadDir(filepath.Join(home, ".config", "omnishell", "backups"))
	if len(backups) != 1 {
		t.Fatalf("no-op apply created a backup; backups=%d", len(backups))
	}
}

func TestApplyDryRunWritesNothing(t *testing.T) {
	home := t.TempDir()
	var out bytes.Buffer
	e := applyEngine(t, home, &pkgmgr.MockManager{NameV: "apt", DetectV: true}, &out)
	cfgPath := filepath.Join(home, ".config", "omnishell", "config.toml")
	lockPath := filepath.Join(home, ".config", "omnishell", "state.lock.json")
	writeConfig(t, cfgPath, "[omnishell]\nversion=1\nshells=[\"bash\"]\n[modules.completion]\nenabled=true\n")
	cfg, _ := config.Load(cfgPath)

	res, err := e.Apply(cfg, cfgPath, lockPath, engine.ApplyOptions{DryRun: true, Yes: true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.DryRun || !strings.Contains(res.PlanText, "install") {
		t.Fatalf("dry-run result wrong: %+v", res)
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "omnishell", "init.bash")); !os.IsNotExist(err) {
		t.Fatal("dry-run wrote init.bash")
	}
}

func TestApplyHandEditGuard(t *testing.T) {
	home := t.TempDir()
	var out bytes.Buffer
	e := applyEngine(t, home, &pkgmgr.MockManager{NameV: "apt", DetectV: true}, &out)
	cfgPath := filepath.Join(home, ".config", "omnishell", "config.toml")
	lockPath := filepath.Join(home, ".config", "omnishell", "state.lock.json")
	writeConfig(t, cfgPath, "[omnishell]\nversion=1\nshells=[\"bash\"]\n[modules.completion]\nenabled=true\n")
	cfg, _ := config.Load(cfgPath)
	if _, err := e.Apply(cfg, cfgPath, lockPath, engine.ApplyOptions{Yes: true}); err != nil {
		t.Fatal(err)
	}

	initBash := filepath.Join(home, ".config", "omnishell", "init.bash")
	orig, _ := os.ReadFile(initBash)
	os.WriteFile(initBash, append(orig, []byte("\n# tampered\n")...), 0o644)

	// Force a change so Apply wants to write.
	writeConfig(t, cfgPath, "[omnishell]\nversion=1\nshells=[\"bash\"]\n[modules.completion]\nenabled=true\n[modules.history]\nenabled=true\n")
	cfg2, _ := config.Load(cfgPath)

	_, err := e.Apply(cfg2, cfgPath, lockPath, engine.ApplyOptions{Yes: true})
	if err == nil || !strings.Contains(err.Error(), "force") {
		t.Fatalf("err = %v, want hand-edit guard error", err)
	}

	res, err := e.Apply(cfg2, cfgPath, lockPath, engine.ApplyOptions{Yes: true, Force: true})
	if err != nil {
		t.Fatalf("Apply --force: %v", err)
	}
	if !res.Changed {
		t.Fatal("force apply made no change")
	}
}
```

- [ ] **Step 2: Run, expect failure**

Run: `go test ./internal/engine/ -run Apply -v` → FAIL (undefined).

- [ ] **Step 3: Implement `internal/engine/apply.go`**

Implement exactly the algorithm above. Keep it under ~300 lines by extracting helpers: `installPackages(e, plan, results)`, `buildSections(e, plan, degraded, shell)`, `writeShellFiles(e, plan, sectionsByShell, bk, newLock)`, `rebuildLock(e, cfg, plan, results, prevLock)`. Sketch of the top-level:

```go
package engine

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/JtheGunner/omnishell/internal/atomicfile"
	"github.com/JtheGunner/omnishell/internal/backup"
	"github.com/JtheGunner/omnishell/internal/buildinfo"
	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/JtheGunner/omnishell/internal/initfile"
	"github.com/JtheGunner/omnishell/internal/lockfile"
	"github.com/JtheGunner/omnishell/internal/rcfile"
	"github.com/JtheGunner/omnishell/internal/render"
)

var (
	ErrAborted    = errors.New("aborted by user")
	ErrHandEdited = errors.New("init file has been edited by hand")
	ErrDegraded   = errors.New("one or more modules are degraded")
)

// ApplyOptions are the flags of `omnishell apply`.
type ApplyOptions struct {
	DryRun, Yes, NoPackages, Force, Verbose bool
}

// ModuleResult is the per-module outcome of an apply.
type ModuleResult struct {
	ID     string
	Action ModuleAction
	Status string // applied | degraded | skipped | removed | unchanged
	Note   string
}

// Result is the outcome of Apply.
type Result struct {
	DryRun           bool
	PlanText         string
	Modules          []ModuleResult
	InitFilesWritten []string
	BackupDir        string
	Changed          bool
}

// Apply brings the system to the desired state described by cfg.
func (e Engine) Apply(cfg config.Config, cfgPath, lockPath string, opts ApplyOptions) (Result, error) {
	lock, _, err := lockfile.Load(lockPath)
	if err != nil {
		return Result{}, err
	}
	plan, err := ComputePlan(e, cfg, lock, opts.NoPackages)
	if err != nil {
		return Result{}, err
	}
	res := Result{PlanText: RenderPlan(plan)}

	if opts.DryRun {
		res.DryRun = true
		return res, nil
	}

	drift := e.initOrRCDrift(plan, lock)
	if !plan.HasChanges && !drift {
		return res, nil // idempotent no-op
	}

	if !opts.Yes && e.Prompt != nil {
		fmt.Fprintln(e.Stdout, res.PlanText)
		if !e.Prompt("Proceed?") {
			return res, ErrAborted
		}
	}

	degraded := map[string]string{}
	vendorPaths := map[string][]string{}
	installedNow := map[string]map[string]bool{}
	if !opts.NoPackages {
		e.installPackages(plan, degraded, vendorPaths, installedNow)
	}
	e.runCheckHooks(plan, degraded)

	sectionsByShell := map[string][]initfile.Section{}
	for _, shell := range plan.ManagedShells {
		sectionsByShell[shell] = e.buildSections(plan, degraded, shell)
	}

	// Hand-edit guard — before any write.
	for _, shell := range plan.ManagedShells {
		initPath := e.initPath(shell)
		existing, rerr := os.ReadFile(initPath)
		if rerr == nil {
			edited, _ := initfile.DetectHandEdit(string(existing), sectionsByShell[shell])
			if edited && !opts.Force {
				return res, fmt.Errorf("%w: %s (run with --force to overwrite)", ErrHandEdited, initPath)
			}
		}
	}

	bk, _ := backup.NewSession(e.Platform.ConfigDir, e.now())
	res.BackupDir = bk.Dir
	newLock := e.rebuildLock(cfg, plan, lock, degraded, vendorPaths, installedNow)

	for _, shell := range plan.ManagedShells {
		initPath := e.initPath(shell)
		if _, err := bk.Save(initPath); err != nil {
			return res, err
		}
		content := initfile.Build(shell, sectionsByShell[shell], e.now())
		if err := atomicfile.WriteFile(initPath, []byte(content), 0o644); err != nil {
			return res, err
		}
		res.InitFilesWritten = append(res.InitFilesWritten, initPath)
		newLock.InitFiles[shell] = lockfile.FileState{
			Path:        e.homeRelative(initPath),
			ContentHash: initfile.ContentHash(sectionsByShell[shell]),
		}
		if rcErr := e.ensureRC(shell, initPath, bk, &newLock); rcErr != nil {
			return res, rcErr
		}
	}

	newLock.Schema = lockfile.SchemaVersion
	newLock.OmnishellVersion = buildinfo.Version
	newLock.LastApply = e.now().UTC().Format(time.RFC3339)
	newLock.Platform = string(e.Platform.OS)
	if plan.ManagerAvailable {
		newLock.PackageManager = plan.PackageManager
	}
	if err := newLock.Write(lockPath); err != nil {
		return res, err
	}

	res.Changed = true
	res.Modules = summarise(plan, degraded)
	for _, m := range res.Modules {
		if m.Status == "degraded" {
			return res, ErrDegraded
		}
	}
	return res, nil
}
```

Then implement each helper (`initPath`, `homeRelative`, `initOrRCDrift`, `installPackages`, `runCheckHooks`, `buildSections`, `ensureRC`, `rebuildLock`, `summarise`) in the same file or a sibling `apply_helpers.go`. Every helper is straightforward given the types; write a focused unit test for `homeRelative` and `initOrRCDrift`.

Key detail for `buildSections`: iterate `plan.Order`; skip if `degraded[id] != ""` or module not in `plan.Modules` or `plan.Modules[id].Action == ActionRemove`; skip if `!contains(mp.Shells, shell)`; `mod, _ := e.Registry.Get(id)`; `body, _, _ := mod.Template(shell)`; `rendered, rerr := render.Render(body, e.renderContext(mp, shell, activeSet))`; on `rerr` set `degraded[id]` and restart the shell's section build once (or pre-filter: render all first, collect failures, then build). Simplest: a first pass renders every module for every shell into a `map[[2]string]string`; failures populate `degraded`; a second pass assembles sections excluding degraded.

- [ ] **Step 4: Run, expect pass**

Run: `go test ./internal/engine/ -run Apply -v` → PASS. Then `go test ./... -race` → all green.

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "feat: implement engine Apply (plan, install, render, write, lock)

Claude-Session: https://claude.ai/code/session_012MEGbPB16gL53nKQwspocK"
```

---

## Task 20: `engine` package — `Remove`

**Files:**
- Create: `internal/engine/remove.go`
- Test: `internal/engine/remove_test.go`

**Interfaces:**
- Consumes: `Engine.Apply`, `config.SetEnabled`, `lockfile`, `pkgmgr`.
- Produces:
  - `(Engine) Remove(cfg config.Config, cfgPath, lockPath, moduleID string, purge bool, opts ApplyOptions) (Result, error)`:
    1. Error if `moduleID` is not a known registry module **and** not present in the lock (`fmt.Errorf("unknown module %q", id)`).
    2. `config.SetEnabled(cfgPath, moduleID, false)` — persist the disable.
    3. Re-load config from `cfgPath` (so the in-memory `cfg` matches disk).
    4. Call `e.Apply(reloadedCfg, cfgPath, lockPath, opts)` — this drops the module's section and rewrites init files + lock (Apply already treats a lock-present-but-inactive module as `ActionRemove`).
    5. If `purge`: for each `lockfile.PackageState` with `InstalledByOmnishell` in the **pre-Apply** lock entry for `moduleID`, and for a module with a `remove` hook, run removal:
       - packages: `e.Manager.Install` has no uninstall; add `pkgmgr.Manager.Remove(pkgs []string) error` to the interface? **No** — keep the interface minimal. Instead `Remove` builds the uninstall argv per manager via a new `pkgmgr.UninstallArgv(managerName string, pkgs []string) []string` pure function and runs it through `e.Runner`, announcing sudo where needed. Table: brew→`brew uninstall <pkgs>`; apt→`sudo apt-get remove -y <pkgs>`; dnf→`sudo dnf remove -y <pkgs>`; pacman→`sudo pacman -Rs --noconfirm <pkgs>`; zypper→`sudo zypper remove -y <pkgs>`; apk→`sudo apk del <pkgs>`.
       - hook: materialise `hooks/remove.sh` (`0o700`) and run it.
       - vendor paths from the lock entry: `os.RemoveAll` each.
    6. Return the `Result` from Apply, with `res.Modules` amended to show `moduleID` as `Status: "removed"` (and `"purged"` in `Note` when `purge`).
  - Add to `pkgmgr`: `pkgmgr.UninstallArgv(manager string, pkgs []string) []string` + test.

- [ ] **Step 1: Write failing test `internal/pkgmgr` for `UninstallArgv`** (in `managers_test.go`)

```go
func TestUninstallArgv(t *testing.T) {
	cases := map[string][]string{
		"brew":   {"brew", "uninstall", "fzf"},
		"apt":    {"sudo", "apt-get", "remove", "-y", "fzf"},
		"pacman": {"sudo", "pacman", "-Rs", "--noconfirm", "fzf"},
		"apk":    {"sudo", "apk", "del", "fzf"},
	}
	for mgr, want := range cases {
		got := pkgmgr.UninstallArgv(mgr, []string{"fzf"})
		if strings.Join(got, " ") != strings.Join(want, " ") {
			t.Fatalf("%s: got %v want %v", mgr, got, want)
		}
	}
}
```

- [ ] **Step 2: Implement `pkgmgr.UninstallArgv`** in `internal/pkgmgr/managers.go`

```go
// UninstallArgv returns the command to remove pkgs with the named manager.
func UninstallArgv(manager string, pkgs []string) []string {
	switch manager {
	case "brew":
		return append([]string{"brew", "uninstall"}, pkgs...)
	case "apt":
		return append([]string{"sudo", "apt-get", "remove", "-y"}, pkgs...)
	case "dnf":
		return append([]string{"sudo", "dnf", "remove", "-y"}, pkgs...)
	case "pacman":
		return append([]string{"sudo", "pacman", "-Rs", "--noconfirm"}, pkgs...)
	case "zypper":
		return append([]string{"sudo", "zypper", "remove", "-y"}, pkgs...)
	case "apk":
		return append([]string{"sudo", "apk", "del"}, pkgs...)
	default:
		return nil
	}
}
```

- [ ] **Step 3: Write failing test `internal/engine/remove_test.go`**

```go
package engine_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/JtheGunner/omnishell/internal/engine"
	"github.com/JtheGunner/omnishell/internal/lockfile"
	"github.com/JtheGunner/omnishell/internal/pkgmgr"
)

func TestRemoveDropsSectionAndDisablesInConfig(t *testing.T) {
	home := t.TempDir()
	var out bytes.Buffer
	mgr := &pkgmgr.MockManager{NameV: "apt", DetectV: true, Installed: map[string]bool{}}
	e := applyEngine(t, home, mgr, &out)
	cfgPath := filepath.Join(home, ".config", "omnishell", "config.toml")
	lockPath := filepath.Join(home, ".config", "omnishell", "state.lock.json")
	writeConfig(t, cfgPath, "[omnishell]\nversion=1\nshells=[\"bash\"]\n[modules.completion]\nenabled=true\n[modules.fzf]\nenabled=true\n[modules.fzf.options]\nctrl_r=true\n")
	cfg, _ := config.Load(cfgPath)
	if _, err := e.Apply(cfg, cfgPath, lockPath, engine.ApplyOptions{Yes: true}); err != nil {
		t.Fatal(err)
	}

	cfg2, _ := config.Load(cfgPath)
	res, err := e.Remove(cfg2, cfgPath, lockPath, "fzf", false, engine.ApplyOptions{Yes: true})
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if !res.Changed {
		t.Fatal("Remove made no change")
	}
	body, _ := os.ReadFile(filepath.Join(home, ".config", "omnishell", "init.bash"))
	if strings.Contains(string(body), "omnishell:fzf") {
		t.Fatalf("fzf section still present:\n%s", body)
	}
	after, _ := config.Load(cfgPath)
	if after.Modules["fzf"].Enabled {
		t.Fatal("fzf still enabled in config after Remove")
	}
	lock, _, _ := lockfile.Load(lockPath)
	if _, ok := lock.Modules["fzf"]; ok {
		t.Fatal("fzf still in lockfile after Remove")
	}
}

func TestRemovePurgeUninstallsOmnishellPackages(t *testing.T) {
	home := t.TempDir()
	var out bytes.Buffer
	r := &pkgmgr.MockRunner{}
	mgr := &pkgmgr.MockManager{NameV: "apt", DetectV: true, Installed: map[string]bool{}, SudoV: true}
	e := applyEngine(t, home, mgr, &out)
	e.Runner = r
	cfgPath := filepath.Join(home, ".config", "omnishell", "config.toml")
	lockPath := filepath.Join(home, ".config", "omnishell", "state.lock.json")
	writeConfig(t, cfgPath, "[omnishell]\nversion=1\nshells=[\"bash\"]\n[modules.fzf]\nenabled=true\n[modules.fzf.options]\nctrl_r=true\n")
	cfg, _ := config.Load(cfgPath)
	if _, err := e.Apply(cfg, cfgPath, lockPath, engine.ApplyOptions{Yes: true}); err != nil {
		t.Fatal(err)
	}

	cfg2, _ := config.Load(cfgPath)
	if _, err := e.Remove(cfg2, cfgPath, lockPath, "fzf", true, engine.ApplyOptions{Yes: true}); err != nil {
		t.Fatalf("Remove --purge: %v", err)
	}
	joined := strings.Join(r.Calls, "\n")
	if !strings.Contains(joined, "sudo apt-get remove -y fzf") {
		t.Fatalf("purge did not uninstall fzf; calls:\n%s", joined)
	}
}
```

- [ ] **Step 4: Run, expect failure → implement `internal/engine/remove.go` → run, expect pass**

Run: `go test ./internal/engine/ -run Remove -v` and `go test ./internal/pkgmgr/ -run Uninstall -v` → PASS.

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "feat: implement engine Remove with optional package purge

Claude-Session: https://claude.ai/code/session_012MEGbPB16gL53nKQwspocK"
```

---

## Task 21: `engine` package — `Doctor`

**Files:**
- Create: `internal/engine/doctor.go`
- Test: `internal/engine/doctor_test.go`

**Interfaces:**
- Consumes: `ComputePlan`, `initfile.DetectHandEdit`, `rcfile.BlockPresent`, `lockfile`, `pkgmgr`.
- Produces:
  - `engine.Finding struct { Severity string; Code string; Message string }` — `Severity` ∈ `"drift"`, `"notice"`.
  - `engine.DoctorReport struct { Findings []Finding }`; `(DoctorReport) HasDrift() bool` = any finding with `Severity == "drift"`.
  - `(Engine) Doctor(cfg config.Config, cfgPath, lockPath string) (DoctorReport, error)` — read-only. Checks, each appending a `Finding`:
    1. Lock missing but config has ≥1 enabled module → drift `never-applied`.
    2. `ComputePlan` returns a `ConfigError` → return it (CLI maps to exit 2), not a finding.
    3. For each managed shell: init file missing → drift `initfile-missing:<shell>`; present but `initfile.DetectHandEdit` true → drift `initfile-edited:<shell>`; present and hash ≠ `lock.InitFiles[shell].ContentHash` → drift `initfile-stale:<shell>`.
    4. For each managed shell with a real rc path: file missing or `!rcfile.BlockPresent` → drift `rc-block-missing:<shell>`.
    5. For each enabled module: `ComputePlan` marked it with `MissingPackages` (non-`NoPackages` plan) → drift `packages-missing:<id>` listing names; `DegradedReason` set → drift `module-degraded:<id>`.
    6. Lock has a module not in `cfg` (orphan) → drift `orphan-lock-entry:<id>`.
    7. `e.Registry.Overrides()` non-empty → **notice** `module-override:<id>` (does not set drift).
    8. Plan `Action` for an enabled module is `ActionUpdate` (config changed since last apply) → drift `pending-apply:<id>`.
  - Deterministic order: findings appended in the numbered order above; within a step, iterate ids sorted.

- [ ] **Step 1: Write failing test `internal/engine/doctor_test.go`**

```go
package engine_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/JtheGunner/omnishell/internal/engine"
	"github.com/JtheGunner/omnishell/internal/pkgmgr"
)

func TestDoctorCleanAfterApply(t *testing.T) {
	home := t.TempDir()
	var out bytes.Buffer
	mgr := &pkgmgr.MockManager{NameV: "apt", DetectV: true, Installed: map[string]bool{}}
	e := applyEngine(t, home, mgr, &out)
	cfgPath := filepath.Join(home, ".config", "omnishell", "config.toml")
	lockPath := filepath.Join(home, ".config", "omnishell", "state.lock.json")
	writeConfig(t, cfgPath, "[omnishell]\nversion=1\nshells=[\"bash\"]\n[modules.completion]\nenabled=true\n")
	cfg, _ := config.Load(cfgPath)
	if _, err := e.Apply(cfg, cfgPath, lockPath, engine.ApplyOptions{Yes: true}); err != nil {
		t.Fatal(err)
	}
	rep, err := e.Doctor(cfg, cfgPath, lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if rep.HasDrift() {
		t.Fatalf("clean system reported drift: %+v", rep.Findings)
	}
}

func TestDoctorDetectsHandEditAndMissingRCBlock(t *testing.T) {
	home := t.TempDir()
	var out bytes.Buffer
	mgr := &pkgmgr.MockManager{NameV: "apt", DetectV: true, Installed: map[string]bool{}}
	e := applyEngine(t, home, mgr, &out)
	cfgPath := filepath.Join(home, ".config", "omnishell", "config.toml")
	lockPath := filepath.Join(home, ".config", "omnishell", "state.lock.json")
	writeConfig(t, cfgPath, "[omnishell]\nversion=1\nshells=[\"bash\"]\n[modules.completion]\nenabled=true\n")
	cfg, _ := config.Load(cfgPath)
	if _, err := e.Apply(cfg, cfgPath, lockPath, engine.ApplyOptions{Yes: true}); err != nil {
		t.Fatal(err)
	}
	// tamper init file, wipe .bashrc
	initBash := filepath.Join(home, ".config", "omnishell", "init.bash")
	b, _ := os.ReadFile(initBash)
	os.WriteFile(initBash, append(b, []byte("\nrm -rf /\n")...), 0o644)
	os.WriteFile(filepath.Join(home, ".bashrc"), []byte("# nothing here\n"), 0o644)

	rep, err := e.Doctor(cfg, cfgPath, lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.HasDrift() {
		t.Fatal("expected drift")
	}
	codes := map[string]bool{}
	for _, f := range rep.Findings {
		codes[f.Code] = true
	}
	if !codes["initfile-edited:bash"] || !codes["rc-block-missing:bash"] {
		t.Fatalf("missing expected findings: %+v", rep.Findings)
	}
}

func TestDoctorNeverApplied(t *testing.T) {
	home := t.TempDir()
	var out bytes.Buffer
	e := applyEngine(t, home, &pkgmgr.MockManager{NameV: "apt", DetectV: true}, &out)
	cfgPath := filepath.Join(home, ".config", "omnishell", "config.toml")
	lockPath := filepath.Join(home, ".config", "omnishell", "state.lock.json")
	writeConfig(t, cfgPath, "[omnishell]\nversion=1\nshells=[\"bash\"]\n[modules.completion]\nenabled=true\n")
	cfg, _ := config.Load(cfgPath)
	rep, err := e.Doctor(cfg, cfgPath, lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.HasDrift() || rep.Findings[0].Code != "never-applied" {
		t.Fatalf("want never-applied drift, got %+v", rep.Findings)
	}
}
```

- [ ] **Step 2: Run → implement `internal/engine/doctor.go` → run**

Run: `go test ./internal/engine/ -run Doctor -v` → PASS.

- [ ] **Step 3: Commit**

```bash
git add -A
git commit -m "feat: implement engine Doctor drift detection

Claude-Session: https://claude.ai/code/session_012MEGbPB16gL53nKQwspocK"
```

---

## Task 22: `engine` package — `Uninstall`

**Files:**
- Create: `internal/engine/uninstall.go`
- Test: `internal/engine/uninstall_test.go`

**Interfaces:**
- Consumes: `rcfile.RemoveBlock`, `backup`, `atomicfile`, `lockfile`, `platform`.
- Produces:
  - `engine.UninstallOptions struct { PurgeConfigDir bool; Yes bool }`.
  - `(Engine) Uninstall(lockPath string, opts UninstallOptions) (Result, error)`:
    1. `lock, exists, _ := lockfile.Load(lockPath)`. If `!exists` and no init files found under `ConfigDir` → return `Result{}` with a note "nothing to uninstall".
    2. Prompt (unless `opts.Yes`) via `e.Prompt("Remove omnishell's shell integration?")`; false → `ErrAborted`.
    3. `bk := backup.NewSession(...)`.
    4. For each shell in `platform.SupportedShells`: rc path from `e.Platform.Shells`; if the file exists and `rcfile.BlockPresent` → `bk.Save`, `rcfile.RemoveBlock`, `atomicfile.WriteFile`.
    5. Delete `ConfigDir/init.zsh` and `ConfigDir/init.bash` if present (back them up first).
    6. If `opts.PurgeConfigDir`: `os.RemoveAll(e.Platform.ConfigDir)` **except** the just-created backup dir — so: move backups aside? Simpler: refuse to purge if the backup dir is inside ConfigDir (it is). Resolution: when `PurgeConfigDir`, write backups to a sibling temp dir (`os.MkdirTemp("", "omnishell-uninstall-*")`) and print its path; then `RemoveAll(ConfigDir)`.
    7. Otherwise leave `config.toml`, `state.lock.json`, `modules/`, `vendor/`, `backups/` in place; delete only `state.lock.json`? **No** — keep the lock unless purging. Just remove init files + rc blocks.
    8. Return a `Result` with `Changed` true if anything was removed, `BackupDir` set, and a `Modules` list noting each shell block removed.
- **Does not** uninstall packages — that is `remove <id> --purge` territory; document this in the command help.

- [ ] **Step 1: Write failing test `internal/engine/uninstall_test.go`**

```go
package engine_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/JtheGunner/omnishell/internal/engine"
	"github.com/JtheGunner/omnishell/internal/pkgmgr"
)

func TestUninstallRemovesRCBlockAndInitFiles(t *testing.T) {
	home := t.TempDir()
	var out bytes.Buffer
	mgr := &pkgmgr.MockManager{NameV: "apt", DetectV: true, Installed: map[string]bool{}}
	e := applyEngine(t, home, mgr, &out)
	cfgPath := filepath.Join(home, ".config", "omnishell", "config.toml")
	lockPath := filepath.Join(home, ".config", "omnishell", "state.lock.json")
	rc := filepath.Join(home, ".bashrc")
	os.WriteFile(rc, []byte("export X=1\n"), 0o644)
	writeConfig(t, cfgPath, "[omnishell]\nversion=1\nshells=[\"bash\"]\n[modules.completion]\nenabled=true\n")
	cfg, _ := config.Load(cfgPath)
	if _, err := e.Apply(cfg, cfgPath, lockPath, engine.ApplyOptions{Yes: true}); err != nil {
		t.Fatal(err)
	}

	res, err := e.Uninstall(lockPath, engine.UninstallOptions{Yes: true})
	if err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if !res.Changed {
		t.Fatal("Uninstall made no change")
	}
	got, _ := os.ReadFile(rc)
	if strings.Contains(string(got), "omnishell") {
		t.Fatalf(".bashrc still has omnishell block:\n%s", got)
	}
	if string(got) != "export X=1\n" {
		t.Fatalf(".bashrc not restored cleanly:\n%q", got)
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "omnishell", "init.bash")); !os.IsNotExist(err) {
		t.Fatal("init.bash still present after uninstall")
	}
}

func TestUninstallPurgeRemovesConfigDir(t *testing.T) {
	home := t.TempDir()
	var out bytes.Buffer
	mgr := &pkgmgr.MockManager{NameV: "apt", DetectV: true, Installed: map[string]bool{}}
	e := applyEngine(t, home, mgr, &out)
	cfgPath := filepath.Join(home, ".config", "omnishell", "config.toml")
	lockPath := filepath.Join(home, ".config", "omnishell", "state.lock.json")
	writeConfig(t, cfgPath, "[omnishell]\nversion=1\nshells=[\"bash\"]\n[modules.completion]\nenabled=true\n")
	cfg, _ := config.Load(cfgPath)
	if _, err := e.Apply(cfg, cfgPath, lockPath, engine.ApplyOptions{Yes: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Uninstall(lockPath, engine.UninstallOptions{Yes: true, PurgeConfigDir: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "omnishell")); !os.IsNotExist(err) {
		t.Fatal("config dir still present after --purge")
	}
}
```

- [ ] **Step 2: Run → implement `internal/engine/uninstall.go` → run**

Run: `go test ./internal/engine/ -v` → PASS. `go test ./... -race` → green.

- [ ] **Step 3: Commit**

```bash
git add -A
git commit -m "feat: implement engine Uninstall

Claude-Session: https://claude.ai/code/session_012MEGbPB16gL53nKQwspocK"
```

---

## Task 23: CLI wiring — `engine` construction, exit mapping, `init`, `list`

**Files:**
- Create: `internal/cli/context.go`, `internal/cli/init.go`, `internal/cli/list.go`
- Modify: `internal/cli/root.go` (attach new commands), `internal/cli/exit.go` (real mapping)
- Modify: `modules/embed.go` — create it now as a stub returning an empty `fs.FS` (real modules land in Phase 9)
- Test: `internal/cli/init_test.go`, `internal/cli/list_test.go`, `internal/cli/exit_test.go`

**Interfaces:**
- Consumes: `platform.Detect`, `module.LoadRegistry`, `pkgmgr.DetectManager`, `engine.*`, `config`.
- Produces:
  - `modules.FS() fs.FS` — returns the embedded built-in modules filesystem (stub: `fstest.MapFS{}` equivalent — actually return an `embed.FS` over an empty `modules/.keep`; simplest is `//go:embed all:builtin` once Phase 9 adds `modules/builtin/`; for now embed a placeholder dir `modules/builtin/.keep` and `fs.Sub` to `"builtin"`).
  - `cli.buildEngine(stdout, stderr io.Writer, cfg config.Config) (engine.Engine, string, string, error)` — detects platform, loads registry (`modules.FS()` + `<ConfigDir>/modules`), detects the package manager, returns the engine plus `cfgPath` and `lockPath` (`<ConfigDir>/config.toml`, `<ConfigDir>/state.lock.json`). `Prompt` reads a `y/N` line from stdin (a package-level `var promptFn = defaultPrompt` so tests can override).
  - `cli.errDrift = errors.New("drift detected")` — defined here in `exit.go`, package-private; only the `doctor` command returns it (Task 25).
  - `cli.ClassifyError(err error) int` — `nil`→0; `errors.As(err, &engine.ConfigError{})` or `errors.As(err, &config.Error{})` or `errors.Is(err, config.ErrNotFound)`→2; `errors.Is(err, cli.errDrift)`→3; `errors.Is(err, engine.ErrDegraded)` or `errors.Is(err, engine.ErrAborted)`→1; else→1.
  - `cli.newInitCmd`, `cli.newListCmd` returning `*cobra.Command`.
- **`init` behaviour:** create `<ConfigDir>/config.toml` from `config.RenderDefault()` if absent (never overwrite); for each detected present shell, ensure the rc marker block pointing at `<ConfigDir>/init.<shell>` (create an empty init file so the `source` guard passes). Print what it did. Idempotent.
- **`list` behaviour:** table to stdout — columns `MODULE`, `STATUS` (`enabled`/`disabled`/`—`), `PACKAGES` (`ok`/`missing`/`n/a`), `PLATFORMS`, `SHELLS`, `SRC` (`builtin`/`user`, `user*` if it overrides). Sorted by id. `--json` flag emits a JSON array instead.

- [ ] **Step 1: Create `modules/embed.go` (stub)**

```go
// Package modules embeds the built-in module folders.
package modules

import (
	"embed"
	"io/fs"
)

//go:embed all:builtin
var builtin embed.FS

// FS returns the built-in modules filesystem, rooted so each entry is a module dir.
func FS() fs.FS {
	sub, err := fs.Sub(builtin, "builtin")
	if err != nil {
		panic(err)
	}
	return sub
}
```
Create `modules/builtin/.keep` (empty file) so the embed compiles.

- [ ] **Step 2: Write failing tests**

`internal/cli/exit_test.go`:
```go
package cli_test

import (
	"errors"
	"testing"

	"github.com/JtheGunner/omnishell/internal/cli"
	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/JtheGunner/omnishell/internal/engine"
)

func TestClassifyError(t *testing.T) {
	if cli.ClassifyError(nil) != 0 {
		t.Fatal("nil → 0")
	}
	if cli.ClassifyError(config.Error{Path: "x", Msg: "y"}) != 2 {
		t.Fatal("config.Error → 2")
	}
	if cli.ClassifyError(engine.ConfigError{Err: errors.New("bad")}) != 2 {
		t.Fatal("engine.ConfigError → 2")
	}
	if cli.ClassifyError(engine.ErrDegraded) != 1 {
		t.Fatal("ErrDegraded → 1")
	}
}
```

`internal/cli/init_test.go`:
```go
package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JtheGunner/omnishell/internal/cli"
)

func TestInitCreatesConfigAndRCBlock(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	os.WriteFile(filepath.Join(home, ".zshrc"), []byte("export A=1\n"), 0o644)
	// Make zsh look present:
	cli.SetLookPathForTest(func(bin string) (string, error) {
		if bin == "zsh" {
			return "/bin/zsh", nil
		}
		return "", os.ErrNotExist
	})
	defer cli.SetLookPathForTest(nil)

	var out, errb bytes.Buffer
	code := cli.Execute([]string{"init"}, &out, &errb)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	cfg := filepath.Join(home, ".config", "omnishell", "config.toml")
	if _, err := os.Stat(cfg); err != nil {
		t.Fatalf("config.toml not created: %v", err)
	}
	rc, _ := os.ReadFile(filepath.Join(home, ".zshrc"))
	if !strings.Contains(string(rc), "# >>> omnishell >>>") {
		t.Fatalf(".zshrc missing block:\n%s", rc)
	}
	// idempotent
	if code := cli.Execute([]string{"init"}, &out, &errb); code != 0 {
		t.Fatalf("second init exit %d", code)
	}
}
```

- **Note:** add `cli.SetLookPathForTest(func(string)(string,error))` and a `var lookPath = exec.LookPath` seam in `context.go`, used by `buildEngine`'s platform detection (pass a custom `platform.Env`).

`internal/cli/list_test.go`: run `list` after `init` + enabling a module, assert the module row shows `enabled`.

- [ ] **Step 3: Run → implement `context.go`, `exit.go`, `init.go`, `list.go`, wire into `root.go` → run**

Run: `go test ./internal/cli/ -v` → PASS.

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "feat: add engine wiring, init and list commands

Claude-Session: https://claude.ai/code/session_012MEGbPB16gL53nKQwspocK"
```

---

## Task 24: CLI — `enable`, `disable`, `set`

**Files:**
- Create: `internal/cli/enable.go` (holds `enable` + `disable`), `internal/cli/set.go`
- Modify: `internal/cli/root.go`
- Test: `internal/cli/enable_test.go`, `internal/cli/set_test.go`

**Interfaces:**
- Consumes: `config.SetEnabled`, `config.SetOption`, `module.Registry`, `module.OptionSchema.ParseValue`.
- Produces:
  - `cli.newEnableCmd() *cobra.Command` — `omnishell enable <id>`: error (exit 2) if `<id>` is unknown to the registry; else `config.SetEnabled(cfgPath, id, true)`; print `enabled <id> — run 'omnishell apply' to apply`. `disable` is the mirror.
  - `cli.newSetCmd() *cobra.Command` — `omnishell set <id>.<key> <value>`: split the first arg on the last `.`; error if `<id>` unknown or `<key>` not in that module's option schema (exit 2); `typed, err := schema[key].ParseValue(value)` (exit 2 on parse/enum error); `config.SetOption(cfgPath, id, key, typed)`; print confirmation.
- Both require the config file to exist (`init` first); if `config.ErrNotFound`, print `run 'omnishell init' first` and exit 2.

- [ ] **Step 1: Write failing tests**

`internal/cli/enable_test.go`:
```go
func TestEnableUnknownModuleExitsTwo(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	var out, errb bytes.Buffer
	cli.Execute([]string{"init"}, &out, &errb)
	code := cli.Execute([]string{"enable", "nonexistent"}, &out, &errb)
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
}

func TestEnableThenConfigReflectsIt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	var out, errb bytes.Buffer
	cli.Execute([]string{"init"}, &out, &errb)
	if code := cli.Execute([]string{"enable", "completion"}, &out, &errb); code != 0 {
		t.Fatalf("enable exit %d: %s", code, errb.String())
	}
	c, err := config.Load(filepath.Join(home, ".config", "omnishell", "config.toml"))
	if err != nil || !c.Modules["completion"].Enabled {
		t.Fatalf("completion not enabled: %+v err=%v", c.Modules, err)
	}
}
```

`internal/cli/set_test.go`: `set fzf.ctrl_r false` then load config and assert `Options["ctrl_r"] == false`; `set fzf.bogus x` → exit 2; `set modern-aliases.replace "ls,grep"` → exit 2 (grep not in enum).

**Note:** these tests need `completion` / `fzf` / `modern-aliases` in the registry. Until Phase 9, point the tests at a fixture modules dir via `XDG` config `modules/` folder, OR gate these assertions behind Phase 9. Decision: create `internal/cli/testdata/modules/` with minimal `completion`, `fzf`, `modern-aliases` manifests and set `t.Setenv` so `<ConfigDir>/modules` resolves there (copy the fixtures into `<home>/.config/omnishell/modules/` in test setup). Add a helper `installFixtureModules(t, configDir)`.

- [ ] **Step 2: Run → implement → run** → PASS.

- [ ] **Step 3: Commit**

```bash
git add -A
git commit -m "feat: add enable, disable, set commands

Claude-Session: https://claude.ai/code/session_012MEGbPB16gL53nKQwspocK"
```

---

## Task 25: CLI — `apply`, `diff`, `doctor`

**Files:**
- Create: `internal/cli/apply.go` (holds `apply` + `diff`), `internal/cli/doctor.go`
- Modify: `internal/cli/root.go`
- Test: `internal/cli/apply_test.go`, `internal/cli/doctor_test.go`

**Interfaces:**
- Consumes: `engine.Apply`, `engine.Doctor`, `config.Load`.
- Produces:
  - `cli.newApplyCmd() *cobra.Command` — flags `--dry-run`, `--yes`/`-y`, `--no-packages`, `--force`, `--verbose` (verbose is the persistent flag). Loads config (`ErrNotFound`→ hint + exit 2), builds engine, calls `e.Apply(cfg, cfgPath, lockPath, opts)`. Prints `res.PlanText` on dry-run; prints a per-module summary + `res.BackupDir` otherwise. Maps the returned error via `ClassifyError` (so `ErrDegraded`→1 but the summary still prints; `ErrAborted`→1 with "aborted").
  - `cli.newDiffCmd()` — literally runs `apply` logic with `DryRun=true` forced.
  - `cli.newDoctorCmd()` — calls `e.Doctor`; prints each finding as `[drift] <code>  <message>` / `[notice] ...`; if `report.HasDrift()` returns a `cli.errDrift` sentinel so `Execute` maps it to exit 3; a clean report prints `no drift detected` and exits 0. A `ConfigError` → exit 2.
  - (`cli.errDrift` is defined in `exit.go` in Task 23; `doctor` returns it when `report.HasDrift()`.)

- [ ] **Step 1: Write failing tests**

`internal/cli/apply_test.go`:
```go
func TestApplyCommandEndToEnd(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	cli.SetLookPathForTest(func(bin string) (string, error) {
		if bin == "bash" || bin == "brew" {
			return "/bin/" + bin, nil
		}
		return "", os.ErrNotExist
	})
	defer cli.SetLookPathForTest(nil)
	cli.SetRunnerForTest(&pkgmgr.MockRunner{
		LookOK:    map[string]bool{"brew": true},
		Responses: map[string]pkgmgr.MockResponse{"brew list --versions fzf": {Out: []byte("")}, "brew install fzf": {Out: []byte("ok")}},
	})
	defer cli.SetRunnerForTest(nil)

	installFixtureModules(t, filepath.Join(home, ".config", "omnishell"))
	var out, errb bytes.Buffer
	cli.Execute([]string{"init"}, &out, &errb)
	cli.Execute([]string{"enable", "completion"}, &out, &errb)
	cli.Execute([]string{"enable", "fzf"}, &out, &errb)

	code := cli.Execute([]string{"apply", "--yes"}, &out, &errb)
	if code != 0 {
		t.Fatalf("apply exit %d: %s\n%s", code, errb.String(), out.String())
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "omnishell", "init.bash")); err != nil {
		t.Fatalf("init.bash missing: %v", err)
	}
}

func TestApplyDryRunExitsZeroAndWritesNothing(t *testing.T) { /* ... assert no init.bash, output contains "Plan (" */ }
```

`internal/cli/doctor_test.go`: after a successful `apply`, `doctor` → exit 0; after tampering init file, `doctor` → exit 3 and stderr/stdout contains `initfile-edited:bash`.

- **Note:** add `cli.SetRunnerForTest(pkgmgr.Runner)` seam (`var runnerOverride pkgmgr.Runner` used by `buildEngine`).

- [ ] **Step 2: Run → implement → run** → PASS. `go test ./... -race` green.

- [ ] **Step 3: Commit**

```bash
git add -A
git commit -m "feat: add apply, diff, doctor commands

Claude-Session: https://claude.ai/code/session_012MEGbPB16gL53nKQwspocK"
```

---

## Task 26: CLI — `remove`, `uninstall`

**Files:**
- Create: `internal/cli/remove.go`, `internal/cli/uninstall.go`
- Modify: `internal/cli/root.go`
- Test: `internal/cli/remove_test.go`, `internal/cli/uninstall_test.go`

**Interfaces:**
- Consumes: `engine.Remove`, `engine.Uninstall`.
- Produces:
  - `cli.newRemoveCmd()` — `omnishell remove <id>`, flags `--purge`, `--yes`/`-y`, `--dry-run`. Loads config, builds engine, `e.Remove(cfg, cfgPath, lockPath, id, purge, opts)`. Prints summary + backup dir. Unknown id (not in registry and not in lock) → exit 1 with a clear message.
  - `cli.newUninstallCmd()` — `omnishell uninstall`, flags `--purge` (removes `~/.config/omnishell` entirely), `--yes`/`-y`. Calls `e.Uninstall(lockPath, engine.UninstallOptions{PurgeConfigDir: purge, Yes: yes})`. Prints which rc blocks and init files were removed, the backup location (and, on `--purge`, the temp backup path). Help text notes it does **not** uninstall packages.

- [ ] **Step 1: Write failing tests**

`internal/cli/remove_test.go`: end-to-end — `init`, enable `completion`+`fzf`, `apply --yes`, then `remove fzf --yes`; assert `init.bash` no longer contains `omnishell:fzf`, config has `fzf` disabled, exit 0.

`internal/cli/uninstall_test.go`: after `apply`, `uninstall --yes` restores `.bashrc` (no `omnishell` markers) and deletes `init.bash`; `uninstall --yes --purge` also removes the config dir.

- [ ] **Step 2: Run → implement → run** → PASS.

- [ ] **Step 3: Commit**

```bash
git add -A
git commit -m "feat: add remove and uninstall commands

Claude-Session: https://claude.ai/code/session_012MEGbPB16gL53nKQwspocK"
```

---

## Phase 9 — Built-in modules

Each built-in module is a folder under `modules/builtin/<id>/`. `modules/embed.go` (Task 23) already embeds `all:builtin`. Delete `modules/builtin/.keep` once the first real module exists. Every module task:
1. writes `manifest.toml` + `zsh.tmpl` (+ `bash.tmpl` where the module supports bash) (+ `hooks/` where needed),
2. adds a **golden test** in `modules/builtin_test.go` that loads the module through `module.LoadRegistry(modules.FS(), "")`, renders each shell template with a fixed option set, and compares to `modules/builtin/<id>/testdata/<shell>.golden`,
3. runs `go test ./modules/ -run <Id> -update` once, eyeballs the golden, then re-runs without `-update`.

`modules/builtin_test.go` skeleton (added in Task 27, extended by later tasks):

```go
package modules_test

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/JtheGunner/omnishell/internal/module"
	"github.com/JtheGunner/omnishell/internal/render"
	"github.com/JtheGunner/omnishell/modules"
)

var update = flag.Bool("update", false, "update golden files")

func renderModule(t *testing.T, id, shell string, opts map[string]any) string {
	t.Helper()
	reg, err := module.LoadRegistry(modules.FS(), "")
	if err != nil {
		t.Fatal(err)
	}
	m, ok := reg.Get(id)
	if !ok {
		t.Fatalf("module %q not embedded", id)
	}
	body, has, err := m.Template(shell)
	if err != nil || !has {
		t.Fatalf("%s/%s template: has=%v err=%v", id, shell, has, err)
	}
	norm, err := module.ValidateOptions(m.Manifest.Options, opts)
	if err != nil {
		t.Fatalf("options: %v", err)
	}
	out, err := render.Render(body, render.Context{
		Options: norm, Platform: "macos", Shell: shell,
		VendorDir: "/home/j/.config/omnishell/vendor",
		ConfigDir: "/home/j/.config/omnishell",
		Bin:       map[string]string{}, Active: map[string]bool{},
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	return out
}

func assertGolden(t *testing.T, id, shell, got string) {
	t.Helper()
	p := filepath.Join("builtin", id, "testdata", shell+".golden")
	if *update {
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(got), 0o644)
	}
	want, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("%s/%s golden mismatch:\n--- got ---\n%s\n--- want ---\n%s", id, shell, got, want)
	}
}
```

---

## Task 27: built-in modules `completion` + `history`

**Files:**
- Create: `modules/builtin/completion/{manifest.toml,zsh.tmpl,bash.tmpl}`, `modules/builtin/history/{manifest.toml,zsh.tmpl,bash.tmpl}`
- Create: `modules/builtin_test.go` (skeleton above) + `TestCompletion`, `TestHistory`
- Delete: `modules/builtin/.keep`

**`modules/builtin/completion/manifest.toml`:**
```toml
[module]
id          = "completion"
name        = "Completion"
description = "Enable shell completion with case-insensitive matching"
version     = "1.0.0"
schema      = 1
platforms   = ["macos", "linux"]
shells      = ["zsh", "bash"]
```
**`modules/builtin/completion/zsh.tmpl`:**
```
autoload -Uz compinit && compinit
zstyle ':completion:*' matcher-list 'm:{a-zA-Z}={A-Za-z}'
```
**`modules/builtin/completion/bash.tmpl`:**
```
if ! shopt -oq posix; then
  if [ -f /usr/share/bash-completion/bash_completion ]; then
    . /usr/share/bash-completion/bash_completion
  elif [ -f /etc/bash_completion ]; then
    . /etc/bash_completion
  fi
fi
bind 'set completion-ignore-case on' 2>/dev/null
```

**`modules/builtin/history/manifest.toml`:**
```toml
[module]
id          = "history"
name        = "History tuning"
description = "Larger, de-duplicated, shared shell history and prefix search"
version     = "1.0.0"
schema      = 1
platforms   = ["macos", "linux"]
shells      = ["zsh", "bash"]
after       = ["completion"]

[options.size]
type    = "int"
default = 50000
help    = "Number of history entries to keep in memory and on disk"
```
**`modules/builtin/history/zsh.tmpl`:**
```
HISTFILE=${HISTFILE:-$HOME/.zsh_history}
HISTSIZE={{ .Options.size }}
SAVEHIST={{ .Options.size }}
setopt HIST_IGNORE_ALL_DUPS HIST_SAVE_NO_DUPS INC_APPEND_HISTORY SHARE_HISTORY
autoload -U up-line-or-beginning-search down-line-or-beginning-search
zle -N up-line-or-beginning-search
zle -N down-line-or-beginning-search
bindkey "^[[A" up-line-or-beginning-search
bindkey "^[[B" down-line-or-beginning-search
```
**`modules/builtin/history/bash.tmpl`:**
```
HISTFILE=${HISTFILE:-$HOME/.bash_history}
HISTSIZE={{ .Options.size }}
HISTFILESIZE={{ .Options.size }}
HISTCONTROL=ignoreboth
shopt -s histappend
bind '"\e[A": history-search-backward' 2>/dev/null
bind '"\e[B": history-search-forward' 2>/dev/null
```

- [ ] **Step 1: Write `modules/builtin_test.go` skeleton + tests**

```go
func TestCompletion(t *testing.T) {
	assertGolden(t, "completion", "zsh", renderModule(t, "completion", "zsh", nil))
	assertGolden(t, "completion", "bash", renderModule(t, "completion", "bash", nil))
}

func TestHistory(t *testing.T) {
	opts := map[string]any{"size": int64(50000)}
	assertGolden(t, "history", "zsh", renderModule(t, "history", "zsh", opts))
	assertGolden(t, "history", "bash", renderModule(t, "history", "bash", opts))
}
```

- [ ] **Step 2: Run, expect failure**

Run: `go test ./modules/ -v` → FAIL (golden files missing).

- [ ] **Step 3: Create the module files above; delete `modules/builtin/.keep`**

- [ ] **Step 4: Generate & verify goldens**

Run: `go test ./modules/ -run 'TestCompletion|TestHistory' -update`
Eyeball the four `.golden` files. Then: `go test ./modules/ -run 'TestCompletion|TestHistory' -v` → PASS.
Also `go build ./...` to confirm the embed still compiles.

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "feat: add completion and history built-in modules

Claude-Session: https://claude.ai/code/session_012MEGbPB16gL53nKQwspocK"
```

---

## Task 28: built-in modules `autosuggestions` + `syntax-highlighting`

**Files:**
- Create: `modules/builtin/autosuggestions/{manifest.toml,zsh.tmpl}`, `modules/builtin/syntax-highlighting/{manifest.toml,zsh.tmpl}`
- Modify: `modules/builtin_test.go` — add `TestAutosuggestions`, `TestSyntaxHighlighting`

**`modules/builtin/autosuggestions/manifest.toml`:**
```toml
[module]
id          = "autosuggestions"
name        = "Autosuggestions"
description = "Fish-style grey inline command suggestions from history (zsh)"
version     = "1.0.0"
schema      = 1
platforms   = ["macos", "linux"]
shells      = ["zsh"]
after       = ["completion", "history"]

[packages]
brew   = ["zsh-autosuggestions"]
apt    = ["zsh-autosuggestions"]
dnf    = ["zsh-autosuggestions"]
pacman = ["zsh-autosuggestions"]
zypper = ["zsh-autosuggestions"]
apk    = ["zsh-autosuggestions"]

[[packages.fallback]]
type = "git"
repo = "https://github.com/zsh-users/zsh-autosuggestions.git"
dest = "{{.VendorDir}}/zsh-autosuggestions"

[options.highlight_style]
type    = "string"
default = "fg=8"
help    = "ZSH_AUTOSUGGEST_HIGHLIGHT_STYLE value"
```
**`modules/builtin/autosuggestions/zsh.tmpl`:**
```
for _oms_f in \
  /opt/homebrew/share/zsh-autosuggestions/zsh-autosuggestions.zsh \
  /usr/share/zsh-autosuggestions/zsh-autosuggestions.zsh \
  /usr/share/zsh/plugins/zsh-autosuggestions/zsh-autosuggestions.zsh \
  {{ pathjoin .VendorDir "zsh-autosuggestions" "zsh-autosuggestions.zsh" }}
do
  [[ -r "$_oms_f" ]] && { source "$_oms_f"; break; }
done
unset _oms_f
ZSH_AUTOSUGGEST_HIGHLIGHT_STYLE={{ .Options.highlight_style | shellquote }}
```

**`modules/builtin/syntax-highlighting/manifest.toml`:**
```toml
[module]
id          = "syntax-highlighting"
name        = "Syntax highlighting"
description = "Colour commands green/red as you type depending on validity (zsh)"
version     = "1.0.0"
schema      = 1
platforms   = ["macos", "linux"]
shells      = ["zsh"]
after       = ["completion", "history", "fzf", "zoxide", "modern-aliases", "autosuggestions"]

[packages]
brew   = ["zsh-syntax-highlighting"]
apt    = ["zsh-syntax-highlighting"]
dnf    = ["zsh-syntax-highlighting"]
pacman = ["zsh-syntax-highlighting"]
zypper = ["zsh-syntax-highlighting"]
apk    = ["zsh-syntax-highlighting"]

[[packages.fallback]]
type = "git"
repo = "https://github.com/zsh-users/zsh-syntax-highlighting.git"
dest = "{{.VendorDir}}/zsh-syntax-highlighting"
```
**`modules/builtin/syntax-highlighting/zsh.tmpl`:**
```
for _oms_f in \
  /opt/homebrew/share/zsh-syntax-highlighting/zsh-syntax-highlighting.zsh \
  /usr/share/zsh-syntax-highlighting/zsh-syntax-highlighting.zsh \
  /usr/share/zsh/plugins/zsh-syntax-highlighting/zsh-syntax-highlighting.zsh \
  {{ pathjoin .VendorDir "zsh-syntax-highlighting" "zsh-syntax-highlighting.zsh" }}
do
  [[ -r "$_oms_f" ]] && { source "$_oms_f"; break; }
done
unset _oms_f
```

- [ ] **Step 1: Add tests** (`TestAutosuggestions` renders `zsh` with `{"highlight_style":"fg=8"}`; `TestSyntaxHighlighting` renders `zsh` with `nil`). Assert `module.ValidateManifest` passes for both (implicit via `LoadRegistry`).
- [ ] **Step 2: Run → FAIL. Step 3: create files. Step 4: `-update`, eyeball, re-run → PASS.**
- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "feat: add autosuggestions and syntax-highlighting modules

Claude-Session: https://claude.ai/code/session_012MEGbPB16gL53nKQwspocK"
```

---

## Task 29: built-in module `fzf`

**Files:**
- Create: `modules/builtin/fzf/{manifest.toml,zsh.tmpl,bash.tmpl}`
- Modify: `modules/builtin_test.go` — add `TestFzf`

**`modules/builtin/fzf/manifest.toml`:**
```toml
[module]
id          = "fzf"
name        = "FZF fuzzy finder"
description = "Ctrl+R history search as a fuzzy, scrollable list (+ optional Ctrl+T)"
version     = "1.0.0"
schema      = 1
platforms   = ["macos", "linux"]
shells      = ["zsh", "bash"]
after       = ["completion", "history"]

[packages]
brew   = ["fzf"]
apt    = ["fzf"]
dnf    = ["fzf"]
pacman = ["fzf"]
zypper = ["fzf"]
apk    = ["fzf"]

[[packages.fallback]]
type = "git"
repo = "https://github.com/junegunn/fzf.git"
dest = "{{.VendorDir}}/fzf"
run  = "{{.VendorDir}}/fzf/install --bin --no-update-rc"

[options.ctrl_r]
type    = "bool"
default = true
help    = "Bind Ctrl+R to the fzf history widget"

[options.ctrl_t]
type    = "bool"
default = false
help    = "Bind Ctrl+T to the fzf file widget"

[options.default_opts]
type    = "string"
default = "--height 40% --reverse --border"
help    = "FZF_DEFAULT_OPTS"
```
**`modules/builtin/fzf/zsh.tmpl`:**
```
export FZF_DEFAULT_OPTS={{ .Options.default_opts | shellquote }}
for _oms_kb in \
  "$(brew --prefix 2>/dev/null)/opt/fzf/shell/key-bindings.zsh" \
  /usr/share/fzf/key-bindings.zsh \
  /usr/share/doc/fzf/examples/key-bindings.zsh \
  {{ pathjoin .VendorDir "fzf" "shell" "key-bindings.zsh" }}
do
  [[ -r "$_oms_kb" ]] && { source "$_oms_kb"; break; }
done
unset _oms_kb
{{- if not .Options.ctrl_r }}
bindkey | grep -q fzf-history-widget && bindkey -r '^R'
{{- end }}
{{- if .Options.ctrl_t }}
# Ctrl+T left bound by key-bindings.zsh
{{- else }}
bindkey | grep -q fzf-file-widget && bindkey -r '^T'
{{- end }}
```
**`modules/builtin/fzf/bash.tmpl`:**
```
export FZF_DEFAULT_OPTS={{ .Options.default_opts | shellquote }}
for _oms_kb in \
  "$(brew --prefix 2>/dev/null)/opt/fzf/shell/key-bindings.bash" \
  /usr/share/fzf/key-bindings.bash \
  /usr/share/doc/fzf/examples/key-bindings.bash \
  {{ pathjoin .VendorDir "fzf" "shell" "key-bindings.bash" }}
do
  [ -r "$_oms_kb" ] && { source "$_oms_kb"; break; }
done
unset _oms_kb
{{- if not .Options.ctrl_r }}
bind -r '\C-r' 2>/dev/null
{{- end }}
{{- if not .Options.ctrl_t }}
bind -r '\C-t' 2>/dev/null
{{- end }}
```

- [ ] **Step 1: Add `TestFzf`** — render `zsh` and `bash` with `{"ctrl_r":true,"ctrl_t":false,"default_opts":"--height 40% --reverse --border"}`, and a second golden pair with `{"ctrl_r":false,...}` (name goldens `zsh.golden`, `bash.golden`, `zsh-noctrlr.golden`, `bash-noctrlr.golden` — extend `assertGolden` with a variant suffix param or add `assertGoldenNamed`).
- [ ] **Step 2: Run → FAIL. Step 3: create files. Step 4: `-update`, eyeball, re-run → PASS.**
- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "feat: add fzf built-in module

Claude-Session: https://claude.ai/code/session_012MEGbPB16gL53nKQwspocK"
```

---

## Task 30: built-in modules `zoxide` + `modern-aliases`

**Files:**
- Create: `modules/builtin/zoxide/{manifest.toml,zsh.tmpl,bash.tmpl}`, `modules/builtin/modern-aliases/{manifest.toml,zsh.tmpl,bash.tmpl}`
- Modify: `modules/builtin_test.go` — add `TestZoxide`, `TestModernAliases`

**`modules/builtin/zoxide/manifest.toml`:**
```toml
[module]
id          = "zoxide"
name        = "zoxide"
description = "Smarter cd that learns your most-used directories"
version     = "1.0.0"
schema      = 1
platforms   = ["macos", "linux"]
shells      = ["zsh", "bash"]
after       = ["completion"]

[packages]
brew   = ["zoxide"]
apt    = ["zoxide"]
dnf    = ["zoxide"]
pacman = ["zoxide"]
zypper = ["zoxide"]
apk    = ["zoxide"]

[options.cmd]
type    = "string"
default = "z"
help    = "Name of the jump command zoxide defines"
```
**`modules/builtin/zoxide/zsh.tmpl`:**
```
command -v zoxide >/dev/null 2>&1 && eval "$(zoxide init zsh --cmd {{ .Options.cmd }})"
```
**`modules/builtin/zoxide/bash.tmpl`:**
```
command -v zoxide >/dev/null 2>&1 && eval "$(zoxide init bash --cmd {{ .Options.cmd }})"
```

**`modules/builtin/modern-aliases/manifest.toml`:**
```toml
[module]
id          = "modern-aliases"
name        = "Modern CLI aliases"
description = "Replace ls/cat/find with eza/bat/fd when selected"
version     = "1.0.0"
schema      = 1
platforms   = ["macos", "linux"]
shells      = ["zsh", "bash"]
after       = ["completion"]

# Package sets are per-tool; the engine installs the whole list for the
# detected manager. Users who only pick a subset via `replace` still get
# all three tools installed — acceptable and simple for v1. (Documented.)
[packages]
brew   = ["eza", "bat", "fd"]
apt    = ["eza", "bat", "fd-find"]
dnf    = ["eza", "bat", "fd-find"]
pacman = ["eza", "bat", "fd"]
zypper = ["eza", "bat", "fd"]
apk    = ["eza", "bat", "fd"]

[options.replace]
type    = "list<enum>"
values  = ["ls", "cat", "find"]
default = ["ls", "cat", "find"]
help    = "Which commands to alias to modern equivalents"
```
**`modules/builtin/modern-aliases/zsh.tmpl`** (identical `bash.tmpl`):
```
{{- if has_item .Options.replace "ls" }}
command -v eza >/dev/null 2>&1 && alias ls='eza --icons --git --group-directories-first'
command -v eza >/dev/null 2>&1 && alias ll='eza -lah --icons --git --group-directories-first'
{{- end }}
{{- if has_item .Options.replace "cat" }}
command -v bat >/dev/null 2>&1 && alias cat='bat'
{{- end }}
{{- if has_item .Options.replace "find" }}
command -v fd >/dev/null 2>&1 && alias find='fd' || { command -v fdfind >/dev/null 2>&1 && alias find='fdfind'; }
{{- end }}
```
- **New template func needed:** `has_item <list> <value>` → true if the string slice contains value. Add it to `render.Render`'s `FuncMap` in this task (with a unit test in `internal/render/render_test.go`: `{{ if has_item .Options.replace "ls" }}yes{{ end }}` where `Options.replace = []string{"ls"}`).

- [ ] **Step 1: Add `has_item` to `render` + test → run render tests → PASS.**
- [ ] **Step 2: Add `TestZoxide` (opts `{"cmd":"z"}`), `TestModernAliases` (opts `{"replace":[]any{"ls","find"}}`, plus a full-set golden).**
- [ ] **Step 3: Run → FAIL. Step 4: create module files. Step 5: `-update`, eyeball, re-run → PASS.** Run `go test ./... -race` → all green.
- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "feat: add zoxide and modern-aliases modules

Claude-Session: https://claude.ai/code/session_012MEGbPB16gL53nKQwspocK"
```

---

## Task 31: End-to-end integration test suite (mock package manager)

**Files:**
- Create: `internal/cli/e2e_test.go`
- Uses: the real CLI (`cli.Execute`), a fake `$HOME`, `cli.SetLookPathForTest`, `cli.SetRunnerForTest`, and the **real embedded built-in modules** (`modules.FS()`), with a `pkgmgr.MockRunner` so nothing is really installed.

**Interfaces:**
- Consumes: `cli.Execute`, `cli.SetLookPathForTest`, `cli.SetRunnerForTest`, `pkgmgr.MockRunner`.
- Produces: no new code — this task only adds tests that exercise full user journeys against the shipped modules. If a test reveals a bug, fix the offending package and note it in the commit.

- [ ] **Step 1: Write `internal/cli/e2e_test.go`**

```go
package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JtheGunner/omnishell/internal/cli"
	"github.com/JtheGunner/omnishell/internal/pkgmgr"
)

func setupHome(t *testing.T) (home string, run func(args ...string) (int, string)) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	os.WriteFile(filepath.Join(home, ".zshrc"), []byte("# existing zshrc\nexport EXISTING=1\n"), 0o644)

	cli.SetLookPathForTest(func(bin string) (string, error) {
		switch bin {
		case "zsh", "brew", "git":
			return "/bin/" + bin, nil
		}
		return "", os.ErrNotExist
	})
	t.Cleanup(func() { cli.SetLookPathForTest(nil) })

	cli.SetRunnerForTest(&pkgmgr.MockRunner{
		LookOK: map[string]bool{"brew": true, "git": true},
		Responses: map[string]pkgmgr.MockResponse{
			// pretend nothing is installed, every install "succeeds"
		},
	})
	t.Cleanup(func() { cli.SetRunnerForTest(nil) })

	run = func(args ...string) (int, string) {
		var b bytes.Buffer
		code := cli.Execute(args, &b, &b)
		return code, b.String()
	}
	return home, run
}

func TestJourneyInitEnableApplyDoctorRemove(t *testing.T) {
	home, run := setupHome(t)

	if code, out := run("init"); code != 0 {
		t.Fatalf("init: %d %s", code, out)
	}
	for _, m := range []string{"completion", "history", "fzf", "autosuggestions", "syntax-highlighting"} {
		if code, out := run("enable", m); code != 0 {
			t.Fatalf("enable %s: %d %s", m, code, out)
		}
	}
	if code, out := run("apply", "--yes"); code != 0 {
		t.Fatalf("apply: %d %s", code, out)
	}

	initZsh := filepath.Join(home, ".config", "omnishell", "init.zsh")
	body, err := os.ReadFile(initZsh)
	if err != nil {
		t.Fatalf("init.zsh: %v", err)
	}
	s := string(body)
	// order: completion before history before fzf before autosuggestions before syntax-highlighting
	idx := func(sub string) int { return strings.Index(s, sub) }
	if !(idx("omnishell:completion") < idx("omnishell:history") &&
		idx("omnishell:history") < idx("omnishell:fzf") &&
		idx("omnishell:fzf") < idx("omnishell:autosuggestions") &&
		idx("omnishell:autosuggestions") < idx("omnishell:syntax-highlighting")) {
		t.Fatalf("section order wrong:\n%s", s)
	}

	zshrc, _ := os.ReadFile(filepath.Join(home, ".zshrc"))
	if !strings.HasPrefix(string(zshrc), "# existing zshrc\nexport EXISTING=1\n") {
		t.Fatalf("existing .zshrc content disturbed:\n%s", zshrc)
	}
	if !strings.Contains(string(zshrc), "# >>> omnishell >>>") {
		t.Fatal(".zshrc missing marker block")
	}

	// doctor is clean
	if code, out := run("doctor"); code != 0 {
		t.Fatalf("doctor after apply: %d %s", code, out)
	}

	// second apply is a no-op
	info1, _ := os.Stat(initZsh)
	if code, out := run("apply", "--yes"); code != 0 {
		t.Fatalf("second apply: %d %s", code, out)
	}
	info2, _ := os.Stat(initZsh)
	if !info1.ModTime().Equal(info2.ModTime()) {
		t.Fatal("second apply rewrote init.zsh")
	}

	// tamper → doctor drift (exit 3)
	os.WriteFile(initZsh, append(body, []byte("\n# tamper\n")...), 0o644)
	if code, _ := run("doctor"); code != 3 {
		t.Fatalf("doctor after tamper: exit %d, want 3", code)
	}
	// apply --force repairs
	if code, out := run("apply", "--yes", "--force"); code != 0 {
		t.Fatalf("apply --force: %d %s", code, out)
	}
	if code, _ := run("doctor"); code != 0 {
		t.Fatalf("doctor after repair: exit %d, want 0", code)
	}

	// remove one module
	if code, out := run("remove", "fzf", "--yes"); code != 0 {
		t.Fatalf("remove fzf: %d %s", code, out)
	}
	body2, _ := os.ReadFile(initZsh)
	if strings.Contains(string(body2), "omnishell:fzf") {
		t.Fatalf("fzf section survived remove:\n%s", body2)
	}
	if code, _ := run("doctor"); code != 0 {
		t.Fatalf("doctor after remove: want clean")
	}

	// uninstall restores the original .zshrc
	if code, out := run("uninstall", "--yes"); code != 0 {
		t.Fatalf("uninstall: %d %s", code, out)
	}
	final, _ := os.ReadFile(filepath.Join(home, ".zshrc"))
	if string(final) != "# existing zshrc\nexport EXISTING=1\n" {
		t.Fatalf("uninstall did not restore .zshrc:\n%q", final)
	}
	if _, err := os.Stat(initZsh); !os.IsNotExist(err) {
		t.Fatal("init.zsh survived uninstall")
	}
}

func TestJourneyNoPackageManager(t *testing.T) {
	home, run := setupHome(t)
	cli.SetLookPathForTest(func(bin string) (string, error) {
		if bin == "zsh" {
			return "/bin/zsh", nil
		}
		return "", os.ErrNotExist // no brew, no apt...
	})
	cli.SetRunnerForTest(&pkgmgr.MockRunner{LookOK: map[string]bool{}})

	run("init")
	run("enable", "history")     // config-only
	run("enable", "completion")  // config-only
	run("enable", "fzf")         // needs a package
	code, out := run("apply", "--yes")
	// history + completion apply; fzf is degraded → exit 1 but init.zsh still written
	if code != 1 {
		t.Fatalf("apply exit = %d, want 1 (degraded fzf); out:\n%s", code, out)
	}
	body, _ := os.ReadFile(filepath.Join(home, ".config", "omnishell", "init.zsh"))
	if !strings.Contains(string(body), "omnishell:history") || strings.Contains(string(body), "omnishell:fzf") {
		t.Fatalf("expected history applied, fzf skipped:\n%s", body)
	}
}
```

- [ ] **Step 2: Run**

Run: `go test ./internal/cli/ -run Journey -v`. Fix any real bug the journeys expose in the owning package (commit the fix with the failing scenario named). Then `go test ./... -race -count=1` → all green.

- [ ] **Step 3: Commit**

```bash
git add -A
git commit -m "test: add full user-journey integration tests

Claude-Session: https://claude.ai/code/session_012MEGbPB16gL53nKQwspocK"
```

---

## Task 32: Release plumbing — GoReleaser, install.sh, CI release job

**Files:**
- Create: `.goreleaser.yaml`, `install.sh`, `.github/workflows/release.yml`
- Modify: `.github/workflows/ci.yml` (add `go build` on macOS + Linux matrix), `cmd/omnishell/main.go` (no change — version already via ldflags), `README.md` (install section — full README is Task 33)

**Interfaces:**
- Consumes: `internal/buildinfo` ldflags path.
- Produces: no Go code. Deliverables: a tagged build produces cross-platform binaries + a Homebrew tap formula; `install.sh` fetches the right asset.

- [ ] **Step 1: Create `.goreleaser.yaml`**

```yaml
version: 2
project_name: omnishell
before:
  hooks: [go mod tidy]
builds:
  - id: omnishell
    main: ./cmd/omnishell
    binary: omnishell
    env: [CGO_ENABLED=0]
    goos: [linux, darwin]
    goarch: [amd64, arm64]
    ldflags:
      - -s -w
      - -X github.com/JtheGunner/omnishell/internal/buildinfo.Version={{.Version}}
      - -X github.com/JtheGunner/omnishell/internal/buildinfo.Commit={{.ShortCommit}}
      - -X github.com/JtheGunner/omnishell/internal/buildinfo.Date={{.Date}}
archives:
  - formats: [tar.gz]
    name_template: "{{ .ProjectName }}_{{ .Os }}_{{ .Arch }}"
checksum:
  name_template: checksums.txt
brews:
  - repository:
      owner: JtheGunner
      name: homebrew-tap
    homepage: https://github.com/JtheGunner/omnishell
    description: Modular, declarative terminal configuration for macOS and Linux
    install: bin.install "omnishell"
    test: system "#{bin}/omnishell version"
release:
  github:
    owner: JtheGunner
    name: omnishell
```

- [ ] **Step 2: Create `install.sh`**

```sh
#!/bin/sh
# Install the latest omnishell release into ~/.local/bin (override with OMNISHELL_BIN_DIR).
set -eu

repo="JtheGunner/omnishell"
bin_dir="${OMNISHELL_BIN_DIR:-$HOME/.local/bin}"

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$os" in
  linux|darwin) : ;;
  *) echo "unsupported OS: $os" >&2; exit 1 ;;
esac

arch="$(uname -m)"
case "$arch" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) echo "unsupported arch: $arch" >&2; exit 1 ;;
esac

tag="$(curl -fsSL "https://api.github.com/repos/$repo/releases/latest" | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p')"
[ -n "$tag" ] || { echo "could not determine latest release" >&2; exit 1; }

url="https://github.com/$repo/releases/download/$tag/omnishell_${os}_${arch}.tar.gz"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "downloading $url"
curl -fsSL "$url" | tar -xz -C "$tmp"
mkdir -p "$bin_dir"
install -m 0755 "$tmp/omnishell" "$bin_dir/omnishell"
echo "installed omnishell $tag to $bin_dir/omnishell"
echo "make sure $bin_dir is on your PATH, then run: omnishell init"
```

Make it executable: `chmod +x install.sh`.

- [ ] **Step 3: Create `.github/workflows/release.yml`**

```yaml
name: release
on:
  push:
    tags: ['v*']
permissions:
  contents: write
jobs:
  goreleaser:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with: { fetch-depth: 0 }
      - uses: actions/setup-go@v5
        with: { go-version: '1.23' }
      - uses: goreleaser/goreleaser-action@v6
        with: { version: '~> v2', args: release --clean }
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          HOMEBREW_TAP_GITHUB_TOKEN: ${{ secrets.HOMEBREW_TAP_GITHUB_TOKEN }}
```

- [ ] **Step 4: Extend `.github/workflows/ci.yml` with a build matrix**

Add a second job:
```yaml
  build:
    strategy:
      matrix:
        os: [ubuntu-latest, macos-latest]
    runs-on: ${{ matrix.os }}
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.23' }
      - run: go build ./...
      - run: go run ./cmd/omnishell version
```

- [ ] **Step 5: Verify locally**

Run: `go tool dist list | grep -E 'linux/(amd64|arm64)|darwin/(amd64|arm64)'` to confirm targets, then:
`GOOS=linux GOARCH=arm64 go build -o /dev/null ./cmd/omnishell` and the darwin variants — all succeed.
If `goreleaser` is installed locally: `goreleaser check` → passes. `sh -n install.sh` → no syntax errors.

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "ci: add goreleaser, install.sh, release workflow, build matrix

Claude-Session: https://claude.ai/code/session_012MEGbPB16gL53nKQwspocK"
```

---

## Task 33: Documentation — README and the module authoring guide

**Files:**
- Create/replace: `README.md`, `docs/writing-a-module.md`

**Interfaces:** none. Docs must match the shipped CLI exactly (command names, flags, paths).

- [ ] **Step 1: Write `README.md`** with these sections, each accurate to the built CLI:
  - **What it is** — one paragraph (the spec's Purpose §1).
  - **Install** — `brew install jthegunner/tap/omnishell`; `curl -fsSL https://raw.githubusercontent.com/JtheGunner/omnishell/main/install.sh | sh`; `go install github.com/JtheGunner/omnishell/cmd/omnishell@latest`.
  - **Quick start** — `omnishell init`, edit `~/.config/omnishell/config.toml` or `omnishell enable fzf`, `omnishell apply`, open a new shell.
  - **Commands** — the table from spec §3 / Task 23–26, verbatim flag names.
  - **How it works** — the managed `init.<shell>` file + one `source` block in your rc; `state.lock.json`; backups under `~/.config/omnishell/backups/`.
  - **Built-in modules** — the table from spec §9.
  - **Writing your own module** — link to `docs/writing-a-module.md`, one-paragraph teaser (drop a folder in `~/.config/omnishell/modules/<id>/`).
  - **Safety** — nothing changes without `apply`/`remove`; every write is backed up; `omnishell doctor` for drift; `omnishell uninstall` to back out.
  - **Exit codes** — 0/1/2/3 table.
  - **Not in v1** — fish, remote modules, profiles (spec §10), so expectations are set.

- [ ] **Step 2: Write `docs/writing-a-module.md`**:
  - Folder layout (`manifest.toml`, `zsh.tmpl`/`bash.tmpl`, optional `hooks/`).
  - Full annotated `manifest.toml` (every field, the six `[packages.<mgr>]` keys, `[[packages.fallback]]`, option schema types `bool|string|int|enum|list<string>|list<enum>`).
  - Template context reference: `.Options.<key>`, `.Platform`, `.Shell`, `.VendorDir`, `.ConfigDir`, `.Bin.<tool>`; funcs `shellquote`, `pathjoin`, `has`, `has_item`.
  - `requires` vs `after`.
  - Hook contract: `check.sh` exit 0/1, `install.sh`, `remove.sh`; env vars `OMNISHELL_VENDOR_DIR`, `OMNISHELL_CONFIG_DIR`, `OMNISHELL_PLATFORM`, `OMNISHELL_SHELL`, `OMNISHELL_OPT_<KEY>`.
  - A worked example: a `direnv` module from scratch, then `omnishell list` / `omnishell enable direnv` / `omnishell apply`.
  - Testing your module: `omnishell apply --dry-run`, `omnishell doctor`.

- [ ] **Step 3: Cross-check**

Run `go run ./cmd/omnishell --help` and each `... <cmd> --help`; every flag and command named in the docs must appear. Fix mismatches.

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "docs: add README and module authoring guide

Claude-Session: https://claude.ai/code/session_012MEGbPB16gL53nKQwspocK"
```

---

## Task 34: Opt-in per-distro E2E tests (real package managers)

**Files:**
- Create: `.github/workflows/e2e.yml`, `test/e2e/run.sh`
- Test: the script asserts real installs on real distros; not part of `go test ./...`.

**Interfaces:** none (shell + CI only). Guarded by `workflow_dispatch` and a nightly schedule so the fast PR loop stays fast.

- [ ] **Step 1: Write `test/e2e/run.sh`**

```sh
#!/bin/sh
# Build omnishell, run a real init+enable+apply+doctor cycle, assert the
# init file and rc block exist and doctor is clean. Intended to run as root
# inside a minimal distro container.
set -eu

go build -o /usr/local/bin/omnishell ./cmd/omnishell

export HOME=/root
touch "$HOME/.bashrc"

omnishell init
omnishell enable history
omnishell enable completion
omnishell enable fzf
omnishell apply --yes

test -f "$HOME/.config/omnishell/init.bash" || { echo "init.bash missing"; exit 1; }
grep -q '>>> omnishell >>>' "$HOME/.bashrc" || { echo "rc block missing"; exit 1; }
grep -q 'omnishell:fzf' "$HOME/.config/omnishell/init.bash" || { echo "fzf not applied (package install failed?)"; exit 1; }

omnishell doctor
omnishell remove fzf --yes
! grep -q 'omnishell:fzf' "$HOME/.config/omnishell/init.bash" || { echo "fzf survived remove"; exit 1; }
omnishell uninstall --yes
! grep -q 'omnishell' "$HOME/.bashrc" || { echo "uninstall left markers"; exit 1; }
echo "E2E OK"
```
`chmod +x test/e2e/run.sh`.

- [ ] **Step 2: Write `.github/workflows/e2e.yml`**

```yaml
name: e2e
on:
  workflow_dispatch:
  schedule: [{ cron: '0 4 * * *' }]
jobs:
  linux:
    strategy:
      fail-fast: false
      matrix:
        image: [debian:stable-slim, fedora:latest, archlinux:latest, alpine:latest]
    runs-on: ubuntu-latest
    container: ${{ matrix.image }}
    steps:
      - name: Install prerequisites
        run: |
          set -eux
          if command -v apt-get; then apt-get update && apt-get install -y golang git ca-certificates; fi
          if command -v dnf; then dnf install -y golang git; fi
          if command -v pacman; then pacman -Sy --noconfirm go git; fi
          if command -v apk; then apk add --no-cache go git bash; fi
      - uses: actions/checkout@v4
      - run: sh test/e2e/run.sh
  macos:
    runs-on: macos-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.23' }
      - run: |
          brew --version
          HOME_BAK=$HOME
          sh test/e2e/run.sh || exit 1
```

- [ ] **Step 3: Local smoke (Docker, optional)**

If Docker is available:
`docker run --rm -v "$PWD":/src -w /src debian:stable-slim sh -c 'apt-get update && apt-get install -y golang git ca-certificates && sh test/e2e/run.sh'`
Expect `E2E OK`. (If Docker is unavailable, note that CI covers it.)

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "ci: add opt-in per-distro end-to-end tests

Claude-Session: https://claude.ai/code/session_012MEGbPB16gL53nKQwspocK"
```

---

## Appendix A: Engine helper signatures (Tasks 19–22)

The orchestration helpers referenced by `Apply`/`Remove`/`Doctor`/`Uninstall`. Each is small; implement in `apply.go` / `apply_helpers.go` / `remove.go` / `doctor.go` / `uninstall.go` as noted. Behaviour is fully specified in the owning task; these are the exact names and shapes so the tasks stay type-consistent.

```go
// apply_helpers.go
func (e Engine) initPath(shell string) string            // filepath.Join(e.Platform.ConfigDir, "init."+shell)
func (e Engine) homeRelative(path string) string          // "$HOME"+rest if under HomeDir, else path verbatim
func (e Engine) renderContext(mp ModulePlan, shell string, active map[string]bool) render.Context
func (e Engine) initOrRCDrift(plan Plan, lock lockfile.Lock) bool
    // true if, for any managed shell: the would-be init file hash differs from lock.InitFiles[shell].ContentHash,
    // or the rc file is missing the marker block. Used only for the idempotent no-op short-circuit.
func (e Engine) installPackages(plan Plan, degraded map[string]string,
    vendorPaths map[string][]string, installedNow map[string]map[string]bool)
    // iterates plan.Order; for each ModulePlan with MissingPackages: announce to e.Stdout
    // ("installing X, Y via <mgr>"; if e.Manager.NeedsSudo() also "(sudo may prompt for your password)"),
    // call e.Manager.Install / pkgmgr.InstallGitFallback; re-check IsInstalled / FallbackSatisfied;
    // still missing → degraded[id] = reason. installedNow[id][pkg] = true for freshly installed packages.
func (e Engine) runCheckHooks(plan Plan, degraded map[string]string)   // Task 19 step 7 (check + install hook)
func (e Engine) renderAll(plan Plan, degraded map[string]string) map[[2]string]string
    // key {id, shell} → rendered snippet; a render error sets degraded[id] and omits the key.
func (e Engine) buildSections(plan Plan, rendered map[[2]string]string, degraded map[string]string, shell string) []initfile.Section
    // walk plan.Order; include a Section only when: not degraded, Action != ActionRemove,
    // contains(mp.Shells, shell), and rendered[{id,shell}] exists.
func (e Engine) ensureRC(shell, initPath string, bk backup.Session, lock *lockfile.Lock) error
    // read rc (missing = ""), bk.Save, rcfile.EnsureBlock(content, shell, e.homeRelative(initPath));
    // if changed → atomicfile.WriteFile; set (*lock).RCFiles[shell].
func (e Engine) rebuildLock(cfg config.Config, plan Plan, prev lockfile.Lock,
    degraded map[string]string, vendorPaths map[string][]string,
    installedNow map[string]map[string]bool) lockfile.Lock
    // returns a NEW Lock: carry prev metadata, drop ActionRemove modules, upsert the rest with
    // Status ("ok"/"degraded"), OptionsHash, ModuleVersion, ShellsRendered, VendorPaths, and a
    // merged Packages slice (InstalledByOmnishell = prev truth OR installedNow[id][pkg]).
func summarise(plan Plan, degraded map[string]string) []engine.ModuleResult

// doctor.go
func (r DoctorReport) HasDrift() bool

// uninstall.go
func (e Engine) removeShellIntegration(shell string, bk backup.Session) (removed bool, err error)
```

---

## Appendix B: Self-Review

Reviewed the plan against `docs/superpowers/specs/2026-09-02-omnishell-design.md` section by section.

**Spec coverage — every section maps to tasks:**

| Spec section | Tasks |
|---|---|
| §1 Purpose / non-negotiables | Global Constraints; engine Tasks 17–22 |
| §2 Decisions D1–D8 | Global Constraints; Task choices throughout |
| §3 CLI surface + exit codes | 1 (version), 23 (init/list + `ClassifyError`), 24 (enable/disable/set), 25 (apply/diff/doctor), 26 (remove/uninstall) |
| §4 config.toml (read + rules) | 4 (read, unknown-key/version rejection), 5 (comment-preserving edits) |
| §5 module format (manifest/templates/hooks/lifecycle) | 6 (manifest), 7 (option schema), 8 (accessor + registry + `ReadHook`), 10 + 30 (template funcs), 19 step 7 (check/install hooks), 20 (remove hook) |
| §6 init file + rc block + dep graph + safety net | 9 (graph), 11 (initfile + hash + hand-edit), 12 (rcfile), 19 (backups, atomic writes, guard) |
| §7 lockfile + apply algorithm + PM abstraction | 13 (lockfile), 14–16 (pkgmgr + detection + fallback), 17 (plan), 18 (plan render), 19 (apply) |
| §8 repo layout / errors / testing / distribution | File Structure; 1 + 32 (CI, goreleaser, install.sh); 34 (e2e); tests in every task |
| §9 built-in modules (7) | 27 (completion, history), 28 (autosuggestions, syntax-highlighting), 29 (fzf), 30 (zoxide, modern-aliases) |
| §10 not-in-v1 | Not implemented; documented in README (Task 33) |
| §11 open questions | none |

**Gaps found and fixed inline:**
- `install.sh` hook execution was missing from the apply flow → Task 19 step 7 now runs `install.sh` on a failed `check.sh` and re-checks, and specifies the hook env-var contract.
- The drift sentinel was described as an engine error in one place and a CLI error in another → standardised on `cli.errDrift`, defined in `exit.go` (Task 23), returned by `doctor` (Task 25).
- `engine.New` was referenced but never specified or used → removed; callers use a struct literal, `(Engine) now()` handles the nil-`Now` default.

**Placeholder scan:** no `TBD`/`TODO`/`implement later`/`similar to Task N`. The engine orchestration tasks (19–22) give full type definitions, the top-level `Apply` body, complete test suites, and — via Appendix A — every helper signature with its behaviour; the CLI tasks (24–26) are thin wrappers fully pinned by their Interfaces blocks and follow the seam pattern established with tests in Task 23.

**Type consistency:** verified the cross-task API surface — `engine.Apply(cfg, cfgPath, lockPath, opts) (Result, error)`, `ComputePlan(e, cfg, lock, noPackages)`, `module.Module.Template → (string,bool,error)`, `module.LoadRegistry(fs.FS, string)`, `lockfile.Load → (Lock,bool,error)`, `rcfile.EnsureBlock/RemoveBlock → (string,bool)`, `backup.NewSession(dir, time)`, `render.Context` field set, `pkgmgr.Manager` method set — all call sites match their definitions.

---

## Execution order summary

Tasks are strictly ordered; each builds only on earlier ones. Phases 1–5 (Tasks 2–13) are independent enough to parallelise across subagents if desired, but Tasks 14–34 form a dependency chain (pkgmgr → engine → cli → modules → integration → release). Recommended: sequential, one subagent per task, review between.
