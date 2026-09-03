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

// FallbackSatisfied reports whether the fallback destination already exists as a
// non-empty directory. It has no side effects and also returns the resolved dest.
func FallbackSatisfied(fb module.Fallback, ctx FallbackContext) (bool, string) {
	dest, err := renderPath(fb.Dest, ctx)
	if err != nil {
		return false, ""
	}
	return populatedDir(dest), dest
}

// InstallGitFallback clones fb.Repo into the rendered fb.Dest and, if set, runs
// fb.Run. A populated dest short-circuits without cloning. It returns the
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
	if _, err := r.Run("git", "clone", "--depth", "1", fb.Repo, dest); err != nil {
		return "", fmt.Errorf("git clone %s: %w", fb.Repo, err)
	}
	if len(fb.Run) > 0 {
		argv := make([]string, 0, len(fb.Run))
		for _, part := range fb.Run {
			rp, err := renderPath(part, ctx)
			if err != nil {
				return "", fmt.Errorf("render fallback run: %w", err)
			}
			argv = append(argv, rp)
		}
		if len(argv) == 0 || argv[0] == "" {
			return dest, nil
		}
		if _, err := r.Run(argv[0], argv[1:]...); err != nil {
			os.RemoveAll(dest)
			return "", fmt.Errorf("fallback run %q: %w", strings.Join(argv, " "), err)
		}
	}
	return dest, nil
}
