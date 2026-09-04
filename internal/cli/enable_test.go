package cli_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/JtheGunner/omnishell/internal/cli"
	"github.com/JtheGunner/omnishell/internal/config"
)

func TestEnableUnknownModuleExitsTwo(t *testing.T) {
	_, cfgPath := setupModuleCLITest(t)

	var out, errb bytes.Buffer
	if code := cli.Execute([]string{"init"}, &out, &errb); code != 0 {
		t.Fatalf("init exit %d: %s", code, errb.String())
	}

	out.Reset()
	errb.Reset()
	code := cli.Execute([]string{"enable", "nonexistent"}, &out, &errb)
	if code != 2 {
		t.Fatalf("exit = %d, want 2 (stderr: %s)", code, errb.String())
	}
	if out.Len() != 0 {
		t.Fatalf("nothing should be printed to stdout on error, got: %s", out.String())
	}
	if _, err := config.Load(cfgPath); err != nil {
		t.Fatalf("config should be untouched: %v", err)
	}
}

func TestEnableWithoutInitExitsTwo(t *testing.T) {
	setupModuleCLITest(t)

	var out, errb bytes.Buffer
	code := cli.Execute([]string{"enable", "completion"}, &out, &errb)
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if !strings.Contains(errb.String(), "init") {
		t.Fatalf("stderr should mention init, got: %s", errb.String())
	}
	if out.Len() != 0 {
		t.Fatalf("nothing should be printed to stdout, got: %s", out.String())
	}
}

func TestEnableThenConfigReflectsIt(t *testing.T) {
	_, cfgPath := setupModuleCLITest(t)
	cli.SetLookPathForTest(bashPresentLookPath)

	var out, errb bytes.Buffer
	if code := cli.Execute([]string{"init"}, &out, &errb); code != 0 {
		t.Fatalf("init exit %d: %s", code, errb.String())
	}

	out.Reset()
	errb.Reset()
	if code := cli.Execute([]string{"enable", "completion"}, &out, &errb); code != 0 {
		t.Fatalf("enable exit %d: %s", code, errb.String())
	}
	if !strings.Contains(out.String(), "enabled completion") {
		t.Fatalf("unexpected stdout: %s", out.String())
	}

	c, err := config.Load(cfgPath)
	if err != nil || !c.Modules["completion"].Enabled {
		t.Fatalf("completion not enabled: %+v err=%v", c.Modules, err)
	}

	// disable is the mirror.
	out.Reset()
	errb.Reset()
	if code := cli.Execute([]string{"disable", "completion"}, &out, &errb); code != 0 {
		t.Fatalf("disable exit %d: %s", code, errb.String())
	}
	if !strings.Contains(out.String(), "disabled completion") {
		t.Fatalf("unexpected stdout: %s", out.String())
	}
	c, err = config.Load(cfgPath)
	if err != nil || c.Modules["completion"].Enabled {
		t.Fatalf("completion still enabled: %+v err=%v", c.Modules, err)
	}
}

// TestEnableRejectsModuleIncompatibleWithManagedShells covers the bug found
// by a live end-to-end test on a bash-only Ubuntu host: `enable` accepted a
// zsh-only module even though zsh wasn't a managed shell, leaving it
// permanently degraded ("no snippet for any managed shell") with no signal
// besides `doctor`. `enable` must refuse up front instead.
func TestEnableRejectsModuleIncompatibleWithManagedShells(t *testing.T) {
	_, cfgPath := setupModuleCLITest(t)
	cli.SetLookPathForTest(bashPresentLookPath) // only bash is present, not zsh

	var out, errb bytes.Buffer
	if code := cli.Execute([]string{"init"}, &out, &errb); code != 0 {
		t.Fatalf("init exit %d: %s", code, errb.String())
	}

	out.Reset()
	errb.Reset()
	code := cli.Execute([]string{"enable", "zshonly"}, &out, &errb)
	if code != 2 {
		t.Fatalf("exit = %d, want 2 (stderr: %s)", code, errb.String())
	}
	if !strings.Contains(errb.String(), "zshonly") || !strings.Contains(errb.String(), "zsh") {
		t.Fatalf("stderr should name the module and the unmet shell, got: %s", errb.String())
	}
	if out.Len() != 0 {
		t.Fatalf("nothing should be printed to stdout on error, got: %s", out.String())
	}

	c, err := config.Load(cfgPath)
	if err != nil || c.Modules["zshonly"].Enabled {
		t.Fatalf("zshonly should not have been enabled: %+v err=%v", c.Modules, err)
	}
}

// TestEnableAllowsModuleWhenItsShellIsManaged is the mirror: once the shell a
// module requires is actually present, enabling it succeeds.
func TestEnableAllowsModuleWhenItsShellIsManaged(t *testing.T) {
	_, cfgPath := setupModuleCLITest(t)
	cli.SetLookPathForTest(zshPresentLookPath)

	var out, errb bytes.Buffer
	if code := cli.Execute([]string{"init"}, &out, &errb); code != 0 {
		t.Fatalf("init exit %d: %s", code, errb.String())
	}

	out.Reset()
	errb.Reset()
	if code := cli.Execute([]string{"enable", "zshonly"}, &out, &errb); code != 0 {
		t.Fatalf("enable exit %d: %s", code, errb.String())
	}

	c, err := config.Load(cfgPath)
	if err != nil || !c.Modules["zshonly"].Enabled {
		t.Fatalf("zshonly not enabled: %+v err=%v", c.Modules, err)
	}
}
