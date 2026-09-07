package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"
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

// defaultReloadInteractive reports whether stdin is a terminal, so
// `apply --reload` only re-execs the shell in an interactive session.
func defaultReloadInteractive() bool {
	fi, err := os.Stdin.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// defaultReloadExec replaces the current process with a fresh login shell. On
// success it never returns.
func defaultReloadExec(shell string) error {
	return syscall.Exec(shell, []string{shell}, os.Environ())
}

// reloadInteractive / reloadExec are the `apply --reload` seams; tests swap
// them via SetReloadForTest.
var (
	reloadInteractive = defaultReloadInteractive
	reloadExec        = defaultReloadExec
)

// SetReloadForTest swaps the `apply --reload` seams. A nil argument restores
// that seam's real implementation.
func SetReloadForTest(interactive func() bool, execFn func(string) error) {
	if interactive == nil {
		reloadInteractive = defaultReloadInteractive
	} else {
		reloadInteractive = interactive
	}
	if execFn == nil {
		reloadExec = defaultReloadExec
	} else {
		reloadExec = execFn
	}
}

// defaultBenchRun times `shell -c :` (baseline) and `shell -c '. <initPath>'`
// (sourced), each `runs` times, and returns the median of each. The added cost
// is (sourced - baseline). Tests override it via SetBenchForTest.
func defaultBenchRun(shellPath, initPath string, runs int) (baseline, sourced time.Duration, err error) {
	median := func(script string) (time.Duration, error) {
		samples := make([]time.Duration, 0, runs)
		for i := 0; i < runs+1; i++ { // one warm-up run, discarded
			start := time.Now()
			cmd := exec.Command(shellPath, "-c", script)
			cmd.Stdout, cmd.Stderr = nil, nil
			if rerr := cmd.Run(); rerr != nil {
				return 0, fmt.Errorf("%s -c %q: %w", shellPath, script, rerr)
			}
			if i > 0 {
				samples = append(samples, time.Since(start))
			}
		}
		sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
		return samples[len(samples)/2], nil
	}
	if baseline, err = median(":"); err != nil {
		return 0, 0, err
	}
	if sourced, err = median(". " + shellSingleQuote(initPath)); err != nil {
		return 0, 0, err
	}
	return baseline, sourced, nil
}

// shellSingleQuote wraps s in single quotes for safe use in a shell command.
func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// benchRun is the `omnishell bench` timing seam; tests swap it via
// SetBenchForTest.
var benchRun = defaultBenchRun

// SetBenchForTest swaps the bench timing seam. Passing nil restores the real
// implementation.
func SetBenchForTest(fn func(shellPath, initPath string, runs int) (time.Duration, time.Duration, error)) {
	if fn == nil {
		benchRun = defaultBenchRun
		return
	}
	benchRun = fn
}

// promptFn answers interactive y/N questions; tests reassign it.
var promptFn = defaultPrompt

// defaultPrompt reads a single y/N line from stdin, defaulting to no.
func defaultPrompt(question string) bool {
	_, _ = fmt.Fprintf(os.Stderr, "%s [y/N]: ", question)
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
// generated rc snippet is portable; otherwise it returns path unchanged. It
// delegates to engine.HomeRelative so `init` and `apply` never disagree.
func homeRelative(path, home string) string {
	return engine.HomeRelative(path, home)
}
