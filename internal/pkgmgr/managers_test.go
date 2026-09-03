package pkgmgr_test

import (
	"strings"
	"testing"

	"github.com/JtheGunner/omnishell/internal/pkgmgr"
)

func TestUninstallArgv(t *testing.T) {
	cases := map[string][]string{
		"brew":   {"brew", "uninstall", "--", "fzf"},
		"apt":    {"sudo", "apt-get", "remove", "-y", "--", "fzf"},
		"dnf":    {"sudo", "dnf", "remove", "-y", "--", "fzf"},
		"pacman": {"sudo", "pacman", "-Rs", "--noconfirm", "--", "fzf"},
		"zypper": {"sudo", "zypper", "remove", "-y", "--", "fzf"},
		"apk":    {"sudo", "apk", "del", "--", "fzf"},
	}
	for mgr, want := range cases {
		got := pkgmgr.UninstallArgv(mgr, []string{"fzf"})
		if strings.Join(got, " ") != strings.Join(want, " ") {
			t.Fatalf("%s: got %v want %v", mgr, got, want)
		}
	}
	if got := pkgmgr.UninstallArgv("unknown", []string{"fzf"}); got != nil {
		t.Fatalf("unknown manager: got %v want nil", got)
	}
	if got := pkgmgr.UninstallArgv("apt", []string{"fzf", "zoxide"}); strings.Join(got, " ") != "sudo apt-get remove -y -- fzf zoxide" {
		t.Fatalf("multi-pkg apt: got %v", got)
	}
}

func TestUninstallArgvAllManagers(t *testing.T) {
	cases := map[string]string{
		"brew":   "brew uninstall -- fzf",
		"apt":    "sudo apt-get remove -y -- fzf",
		"dnf":    "sudo dnf remove -y -- fzf",
		"pacman": "sudo pacman -Rs --noconfirm -- fzf",
		"zypper": "sudo zypper remove -y -- fzf",
		"apk":    "sudo apk del -- fzf",
	}
	for mgr, want := range cases {
		if got := strings.Join(pkgmgr.UninstallArgv(mgr, []string{"fzf"}), " "); got != want {
			t.Fatalf("%s: got %q want %q", mgr, got, want)
		}
	}
}

func TestInstallArgvNoSudoWhenRoot(t *testing.T) {
	restore := pkgmgr.SetRunningAsRootForTest(true)
	defer restore()

	r := &pkgmgr.MockRunner{LookOK: map[string]bool{"apt-get": true}}
	m, ok := pkgmgr.DetectManager("linux", r)
	if !ok || m.Name() != "apt" {
		t.Fatalf("got %v ok=%v, want apt", m, ok)
	}
	if err := m.Install([]string{"fzf"}); err != nil {
		t.Fatalf("Install: %v", err)
	}
	last := r.Calls[len(r.Calls)-1]
	if last != "apt-get install -y -- fzf" {
		t.Fatalf("root apt install argv = %q, want %q", last, "apt-get install -y -- fzf")
	}
	if got := strings.Join(pkgmgr.UninstallArgv("apt", []string{"fzf"}), " "); got != "apt-get remove -y -- fzf" {
		t.Fatalf("root apt uninstall argv = %q, want %q", got, "apt-get remove -y -- fzf")
	}
	if m.NeedsSudo() {
		t.Fatal("apt should not need sudo when running as root")
	}
}

func TestDetectManagerMacOSPrefersBrew(t *testing.T) {
	r := &pkgmgr.MockRunner{LookOK: map[string]bool{"brew": true}}
	m, ok := pkgmgr.DetectManager("darwin", r)
	if !ok || m.Name() != "brew" {
		t.Fatalf("got %v ok=%v, want brew", m, ok)
	}
}

func TestDetectManagerLinuxOrder(t *testing.T) {
	r := &pkgmgr.MockRunner{LookOK: map[string]bool{"dnf": true, "pacman": true}}
	m, ok := pkgmgr.DetectManager("linux", r)
	if !ok || m.Name() != "dnf" {
		t.Fatalf("got %v ok=%v, want dnf (first in order that is present)", m, ok)
	}
}

func TestDetectManagerNoneFound(t *testing.T) {
	r := &pkgmgr.MockRunner{LookOK: map[string]bool{}}
	if _, ok := pkgmgr.DetectManager("linux", r); ok {
		t.Fatal("want ok=false when no manager present")
	}
}

func TestBrewIsInstalledAndInstall(t *testing.T) {
	r := &pkgmgr.MockRunner{
		LookOK: map[string]bool{"brew": true},
		Responses: map[string]pkgmgr.MockResponse{
			"brew list --versions fzf":     {Out: []byte("fzf 0.54.0\n")},
			"brew list --versions ripgrep": {Out: []byte("")},
			"brew install -- fzf":          {Out: []byte("installed")},
		},
	}
	m, ok := pkgmgr.DetectManager("darwin", r)
	if !ok {
		t.Fatal("brew not detected")
	}
	if got, _ := m.IsInstalled("fzf"); !got {
		t.Fatal("fzf should be installed")
	}
	if got, _ := m.IsInstalled("ripgrep"); got {
		t.Fatal("ripgrep should not be installed")
	}
	if err := m.Install([]string{"fzf"}); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if m.NeedsSudo() {
		t.Fatal("brew should not need sudo")
	}
}

func TestAptInstallUsesSudoArgv(t *testing.T) {
	r := &pkgmgr.MockRunner{LookOK: map[string]bool{"apt-get": true}}
	m, ok := pkgmgr.DetectManager("linux", r)
	if !ok || m.Name() != "apt" {
		t.Fatalf("got %v, want apt", m)
	}
	if !m.NeedsSudo() {
		t.Fatal("apt should need sudo")
	}
	if err := m.Install([]string{"fzf", "zoxide"}); err != nil {
		t.Fatalf("Install: %v", err)
	}
	last := r.Calls[len(r.Calls)-1]
	if last != "sudo apt-get install -y -- fzf zoxide" {
		t.Fatalf("apt install argv = %q", last)
	}
}

func TestPacmanIsInstalled(t *testing.T) {
	r := &pkgmgr.MockRunner{
		LookOK:    map[string]bool{"pacman": true},
		Responses: map[string]pkgmgr.MockResponse{"pacman -Q fzf": {Out: []byte("fzf 0.54.0-1\n")}},
	}
	m, _ := pkgmgr.DetectManager("linux", r)
	if got, _ := m.IsInstalled("fzf"); !got {
		t.Fatal("pacman -Q success should mean installed")
	}
}
