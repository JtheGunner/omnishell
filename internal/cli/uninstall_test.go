package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JtheGunner/omnishell/internal/cli"
)

// bashPresentLookPath reports only bash as an installed shell.
func bashPresentLookPath(bin string) (string, error) {
	if bin == "bash" {
		return "/bin/bash", nil
	}
	return "", os.ErrNotExist
}

// TestUninstallCommandRestoresRC checks that `uninstall` strips the omnishell
// marker block from .bashrc (restoring it byte-for-byte) and deletes the
// generated init.bash.
func TestUninstallCommandRestoresRC(t *testing.T) {
	home, _ := setupModuleCLITest(t)
	configDir := filepath.Join(home, ".config", "omnishell")

	rc := filepath.Join(home, ".bashrc")
	if err := os.WriteFile(rc, []byte("export X=1\n"), 0o644); err != nil {
		t.Fatalf("seed .bashrc: %v", err)
	}

	cli.SetLookPathForTest(bashPresentLookPath)

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

	if body, _ := os.ReadFile(rc); !strings.Contains(string(body), "omnishell") {
		t.Fatalf(".bashrc missing omnishell block before uninstall:\n%s", body)
	}

	out.Reset()
	errb.Reset()
	if code := cli.Execute([]string{"uninstall", "--yes"}, &out, &errb); code != 0 {
		t.Fatalf("uninstall exit %d: %s\n%s", code, errb.String(), out.String())
	}

	got, err := os.ReadFile(rc)
	if err != nil {
		t.Fatalf("read .bashrc: %v", err)
	}
	if string(got) != "export X=1\n" {
		t.Fatalf(".bashrc not restored cleanly:\n%q", string(got))
	}
	if _, err := os.Stat(filepath.Join(configDir, "init.bash")); !os.IsNotExist(err) {
		t.Fatalf("init.bash still present after uninstall (err=%v)", err)
	}
}

// TestUninstallPurgeRemovesConfigDir checks that `uninstall --purge` deletes the
// whole omnishell config dir.
func TestUninstallPurgeRemovesConfigDir(t *testing.T) {
	home, _ := setupModuleCLITest(t)
	configDir := filepath.Join(home, ".config", "omnishell")

	if err := os.WriteFile(filepath.Join(home, ".bashrc"), []byte("export X=1\n"), 0o644); err != nil {
		t.Fatalf("seed .bashrc: %v", err)
	}
	cli.SetLookPathForTest(bashPresentLookPath)

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
	if code := cli.Execute([]string{"uninstall", "--yes", "--purge"}, &out, &errb); code != 0 {
		t.Fatalf("uninstall --purge exit %d: %s\n%s", code, errb.String(), out.String())
	}

	if _, err := os.Stat(configDir); !os.IsNotExist(err) {
		t.Fatalf("config dir still present after --purge (err=%v)", err)
	}
}
