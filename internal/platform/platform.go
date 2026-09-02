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
