package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/JtheGunner/omnishell/internal/config"
)

func writeConfigFile(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadStartupBudget(t *testing.T) {
	c, err := config.Load(writeConfigFile(t, "[omnishell]\nversion = 1\nstartup_budget_ms = 300\n"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Omnishell.StartupBudgetMs != 300 {
		t.Fatalf("StartupBudgetMs = %d, want 300", c.Omnishell.StartupBudgetMs)
	}
}

func TestLoadStartupBudgetDefaultsToZeroWhenAbsent(t *testing.T) {
	c, err := config.Load(writeConfigFile(t, "[omnishell]\nversion = 1\n"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Omnishell.StartupBudgetMs != 0 {
		t.Fatalf("StartupBudgetMs = %d, want 0 (means: use the built-in default)", c.Omnishell.StartupBudgetMs)
	}
}

func TestLoadRejectsNegativeStartupBudget(t *testing.T) {
	_, err := config.Load(writeConfigFile(t, "[omnishell]\nversion = 1\nstartup_budget_ms = -5\n"))
	var cErr config.Error
	if !errors.As(err, &cErr) {
		t.Fatalf("err = %v, want config.Error for a negative budget", err)
	}
}

func TestLoadFullConfig(t *testing.T) {
	c, err := config.Load(filepath.Join("testdata", "full.toml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Omnishell.Version != 1 {
		t.Fatalf("version = %d", c.Omnishell.Version)
	}
	if len(c.Omnishell.Shells) != 2 {
		t.Fatalf("shells = %v", c.Omnishell.Shells)
	}
	if !c.Modules["completion"].Enabled {
		t.Fatal("completion should be enabled")
	}
	if c.Modules["history"].Enabled {
		t.Fatal("history should be disabled")
	}
	if got := c.Modules["fzf"].Options["ctrl_r"]; got != true {
		t.Fatalf("fzf.ctrl_r = %v (%T), want true", got, got)
	}
	if got := c.Modules["fzf"].Options["default_opts"]; got != "--height 40% --reverse" {
		t.Fatalf("fzf.default_opts = %v", got)
	}
}

func TestLoadMissingFileReturnsSentinel(t *testing.T) {
	_, err := config.Load(filepath.Join(t.TempDir(), "nope.toml"))
	if !errors.Is(err, config.ErrNotFound) {
		t.Fatalf("err = %v, want wrapping ErrNotFound", err)
	}
}

func TestLoadRejectsUnknownTopLevelKey(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "c.toml")
	if err := writeFile(p, "[omnishel]\nversion = 1\n"); err != nil {
		t.Fatal(err)
	}
	_, err := config.Load(p)
	var cerr config.Error
	if !errors.As(err, &cerr) {
		t.Fatalf("err = %v, want config.Error", err)
	}
}

func TestLoadRejectsUnknownSchemaVersion(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "c.toml")
	if err := writeFile(p, "[omnishell]\nversion = 99\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(p); err == nil {
		t.Fatal("want error for version 99")
	}
}

func TestDefault(t *testing.T) {
	d := config.Default()
	if d.Omnishell.Version != config.SchemaVersion {
		t.Fatalf("default version = %d", d.Omnishell.Version)
	}
	if d.Modules == nil {
		t.Fatal("default Modules map is nil")
	}
}
