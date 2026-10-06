package pkgmgr

import (
	"errors"
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

// FallbackSatisfied reports whether the fallback destination already exists as a
// non-empty directory. It has no side effects and also returns the resolved dest.
func FallbackSatisfied(fb module.Fallback, ctx FallbackContext) (bool, string) {
	dest, err := renderPath(fb.Dest, ctx)
	if err != nil {
		return false, ""
	}
	return populatedDir(dest), dest
}

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
