package pkgmgr

import (
	"fmt"
	"strings"
)

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

func (m cmdManager) Name() string   { return m.name }
func (m cmdManager) NeedsSudo() bool { return m.sudo }
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

// newManager builds the concrete Manager for name, or nil for an unknown name.
func newManager(name string, r Runner) Manager {
	switch name {
	case "brew":
		return cmdManager{
			name: "brew", bin: "brew", sudo: false, runner: r,
			isInstalled: func(r Runner, pkg string) (bool, error) {
				return outputNonEmpty(r, "brew", "list", "--versions", pkg)
			},
			installArgv: func(p []string) []string { return append([]string{"brew", "install"}, p...) },
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
				return append([]string{"sudo", "apt-get", "install", "-y"}, p...)
			},
		}
	case "dnf":
		return cmdManager{
			name: "dnf", bin: "dnf", sudo: true, runner: r,
			isInstalled: func(r Runner, pkg string) (bool, error) { return exitZero(r, "rpm", "-q", pkg) },
			installArgv: func(p []string) []string {
				return append([]string{"sudo", "dnf", "install", "-y"}, p...)
			},
		}
	case "pacman":
		return cmdManager{
			name: "pacman", bin: "pacman", sudo: true, runner: r,
			isInstalled: func(r Runner, pkg string) (bool, error) { return exitZero(r, "pacman", "-Q", pkg) },
			installArgv: func(p []string) []string {
				return append([]string{"sudo", "pacman", "-S", "--noconfirm"}, p...)
			},
		}
	case "zypper":
		return cmdManager{
			name: "zypper", bin: "zypper", sudo: true, runner: r,
			isInstalled: func(r Runner, pkg string) (bool, error) { return exitZero(r, "rpm", "-q", pkg) },
			installArgv: func(p []string) []string {
				return append([]string{"sudo", "zypper", "install", "-y"}, p...)
			},
		}
	case "apk":
		return cmdManager{
			name: "apk", bin: "apk", sudo: true, runner: r,
			isInstalled: func(r Runner, pkg string) (bool, error) {
				return outputNonEmpty(r, "apk", "info", "-e", pkg)
			},
			installArgv: func(p []string) []string { return append([]string{"sudo", "apk", "add"}, p...) },
		}
	default:
		return nil
	}
}
