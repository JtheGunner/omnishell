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
