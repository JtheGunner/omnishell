package modedit_test

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/JtheGunner/omnishell/internal/config"
)

func TestEnableThenDisableUpdatesConfig(t *testing.T) {
	ed, cfgPath := newEditor(t, true, "bash")

	if err := ed.Enable("fzf"); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	c, err := config.Load(cfgPath)
	if err != nil || !c.Modules["fzf"].Enabled {
		t.Fatalf("fzf should be enabled: %+v err=%v", c.Modules, err)
	}

	if err := ed.Disable("fzf"); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	c, err = config.Load(cfgPath)
	if err != nil || c.Modules["fzf"].Enabled {
		t.Fatalf("fzf should be disabled: %+v err=%v", c.Modules, err)
	}
}

func TestEnablePreservesComments(t *testing.T) {
	ed, cfgPath := newEditor(t, true, "bash")

	if err := ed.Enable("fzf"); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	src, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(src), "# Edit this file by hand") {
		t.Fatalf("header comment lost:\n%s", src)
	}
}

func TestEnableUnknownModuleIsConfigError(t *testing.T) {
	ed, _ := newEditor(t, true, "bash")

	err := ed.Enable("nope")
	var cfgErr config.Error
	if !errors.As(err, &cfgErr) || !strings.Contains(cfgErr.Msg, `unknown module "nope"`) {
		t.Fatalf("err = %v, want config.Error naming the unknown module", err)
	}
}

func TestEnableRejectsModuleIncompatibleWithManagedShells(t *testing.T) {
	ed, cfgPath := newEditor(t, true, "bash") // only bash is present

	err := ed.Enable("zshonly")
	var cfgErr config.Error
	if !errors.As(err, &cfgErr) {
		t.Fatalf("err = %v, want config.Error", err)
	}
	for _, want := range []string{"zshonly", "zsh", "none of your managed shells"} {
		if !strings.Contains(cfgErr.Msg, want) {
			t.Fatalf("message %q should contain %q", cfgErr.Msg, want)
		}
	}
	c, err := config.Load(cfgPath)
	if err != nil || c.Modules["zshonly"].Enabled {
		t.Fatalf("zshonly must not be enabled: %+v err=%v", c.Modules, err)
	}
}

func TestEnableAllowsModuleWhenItsShellIsManaged(t *testing.T) {
	ed, cfgPath := newEditor(t, true, "zsh")

	if err := ed.Enable("zshonly"); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	c, err := config.Load(cfgPath)
	if err != nil || !c.Modules["zshonly"].Enabled {
		t.Fatalf("zshonly should be enabled: %+v err=%v", c.Modules, err)
	}
}

// Only enabling is shell-checked: a user must always be able to switch an
// incompatible module off.
func TestDisableSkipsShellCompatibilityCheck(t *testing.T) {
	ed, cfgPath := newEditor(t, true, "bash")

	if err := ed.Disable("zshonly"); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	c, err := config.Load(cfgPath)
	if err != nil || c.Modules["zshonly"].Enabled {
		t.Fatalf("zshonly should be disabled: %+v err=%v", c.Modules, err)
	}
}

func TestEnableAndDisableWithoutConfigReturnNotFoundAndCreateNothing(t *testing.T) {
	ed, cfgPath := newEditor(t, false, "bash")

	for name, fn := range map[string]func(string) error{"Enable": ed.Enable, "Disable": ed.Disable} {
		if err := fn("fzf"); !errors.Is(err, config.ErrNotFound) {
			t.Fatalf("%s err = %v, want ErrNotFound", name, err)
		}
	}
	if _, err := os.Stat(cfgPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("config.toml must not be created, stat err = %v", err)
	}
}
