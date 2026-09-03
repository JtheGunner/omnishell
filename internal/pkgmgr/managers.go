package pkgmgr

import (
	"fmt"
	"os"
	"strings"
)

// runningAsRoot reports whether the current process's effective uid is 0.
// Overridable in tests. When true, package-manager commands skip the sudo prefix.
var runningAsRoot = os.Geteuid() == 0

// sudoPrefix returns the argv prefix for a privileged command: nil when
// already root, otherwise []string{"sudo"}.
func sudoPrefix() []string {
	if runningAsRoot {
		return nil
	}
	return []string{"sudo"}
}

// cmdManager is a Manager driven by a per-manager command table. It is
// responsible only for building the correct argv (including prepending
// "sudo" where required); announcing sudo to the user is the engine's job.
type cmdManager struct {
	name        string
	bin         string
	sudo        bool
	runner      Runner
	isInstalled func(r Runner, pkg string) (bool, error)
	installArgv func(pkgs []string) []string
}

func (m cmdManager) Name() string    { return m.name }
func (m cmdManager) NeedsSudo() bool { return m.sudo && !runningAsRoot }
func (m cmdManager) Detect() bool    { _, err := m.runner.Look(m.bin); return err == nil }

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

// UninstallArgv returns the argv (sudo-prefixed where the manager requires it
// and the process is not already root) that removes pkgs with the named
// manager, or nil for an unknown manager. It is a pure function: announcing the
// sudo prompt and running the command are the engine's job.
func UninstallArgv(manager string, pkgs []string) []string {
	// The trailing "--" stops a package name that begins with "-" from being
	// parsed as a flag. All six managers accept it.
	switch manager {
	case "brew":
		return append([]string{"brew", "uninstall", "--"}, pkgs...)
	case "apt":
		return append(append(sudoPrefix(), "apt-get", "remove", "-y", "--"), pkgs...)
	case "dnf":
		return append(append(sudoPrefix(), "dnf", "remove", "-y", "--"), pkgs...)
	case "pacman":
		return append(append(sudoPrefix(), "pacman", "-Rs", "--noconfirm", "--"), pkgs...)
	case "zypper":
		return append(append(sudoPrefix(), "zypper", "remove", "-y", "--"), pkgs...)
	case "apk":
		return append(append(sudoPrefix(), "apk", "del", "--"), pkgs...)
	default:
		return nil
	}
}

// newManager builds the concrete Manager for name, or nil for an unknown name.
func newManager(name string, r Runner) Manager {
	switch name {
	case "brew":
		return cmdManager{
			name: "brew", bin: "brew", sudo: false, runner: r,
			isInstalled: func(r Runner, pkg string) (bool, error) {
				return outputNonEmpty(r, "brew", "list", "--versions", pkg)
			},
			installArgv: func(p []string) []string { return append([]string{"brew", "install", "--"}, p...) },
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
				return append(append(sudoPrefix(), "apt-get", "install", "-y", "--"), p...)
			},
		}
	case "dnf":
		return cmdManager{
			name: "dnf", bin: "dnf", sudo: true, runner: r,
			isInstalled: func(r Runner, pkg string) (bool, error) { return exitZero(r, "rpm", "-q", pkg) },
			installArgv: func(p []string) []string {
				return append(append(sudoPrefix(), "dnf", "install", "-y", "--"), p...)
			},
		}
	case "pacman":
		return cmdManager{
			name: "pacman", bin: "pacman", sudo: true, runner: r,
			isInstalled: func(r Runner, pkg string) (bool, error) { return exitZero(r, "pacman", "-Q", pkg) },
			installArgv: func(p []string) []string {
				return append(append(sudoPrefix(), "pacman", "-S", "--noconfirm", "--"), p...)
			},
		}
	case "zypper":
		return cmdManager{
			name: "zypper", bin: "zypper", sudo: true, runner: r,
			isInstalled: func(r Runner, pkg string) (bool, error) { return exitZero(r, "rpm", "-q", pkg) },
			installArgv: func(p []string) []string {
				return append(append(sudoPrefix(), "zypper", "install", "-y", "--"), p...)
			},
		}
	case "apk":
		return cmdManager{
			name: "apk", bin: "apk", sudo: true, runner: r,
			isInstalled: func(r Runner, pkg string) (bool, error) {
				return outputNonEmpty(r, "apk", "info", "-e", pkg)
			},
			installArgv: func(p []string) []string {
				return append(append(sudoPrefix(), "apk", "add", "--"), p...)
			},
		}
	default:
		return nil
	}
}
