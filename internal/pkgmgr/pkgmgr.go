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

// Run executes name+args, returning the captured stdout.
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
