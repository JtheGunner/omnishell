package modedit_test

import (
	"bytes"
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

func TestSetOptionWritesTypedValues(t *testing.T) {
	ed, cfgPath := newEditor(t, true, "bash")

	if err := ed.SetOption("fzf", "ctrl_r", "false"); err != nil {
		t.Fatalf("SetOption bool: %v", err)
	}
	if err := ed.SetOption("fzf", "theme", "light"); err != nil {
		t.Fatalf("SetOption enum: %v", err)
	}
	c, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	opts := c.Modules["fzf"].Options
	if opts["ctrl_r"] != false || opts["theme"] != "light" {
		t.Fatalf("options = %#v", opts)
	}
	if c.Modules["fzf"].Enabled {
		t.Fatal("SetOption must never change enablement")
	}
}

func TestSetOptionUnknownKeyListsValidKeysSorted(t *testing.T) {
	ed, _ := newEditor(t, true, "bash")

	err := ed.SetOption("fzf", "bogus", "x")
	var cfgErr config.Error
	if !errors.As(err, &cfgErr) {
		t.Fatalf("err = %v, want config.Error", err)
	}
	want := `unknown option "bogus" for module "fzf" (valid: ctrl_r, prefix, theme)`
	if cfgErr.Msg != want {
		t.Fatalf("message = %q, want %q", cfgErr.Msg, want)
	}
}

func TestSetOptionUnknownModuleIsConfigError(t *testing.T) {
	ed, _ := newEditor(t, true, "bash")

	err := ed.SetOption("nope", "ctrl_r", "true")
	var cfgErr config.Error
	if !errors.As(err, &cfgErr) || !strings.Contains(cfgErr.Msg, `unknown module "nope"`) {
		t.Fatalf("err = %v, want config.Error naming the unknown module", err)
	}
}

// A rejected value must not touch the file at all.
func TestSetOptionRejectedValueLeavesConfigByteIdentical(t *testing.T) {
	ed, cfgPath := newEditor(t, true, "bash")
	before, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}

	cases := []struct{ key, raw, wantMsg string }{
		{"theme", "purple", "invalid value for fzf.theme"},
		{"prefix", "ABC", "invalid value for fzf.prefix"},
		{"ctrl_r", "maybe", "invalid value for fzf.ctrl_r"},
	}
	for _, tc := range cases {
		err := ed.SetOption("fzf", tc.key, tc.raw)
		var cfgErr config.Error
		if !errors.As(err, &cfgErr) || !strings.Contains(cfgErr.Msg, tc.wantMsg) {
			t.Fatalf("%s=%s: err = %v, want config.Error containing %q", tc.key, tc.raw, err, tc.wantMsg)
		}
	}

	after, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("config changed after rejected values:\n%s", after)
	}
}

func TestSetOptionWithoutConfigReturnsNotFoundAndCreatesNothing(t *testing.T) {
	ed, cfgPath := newEditor(t, false, "bash")

	if err := ed.SetOption("fzf", "ctrl_r", "true"); !errors.Is(err, config.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if _, err := os.Stat(cfgPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("config.toml must not be created, stat err = %v", err)
	}
}
