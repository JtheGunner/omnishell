package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JtheGunner/omnishell/internal/cli"
)

// staleConfig has one option the fixture fzf module no longer defines, is
// missing its ctrl_r option, and configures a module that does not exist.
const staleConfig = `[omnishell]
version = 1

# my fzf setup
[modules.fzf]
enabled = true

[modules.fzf.options]
legacy = 1

[modules.gone]
enabled = true
`

func writeConfig(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

func runCLI(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var out, errb bytes.Buffer
	code = cli.Execute(args, &out, &errb)
	return out.String(), errb.String(), code
}

func TestConfigSyncDryRunShowsDiffAndChangesNothing(t *testing.T) {
	cfgPath := setTestInit(t)
	writeConfig(t, cfgPath, staleConfig)

	out, errOut, code := runCLI(t, "config", "sync")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	for _, want := range []string{
		"+ fzf.ctrl_r = true",
		"Bind Ctrl+R to the fzf history widget",
		"- fzf.legacy",
		"- [modules.gone]",
		"config sync --write",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	got, _ := os.ReadFile(cfgPath)
	if string(got) != staleConfig {
		t.Fatalf("dry run changed the file:\n%s", got)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(cfgPath), "backups")); err == nil {
		t.Fatal("dry run created a backup")
	}
}

func TestConfigSyncUpToDateExitsZero(t *testing.T) {
	setTestInit(t)

	out, errOut, code := runCLI(t, "config", "sync")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if !strings.Contains(out, "config.toml is up to date") {
		t.Fatalf("output = %q", out)
	}
}

func TestConfigSyncWriteUpdatesFileAndRollbackRestoresIt(t *testing.T) {
	cfgPath := setTestInit(t)
	writeConfig(t, cfgPath, staleConfig)

	out, errOut, code := runCLI(t, "config", "sync", "--write")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if !strings.Contains(out, "backup:") {
		t.Fatalf("output lacks the backup path:\n%s", out)
	}

	got, _ := os.ReadFile(cfgPath)
	want := `[omnishell]
version = 1

# my fzf setup
[modules.fzf]
enabled = true

[modules.fzf.options]
# ctrl_r = true   # Bind Ctrl+R to the fzf history widget
`
	if string(got) != want {
		t.Fatalf("config =\n%s\nwant =\n%s", got, want)
	}

	if _, _, code := runCLI(t, "validate"); code != 0 {
		t.Fatalf("validate exit %d after sync", code)
	}
	if out, _, _ := runCLI(t, "config", "sync"); !strings.Contains(out, "up to date") {
		t.Fatalf("second sync not a no-op:\n%s", out)
	}

	// Snapshots are named by the second; rolling back in the same second would
	// write the current file over the backup it is about to restore.
	time.Sleep(1100 * time.Millisecond)

	list, _, _ := runCLI(t, "rollback")
	var stamp string
	for _, line := range strings.Split(list, "\n") {
		if f := strings.Fields(line); len(f) > 1 && f[1] == "config-sync" {
			stamp = f[0]
		}
	}
	if stamp == "" {
		t.Fatalf("no config-sync snapshot in:\n%s", list)
	}
	if _, errOut, code := runCLI(t, "rollback", "--to", stamp, "--yes"); code != 0 {
		t.Fatalf("rollback exit %d: %s", code, errOut)
	}
	restored, _ := os.ReadFile(cfgPath)
	if string(restored) != staleConfig {
		t.Fatalf("rollback did not restore the file:\n%s", restored)
	}
}

func TestConfigSyncWithoutConfigExitsTwo(t *testing.T) {
	setupModuleCLITest(t)

	_, errOut, code := runCLI(t, "config", "sync")
	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if !strings.Contains(errOut, "omnishell init") {
		t.Fatalf("stderr lacks the init hint: %q", errOut)
	}
}

func TestConfigSyncBrokenConfigExitsTwoAndWritesNothing(t *testing.T) {
	cfgPath := setTestInit(t)
	writeConfig(t, cfgPath, "[modules.fzf\nenabled = true\n")

	_, _, code := runCLI(t, "config", "sync", "--write")
	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	got, _ := os.ReadFile(cfgPath)
	if string(got) != "[modules.fzf\nenabled = true\n" {
		t.Fatalf("broken file was modified:\n%s", got)
	}
}

func TestConfigSyncKeepsModuleWhoseManifestIsBroken(t *testing.T) {
	cfgPath := setTestInit(t)
	configDir := filepath.Dir(cfgPath)
	if err := os.MkdirAll(filepath.Join(configDir, "modules", "mine"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "modules", "mine", "manifest.toml"), []byte("not toml ["), 0o644); err != nil {
		t.Fatal(err)
	}
	writeConfig(t, cfgPath, "[modules.mine]\nenabled = true\n")

	out, _, code := runCLI(t, "config", "sync", "--write")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if strings.Contains(out, "[modules.mine]") {
		t.Fatalf("a module with a broken manifest was reported as removed:\n%s", out)
	}
	got, _ := os.ReadFile(cfgPath)
	if string(got) != "[modules.mine]\nenabled = true\n" {
		t.Fatalf("file changed:\n%s", got)
	}
}

func TestValidateAndApplyPointToConfigSync(t *testing.T) {
	cfgPath := setTestInit(t)
	writeConfig(t, cfgPath, "[modules.fzf]\nenabled = true\n[modules.fzf.options]\nlegacy = 1\n")

	_, errOut, code := runCLI(t, "validate")
	if code != 2 {
		t.Fatalf("validate exit %d, want 2", code)
	}
	if !strings.Contains(errOut, "omnishell config sync") {
		t.Errorf("validate error lacks the hint: %q", errOut)
	}

	_, errOut, code = runCLI(t, "apply", "--dry-run")
	if code != 2 {
		t.Fatalf("apply exit %d, want 2", code)
	}
	if !strings.Contains(errOut, "omnishell config sync") {
		t.Errorf("apply error lacks the hint: %q", errOut)
	}
}

func TestApplyWarnsAboutUnknownModuleWithHint(t *testing.T) {
	cfgPath := setTestInit(t)
	writeConfig(t, cfgPath, "[modules.gone]\nenabled = true\n")

	_, errOut, _ := runCLI(t, "apply", "--dry-run")
	if !strings.Contains(errOut, `unknown module "gone"`) || !strings.Contains(errOut, "omnishell config sync") {
		t.Fatalf("stderr = %q", errOut)
	}
}
