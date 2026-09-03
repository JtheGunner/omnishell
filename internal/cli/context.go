package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/JtheGunner/omnishell/internal/engine"
	"github.com/JtheGunner/omnishell/internal/module"
	"github.com/JtheGunner/omnishell/internal/pkgmgr"
	"github.com/JtheGunner/omnishell/internal/platform"
	"github.com/JtheGunner/omnishell/modules"
)

// lookPath is the seam platform detection uses to probe for shells; tests
// override it via SetLookPathForTest.
var lookPath = exec.LookPath

// SetLookPathForTest swaps the exec.LookPath seam. Passing nil restores the
// real implementation.
func SetLookPathForTest(fn func(string) (string, error)) {
	if fn == nil {
		lookPath = exec.LookPath
		return
	}
	lookPath = fn
}

// runnerOverride, when non-nil, replaces the real pkgmgr.ExecRunner in
// buildEngine so list/init flow tests never spawn brew/apt. Pulled forward from
// Task 25.
var runnerOverride pkgmgr.Runner

// SetRunnerForTest sets (or, with nil, clears) the package-manager runner
// override used by buildEngine.
func SetRunnerForTest(r pkgmgr.Runner) { runnerOverride = r }

// promptFn answers interactive y/N questions; tests reassign it.
var promptFn = defaultPrompt

// defaultPrompt reads a single y/N line from stdin, defaulting to no.
func defaultPrompt(question string) bool {
	fmt.Fprintf(os.Stderr, "%s [y/N]: ", question)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	default:
		return false
	}
}

// buildEngine detects the host, loads the module registry (built-in + user),
// detects the package manager, and assembles an engine.Engine. It does NOT load
// config.toml — commands that need it call config.Load(cfgPath) themselves.
func buildEngine(stdout, stderr io.Writer) (e engine.Engine, cfgPath, lockPath string, err error) {
	env := platform.Env{
		GOOS:     runtime.GOOS,
		GOARCH:   runtime.GOARCH,
		Getenv:   os.Getenv,
		LookPath: lookPath,
	}
	info, derr := platform.DetectWith(env)
	if derr != nil {
		return engine.Engine{}, "", "", fmt.Errorf("detect platform: %w", derr)
	}

	cfgPath = filepath.Join(info.ConfigDir, "config.toml")
	lockPath = filepath.Join(info.ConfigDir, "state.lock.json")

	modulesDir := filepath.Join(info.ConfigDir, "modules")
	reg, rerr := module.LoadRegistry(modules.FS(), modulesDir)
	if rerr != nil {
		// A genuinely unreadable modules dir (or a broken built-in) is a
		// configuration-level problem: exit 2, not a bare error. A single
		// malformed USER module no longer reaches here — LoadRegistry skips it.
		return engine.Engine{}, "", "", config.Error{Path: modulesDir, Msg: "load module registry: " + rerr.Error()}
	}

	var runner pkgmgr.Runner
	if runnerOverride != nil {
		runner = runnerOverride
	} else {
		runner = pkgmgr.ExecRunner{Stdout: stdout, Stderr: stderr}
	}
	manager, ok := pkgmgr.DetectManager(runtime.GOOS, runner)

	e = engine.Engine{
		Platform:  info,
		Registry:  reg,
		Manager:   manager,
		ManagerOK: ok,
		Runner:    runner,
		Now:       time.Now,
		Stdout:    stdout,
		Stderr:    stderr,
		Prompt:    promptFn,
	}
	return e, cfgPath, lockPath, nil
}

// homeRelative renders path as "$HOME/..." when it lives under home, so the
// generated rc snippet is portable; otherwise it returns path unchanged.
func homeRelative(path, home string) string {
	if home == "" {
		return path
	}
	if path == home {
		return "$HOME"
	}
	prefix := home + string(filepath.Separator)
	if strings.HasPrefix(path, prefix) {
		return "$HOME/" + filepath.ToSlash(path[len(prefix):])
	}
	return path
}
