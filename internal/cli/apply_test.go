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

	var out, errb bytes.Buffer
	// init while no shell is detected: config.toml only, no init files yet.
	if code := cli.Execute([]string{"init"}, &out, &errb); code != 0 {
		t.Fatalf("init exit %d: %s", code, errb.String())
	}
	if code := cli.Execute([]string{"enable", "completion"}, &out, &errb); code != 0 {
		t.Fatalf("enable exit %d: %s", code, errb.String())
	}

	// Now zsh is on PATH; apply must render and hook it.
	cli.SetLookPathForTest(zshPresentLookPath)

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

	var out, errb bytes.Buffer
	if code := cli.Execute([]string{"init"}, &out, &errb); code != 0 {
		t.Fatalf("init exit %d: %s", code, errb.String())
	}
	if code := cli.Execute([]string{"enable", "completion"}, &out, &errb); code != 0 {
		t.Fatalf("enable exit %d: %s", code, errb.String())
	}

	cli.SetLookPathForTest(zshPresentLookPath)

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

func TestDiffIsApplyDryRun(t *testing.T) {
	setupModuleCLITest(t)

	var out, errb bytes.Buffer
	if code := cli.Execute([]string{"init"}, &out, &errb); code != 0 {
		t.Fatalf("init exit %d: %s", code, errb.String())
	}
	if code := cli.Execute([]string{"enable", "completion"}, &out, &errb); code != 0 {
		t.Fatalf("enable exit %d: %s", code, errb.String())
	}
	cli.SetLookPathForTest(zshPresentLookPath)

	out.Reset()
	errb.Reset()
	if code := cli.Execute([]string{"diff"}, &out, &errb); code != 0 {
		t.Fatalf("diff exit %d: %s", code, errb.String())
	}
	if !strings.Contains(out.String(), "Plan (") {
		t.Fatalf("diff stdout missing plan header:\n%s", out.String())
	}
}
