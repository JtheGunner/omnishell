package pkgmgr_test

import (
	"testing"

	"github.com/JtheGunner/omnishell/internal/pkgmgr"
)

func TestMockManagerInstallRecords(t *testing.T) {
	m := &pkgmgr.MockManager{NameV: "brew", DetectV: true, Installed: map[string]bool{}}
	if err := m.Install([]string{"fzf", "zoxide"}); err != nil {
		t.Fatal(err)
	}
	ok, _ := m.IsInstalled("fzf")
	if !ok || len(m.InstallCalls) != 1 {
		t.Fatalf("install not recorded: %+v", m.InstallCalls)
	}
	if got := m.InstallCalls[0]; len(got) != 2 || got[0] != "fzf" || got[1] != "zoxide" {
		t.Fatalf("install call args not captured: %+v", got)
	}
}

func TestMockManagerInstallErr(t *testing.T) {
	sentinel := errBoom{}
	m := &pkgmgr.MockManager{NameV: "apt", InstallErr: sentinel}
	if err := m.Install([]string{"fzf"}); err != sentinel {
		t.Fatalf("want sentinel error, got %v", err)
	}
	if ok, _ := m.IsInstalled("fzf"); ok {
		t.Fatal("pkg should not be marked installed when InstallErr is set")
	}
	if len(m.InstallCalls) != 1 {
		t.Fatalf("call should still be recorded: %+v", m.InstallCalls)
	}
}

func TestMockManagerAccessors(t *testing.T) {
	m := &pkgmgr.MockManager{NameV: "pacman", DetectV: true, SudoV: true}
	if m.Name() != "pacman" || !m.Detect() || !m.NeedsSudo() {
		t.Fatalf("accessors wrong: name=%q detect=%v sudo=%v", m.Name(), m.Detect(), m.NeedsSudo())
	}
}

func TestMockRunnerLook(t *testing.T) {
	r := &pkgmgr.MockRunner{LookOK: map[string]bool{"brew": true}}
	if path, err := r.Look("brew"); err != nil || path != "/usr/bin/brew" {
		t.Fatalf("Look(brew) = %q, %v; want /usr/bin/brew, nil", path, err)
	}
	if _, err := r.Look("apt"); err == nil {
		t.Fatal("Look(apt) should error when not in LookOK")
	}
}

func TestMockRunnerRunRecordsAndResponds(t *testing.T) {
	r := &pkgmgr.MockRunner{
		Responses: map[string]pkgmgr.MockResponse{
			"brew list fzf": {Out: []byte("fzf 0.44.1")},
		},
	}
	out, err := r.Run("brew", "list", "fzf")
	if err != nil || string(out) != "fzf 0.44.1" {
		t.Fatalf("Run = %q, %v; want canned response", out, err)
	}
	if out, err := r.Run("brew", "list", "missing"); err != nil || out != nil {
		t.Fatalf("unknown key should yield nil, nil; got %q, %v", out, err)
	}
	if len(r.Calls) != 2 || r.Calls[0] != "brew list fzf" || r.Calls[1] != "brew list missing" {
		t.Fatalf("calls not recorded: %+v", r.Calls)
	}
}

type errBoom struct{}

func (errBoom) Error() string { return "boom" }
