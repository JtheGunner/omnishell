package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JtheGunner/omnishell/internal/cli"
	"github.com/JtheGunner/omnishell/internal/pkgmgr"
)

func TestInitCreatesConfigAndRCBlock(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	if err := os.WriteFile(filepath.Join(home, ".zshrc"), []byte("export A=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cli.SetLookPathForTest(func(bin string) (string, error) {
		if bin == "zsh" {
			return "/bin/zsh", nil
		}
		return "", os.ErrNotExist
	})
	defer cli.SetLookPathForTest(nil)
	cli.SetRunnerForTest(&pkgmgr.MockRunner{})
	defer cli.SetRunnerForTest(nil)

	var out, errb bytes.Buffer
	code := cli.Execute([]string{"init"}, &out, &errb)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}

	cfg := filepath.Join(home, ".config", "omnishell", "config.toml")
	if _, err := os.Stat(cfg); err != nil {
		t.Fatalf("config.toml not created: %v", err)
	}

	initFile := filepath.Join(home, ".config", "omnishell", "init.zsh")
	if _, err := os.Stat(initFile); err != nil {
		t.Fatalf("init.zsh not created: %v", err)
	}

	rc, _ := os.ReadFile(filepath.Join(home, ".zshrc"))
	if !strings.Contains(string(rc), "# >>> omnishell >>>") {
		t.Fatalf(".zshrc missing block:\n%s", rc)
	}
	if !strings.Contains(string(rc), "$HOME/.config/omnishell/init.zsh") {
		t.Fatalf(".zshrc block does not use $HOME-relative path:\n%s", rc)
	}
	if !strings.Contains(string(rc), "export A=1") {
		t.Fatalf(".zshrc lost pre-existing content:\n%s", rc)
	}

	// Idempotent: a second run changes nothing and still exits 0.
	before, _ := os.ReadFile(filepath.Join(home, ".zshrc"))
	var out2, errb2 bytes.Buffer
	if code := cli.Execute([]string{"init"}, &out2, &errb2); code != 0 {
		t.Fatalf("second init exit %d: %s", code, errb2.String())
	}
	after, _ := os.ReadFile(filepath.Join(home, ".zshrc"))
	if string(before) != string(after) {
		t.Fatalf("second init mutated .zshrc:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	if c := strings.Count(string(after), "# >>> omnishell >>>"); c != 1 {
		t.Fatalf("duplicate marker block: count=%d", c)
	}
}
