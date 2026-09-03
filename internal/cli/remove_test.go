package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JtheGunner/omnishell/internal/cli"
	"github.com/JtheGunner/omnishell/internal/config"
)

// TestRemoveCommandEndToEnd walks the natural flow: init a hooked shell, enable
// the package-free `completion` fixture, apply it, then `remove completion`.
// Afterwards the init file must no longer carry the module's section and the
// config on disk must show it disabled.
func TestRemoveCommandEndToEnd(t *testing.T) {
	home, cfgPath := setupModuleCLITest(t)
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
	if body, err := os.ReadFile(initFile); err != nil {
		t.Fatalf("init.zsh missing after apply: %v", err)
	} else if !strings.Contains(string(body), "omnishell:completion") {
		t.Fatalf("init.zsh has no completion section before remove:\n%s", body)
	}

	out.Reset()
	errb.Reset()
	if code := cli.Execute([]string{"remove", "completion", "--yes"}, &out, &errb); code != 0 {
		t.Fatalf("remove exit %d: %s\n%s", code, errb.String(), out.String())
	}

	if body, err := os.ReadFile(initFile); err == nil {
		if strings.Contains(string(body), "omnishell:completion") {
			t.Fatalf("init.zsh still carries completion section after remove:\n%s", body)
		}
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat init.zsh: %v", err)
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if cfg.Modules["completion"].Enabled {
		t.Fatalf("completion still enabled in config after remove: %+v", cfg.Modules["completion"])
	}
}

// TestRemoveUnknownModuleExitsNonZero surfaces the engine's "unknown module"
// error to the user with a non-zero exit.
func TestRemoveUnknownModuleExitsNonZero(t *testing.T) {
	setupModuleCLITest(t)
	cli.SetLookPathForTest(zshPresentLookPath)

	var out, errb bytes.Buffer
	if code := cli.Execute([]string{"init"}, &out, &errb); code != 0 {
		t.Fatalf("init exit %d: %s", code, errb.String())
	}

	out.Reset()
	errb.Reset()
	code := cli.Execute([]string{"remove", "nonexistent", "--yes"}, &out, &errb)
	if code == 0 {
		t.Fatalf("remove unknown module exit 0, want non-zero (stderr: %s)", errb.String())
	}
	if !strings.Contains(errb.String(), "unknown module") {
		t.Fatalf("stderr should mention 'unknown module', got: %s", errb.String())
	}
}
