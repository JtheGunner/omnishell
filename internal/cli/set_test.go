package cli_test

import (
	"bytes"
	"testing"

	"github.com/JtheGunner/omnishell/internal/cli"
	"github.com/JtheGunner/omnishell/internal/config"
)

func setTestInit(t *testing.T) (cfgPath string) {
	t.Helper()
	var out, errb bytes.Buffer
	home, cfgPath := setupModuleCLITest(t)
	_ = home
	if code := cli.Execute([]string{"init"}, &out, &errb); code != 0 {
		t.Fatalf("init exit %d: %s", code, errb.String())
	}
	return cfgPath
}

func TestSetBoolOptionReflectedInConfig(t *testing.T) {
	cfgPath := setTestInit(t)

	var out, errb bytes.Buffer
	if code := cli.Execute([]string{"set", "fzf.ctrl_r", "false"}, &out, &errb); code != 0 {
		t.Fatalf("set exit %d: %s", code, errb.String())
	}

	c, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := c.Modules["fzf"].Options["ctrl_r"]; got != false {
		t.Fatalf("ctrl_r = %#v, want false", got)
	}
}

func TestSetUnknownKeyExitsTwo(t *testing.T) {
	setTestInit(t)

	var out, errb bytes.Buffer
	code := cli.Execute([]string{"set", "fzf.bogus", "x"}, &out, &errb)
	if code != 2 {
		t.Fatalf("exit = %d, want 2 (stderr: %s)", code, errb.String())
	}
	if out.Len() != 0 {
		t.Fatalf("nothing to stdout on error, got: %s", out.String())
	}
}

func TestSetEnumValueNotInValuesExitsTwo(t *testing.T) {
	setTestInit(t)

	var out, errb bytes.Buffer
	code := cli.Execute([]string{"set", "modern-aliases.replace", "ls,grep"}, &out, &errb)
	if code != 2 {
		t.Fatalf("exit = %d, want 2 (stderr: %s)", code, errb.String())
	}
	if out.Len() != 0 {
		t.Fatalf("nothing to stdout on error, got: %s", out.String())
	}
}

func TestSetWithoutDotExitsNonZero(t *testing.T) {
	setTestInit(t)

	var out, errb bytes.Buffer
	if code := cli.Execute([]string{"set", "fzf"}, &out, &errb); code == 0 {
		t.Fatalf("expected non-zero exit for missing <key> / arg")
	}
}

func TestSetUnknownModuleExitsTwo(t *testing.T) {
	setTestInit(t)

	var out, errb bytes.Buffer
	code := cli.Execute([]string{"set", "nope.key", "v"}, &out, &errb)
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
}

func TestSetWithoutInitExitsTwo(t *testing.T) {
	setupModuleCLITest(t)

	var out, errb bytes.Buffer
	code := cli.Execute([]string{"set", "fzf.ctrl_r", "false"}, &out, &errb)
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
}
