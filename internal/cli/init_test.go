package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JtheGunner/omnishell/internal/backup"
	"github.com/JtheGunner/omnishell/internal/cli"
	"github.com/JtheGunner/omnishell/internal/pkgmgr"
)

// initTestShell wires the test doubles init needs: only zsh is present, and
// package-manager detection is stubbed.
func initTestShell(t *testing.T) {
	t.Helper()
	cli.SetLookPathForTest(func(bin string) (string, error) {
		if bin == "zsh" {
			return "/bin/zsh", nil
		}
		return "", os.ErrNotExist
	})
	t.Cleanup(func() { cli.SetLookPathForTest(nil) })
	cli.SetRunnerForTest(&pkgmgr.MockRunner{})
	t.Cleanup(func() { cli.SetRunnerForTest(nil) })
}

func TestInitWritesBackupManifest(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	rcPath := filepath.Join(home, ".zshrc")
	if err := os.WriteFile(rcPath, []byte("export A=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	initTestShell(t)

	var out, errb bytes.Buffer
	if code := cli.Execute([]string{"init"}, &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}

	backupsDir := filepath.Join(home, ".config", "omnishell", "backups")
	entries, err := os.ReadDir(backupsDir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("backups dir: entries=%v err=%v, want exactly one session", entries, err)
	}
	m, err := backup.ReadManifest(filepath.Join(backupsDir, entries[0].Name()))
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	if m.Kind != "init" {
		t.Fatalf("manifest kind = %q, want \"init\"", m.Kind)
	}
	rcRecorded := false
	for _, f := range m.Files {
		if f.OriginalPath == rcPath && f.ExistedBefore && f.BackupName != "" {
			rcRecorded = true
		}
	}
	if !rcRecorded {
		t.Fatalf("manifest does not record %s as existed-before with a backup copy: %+v", rcPath, m.Files)
	}

	// A second run hooks nothing new, so it must not create a fresh snapshot.
	var out2, errb2 bytes.Buffer
	if code := cli.Execute([]string{"init"}, &out2, &errb2); code != 0 {
		t.Fatalf("second init exit %d: %s", code, errb2.String())
	}
	entries2, _ := os.ReadDir(backupsDir)
	if len(entries2) != 1 {
		t.Fatalf("second init created a new snapshot: %d dirs, want 1", len(entries2))
	}
}

func TestInitSnapshotListedByRollback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	if err := os.WriteFile(filepath.Join(home, ".zshrc"), []byte("export A=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	initTestShell(t)

	var out, errb bytes.Buffer
	if code := cli.Execute([]string{"init"}, &out, &errb); code != 0 {
		t.Fatalf("init exit %d: %s", code, errb.String())
	}
	var rout, rerr bytes.Buffer
	if code := cli.Execute([]string{"rollback"}, &rout, &rerr); code != 0 {
		t.Fatalf("rollback list exit %d: %s", code, rerr.String())
	}
	if !strings.Contains(rout.String(), "  init  ") {
		t.Fatalf("rollback listing does not show the init snapshot:\n%s", rout.String())
	}
}

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

	// init must NOT create the init file: apply owns it. A zero-byte file here
	// would trip apply's hand-edit guard on the first ever apply.
	initFile := filepath.Join(home, ".config", "omnishell", "init.zsh")
	if _, err := os.Stat(initFile); !os.IsNotExist(err) {
		t.Fatalf("init.zsh should not exist after init (err=%v)", err)
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
