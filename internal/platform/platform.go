// Package platform detects the host OS, architecture, config directory,
// and the shells omnishell can manage.
package platform

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
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
	OS   OS
	Arch string
	// GoARM is the 32-bit ARM variant ("6" or "7") the running binary was built
	// for. It is empty off 32-bit ARM and when the build did not record one.
	GoARM     string
	HomeDir   string
	ConfigDir string
	Shells    []ShellInfo
}

// Env holds the runtime/environment dependencies of detection, injectable for tests.
type Env struct {
	GOOS   string
	GOARCH string
	// GoARM is the raw GOARM build setting of the running binary (for example
	// "7" or "7,softfloat"); see GoARMFromBuild.
	GoARM    string
	Getenv   func(string) string
	LookPath func(string) (string, error)
}

// GoARMFromBuild returns the GOARM setting the running binary was built with,
// or "" when the build info does not record one.
func GoARMFromBuild() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, s := range bi.Settings {
		if s.Key == "GOARM" {
			return s.Value
		}
	}
	return ""
}

// goARMVariant normalizes a raw GOARM setting to "6" or "7": the leading digit,
// ignoring a float-ABI suffix such as ",softfloat". Other values are unknown.
func goARMVariant(arch, raw string) string {
	if arch != "arm" {
		return ""
	}
	variant, _, _ := strings.Cut(raw, ",")
	if variant == "6" || variant == "7" {
		return variant
	}
	return ""
}

// Detect inspects the real host.
func Detect() (Info, error) {
	return DetectWith(Env{
		GOOS:     runtime.GOOS,
		GOARCH:   runtime.GOARCH,
		GoARM:    GoARMFromBuild(),
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
		GoARM:     goARMVariant(env.GOARCH, env.GoARM),
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
