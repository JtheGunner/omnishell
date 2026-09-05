package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JtheGunner/omnishell/internal/cli"
)

func TestRollbackListsSnapshotsWithoutTo(t *testing.T) {
	setupModuleCLITest(t)
	cli.SetLookPathForTest(zshPresentLookPath)

	var out, errb bytes.Buffer
	if code := cli.Execute([]string{"init"}, &out, &errb); code != 0 {
		t.Fatalf("init exit %d: %s", code, errb.String())
	}
	if code := cli.Execute([]string{"enable", "completion"}, &out, &errb); code != 0 {
		t.Fatalf("enable exit %d: %s", code, errb.String())
	}
	if code := cli.Execute([]string{"apply", "--yes"}, &out, &errb); code != 0 {
		t.Fatalf("apply exit %d: %s\n%s", code, errb.String(), out.String())
	}

	out.Reset()
	errb.Reset()
	if code := cli.Execute([]string{"rollback"}, &out, &errb); code != 0 {
		t.Fatalf("rollback list exit %d: %s", code, errb.String())
	}
	if !strings.Contains(out.String(), "apply") {
		t.Fatalf("rollback listing missing a snapshot line:\n%s", out.String())
	}
}

func TestRollbackToRestoresPriorState(t *testing.T) {
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
	if code := cli.Execute([]string{"apply", "--yes"}, &out, &errb); code != 0 {
		t.Fatalf("apply exit %d: %s\n%s", code, errb.String(), out.String())
	}

	initFile := filepath.Join(configDir, "init.zsh")
	if _, err := os.Stat(initFile); err != nil {
		t.Fatalf("init.zsh missing before rollback: %v", err)
	}

	out.Reset()
	errb.Reset()
	if code := cli.Execute([]string{"rollback"}, &out, &errb); code != 0 {
		t.Fatalf("rollback list exit %d: %s", code, errb.String())
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) == 0 || lines[0] == "" {
		t.Fatalf("no snapshot listed:\n%s", out.String())
	}
	target := strings.Fields(lines[0])[0]

	out.Reset()
	errb.Reset()
	if code := cli.Execute([]string{"rollback", "--to", target, "--yes"}, &out, &errb); code != 0 {
		t.Fatalf("rollback exit %d: %s\n%s", code, errb.String(), out.String())
	}

	if _, err := os.Stat(initFile); !os.IsNotExist(err) {
		t.Fatalf("init.zsh still exists after rollback: err=%v", err)
	}
}

func TestRollbackUnknownTargetExitsNonZero(t *testing.T) {
	setupModuleCLITest(t)
	cli.SetLookPathForTest(zshPresentLookPath)

	var out, errb bytes.Buffer
	if code := cli.Execute([]string{"init"}, &out, &errb); code != 0 {
		t.Fatalf("init exit %d: %s", code, errb.String())
	}

	out.Reset()
	errb.Reset()
	code := cli.Execute([]string{"rollback", "--to", "20200101T000000Z", "--yes"}, &out, &errb)
	if code != 2 {
		t.Fatalf("rollback unknown target exit = %d, want 2 (stderr: %s)", code, errb.String())
	}
}
