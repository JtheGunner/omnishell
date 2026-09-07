package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JtheGunner/omnishell/internal/cli"
)

// zshPresentLookPath reports only zsh as an installed shell.
func zshPresentLookPath(bin string) (string, error) {
	if bin == "zsh" {
		return "/bin/zsh", nil
	}
	return "", os.ErrNotExist
}

func TestApplyCommandEndToEnd(t *testing.T) {
	home, _ := setupModuleCLITest(t)
	configDir := filepath.Join(home, ".config", "omnishell")

	// zsh is installed from the start — the natural flow. init hooks .zshrc but
	// does NOT create init.zsh; apply creates it on the first ever run.
	cli.SetLookPathForTest(zshPresentLookPath)

	var out, errb bytes.Buffer
	if code := cli.Execute([]string{"init"}, &out, &errb); code != 0 {
		t.Fatalf("init exit %d: %s", code, errb.String())
	}
	if _, err := os.Stat(filepath.Join(configDir, "init.zsh")); !os.IsNotExist(err) {
		t.Fatalf("init created init.zsh (err=%v)", err)
	}
	if code := cli.Execute([]string{"enable", "completion"}, &out, &errb); code != 0 {
		t.Fatalf("enable exit %d: %s", code, errb.String())
	}

	out.Reset()
	errb.Reset()
	code := cli.Execute([]string{"apply", "--yes"}, &out, &errb)
	if code != 0 {
		t.Fatalf("apply exit %d: %s\n%s", code, errb.String(), out.String())
	}

	initFile := filepath.Join(configDir, "init.zsh")
	body, err := os.ReadFile(initFile)
	if err != nil {
		t.Fatalf("init.zsh missing: %v", err)
	}
	if !strings.Contains(string(body), "# >>> omnishell:completion") {
		t.Fatalf("init.zsh has no completion section:\n%s", body)
	}

	rc, _ := os.ReadFile(filepath.Join(home, ".zshrc"))
	if !strings.Contains(string(rc), "# >>> omnishell >>>") {
		t.Fatalf(".zshrc missing omnishell block:\n%s", rc)
	}
}

func TestApplyDryRunExitsZeroAndWritesNothing(t *testing.T) {
	home, _ := setupModuleCLITest(t)
	configDir := filepath.Join(home, ".config", "omnishell")
	cli.SetLookPathForTest(zshPresentLookPath)

	var out, errb bytes.Buffer
	if code := cli.Execute([]string{"init"}, &out, &errb); code != 0 {
		t.Fatalf("init exit %d: %s", code, errb.String())
	}
	if code := cli.Execute([]string{"enable", "completion"}, &out, &errb); code != 0 {
		t.Fatalf("enable exit %d: %s", code, errb.String())
	}

	out.Reset()
	errb.Reset()
	if code := cli.Execute([]string{"apply", "--dry-run"}, &out, &errb); code != 0 {
		t.Fatalf("apply --dry-run exit %d: %s", code, errb.String())
	}
	if !strings.Contains(out.String(), "Plan (") {
		t.Fatalf("dry-run stdout missing plan header:\n%s", out.String())
	}
	if _, err := os.Stat(filepath.Join(configDir, "init.zsh")); !os.IsNotExist(err) {
		t.Fatalf("dry-run created init.zsh (err=%v)", err)
	}
}

func TestApplyWithoutInitExitsTwo(t *testing.T) {
	setupModuleCLITest(t)

	var out, errb bytes.Buffer
	code := cli.Execute([]string{"apply", "--yes"}, &out, &errb)
	if code != 2 {
		t.Fatalf("apply without init exit %d, want 2 (stderr: %s)", code, errb.String())
	}
	if !strings.Contains(errb.String(), "init") {
		t.Fatalf("stderr should mention init, got: %s", errb.String())
	}
}

// applyReloadSetup runs init + enable so a following `apply` succeeds, and
// returns with zsh reported as the only installed shell.
func applyReloadSetup(t *testing.T) {
	t.Helper()
	setupModuleCLITest(t)
	cli.SetLookPathForTest(zshPresentLookPath)
	var out, errb bytes.Buffer
	if code := cli.Execute([]string{"init"}, &out, &errb); code != 0 {
		t.Fatalf("init exit %d: %s", code, errb.String())
	}
	if code := cli.Execute([]string{"enable", "completion"}, &out, &errb); code != 0 {
		t.Fatalf("enable exit %d: %s", code, errb.String())
	}
}

func TestApplyReloadExecsShellWhenInteractive(t *testing.T) {
	applyReloadSetup(t)
	t.Setenv("SHELL", "/bin/zsh")
	var got string
	cli.SetReloadForTest(func() bool { return true }, func(shell string) error { got = shell; return nil })
	defer cli.SetReloadForTest(nil, nil)

	var out, errb bytes.Buffer
	if code := cli.Execute([]string{"apply", "--yes", "--reload"}, &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if got != "/bin/zsh" {
		t.Fatalf("reloadExec called with %q, want /bin/zsh", got)
	}
	if !strings.Contains(out.String(), "reloading /bin/zsh") {
		t.Fatalf("output missing reload line:\n%s", out.String())
	}
}

func TestApplyReloadNoopWhenNotInteractive(t *testing.T) {
	applyReloadSetup(t)
	t.Setenv("SHELL", "/bin/zsh")
	called := false
	cli.SetReloadForTest(func() bool { return false }, func(string) error { called = true; return nil })
	defer cli.SetReloadForTest(nil, nil)

	var out, errb bytes.Buffer
	if code := cli.Execute([]string{"apply", "--yes", "--reload"}, &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if called {
		t.Fatal("reloadExec called in a non-interactive shell")
	}
	if !strings.Contains(out.String(), "not an interactive shell") {
		t.Fatalf("output missing skip hint:\n%s", out.String())
	}
}

func TestApplyReloadSkippedOnDryRun(t *testing.T) {
	applyReloadSetup(t)
	t.Setenv("SHELL", "/bin/zsh")
	called := false
	cli.SetReloadForTest(func() bool { return true }, func(string) error { called = true; return nil })
	defer cli.SetReloadForTest(nil, nil)

	var out, errb bytes.Buffer
	if code := cli.Execute([]string{"apply", "--reload", "--dry-run"}, &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if called {
		t.Fatal("reloadExec called on a dry run")
	}
}

func TestApplyWithoutReloadFlagDoesNotReload(t *testing.T) {
	applyReloadSetup(t)
	t.Setenv("SHELL", "/bin/zsh")
	called := false
	cli.SetReloadForTest(func() bool { return true }, func(string) error { called = true; return nil })
	defer cli.SetReloadForTest(nil, nil)

	var out, errb bytes.Buffer
	if code := cli.Execute([]string{"apply", "--yes"}, &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if called {
		t.Fatal("reloadExec called without --reload")
	}
}

func TestDiffRejectsReloadFlag(t *testing.T) {
	setupModuleCLITest(t)
	cli.SetLookPathForTest(zshPresentLookPath)
	var out, errb bytes.Buffer
	if code := cli.Execute([]string{"diff", "--reload"}, &out, &errb); code == 0 {
		t.Fatalf("diff --reload should fail with a flag error, got exit 0")
	}
}

func TestDiffIsApplyDryRun(t *testing.T) {
	setupModuleCLITest(t)
	cli.SetLookPathForTest(zshPresentLookPath)

	var out, errb bytes.Buffer
	if code := cli.Execute([]string{"init"}, &out, &errb); code != 0 {
		t.Fatalf("init exit %d: %s", code, errb.String())
	}
	if code := cli.Execute([]string{"enable", "completion"}, &out, &errb); code != 0 {
		t.Fatalf("enable exit %d: %s", code, errb.String())
	}

	out.Reset()
	errb.Reset()
	if code := cli.Execute([]string{"diff"}, &out, &errb); code != 0 {
		t.Fatalf("diff exit %d: %s", code, errb.String())
	}
	if !strings.Contains(out.String(), "Plan (") {
		t.Fatalf("diff stdout missing plan header:\n%s", out.String())
	}
}
