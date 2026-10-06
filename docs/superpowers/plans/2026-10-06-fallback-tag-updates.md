# Fallback Tag Updates Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Keep the pinned `ref` of every built-in `git` fallback current through a weekly, reviewable pull request (optionally auto-merged behind a guard), and make `omnishell apply` move an existing vendor clone to the pinned tag.

**Architecture:** Phase 1 adds a small Go tool (`tools/update-fallback-tags`) that reads the built-in manifests, asks each upstream for its tags with `git ls-remote`, rewrites only the `ref` line, and emits a Markdown summary; a scheduled workflow runs it and opens a PR. Phase 2 records the ref a fallback clone was built from in the lockfile, lets `ComputePlan` plan an "update" when that ref differs from the manifest's, and lets `Apply` fetch, check out and rebuild.

**Tech Stack:** Go 1.26, BurntSushi/toml (already a dependency, used through `internal/module`), GitHub Actions, `peter-evans/create-pull-request`, `gh` CLI.

**Spec:** `docs/superpowers/specs/2026-10-06-fallback-tag-updates-design.md`

## Global Constraints

- Phase 1 (Tasks 1-4) and Phase 2 (Tasks 5-8) are separate PRs under OMNIS-27. Phase 1 lives on branch `chore/OMNIS-27-automate-fallback-tag-updates`. Cut Phase 2 from `main` after Phase 1 merged: `git fetch origin && git worktree add .worktrees/OMNIS-27-refresh-vendor-clones -b feat/OMNIS-27-refresh-vendor-clones origin/main`.
- The tool lives in `tools/update-fallback-tags/`, outside `cmd/omnishell`; GoReleaser builds only `./cmd/omnishell`, so the tool must not be shipped.
- Candidate tags match `^v?\d+(\.\d+)+$` **and** use the same `v`-prefix style as the current `ref`; never select a tag lower than the current one; compare versions numerically.
- Rewrite only the `ref = "…"` line of the first `[[packages.fallback]]` table by text substitution (no TOML re-marshal).
- The tool exits 0 when nothing changes; it exits non-zero only when there are failures **and** no change.
- Auto-merge is off unless the repository variable `FALLBACK_TAGS_AUTOMERGE` is `true`; the workflow fails closed when `main` has no required status checks, and only requests it when every bump stays within its compatibility line (same major, or same minor while the major is 0); a later run that no longer qualifies withdraws it.
- Workflow token: `secrets.FALLBACK_TAGS_TOKEN || github.token`. PR branch `chore/update-fallback-tags`, title `chore: update pinned fallback tags`.
- Lockfile field: `fallback_ref` (JSON, `omitempty`); an absent value means "unknown" and triggers one refresh.
- `ComputePlan` must stay side-effect-free (no git probing of the clone). A clone with tracked local changes (`git status --porcelain --untracked-files=no`) is never touched; untracked build output such as `target/` is ignored.
- All shelling out goes through the injectable `Runner`; tests never run real `git` or touch the network.
- Git artifacts (commits, branch names) in English, no attribution or trailers. Code comments in English.
- Run `go test ./... -race -count=1`, `go vet ./...` and `gofmt -l .` (must print nothing) before each commit that touches Go code; run `make lint` when `golangci-lint` is installed.

## Review Focus

- An upstream whose `git ls-remote` fails or lists no usable tag: the module is left unchanged, the others are still processed, and a run where nothing changed and something failed goes red (Task 3, Task 4).
- A `ref` that is not a version (a branch name such as `main`): left alone, never "upgraded" to a tag (Task 1).
- A manifest whose `ref` line has different spacing or a trailing comment: rewriting keeps the rest of the line (Task 2).
- A vendor clone with only untracked build output versus one with edited tracked files: the first is updated, the second is left untouched with a note (Task 7).
- A lockfile written before `fallback_ref` existed: the first `apply` refreshes the clone once and the second `apply` is a no-op (Task 7).

---

# Phase 1: tag updater, workflow, guarded auto-merge

### Task 1: Version parsing and tag selection

**Files:**
- Create: `tools/update-fallback-tags/version.go`
- Create: `tools/update-fallback-tags/select.go`
- Test: `tools/update-fallback-tags/version_test.go`
- Test: `tools/update-fallback-tags/select_test.go`

**Interfaces:**
- Produces: `type version struct{ prefix bool; nums []int }`, `parseTag(tag string) (version, bool)`, `compare(a, b []int) int`, `selectTag(current string, tags []string) (string, bool)`.

- [ ] **Step 1: Write the failing tests**

`tools/update-fallback-tags/version_test.go`:

```go
package main

import (
	"reflect"
	"testing"
)

func TestParseTag(t *testing.T) {
	cases := []struct {
		tag    string
		ok     bool
		prefix bool
		nums   []int
	}{
		{"v1.26.0", true, true, []int{1, 26, 0}},
		{"0.8.0", true, false, []int{0, 8, 0}},
		{"2.69.0", true, false, []int{2, 69, 0}},
		{"v2026.10.3", true, true, []int{2026, 10, 3}},
		{"weekly", false, false, nil},
		{"vfox-v2026.10.3", false, false, nil},
		{"v1.0.0-rc1", false, false, nil},
		{"v1", false, false, nil},
		{"", false, false, nil},
	}
	for _, tc := range cases {
		got, ok := parseTag(tc.tag)
		if ok != tc.ok {
			t.Errorf("parseTag(%q) ok = %v, want %v", tc.tag, ok, tc.ok)
			continue
		}
		if ok && (got.prefix != tc.prefix || !reflect.DeepEqual(got.nums, tc.nums)) {
			t.Errorf("parseTag(%q) = %+v, want prefix %v nums %v", tc.tag, got, tc.prefix, tc.nums)
		}
	}
}

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b []int
		want int
	}{
		{[]int{1, 2, 3}, []int{1, 2, 3}, 0},
		{[]int{2, 10, 0}, []int{2, 9, 0}, 1},
		{[]int{1, 2}, []int{1, 2, 0}, 0},
		{[]int{1, 2}, []int{1, 2, 1}, -1},
		{[]int{0, 8, 0}, []int{0, 7, 1}, 1},
	}
	for _, tc := range cases {
		if got := compare(tc.a, tc.b); got != tc.want {
			t.Errorf("compare(%v, %v) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}
```

`tools/update-fallback-tags/select_test.go`:

```go
package main

import "testing"

func TestSelectTag(t *testing.T) {
	cases := []struct {
		name    string
		current string
		tags    []string
		want    string
		changed bool
	}{
		{"picks the highest newer tag", "v1.25.0", []string{"v1.24.0", "v1.26.0", "v1.25.1"}, "v1.26.0", true},
		{"compares numerically", "v2.9.0", []string{"v2.10.0", "v2.9.5"}, "v2.10.0", true},
		{"keeps the prefix style", "2.68.0", []string{"v1.0.1", "2.69.0", "v3.0.0"}, "2.69.0", true},
		{"ignores weekly and release candidates", "v18.22.0", []string{"weekly", "v18.23.0-rc1", "v18.23.0"}, "v18.23.0", true},
		{"ignores vfox style tags", "v2026.10.2", []string{"weekly", "vfox-v2026.10.3"}, "", false},
		{"never downgrades", "v1.26.0", []string{"v1.25.0"}, "", false},
		{"an equal tag is no change", "v1.26.0", []string{"v1.26.0"}, "", false},
		{"no tags", "v1.26.0", nil, "", false},
		{"a branch ref is left alone", "main", []string{"v9.9.9"}, "", false},
	}
	for _, tc := range cases {
		got, changed := selectTag(tc.current, tc.tags)
		if got != tc.want || changed != tc.changed {
			t.Errorf("%s: selectTag(%q, %v) = (%q, %v), want (%q, %v)",
				tc.name, tc.current, tc.tags, got, changed, tc.want, tc.changed)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./tools/update-fallback-tags/ -count=1`
Expected: FAIL to build with `undefined: parseTag`, `undefined: compare`, `undefined: selectTag`.

- [ ] **Step 3: Write the implementation**

`tools/update-fallback-tags/version.go`:

```go
// Command update-fallback-tags bumps the pinned ref of every built-in git
// fallback to the latest stable upstream release tag.
package main

import (
	"regexp"
	"strconv"
	"strings"
)

// tagRe matches a stable release tag: an optional "v" and at least two dotted
// numbers. Tags such as "weekly", "vfox-v1.2.3" or "v1.0.0-rc1" do not match.
var tagRe = regexp.MustCompile(`^(v?)(\d+(?:\.\d+)+)$`)

// version is a parsed release tag.
type version struct {
	prefix bool // the tag starts with "v"
	nums   []int
}

// parseTag parses a stable release tag; ok is false for anything else.
func parseTag(tag string) (version, bool) {
	m := tagRe.FindStringSubmatch(tag)
	if m == nil {
		return version{}, false
	}
	parts := strings.Split(m[2], ".")
	nums := make([]int, len(parts))
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return version{}, false
		}
		nums[i] = n
	}
	return version{prefix: m[1] == "v", nums: nums}, true
}

// compare returns -1, 0 or 1. A missing trailing component counts as zero.
func compare(a, b []int) int {
	n := max(len(a), len(b))
	for i := range n {
		var x, y int
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		switch {
		case x < y:
			return -1
		case x > y:
			return 1
		}
	}
	return 0
}
```

`tools/update-fallback-tags/select.go`:

```go
package main

// selectTag returns the highest stable tag that is newer than current and uses
// the same "v" prefix style. changed is false when current should stay: no
// newer tag exists, or current is not a version (for example a branch name).
func selectTag(current string, tags []string) (string, bool) {
	cur, ok := parseTag(current)
	if !ok {
		return "", false
	}
	best, bestTag := cur, ""
	for _, tag := range tags {
		v, ok := parseTag(tag)
		if !ok || v.prefix != cur.prefix {
			continue
		}
		if compare(v.nums, best.nums) > 0 {
			best, bestTag = v, tag
		}
	}
	return bestTag, bestTag != ""
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./tools/update-fallback-tags/ -count=1 -race && go vet ./tools/... && gofmt -l tools`
Expected: `ok`, no vet output, gofmt prints nothing.

- [ ] **Step 5: Commit**

```bash
git add tools/update-fallback-tags
git commit -m "feat: add tag selection for the fallback tag updater"
```

### Task 2: Rewrite the ref line

**Files:**
- Create: `tools/update-fallback-tags/rewrite.go`
- Test: `tools/update-fallback-tags/rewrite_test.go`

**Interfaces:**
- Produces: `rewriteRef(manifest, newRef string) (string, error)`.

- [ ] **Step 1: Write the failing tests**

`tools/update-fallback-tags/rewrite_test.go`:

```go
package main

import (
	"strings"
	"testing"
)

const starshipLike = `platforms = ["macos", "linux"]
shells    = ["zsh", "bash"]

[module]
id = "starship"

# a comment that must survive
[packages]
apt = ["starship"]

[[packages.fallback]]
type = "git"
repo = "https://github.com/starship/starship.git"
dest = "{{.VendorDir}}/starship"
ref  = "v1.25.0"
run  = ["cargo", "install"]
requires = ["cargo>=1.95"]
`

func TestRewriteRefChangesOnlyTheRef(t *testing.T) {
	got, err := rewriteRef(starshipLike, "v1.26.0")
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(starshipLike, `ref  = "v1.25.0"`, `ref  = "v1.26.0"`, 1)
	if got != want {
		t.Fatalf("rewrite changed more than the ref:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestRewriteRefKeepsSpacingAndTrailingComment(t *testing.T) {
	in := "[[packages.fallback]]\nrepo = \"r\"\nref=\"v1.0.0\" # pinned\n"
	got, err := rewriteRef(in, "v1.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if want := "[[packages.fallback]]\nrepo = \"r\"\nref=\"v1.1.0\" # pinned\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRewriteRefErrors(t *testing.T) {
	cases := map[string]string{
		"no fallback table": "[module]\nid = \"x\"\n",
		"no ref line":       "[[packages.fallback]]\nrepo = \"r\"\n",
		"ref in a later table, not the fallback": "[[packages.fallback]]\nrepo = \"r\"\n\n[options.x]\nref = \"v1.0.0\"\n",
	}
	for name, in := range cases {
		if _, err := rewriteRef(in, "v2.0.0"); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./tools/update-fallback-tags/ -run TestRewriteRef -count=1`
Expected: FAIL to build with `undefined: rewriteRef`.

- [ ] **Step 3: Write the implementation**

`tools/update-fallback-tags/rewrite.go`:

```go
package main

import (
	"errors"
	"regexp"
	"strings"
)

const fallbackHeader = "[[packages.fallback]]"

// refLineRe matches a ref assignment. Group 1 is everything up to and including
// the opening quote, group 2 the closing quote and any trailing text.
var refLineRe = regexp.MustCompile(`(?m)^(ref\s*=\s*")[^"\n]*(".*)$`)

// rewriteRef sets the ref of the first [[packages.fallback]] table to newRef by
// replacing only the value between the quotes, so comments and formatting are
// preserved.
func rewriteRef(manifest, newRef string) (string, error) {
	start := strings.Index(manifest, fallbackHeader)
	if start < 0 {
		return "", errors.New("manifest has no [[packages.fallback]] table")
	}
	tail := manifest[start:]
	loc := refLineRe.FindStringSubmatchIndex(tail)
	if loc == nil {
		return "", errors.New("fallback has no ref line")
	}
	// The ref must belong to the fallback table, not to a table after it.
	if strings.Contains(tail[len(fallbackHeader):loc[0]], "\n[") {
		return "", errors.New("fallback has no ref line")
	}
	return manifest[:start] + tail[:loc[3]] + newRef + tail[loc[4]:], nil
}
```

Why `tail[:loc[3]]`: group 1 ends at `loc[3]` (after the opening quote), and everything before it, including the `ref =` text, is kept; group 2 starts at `loc[4]` (the closing quote).

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./tools/update-fallback-tags/ -count=1 -race && gofmt -l tools`
Expected: `ok`, gofmt prints nothing.

- [ ] **Step 5: Commit**

```bash
git add tools/update-fallback-tags
git commit -m "feat: rewrite the fallback ref in a manifest without reformatting it"
```

### Task 3: Update the manifests and build the summary

**Files:**
- Create: `tools/update-fallback-tags/update.go`
- Create: `tools/update-fallback-tags/main.go`
- Test: `tools/update-fallback-tags/update_test.go`
- Test: `tools/update-fallback-tags/main_test.go`

**Interfaces:**
- Consumes: `selectTag`, `rewriteRef`, `parseTag` (Tasks 1-2); `module.ParseManifest(data []byte) (module.Manifest, error)` from `internal/module`.
- Produces: `type Runner interface{ Run(name string, args ...string) ([]byte, error) }`, `type Change struct{ Module, Repo, Old, New string }`, `type Failure struct{ Module string; Err error }`, `type Result struct{ Changes []Change; Failures []Failure }`, `Update(dir string, r Runner) (Result, error)`, `summary(res Result) string`, `sameMajor(changes []Change) bool`, `run(args []string, stdout io.Writer, r Runner) error`.

- [ ] **Step 1: Write the failing tests**

`tools/update-fallback-tags/update_test.go`:

```go
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeRunner answers `git ls-remote --tags --refs <repo>` from a table.
type fakeRunner struct {
	tags  map[string]string // repo -> ls-remote output
	fail  map[string]error  // repo -> error
	calls []string
}

func (f *fakeRunner) Run(name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, name+" "+strings.Join(args, " "))
	repo := args[len(args)-1]
	if err := f.fail[repo]; err != nil {
		return nil, err
	}
	return []byte(f.tags[repo]), nil
}

func lsRemote(tags ...string) string {
	var b strings.Builder
	for _, t := range tags {
		_, _ = fmt.Fprintf(&b, "0123456789abcdef0123456789abcdef01234567\trefs/tags/%s\n", t)
	}
	return b.String()
}

const fallbackManifest = `platforms = ["linux"]
shells    = ["bash"]

[module]
id      = %q
name    = %q
version = "1.0.0"
schema  = 1

[[packages.fallback]]
type = "git"
repo = %q
dest = "{{.VendorDir}}/%s"
ref  = %q
`

func writeModule(t *testing.T, dir, id, repo, ref string) string {
	t.Helper()
	path := filepath.Join(dir, id, "manifest.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(fallbackManifest, id, id, repo, id, ref)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestUpdateRewritesANewerTag(t *testing.T) {
	dir := t.TempDir()
	a := writeModule(t, dir, "alpha", "https://example.com/alpha.git", "v1.0.0")
	b := writeModule(t, dir, "beta", "https://example.com/beta.git", "2.0.0")
	beforeB, _ := os.ReadFile(b)
	r := &fakeRunner{tags: map[string]string{
		"https://example.com/alpha.git": lsRemote("v1.0.0", "v1.1.0", "weekly"),
		"https://example.com/beta.git":  lsRemote("2.0.0"),
	}}

	res, err := Update(dir, r)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Changes) != 1 || res.Changes[0] != (Change{Module: "alpha", Repo: "https://example.com/alpha.git", Old: "v1.0.0", New: "v1.1.0"}) {
		t.Fatalf("Changes = %+v", res.Changes)
	}
	gotA, _ := os.ReadFile(a)
	if !strings.Contains(string(gotA), `ref  = "v1.1.0"`) {
		t.Fatalf("alpha not rewritten:\n%s", gotA)
	}
	if gotB, _ := os.ReadFile(b); string(gotB) != string(beforeB) {
		t.Fatal("beta must stay byte-identical")
	}
}

func TestUpdateRecordsAFailurePerModuleAndContinues(t *testing.T) {
	dir := t.TempDir()
	writeModule(t, dir, "alpha", "https://example.com/alpha.git", "v1.0.0")
	writeModule(t, dir, "beta", "https://example.com/beta.git", "v2.0.0")
	r := &fakeRunner{
		fail: map[string]error{"https://example.com/alpha.git": errors.New("exit status 128")},
		tags: map[string]string{"https://example.com/beta.git": lsRemote("v2.1.0")},
	}
	res, err := Update(dir, r)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Failures) != 1 || res.Failures[0].Module != "alpha" {
		t.Fatalf("Failures = %+v", res.Failures)
	}
	if len(res.Changes) != 1 || res.Changes[0].Module != "beta" {
		t.Fatalf("Changes = %+v", res.Changes)
	}
}

func TestUpdateTreatsAnEmptyTagListAsNoChange(t *testing.T) {
	dir := t.TempDir()
	writeModule(t, dir, "alpha", "https://example.com/alpha.git", "v1.0.0")
	res, err := Update(dir, &fakeRunner{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Changes) != 0 || len(res.Failures) != 0 {
		t.Fatalf("res = %+v, want no change and no failure", res)
	}
}

func TestUpdateIgnoresModulesWithoutFallback(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plain", "manifest.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	plain := "platforms = [\"linux\"]\nshells = [\"bash\"]\n\n[module]\nid = \"plain\"\nname = \"plain\"\nversion = \"1.0.0\"\nschema = 1\n"
	if err := os.WriteFile(path, []byte(plain), 0o644); err != nil {
		t.Fatal(err)
	}
	r := &fakeRunner{}
	res, err := Update(dir, r)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.calls) != 0 || len(res.Changes) != 0 || len(res.Failures) != 0 {
		t.Fatalf("a module without a fallback must be skipped: calls=%v res=%+v", r.calls, res)
	}
}

func TestUpdateLeavesANonVersionRefAlone(t *testing.T) {
	dir := t.TempDir()
	writeModule(t, dir, "alpha", "https://example.com/alpha.git", "main")
	r := &fakeRunner{tags: map[string]string{"https://example.com/alpha.git": lsRemote("v9.9.9")}}
	res, err := Update(dir, r)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Changes) != 0 || len(res.Failures) != 0 {
		t.Fatalf("a branch ref must not be upgraded: %+v", res)
	}
}

func TestUpdateReportsAFallbackWithoutRef(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "alpha", "manifest.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "platforms = [\"linux\"]\nshells = [\"bash\"]\n\n[module]\nid = \"alpha\"\nname = \"alpha\"\nversion = \"1.0.0\"\nschema = 1\n\n[[packages.fallback]]\ntype = \"git\"\nrepo = \"https://example.com/alpha.git\"\ndest = \"{{.VendorDir}}/alpha\"\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Update(dir, &fakeRunner{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Failures) != 1 {
		t.Fatalf("Failures = %+v, want one", res.Failures)
	}
}

func TestSameMajor(t *testing.T) {
	cases := []struct {
		name    string
		changes []Change
		want    bool
	}{
		{"no changes", nil, false},
		{"minor and patch bumps", []Change{{Old: "v1.25.0", New: "v1.26.0"}, {Old: "2.68.0", New: "2.69.0"}}, true},
		{"one major bump", []Change{{Old: "v1.25.0", New: "v1.26.0"}, {Old: "v18.9.0", New: "v19.0.0"}}, false},
	}
	for _, tc := range cases {
		if got := sameMajor(tc.changes); got != tc.want {
			t.Errorf("%s: sameMajor = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestSummary(t *testing.T) {
	if got := summary(Result{}); got != "" {
		t.Fatalf("empty result must give an empty summary, got %q", got)
	}
	got := summary(Result{
		Changes:  []Change{{Module: "starship", Old: "v1.25.0", New: "v1.26.0"}},
		Failures: []Failure{{Module: "mise", Err: errors.New("exit status 128")}},
	})
	for _, want := range []string{
		"| `starship` | `v1.25.0` | `v1.26.0` |",
		"`requires`",
		"CI does not build the fallbacks",
		"`mise`: exit status 128",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("summary lacks %q:\n%s", want, got)
		}
	}
}
```

`tools/update-fallback-tags/main_test.go`:

```go
package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunWritesSummaryAndOutputs(t *testing.T) {
	dir := t.TempDir()
	writeModule(t, dir, "alpha", "https://example.com/alpha.git", "v1.0.0")
	r := &fakeRunner{tags: map[string]string{"https://example.com/alpha.git": lsRemote("v1.1.0")}}
	work := t.TempDir()
	summaryPath := filepath.Join(work, "summary.md")
	outputPath := filepath.Join(work, "output")

	var stdout bytes.Buffer
	args := []string{"--dir", dir, "--summary", summaryPath, "--github-output", outputPath}
	if err := run(args, &stdout, r); err != nil {
		t.Fatal(err)
	}
	if s, _ := os.ReadFile(summaryPath); !strings.Contains(string(s), "| `alpha` | `v1.0.0` | `v1.1.0` |") {
		t.Fatalf("summary file:\n%s", s)
	}
	out, _ := os.ReadFile(outputPath)
	if !strings.Contains(string(out), "changed=true\n") || !strings.Contains(string(out), "same_major=true\n") {
		t.Fatalf("github output:\n%s", out)
	}
}

func TestRunReportsNoChange(t *testing.T) {
	dir := t.TempDir()
	writeModule(t, dir, "alpha", "https://example.com/alpha.git", "v1.0.0")
	r := &fakeRunner{tags: map[string]string{"https://example.com/alpha.git": lsRemote("v1.0.0")}}
	outputPath := filepath.Join(t.TempDir(), "output")

	if err := run([]string{"--dir", dir, "--github-output", outputPath}, &bytes.Buffer{}, r); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(outputPath)
	if !strings.Contains(string(out), "changed=false\n") || !strings.Contains(string(out), "same_major=false\n") {
		t.Fatalf("github output:\n%s", out)
	}
}

func TestRunFailsWhenSomethingFailedAndNothingChanged(t *testing.T) {
	dir := t.TempDir()
	writeModule(t, dir, "alpha", "https://example.com/alpha.git", "v1.0.0")
	r := &fakeRunner{fail: map[string]error{"https://example.com/alpha.git": errors.New("exit status 128")}}
	summaryPath := filepath.Join(t.TempDir(), "summary.md")

	err := run([]string{"--dir", dir, "--summary", summaryPath}, &bytes.Buffer{}, r)
	if err == nil {
		t.Fatal("want an error so a run that could check nothing goes red")
	}
	if s, _ := os.ReadFile(summaryPath); !strings.Contains(string(s), "alpha") {
		t.Fatalf("the summary must still be written before the error:\n%s", s)
	}
}

func TestRunSucceedsWhenSomeModulesFailButOthersChange(t *testing.T) {
	dir := t.TempDir()
	writeModule(t, dir, "alpha", "https://example.com/alpha.git", "v1.0.0")
	writeModule(t, dir, "beta", "https://example.com/beta.git", "v2.0.0")
	r := &fakeRunner{
		fail: map[string]error{"https://example.com/alpha.git": errors.New("exit status 128")},
		tags: map[string]string{"https://example.com/beta.git": lsRemote("v2.1.0")},
	}
	if err := run([]string{"--dir", dir}, &bytes.Buffer{}, r); err != nil {
		t.Fatalf("a partial failure with a change must not fail the run: %v", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./tools/update-fallback-tags/ -count=1`
Expected: FAIL to build with `undefined: Update`, `undefined: Change`, `undefined: run` and similar.

- [ ] **Step 3: Write the implementation**

`tools/update-fallback-tags/update.go`:

```go
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/JtheGunner/omnishell/internal/module"
)

// Runner executes an external command and returns its stdout. Injectable so
// tests need neither git nor the network.
type Runner interface {
	Run(name string, args ...string) ([]byte, error)
}

// Change is one bumped fallback ref.
type Change struct {
	Module   string
	Repo     string
	Old, New string
}

// Failure is a module whose upstream could not be checked or rewritten.
type Failure struct {
	Module string
	Err    error
}

// Result is the outcome of one Update run.
type Result struct {
	Changes  []Change
	Failures []Failure
}

// Update bumps the fallback ref of every module under dir (one folder per
// module, each holding manifest.toml) to the latest stable upstream tag. A
// problem with one module is recorded and does not stop the others.
func Update(dir string, r Runner) (Result, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return Result{}, fmt.Errorf("read %s: %w", dir, err)
	}
	var res Result
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		id := entry.Name()
		change, err := updateModule(filepath.Join(dir, id, "manifest.toml"), id, r)
		switch {
		case err != nil:
			res.Failures = append(res.Failures, Failure{Module: id, Err: err})
		case change != nil:
			res.Changes = append(res.Changes, *change)
		}
	}
	return res, nil
}

// updateModule returns a nil Change when the module needs no change.
func updateModule(path, id string, r Runner) (*Change, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	m, err := module.ParseManifest(raw)
	if err != nil {
		return nil, err
	}
	if len(m.Packages.Fallback) == 0 {
		return nil, nil
	}
	fb := m.Packages.Fallback[0]
	if fb.Ref == "" {
		return nil, errors.New("fallback has no ref")
	}
	out, err := r.Run("git", "ls-remote", "--tags", "--refs", fb.Repo)
	if err != nil {
		return nil, fmt.Errorf("git ls-remote %s: %w", fb.Repo, err)
	}
	newRef, ok := selectTag(fb.Ref, parseTags(out))
	if !ok {
		return nil, nil
	}
	updated, err := rewriteRef(string(raw), newRef)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, []byte(updated), info.Mode().Perm()); err != nil {
		return nil, err
	}
	return &Change{Module: id, Repo: fb.Repo, Old: fb.Ref, New: newRef}, nil
}

// parseTags extracts tag names from `git ls-remote --tags --refs` output.
func parseTags(out []byte) []string {
	var tags []string
	for _, line := range strings.Split(string(out), "\n") {
		_, ref, ok := strings.Cut(strings.TrimSpace(line), "\t")
		if !ok {
			continue
		}
		if tag, ok := strings.CutPrefix(ref, "refs/tags/"); ok {
			tags = append(tags, tag)
		}
	}
	return tags
}

// sameMajor reports whether there is at least one change and every change keeps
// its major version. Auto-merge is only requested in that case.
func sameMajor(changes []Change) bool {
	if len(changes) == 0 {
		return false
	}
	for _, c := range changes {
		oldV, okOld := parseTag(c.Old)
		newV, okNew := parseTag(c.New)
		if !okOld || !okNew || oldV.nums[0] != newV.nums[0] {
			return false
		}
	}
	return true
}

// summary renders the pull request body. It is empty when there is nothing to
// report.
func summary(res Result) string {
	if len(res.Changes) == 0 && len(res.Failures) == 0 {
		return ""
	}
	var b strings.Builder
	if len(res.Changes) > 0 {
		b.WriteString("Bumps the pinned `git` fallback tags of the built-in modules to the latest stable upstream release.\n\n")
		b.WriteString("| Module | Old | New |\n| --- | --- | --- |\n")
		for _, c := range res.Changes {
			_, _ = fmt.Fprintf(&b, "| `%s` | `%s` | `%s` |\n", c.Module, c.Old, c.New)
		}
		b.WriteString("\n**Before merging:** check that each module's `requires` still matches the toolchain the new tag needs (for example the Rust `rust-version` in `Cargo.toml`). CI does not build the fallbacks.\n")
	}
	if len(res.Failures) > 0 {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString("**Could not check:**\n")
		for _, f := range res.Failures {
			_, _ = fmt.Fprintf(&b, "- `%s`: %v\n", f.Module, f.Err)
		}
	}
	return b.String()
}
```

`tools/update-fallback-tags/main.go`:

```go
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, execRunner{}); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// execRunner runs real commands.
type execRunner struct{}

func (execRunner) Run(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).Output()
}

// run is main without the process exit, so tests can drive it.
func run(args []string, stdout io.Writer, r Runner) error {
	fs := flag.NewFlagSet("update-fallback-tags", flag.ContinueOnError)
	dir := fs.String("dir", "modules/builtin", "directory holding the built-in module folders")
	summaryPath := fs.String("summary", "", "write the pull request body to this file")
	outputPath := fs.String("github-output", "", "append changed/same_major lines to this file (GITHUB_OUTPUT format)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	res, err := Update(*dir, r)
	if err != nil {
		return err
	}
	if *summaryPath != "" {
		if err := os.WriteFile(*summaryPath, []byte(summary(res)), 0o644); err != nil {
			return fmt.Errorf("write summary: %w", err)
		}
	}
	if *outputPath != "" {
		if err := appendOutputs(*outputPath, len(res.Changes) > 0, sameMajor(res.Changes)); err != nil {
			return err
		}
	}
	_, _ = fmt.Fprintf(stdout, "%d change(s), %d failure(s)\n", len(res.Changes), len(res.Failures))
	for _, f := range res.Failures {
		_, _ = fmt.Fprintf(stdout, "  %s: %v\n", f.Module, f.Err)
	}
	if len(res.Failures) > 0 && len(res.Changes) == 0 {
		return fmt.Errorf("%d module(s) could not be checked and nothing changed", len(res.Failures))
	}
	return nil
}

func appendOutputs(path string, changed, sameMajor bool) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	if _, err := fmt.Fprintf(f, "changed=%t\nsame_major=%t\n", changed, sameMajor); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./tools/update-fallback-tags/ -count=1 -race && go vet ./tools/... && gofmt -l tools`
Expected: `ok`, no vet output, gofmt prints nothing.

- [ ] **Step 5: Smoke-test against the real manifests without writing anything**

Run on a throwaway copy so the checked-in manifests stay untouched:

```bash
tmp="$(mktemp -d)" && cp -R modules/builtin "$tmp/builtin" \
  && go run ./tools/update-fallback-tags --dir "$tmp/builtin" --summary "$tmp/summary.md"; \
  cat "$tmp/summary.md"; rm -rf "$tmp"
```

Expected: `0 change(s), 0 failure(s)` right after OMNIS-26 (all refs are current), or a table of bumps if upstream released since; no failures. This step needs network access.

- [ ] **Step 6: Commit**

```bash
git add tools/update-fallback-tags
git commit -m "feat: add the fallback tag updater tool"
```

### Task 4: Scheduled workflow, guarded auto-merge and contributor docs

**Files:**
- Create: `.github/workflows/fallback-tags.yml`
- Modify: `CONTRIBUTING.md` (append a section)

**Interfaces:**
- Consumes: the tool's `--summary` and `--github-output` flags and its `changed` / `same_major` outputs (Task 3).

- [ ] **Step 1: Write the workflow**

`.github/workflows/fallback-tags.yml`:

```yaml
name: fallback-tags
on:
  schedule: [{ cron: '0 6 * * 1' }]
  workflow_dispatch:
permissions:
  contents: write
  pull-requests: write
jobs:
  update:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7
      - uses: actions/setup-go@v7
        with: { go-version-file: go.mod }
      - name: Look up newer tags
        id: tags
        run: |
          go run ./tools/update-fallback-tags \
            --summary "$RUNNER_TEMP/summary.md" \
            --github-output "$GITHUB_OUTPUT"
      - name: Open or update the pull request
        id: cpr
        if: steps.tags.outputs.changed == 'true'
        # Pinned to a commit SHA because this step runs with write access (and
        # possibly a personal access token); Dependabot keeps the SHA current.
        uses: peter-evans/create-pull-request@5f6978faf089d4d20b00c7766989d076bb2fc7f1 # v8.1.1
        with:
          # A PR created with the default GITHUB_TOKEN does not trigger other
          # workflows, so ci would not run on it. Set FALLBACK_TAGS_TOKEN to a
          # personal access token to get CI on the PR.
          token: ${{ secrets.FALLBACK_TAGS_TOKEN || github.token }}
          branch: chore/update-fallback-tags
          delete-branch: true
          add-paths: modules/builtin
          commit-message: 'chore: update pinned fallback tags'
          title: 'chore: update pinned fallback tags'
          body-path: ${{ runner.temp }}/summary.md
      - name: Enable auto-merge when main requires status checks
        if: >-
          steps.cpr.outputs.pull-request-number != '' &&
          vars.FALLBACK_TAGS_AUTOMERGE == 'true' &&
          steps.tags.outputs.same_major == 'true'
        env:
          GH_TOKEN: ${{ secrets.FALLBACK_TAGS_TOKEN || github.token }}
          PR: ${{ steps.cpr.outputs.pull-request-number }}
          REPO: ${{ github.repository }}
        run: sh .github/scripts/enable-automerge.sh
      # Auto-merge stays on a pull request once enabled, and the PR branch is
      # reused across runs. If a later run no longer qualifies (a major bump, or
      # the variable was switched off), take it back instead of letting the new
      # content merge on the old approval.
      - name: Disable auto-merge when the guard no longer holds
        if: >-
          steps.cpr.outputs.pull-request-number != '' &&
          (vars.FALLBACK_TAGS_AUTOMERGE != 'true' || steps.tags.outputs.same_major != 'true')
        env:
          GH_TOKEN: ${{ secrets.FALLBACK_TAGS_TOKEN || github.token }}
          PR: ${{ steps.cpr.outputs.pull-request-number }}
          REPO: ${{ github.repository }}
        run: gh pr merge "$PR" --repo "$REPO" --disable-auto || true
```

The auto-merge guard lives in `.github/scripts/enable-automerge.sh` and is covered by `tools/update-fallback-tags/guard_test.go` (fake `gh` on PATH; it must fail closed on an API error, an empty or non-numeric answer, and zero required checks). `sameMajor` uses a compatibility key (major, or major.minor while the major is 0).

- [ ] **Step 2: Lint the workflow syntax**

Run: `python3 -c "import yaml,sys; yaml.safe_load(open('.github/workflows/fallback-tags.yml')); print('yaml ok')"`
Expected: `yaml ok`. If `actionlint` is installed, also run `actionlint .github/workflows/fallback-tags.yml` and fix findings.

- [ ] **Step 3: Document the workflow for contributors**

Append to `CONTRIBUTING.md`:

````markdown
## Pinned fallback tags

Every built-in module that falls back to a `git` clone pins a release tag in its manifest (`ref`). The `fallback-tags` workflow (`.github/workflows/fallback-tags.yml`) looks for newer stable tags every Monday and opens a pull request that bumps them. Run it by hand from the Actions tab (`workflow_dispatch`) or locally:

```sh
go run ./tools/update-fallback-tags --summary summary.md
```

Before merging such a pull request, check that each module's `requires` still matches the toolchain the new tag needs; CI does not build the fallbacks.

Two optional repository settings:

- **`FALLBACK_TAGS_TOKEN`** (secret): a personal access token. Without it the pull request is created with the default token and `ci` does not run on it.
- **Auto-merge:** set the repository variable `FALLBACK_TAGS_AUTOMERGE` to `true` to let the workflow request auto-merge for pull requests whose bumps keep their major version. It only takes effect when *Allow auto-merge* is enabled and `main` has branch protection with required status checks; otherwise the workflow leaves the pull request open and says so in the job summary.

The workflow also needs *Settings → Actions → General → Allow GitHub Actions to create and approve pull requests*.
````

- [ ] **Step 4: Run the full verification**

Run: `go test ./... -race -count=1 && go vet ./... && gofmt -l .`
Expected: all packages `ok`, no vet or gofmt output.

- [ ] **Step 5: Commit**

```bash
git add .github/workflows/fallback-tags.yml CONTRIBUTING.md
git commit -m "ci: open a weekly pull request that bumps pinned fallback tags"
```

- [ ] **Step 6: Phase 1 hand-off (needs the owner)**

Do not push on your own. Push and open the PR through `/youtrack-task pr`. After merge, the owner enables the repository settings listed in `CONTRIBUTING.md` and starts the workflow once with *Run workflow* to check it end to end. Phase 2 starts only after Phase 1 is merged.

---

# Phase 2: refresh existing vendor clones

### Task 5: Record the fallback ref in the lockfile

**Files:**
- Modify: `internal/lockfile/lockfile.go` (`ModuleState`)
- Test: `internal/lockfile/lockfile_test.go`

**Interfaces:**
- Produces: `lockfile.ModuleState.FallbackRef string` (`json:"fallback_ref,omitempty"`).

- [ ] **Step 1: Write the failing test**

Add `"os"` and `"strings"` to the imports of `internal/lockfile/lockfile_test.go`, then append:

```go
func TestFallbackRefRoundTripsAndIsOptional(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.lock.json")
	l := sample()
	st := l.Modules["fzf"]
	st.FallbackRef = "v0.74.4"
	l.Modules["fzf"] = st
	if err := l.Write(path); err != nil {
		t.Fatal(err)
	}
	got, ok, err := lockfile.Load(path)
	if err != nil || !ok {
		t.Fatalf("Load: ok=%v err=%v", ok, err)
	}
	if got.Modules["fzf"].FallbackRef != "v0.74.4" {
		t.Fatalf("FallbackRef = %q, want v0.74.4", got.Modules["fzf"].FallbackRef)
	}

	// A lockfile written before this field existed must read as "unknown", and
	// an empty value must not be written at all.
	st.FallbackRef = ""
	l.Modules["fzf"] = st
	if err := l.Write(path); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "fallback_ref") {
		t.Fatalf("an empty FallbackRef must be omitted:\n%s", raw)
	}
	got, _, err = lockfile.Load(path)
	if err != nil || got.Modules["fzf"].FallbackRef != "" {
		t.Fatalf("legacy lockfile: FallbackRef=%q err=%v", got.Modules["fzf"].FallbackRef, err)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/lockfile/ -run TestFallbackRef -count=1`
Expected: FAIL to build with `st.FallbackRef undefined`.

- [ ] **Step 3: Add the field**

In `internal/lockfile/lockfile.go`, extend `ModuleState` (keep the existing fields, add the last one):

```go
type ModuleState struct {
	ModuleVersion  string         `json:"module_version"`
	Enabled        bool           `json:"enabled"`
	OptionsHash    string         `json:"options_hash"`
	ShellsRendered []string       `json:"shells_rendered"`
	Packages       []PackageState `json:"packages"`
	VendorPaths    []string       `json:"vendor_paths"`
	// FallbackRef is the ref the module's git fallback clone was built from.
	// Empty means unknown (a lockfile from before this field) or no pinned ref.
	FallbackRef string `json:"fallback_ref,omitempty"`
	Status      string `json:"status"`
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/lockfile/ -count=1 -race && gofmt -l internal`
Expected: `ok`, gofmt prints nothing.

- [ ] **Step 5: Commit**

```bash
git add internal/lockfile
git commit -m "feat: record the fallback ref a clone was built from in the lockfile"
```

### Task 6: Plan an update for an outdated clone

**Files:**
- Modify: `internal/engine/plan.go` (`PackagePlan`, `planPackages`, `planFallback`, `RenderPlan`, call site in `ComputePlan`)
- Modify: `internal/engine/doctor.go` (the `packages-missing` block)
- Create: `internal/engine/plan_internal_test.go`
- Test: `internal/engine/apply_test.go` (doctor test is added in Task 7 once apply can populate the lock)

**Interfaces:**
- Consumes: `lockfile.ModuleState.FallbackRef` (Task 5).
- Produces: `PackagePlan{Name, Manager string; AlreadyInstalled bool; Update bool; From, To string}`, `planFallback(mp *ModulePlan, e Engine, prev lockfile.ModuleState)`, `planPackages(mp *ModulePlan, e Engine, mod module.Module, shells []string, prev lockfile.ModuleState)`, `describePackage(pp PackagePlan) string`.

- [ ] **Step 1: Write the failing tests**

`internal/engine/plan_internal_test.go`:

```go
package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JtheGunner/omnishell/internal/lockfile"
	"github.com/JtheGunner/omnishell/internal/module"
	"github.com/JtheGunner/omnishell/internal/pkgmgr"
	"github.com/JtheGunner/omnishell/internal/platform"
)

func fallbackModulePlan(ref string) ModulePlan {
	return ModulePlan{Manifest: module.Manifest{Packages: module.Packages{
		Fallback: []module.Fallback{{
			Type: "git", Repo: "https://example.com/x.git", Dest: "{{.VendorDir}}/x", Ref: ref,
		}},
	}}}
}

func engineWithClone(t *testing.T, populated bool) Engine {
	t.Helper()
	configDir := t.TempDir()
	if populated {
		dir := filepath.Join(configDir, "vendor", "x")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "file"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return Engine{Platform: platform.Info{OS: platform.Linux, ConfigDir: configDir}}
}

func TestPlanFallback(t *testing.T) {
	cases := []struct {
		name       string
		populated  bool
		ref        string
		prev       lockfile.ModuleState
		wantMissed int
		wantUpdate bool
	}{
		{"fresh clone", false, "v2", lockfile.ModuleState{}, 1, false},
		{"pinned and recorded", true, "v2", lockfile.ModuleState{FallbackRef: "v2"}, 0, false},
		{"pinned, recorded ref differs", true, "v2", lockfile.ModuleState{FallbackRef: "v1"}, 1, true},
		{"pinned, nothing recorded", true, "v2", lockfile.ModuleState{}, 1, true},
		{"no ref keeps a populated clone", true, "", lockfile.ModuleState{}, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mp := fallbackModulePlan(tc.ref)
			planFallback(&mp, engineWithClone(t, tc.populated), tc.prev)
			if !mp.UsesFallback {
				t.Fatal("UsesFallback = false, want true")
			}
			if len(mp.MissingPackages) != tc.wantMissed {
				t.Fatalf("MissingPackages = %+v, want %d entries", mp.MissingPackages, tc.wantMissed)
			}
			if tc.wantMissed == 0 {
				return
			}
			pp := mp.MissingPackages[0]
			if pp.Manager != "git" || pp.Update != tc.wantUpdate {
				t.Fatalf("entry = %+v, want a git entry with Update=%v", pp, tc.wantUpdate)
			}
			if tc.wantUpdate && (pp.From != tc.prev.FallbackRef || pp.To != tc.ref) {
				t.Fatalf("From/To = %q/%q, want %q/%q", pp.From, pp.To, tc.prev.FallbackRef, tc.ref)
			}
		})
	}
}

func TestPlanFallbackNeverRunsCommands(t *testing.T) {
	runner := &pkgmgr.MockRunner{}
	e := engineWithClone(t, true)
	e.Runner = runner
	mp := fallbackModulePlan("v2")
	planFallback(&mp, e, lockfile.ModuleState{FallbackRef: "v1"})
	if len(runner.Calls) != 0 {
		t.Fatalf("planning must stay side-effect-free, but ran: %v", runner.Calls)
	}
}

func TestRenderPlanNamesAFallbackUpdate(t *testing.T) {
	plan := func(from string) Plan {
		return Plan{
			Order:            []string{"x"},
			ManagedShells:    []string{"bash"},
			PackageManager:   "apt",
			ManagerAvailable: true,
			Modules: map[string]ModulePlan{"x": {
				ID: "x", Action: ActionUpdate, Shells: []string{"bash"},
				MissingPackages: []PackagePlan{{Name: "https://example.com/x.git", Manager: "git", Update: true, From: from, To: "v2"}},
			}},
		}
	}
	if got := RenderPlan(plan("v1")); !strings.Contains(got, "git update v1 → v2, rebuild") {
		t.Fatalf("plan text lacks the update:\n%s", got)
	}
	if got := RenderPlan(plan("")); !strings.Contains(got, "git update unrecorded → v2, rebuild") {
		t.Fatalf("plan text lacks the unrecorded wording:\n%s", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/engine/ -run 'TestPlanFallback|TestRenderPlanNamesAFallbackUpdate' -count=1`
Expected: FAIL to build (`planFallback` takes two arguments, `PackagePlan` has no `Update`).

- [ ] **Step 3: Implement the plan changes**

In `internal/engine/plan.go`:

1. Extend `PackagePlan` (keep the existing `Name`, `Manager`, `AlreadyInstalled`):

```go
// PackagePlan is one package the plan may install.
type PackagePlan struct {
	Name             string
	Manager          string
	AlreadyInstalled bool
	// Update marks a git fallback whose clone already exists but was built from
	// another ref than the manifest pins; From is the recorded ref ("" when
	// unknown), To the pinned one.
	Update   bool
	From, To string
}
```

2. Thread `prev` through. Change the signature and the two calls:

```go
func planPackages(mp *ModulePlan, e Engine, mod module.Module, shells []string, prev lockfile.ModuleState) {
```

Inside `planPackages` replace both `planFallback(mp, e)` calls with `planFallback(mp, e, prev)`. In `ComputePlan`, `prev, inLock := lock.Modules[id]` is declared after the `planPackages` call; move the lookup above it and pass it in:

```go
		prev, inLock := lock.Modules[id]

		// A module with no compatible managed shell can never render its
		// snippet, so installing its packages would only leave software on
		// the system with nothing sourcing it — skip package planning too.
		if !noPackages && mp.DegradedReason == "" {
			planPackages(&mp, e, mod, shells, prev)
		}
```

and delete the later `prev, inLock := lock.Modules[id]` line (keep the `switch` that follows).

3. Replace `planFallback`:

```go
// planFallback selects the module's git fallback. A missing clone is queued as
// a fresh install; an existing clone is queued as an update when the manifest
// pins a ref and the clone was not recorded as built from it. The lockfile
// supplies the recorded ref, so planning never probes the clone itself.
func planFallback(mp *ModulePlan, e Engine, prev lockfile.ModuleState) {
	fb := mp.Manifest.Packages.Fallback[0]
	mp.UsesFallback = true
	ok, _ := pkgmgr.FallbackSatisfied(fb, pkgmgr.FallbackContext{
		VendorDir: e.Platform.ConfigDir + "/vendor",
		Platform:  string(e.Platform.OS),
	})
	switch {
	case !ok:
		mp.MissingPackages = append(mp.MissingPackages, PackagePlan{Name: fb.Repo, Manager: "git"})
	case fb.Ref != "" && prev.FallbackRef != fb.Ref:
		mp.MissingPackages = append(mp.MissingPackages, PackagePlan{
			Name: fb.Repo, Manager: "git", Update: true, From: prev.FallbackRef, To: fb.Ref,
		})
	}
}

// describePackage names a planned package for the plan output.
func describePackage(pp PackagePlan) string {
	if pp.Update {
		from := pp.From
		if from == "" {
			from = "unrecorded"
		}
		return fmt.Sprintf("%s (git update %s → %s, rebuild)", pp.Name, from, pp.To)
	}
	return pp.Name + " (" + pp.Manager + ")"
}
```

4. In `RenderPlan` replace the loop body `names[i] = pp.Name + " (" + pp.Manager + ")"` with `names[i] = describePackage(pp)`.

5. Add `"github.com/JtheGunner/omnishell/internal/lockfile"` to the imports if `plan.go` does not already import it (it does, `ComputePlan` takes a `lockfile.Lock`).

In `internal/engine/doctor.go` replace the `packages-missing` block (step 5) with:

```go
		var missing []string
		for _, pp := range mp.MissingPackages {
			if pp.Update {
				from := pp.From
				if from == "" {
					from = "unrecorded"
				}
				add(SeverityNotice, "fallback-outdated:"+id,
					fmt.Sprintf("module %q: fallback clone was built from %s, the manifest pins %s (run apply to update)", id, from, pp.To))
				continue
			}
			missing = append(missing, pp.Name)
		}
		if len(missing) > 0 {
			add(SeverityDrift, "packages-missing:"+id,
				fmt.Sprintf("module %q is missing packages: %s", id, strings.Join(missing, ", ")))
		}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/engine/ -count=1 -race && go vet ./... && gofmt -l internal`
Expected: `ok` (existing plan, apply and doctor tests still pass), no vet or gofmt output.

- [ ] **Step 5: Commit**

```bash
git add internal/engine
git commit -m "feat: plan an update for a fallback clone built from another ref"
```

### Task 7: Update the clone during apply and record the ref

**Files:**
- Modify: `internal/pkgmgr/gitfallback.go` (factor the build step, add `UpdateGitFallback` and `ErrCloneModified`)
- Modify: `internal/engine/apply.go` (declare and pass `fallbackRefs`)
- Modify: `internal/engine/apply_helpers.go` (`installPackages`, `rebuildLock`)
- Test: `internal/pkgmgr/gitfallback_test.go`
- Test: `internal/engine/apply_test.go`

**Interfaces:**
- Consumes: `PackagePlan.Update/From/To` (Task 6), `lockfile.ModuleState.FallbackRef` (Task 5), `pkgmgr.CheckRequirements`.
- Produces: `pkgmgr.ErrCloneModified`, `pkgmgr.UpdateGitFallback(fb module.Fallback, ctx FallbackContext, r Runner) (string, error)`, and `fallbackRefs map[string]string` threaded through `installPackages` and `rebuildLock`.

- [ ] **Step 1: Write the failing pkgmgr tests**

Add `"errors"` and `"strings"` to the imports of `internal/pkgmgr/gitfallback_test.go` if absent, then append:

```go
func populatedClone(t *testing.T) (vendor, dest string) {
	t.Helper()
	vendor = t.TempDir()
	dest = filepath.Join(vendor, "fzf")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dest, "file"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	return vendor, dest
}

func updateFallbackSpec() module.Fallback {
	return module.Fallback{
		Type: "git", Repo: "https://example.com/fzf.git", Dest: "{{.VendorDir}}/fzf", Ref: "v2",
		Run: []string{"{{.VendorDir}}/fzf/install", "--bin"},
	}
}

func TestUpdateGitFallbackFetchesChecksOutAndRebuilds(t *testing.T) {
	vendor, dest := populatedClone(t)
	r := &pkgmgr.MockRunner{}
	got, err := pkgmgr.UpdateGitFallback(updateFallbackSpec(), pkgmgr.FallbackContext{VendorDir: vendor}, r)
	if err != nil || got != dest {
		t.Fatalf("UpdateGitFallback = %q, %v; want %q, nil", got, err, dest)
	}
	want := []string{
		"git -C " + dest + " status --porcelain --untracked-files=no",
		"git -C " + dest + " fetch --depth 1 origin v2",
		"git -C " + dest + " -c advice.detachedHead=false checkout --detach FETCH_HEAD",
		filepath.Join(dest, "install") + " --bin",
	}
	if strings.Join(r.Calls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("calls:\n%s\nwant:\n%s", strings.Join(r.Calls, "\n"), strings.Join(want, "\n"))
	}
}

func TestUpdateGitFallbackLeavesAModifiedCloneAlone(t *testing.T) {
	vendor, dest := populatedClone(t)
	status := "git -C " + dest + " status --porcelain --untracked-files=no"
	r := &pkgmgr.MockRunner{Responses: map[string]pkgmgr.MockResponse{status: {Out: []byte(" M install\n")}}}
	got, err := pkgmgr.UpdateGitFallback(updateFallbackSpec(), pkgmgr.FallbackContext{VendorDir: vendor}, r)
	if !errors.Is(err, pkgmgr.ErrCloneModified) || got != dest {
		t.Fatalf("UpdateGitFallback = %q, %v; want %q, ErrCloneModified", got, err, dest)
	}
	if len(r.Calls) != 1 {
		t.Fatalf("a modified clone must not be fetched or rebuilt; calls = %v", r.Calls)
	}
}

func TestUpdateGitFallbackFetchFailureStopsBeforeTheBuild(t *testing.T) {
	vendor, dest := populatedClone(t)
	fetch := "git -C " + dest + " fetch --depth 1 origin v2"
	r := &pkgmgr.MockRunner{Responses: map[string]pkgmgr.MockResponse{fetch: {Err: errors.New("network down")}}}
	if _, err := pkgmgr.UpdateGitFallback(updateFallbackSpec(), pkgmgr.FallbackContext{VendorDir: vendor}, r); err == nil || !strings.Contains(err.Error(), "network down") {
		t.Fatalf("err = %v, want the fetch error", err)
	}
	for _, c := range r.Calls {
		if strings.Contains(c, "install --bin") {
			t.Fatalf("the build must not run after a failed fetch: %v", r.Calls)
		}
	}
}

func TestUpdateGitFallbackKeepsTheCloneWhenTheBuildFails(t *testing.T) {
	vendor, dest := populatedClone(t)
	build := filepath.Join(dest, "install") + " --bin"
	r := &pkgmgr.MockRunner{Responses: map[string]pkgmgr.MockResponse{build: {Err: errors.New("build failed")}}}
	if _, err := pkgmgr.UpdateGitFallback(updateFallbackSpec(), pkgmgr.FallbackContext{VendorDir: vendor}, r); err == nil {
		t.Fatal("want the build error")
	}
	if _, err := os.Stat(dest); err != nil {
		t.Fatalf("a failed update must not delete the existing clone: %v", err)
	}
}

func TestUpdateGitFallbackRejectsBadInput(t *testing.T) {
	emptyVendor := t.TempDir() // no clone inside
	if _, err := pkgmgr.UpdateGitFallback(updateFallbackSpec(), pkgmgr.FallbackContext{VendorDir: emptyVendor}, &pkgmgr.MockRunner{}); err == nil {
		t.Fatal("want an error for a missing clone")
	}
	vendor, _ := populatedClone(t)
	fb := updateFallbackSpec()
	fb.Ref = ""
	if _, err := pkgmgr.UpdateGitFallback(fb, pkgmgr.FallbackContext{VendorDir: vendor}, &pkgmgr.MockRunner{}); err == nil {
		t.Fatal("want an error without a ref")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/pkgmgr/ -run TestUpdateGitFallback -count=1`
Expected: FAIL to build with `undefined: pkgmgr.UpdateGitFallback` and `undefined: pkgmgr.ErrCloneModified`.

- [ ] **Step 3: Implement `UpdateGitFallback`**

In `internal/pkgmgr/gitfallback.go`, add `"errors"` to the imports and replace `InstallGitFallback` plus the new function with:

```go
// ErrCloneModified reports that an existing fallback clone has tracked local
// changes; UpdateGitFallback leaves it untouched.
var ErrCloneModified = errors.New("fallback clone has local changes")

// InstallGitFallback clones fb.Repo into the rendered fb.Dest and, if set, runs
// fb.Run (at fb.Ref when set). A populated dest short-circuits without
// cloning; fb.Requires is checked before anything is cloned. It returns the
// resolved dest path as vendorPath.
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
	if err := CheckRequirements(fb.Requires, r); err != nil {
		return "", err
	}
	cloneArgs := []string{"clone", "--depth", "1"}
	if fb.Ref != "" {
		// A tag checkout is a detached HEAD; silence git's long advice for it.
		cloneArgs = append([]string{"-c", "advice.detachedHead=false"}, append(cloneArgs, "--branch", fb.Ref)...)
	}
	if _, err := r.Run("git", append(cloneArgs, fb.Repo, dest)...); err != nil {
		return "", fmt.Errorf("git clone %s: %w", fb.Repo, err)
	}
	if err := runFallbackBuild(fb, ctx, r); err != nil {
		_ = os.RemoveAll(dest) // best-effort cleanup; the build error is what we report
		return "", err
	}
	return dest, nil
}

// UpdateGitFallback moves an existing clone to fb.Ref and re-runs fb.Run. It
// returns ErrCloneModified (with the clone path) and changes nothing when the
// clone has tracked local changes; untracked build output such as target/ does
// not count. A failed fetch, checkout or build leaves the clone in place so the
// next apply can retry.
func UpdateGitFallback(fb module.Fallback, ctx FallbackContext, r Runner) (string, error) {
	if fb.Type != "git" {
		return "", fmt.Errorf("unsupported fallback type %q", fb.Type)
	}
	if fb.Ref == "" {
		return "", errors.New("fallback has no ref to update to")
	}
	dest, err := renderPath(fb.Dest, ctx)
	if err != nil {
		return "", fmt.Errorf("render fallback dest: %w", err)
	}
	if !populatedDir(dest) {
		return "", fmt.Errorf("fallback clone %s does not exist", dest)
	}
	if err := CheckRequirements(fb.Requires, r); err != nil {
		return "", err
	}
	out, err := r.Run("git", "-C", dest, "status", "--porcelain", "--untracked-files=no")
	if err != nil {
		return "", fmt.Errorf("git status %s: %w", dest, err)
	}
	if strings.TrimSpace(string(out)) != "" {
		return dest, ErrCloneModified
	}
	if _, err := r.Run("git", "-C", dest, "fetch", "--depth", "1", "origin", fb.Ref); err != nil {
		return "", fmt.Errorf("git fetch %s: %w", fb.Ref, err)
	}
	if _, err := r.Run("git", "-C", dest, "-c", "advice.detachedHead=false", "checkout", "--detach", "FETCH_HEAD"); err != nil {
		return "", fmt.Errorf("git checkout %s: %w", fb.Ref, err)
	}
	if err := runFallbackBuild(fb, ctx, r); err != nil {
		return "", err
	}
	return dest, nil
}

// runFallbackBuild runs fb.Run, if set, with every element rendered as a
// template. It never removes the clone; callers decide what a failure means.
func runFallbackBuild(fb module.Fallback, ctx FallbackContext, r Runner) error {
	if len(fb.Run) == 0 {
		return nil
	}
	argv := make([]string, 0, len(fb.Run))
	for _, part := range fb.Run {
		rp, err := renderPath(part, ctx)
		if err != nil {
			return fmt.Errorf("render fallback run: %w", err)
		}
		argv = append(argv, rp)
	}
	if argv[0] == "" {
		return nil
	}
	if _, err := r.Run(argv[0], argv[1:]...); err != nil {
		return fmt.Errorf("fallback run %q: %w", strings.Join(argv, " "), err)
	}
	return nil
}
```


- [ ] **Step 4: Run the pkgmgr tests to verify they pass**

Run: `go test ./internal/pkgmgr/ -count=1 -race && gofmt -l internal`
Expected: `ok`, including the existing `InstallGitFallback` tests (the refactor must not change their behavior), gofmt prints nothing.

- [ ] **Step 5: Write the failing engine tests**

Append to `internal/engine/apply_test.go` (it already imports `bytes`, `errors`, `os`, `filepath`, `strings`, `config`, `engine`, `lockfile`, `pkgmgr`):

```go
// fzfSandbox is an apply sandbox whose only module is the fzf fixture, which
// pins ref v0.1.0 and falls back to a git clone when apt lacks the package.
type fzfSandbox struct {
	e                 engine.Engine
	home              string
	cfgPath, lockPath string
	out               *bytes.Buffer
}

func newFzfSandbox(t *testing.T, runner pkgmgr.Runner) fzfSandbox {
	t.Helper()
	home := t.TempDir()
	out := &bytes.Buffer{}
	mgr := &pkgmgr.MockManager{NameV: "apt", DetectV: true, Installed: map[string]bool{}, Unavailable: map[string]bool{"fzf": true}}
	e := applyEngine(t, home, mgr, out)
	e.Runner = runner
	cfgPath := filepath.Join(home, ".config", "omnishell", "config.toml")
	writeConfig(t, cfgPath, "[omnishell]\nversion=1\nshells=[\"bash\"]\n[modules.fzf]\nenabled=true\n")
	return fzfSandbox{e: e, home: home, cfgPath: cfgPath, lockPath: filepath.Join(home, ".config", "omnishell", "state.lock.json"), out: out}
}

func (s fzfSandbox) cloneDir() string {
	return filepath.Join(s.home, ".config", "omnishell", "vendor", "fzf")
}

// populateClone simulates a clone that an earlier apply left behind; the mock
// runner cannot create one.
func (s fzfSandbox) populateClone(t *testing.T) {
	t.Helper()
	if err := os.MkdirAll(s.cloneDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.cloneDir(), "install"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (s fzfSandbox) apply(t *testing.T) (engine.Result, error) {
	t.Helper()
	cfg, err := config.Load(s.cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	return s.e.Apply(cfg, s.cfgPath, s.lockPath, engine.ApplyOptions{Yes: true})
}

func (s fzfSandbox) fallbackRef(t *testing.T) string {
	t.Helper()
	lock, ok, err := lockfile.Load(s.lockPath)
	if err != nil || !ok {
		t.Fatalf("lock: ok=%v err=%v", ok, err)
	}
	return lock.Modules["fzf"].FallbackRef
}

func (s fzfSandbox) statusKey() string {
	return "git -C " + s.cloneDir() + " status --porcelain --untracked-files=no"
}

func anyCallContains(calls []string, sub string) bool {
	for _, c := range calls {
		if strings.Contains(c, sub) {
			return true
		}
	}
	return false
}

func TestApplyRecordsTheFallbackRefOnAFreshClone(t *testing.T) {
	runner := &pkgmgr.MockRunner{}
	s := newFzfSandbox(t, runner)
	if _, err := s.apply(t); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got := s.fallbackRef(t); got != "v0.1.0" {
		t.Fatalf("recorded fallback ref = %q, want v0.1.0", got)
	}
}

func TestApplyUpdatesAnExistingCloneOnceAndThenSettles(t *testing.T) {
	runner := &pkgmgr.MockRunner{}
	s := newFzfSandbox(t, runner)
	s.populateClone(t) // a clone from before the lockfile recorded refs

	if _, err := s.apply(t); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !anyCallContains(runner.Calls, "fetch --depth 1 origin v0.1.0") || !anyCallContains(runner.Calls, "checkout --detach FETCH_HEAD") {
		t.Fatalf("the clone was not moved to the pinned tag: %v", runner.Calls)
	}
	if !strings.Contains(s.out.String(), "updating fzf fallback to v0.1.0") {
		t.Fatalf("output lacks the update line:\n%s", s.out.String())
	}
	if got := s.fallbackRef(t); got != "v0.1.0" {
		t.Fatalf("recorded fallback ref = %q, want v0.1.0", got)
	}

	before := len(runner.Calls)
	if _, err := s.apply(t); err != nil {
		t.Fatalf("second Apply: %v", err)
	}
	if len(runner.Calls) != before {
		t.Fatalf("the second apply must be a no-op, but ran: %v", runner.Calls[before:])
	}
}

func TestApplyKeepsAModifiedFallbackClone(t *testing.T) {
	runner := &pkgmgr.MockRunner{}
	s := newFzfSandbox(t, runner)
	runner.Responses = map[string]pkgmgr.MockResponse{s.statusKey(): {Out: []byte(" M install\n")}}
	s.populateClone(t)

	res, err := s.apply(t)
	if err != nil {
		t.Fatalf("a modified clone must not fail the apply: %v", err)
	}
	if got := moduleResult(t, res, "fzf").Status; got != "applied" {
		t.Fatalf("fzf status = %q, want applied", got)
	}
	if !strings.Contains(s.out.String(), "has local changes") {
		t.Fatalf("output lacks the note:\n%s", s.out.String())
	}
	if anyCallContains(runner.Calls, "fetch") {
		t.Fatalf("a modified clone must not be fetched: %v", runner.Calls)
	}
	if got := s.fallbackRef(t); got != "" {
		t.Fatalf("recorded fallback ref = %q, want it unchanged (empty)", got)
	}
}

func TestApplyDegradesWhenTheFallbackUpdateFails(t *testing.T) {
	runner := &pkgmgr.MockRunner{}
	s := newFzfSandbox(t, runner)
	fetch := "git -C " + s.cloneDir() + " fetch --depth 1 origin v0.1.0"
	runner.Responses = map[string]pkgmgr.MockResponse{fetch: {Err: errors.New("network down")}}
	s.populateClone(t)

	res, err := s.apply(t)
	if !errors.Is(err, engine.ErrDegraded) {
		t.Fatalf("err = %v, want ErrDegraded", err)
	}
	if note := moduleResult(t, res, "fzf").Note; !strings.Contains(note, "fallback update failed") || !strings.Contains(note, "network down") {
		t.Fatalf("note = %q", note)
	}
	if got := s.fallbackRef(t); got != "" {
		t.Fatalf("recorded fallback ref = %q, want it unchanged so the next apply retries", got)
	}
}

func TestDoctorReportsAnOutdatedFallbackClone(t *testing.T) {
	runner := &pkgmgr.MockRunner{}
	s := newFzfSandbox(t, runner)
	if _, err := s.apply(t); err != nil { // fresh clone, records v0.1.0
		t.Fatalf("Apply: %v", err)
	}
	s.populateClone(t)
	lock, _, err := lockfile.Load(s.lockPath)
	if err != nil {
		t.Fatal(err)
	}
	st := lock.Modules["fzf"]
	st.FallbackRef = "v0.0.9"
	lock.Modules["fzf"] = st
	if err := lock.Write(s.lockPath); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(s.cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	rep, err := s.e.Doctor(cfg, s.cfgPath, s.lockPath)
	if err != nil {
		t.Fatal(err)
	}
	codes := map[string]string{}
	for _, f := range rep.Findings {
		codes[f.Code] = f.Severity
	}
	if codes["fallback-outdated:fzf"] != engine.SeverityNotice {
		t.Fatalf("doctor lacks the fallback-outdated notice: %+v", rep.Findings)
	}
	if _, bad := codes["packages-missing:fzf"]; bad {
		t.Fatalf("an outdated clone must not be reported as a missing package: %+v", rep.Findings)
	}
	if _, ok := codes["pending-apply:fzf"]; !ok {
		t.Fatalf("doctor lacks pending-apply for the outdated clone: %+v", rep.Findings)
	}
}
```

`moduleResult` is the helper added in OMNIS-24's apply tests; `applyEngine` and `writeConfig` already exist in this file.

- [ ] **Step 6: Run the engine tests to verify they fail**

Run: `go test ./internal/engine/ -run 'TestApply(RecordsTheFallbackRef|UpdatesAnExisting|KeepsAModified|DegradesWhenTheFallbackUpdate)|TestDoctorReportsAnOutdated' -count=1`
Expected: FAIL (the recorded ref is empty, no `fetch` call, no update line).

- [ ] **Step 7: Implement the engine changes**

In `internal/engine/apply.go` declare the map next to its siblings and pass it on:

```go
	vendorPaths := map[string][]string{}
	fallbackRefs := map[string]string{}
	installedNow := map[string]map[string]bool{}
```

```go
		e.installPackages(plan, degraded, vendorPaths, fallbackRefs, installedNow)
```

```go
	newLock := e.rebuildLock(cfg, plan, lock, degraded, vendorPaths, fallbackRefs, installedNow)
```

In `internal/engine/apply_helpers.go` change the `installPackages` signature and fallback block (add `"errors"` to the imports if absent):

```go
func (e Engine) installPackages(plan Plan, degraded map[string]string,
	vendorPaths map[string][]string, fallbackRefs map[string]string, installedNow map[string]map[string]bool) {
```

Replace the whole `if fallback && mp.UsesFallback && ... { ... }` block with:

```go
		if fallback && mp.UsesFallback && len(mp.Manifest.Packages.Fallback) > 0 {
			fb := mp.Manifest.Packages.Fallback[0]
			ctx := pkgmgr.FallbackContext{VendorDir: e.vendorDir(), Platform: string(e.Platform.OS)}
			if isFallbackUpdate(mp) {
				_, _ = fmt.Fprintf(e.Stdout, "updating %s fallback to %s (rebuilding)\n", id, fb.Ref)
				dest, err := pkgmgr.UpdateGitFallback(fb, ctx, e.Runner)
				if errors.Is(err, pkgmgr.ErrCloneModified) {
					_, _ = fmt.Fprintf(e.Stdout, "%s: fallback clone %s has local changes; keeping it as is\n", id, dest)
					vendorPaths[id] = append(vendorPaths[id], dest)
					continue
				}
				if err != nil {
					degraded[id] = "fallback update failed: " + err.Error()
					continue
				}
				vendorPaths[id] = append(vendorPaths[id], dest)
				fallbackRefs[id] = fb.Ref
				continue
			}
			// UnavailablePackages is only set when a manager was detected.
			if len(mp.UnavailablePackages) > 0 {
				_, _ = fmt.Fprintln(e.Stdout, fallbackNotice(e.Manager.Name(), mp))
			}
			dest, err := pkgmgr.InstallGitFallback(fb, ctx, e.Runner)
			if err != nil {
				degraded[id] = fallbackFailure(e.Manager, mp, err)
				continue
			}
			vendorPaths[id] = append(vendorPaths[id], dest)
			fallbackRefs[id] = fb.Ref
		}
```

Add the helper next to `fallbackNotice`:

```go
// isFallbackUpdate reports whether the module's queued git fallback moves an
// existing clone instead of creating one.
func isFallbackUpdate(mp ModulePlan) bool {
	for _, pp := range mp.MissingPackages {
		if pp.Manager == "git" && pp.Update {
			return true
		}
	}
	return false
}
```

In `rebuildLock` change the signature and record the ref:

```go
func (e Engine) rebuildLock(_ config.Config, plan Plan, prev lockfile.Lock,
	degraded map[string]string, vendorPaths map[string][]string, fallbackRefs map[string]string,
	installedNow map[string]map[string]bool) lockfile.Lock {
```

Inside the module loop, after the `vps` lines, add:

```go
		fallbackRef := prevMod.FallbackRef
		if ref, ok := fallbackRefs[id]; ok {
			fallbackRef = ref
		}
		if !mp.UsesFallback {
			fallbackRef = ""
		}
```

and set `FallbackRef: fallbackRef,` in the `lockfile.ModuleState{...}` literal (next to `VendorPaths: vps`).

- [ ] **Step 8: Run the tests to verify they pass**

Run: `go test ./... -race -count=1 && go vet ./... && gofmt -l .`
Expected: every package `ok`, including all earlier fallback tests; no vet or gofmt output.

A clone with local changes stays planned as an update on every apply (the note repeats) until the user resets or removes it; that is intended, because the update is still pending.

- [ ] **Step 9: Commit**

```bash
git add internal/pkgmgr internal/engine
git commit -m "feat: move an existing fallback clone to the pinned tag during apply"
```

### Task 8: Docs and changelog for the refresh behavior

**Files:**
- Modify: `README.md` (the `vendor/` row of the on-disk layout table)
- Modify: `CHANGELOG.md`
- Modify: `CLAUDE.md` (pkgmgr paragraph)

- [ ] **Step 1: Update the README row**

In `README.md` change the `vendor/` row text `Clones made by the \`git\` package fallback.` to `Clones made by the \`git\` package fallback; \`apply\` moves them to the tag the manifest pins and rebuilds.` (keep the table's column padding aligned).

- [ ] **Step 2: Update the CHANGELOG**

If the OMNIS-26 bullet is still under `## [Unreleased]`, replace its last sentence so the bullet reads:

```markdown
- The built-in modules pin their `git` fallback to a release tag, so a
  fallback build is reproducible. `apply` moves an existing clone in `vendor/`
  to the pinned tag and rebuilds it (the plan lists the update first); a clone
  with local changes is left alone.
```

If OMNIS-26 was already released, add under `## [Unreleased]` instead:

```markdown
### Changed
- `apply` moves an existing `git` fallback clone in `vendor/` to the tag the
  module's manifest pins and rebuilds it. The plan lists the update first, and
  a clone with local changes is left alone. The first `apply` after upgrading
  rebuilds each fallback-built tool once.
```

- [ ] **Step 3: Update CLAUDE.md**

In the `internal/pkgmgr` paragraph, after the sentence about `ref`, add:

```markdown
   The lockfile records the ref a clone was built from (`fallback_ref`);
   `ComputePlan` compares it with the manifest's `ref` (no git probe) and plans
   an update, which `UpdateGitFallback` performs unless the clone has tracked
   local changes.
```

- [ ] **Step 4: Verify and commit**

Run: `go test ./... -race -count=1 && go vet ./... && gofmt -l .`
Expected: all `ok`, no output from vet or gofmt.

```bash
git add README.md CHANGELOG.md CLAUDE.md
git commit -m "docs: describe the fallback clone refresh"
```

- [ ] **Step 5: Phase 2 hand-off**

Do not push on your own; push and open the PR through `/youtrack-task pr`. In the PR description, call out that the first `apply` after upgrading rebuilds every fallback-built tool once, and what a reviewer should run: `omnishell apply` against a sandbox `HOME` with a pre-existing unpinned clone, then a second `apply` (must be a no-op).
