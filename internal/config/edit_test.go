package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JtheGunner/omnishell/internal/config"
)

func TestSetEnabledInExistingTablePreservesComments(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	initial := `# my omnishell config
[omnishell]
version = 1

[modules.fzf]
# keep this comment
enabled = false
[modules.fzf.options]
ctrl_r = true
`
	if err := os.WriteFile(p, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := config.SetEnabled(p, "fzf", true); err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}
	got, _ := os.ReadFile(p)
	s := string(got)
	if !strings.Contains(s, "# keep this comment") {
		t.Fatal("comment was lost")
	}
	if !strings.Contains(s, "enabled = true") {
		t.Fatalf("enabled not updated:\n%s", s)
	}
	if strings.Contains(s, "enabled = false") {
		t.Fatalf("old value still present:\n%s", s)
	}
	// Re-parse to prove it is still valid TOML.
	if _, err := config.Load(p); err != nil {
		t.Fatalf("result no longer parses: %v", err)
	}
}

func TestSetEnabledCreatesTableWhenMissing(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte("[omnishell]\nversion = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := config.SetEnabled(p, "zoxide", true); err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}
	c, err := config.Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !c.Modules["zoxide"].Enabled {
		t.Fatal("zoxide not enabled after SetEnabled")
	}
}

func TestSetEnabledCreatesFileFromDefault(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "config.toml")
	if err := config.SetEnabled(p, "history", true); err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}
	c, err := config.Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Omnishell.Version != config.SchemaVersion || !c.Modules["history"].Enabled {
		t.Fatalf("unexpected config: %+v", c)
	}
}

func TestSetOptionDoesNotEnableAbsentModule(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte("[omnishell]\nversion = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// fzf is not present in the config at all.
	if err := config.SetOption(p, "fzf", "ctrl_r", false); err != nil {
		t.Fatalf("SetOption: %v", err)
	}
	c, err := config.Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Modules["fzf"].Enabled {
		t.Fatal("set enabled a module it should not have")
	}
	if c.Modules["fzf"].Options["ctrl_r"] != false {
		t.Fatalf("ctrl_r = %v, want false", c.Modules["fzf"].Options["ctrl_r"])
	}
}

func TestSetOptionRendersLiterals(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte("[omnishell]\nversion = 1\n[modules.fzf]\nenabled = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := config.SetOption(p, "fzf", "default_opts", "--height 40%"); err != nil {
		t.Fatalf("SetOption string: %v", err)
	}
	if err := config.SetOption(p, "fzf", "ctrl_r", true); err != nil {
		t.Fatalf("SetOption bool: %v", err)
	}
	if err := config.SetOption(p, "history", "size", int64(50000)); err != nil {
		t.Fatalf("SetOption int: %v", err)
	}
	if err := config.SetOption(p, "modern-aliases", "replace", []string{"ls", "cat"}); err != nil {
		t.Fatalf("SetOption list: %v", err)
	}
	c, err := config.Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Modules["fzf"].Options["default_opts"] != "--height 40%" {
		t.Fatalf("default_opts = %v", c.Modules["fzf"].Options["default_opts"])
	}
	if c.Modules["fzf"].Options["ctrl_r"] != true {
		t.Fatalf("ctrl_r = %v", c.Modules["fzf"].Options["ctrl_r"])
	}
}
