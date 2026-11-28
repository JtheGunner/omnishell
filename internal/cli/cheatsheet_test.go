package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JtheGunner/omnishell/internal/cli"
)

// writeCheatModule installs a user module with no packages that supports both
// shells. A non-empty cheatsheet becomes its cheatsheet.tmpl.
func writeCheatModule(t *testing.T, configDir, id, name, description, cheatsheet string) {
	t.Helper()
	dir := filepath.Join(configDir, "modules", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "platforms = [\"macos\", \"linux\"]\nshells = [\"zsh\", \"bash\"]\nrequires = []\nafter = []\n\n" +
		"[module]\nid = \"" + id + "\"\nname = \"" + name + "\"\ndescription = \"" + description + "\"\nversion = \"1.0.0\"\nschema = 1\n"
	files := map[string]string{"manifest.toml": manifest, "zsh.tmpl": "# " + id + "\n", "bash.tmpl": "# " + id + "\n"}
	if cheatsheet != "" {
		files["cheatsheet.tmpl"] = cheatsheet
	}
	for fname, content := range files {
		if err := os.WriteFile(filepath.Join(dir, fname), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// cheatsheetEnv sets up a hermetic sandbox and pins $SHELL's base name to zsh.
// It returns the config dir.
func cheatsheetEnv(t *testing.T) string {
	t.Helper()
	home, _ := setupModuleCLITest(t)
	cli.SetLookPathForTest(func(name string) (string, error) {
		if name == "zsh" || name == "bash" {
			return "/bin/" + name, nil
		}
		return "", os.ErrNotExist
	})
	t.Cleanup(cli.SetEnvShellForTest("zsh"))
	return filepath.Join(home, ".config", "omnishell")
}

func runCheatsheetCLI(args ...string) (int, string, string) {
	var out, errOut bytes.Buffer
	code := cli.Execute(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func initAndEnable(t *testing.T, ids ...string) {
	t.Helper()
	if code, _, e := runCheatsheetCLI("init"); code != 0 {
		t.Fatalf("init exit %d: %s", code, e)
	}
	for _, id := range ids {
		if code, _, e := runCheatsheetCLI("enable", id); code != 0 {
			t.Fatalf("enable %s exit %d: %s", id, code, e)
		}
	}
}

func TestCheatsheetCommandPrintsActiveModules(t *testing.T) {
	configDir := cheatsheetEnv(t)
	writeCheatModule(t, configDir, "alpha", "Alpha", "First module", "- `aa` does a\n")
	writeCheatModule(t, configDir, "beta", "Beta", "Second module", "- `bb` does b ({{ .Shell }})\n")
	initAndEnable(t, "alpha", "beta")

	code, out, errOut := runCheatsheetCLI("cheatsheet")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	want := "# omnishell cheatsheet (zsh)\n" +
		"\n## Alpha (alpha)\nFirst module\n\n- `aa` does a\n" +
		"\n## Beta (beta)\nSecond module\n\n- `bb` does b (zsh)\n"
	if out != want {
		t.Fatalf("output mismatch\n got: %q\nwant: %q", out, want)
	}
}

func TestCheatsheetCommandShellFlag(t *testing.T) {
	configDir := cheatsheetEnv(t)
	writeCheatModule(t, configDir, "alpha", "Alpha", "First module", "shell={{ .Shell }}\n")
	initAndEnable(t, "alpha")

	code, out, errOut := runCheatsheetCLI("cheatsheet", "--shell", "bash")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if !strings.HasPrefix(out, "# omnishell cheatsheet (bash)\n") || !strings.Contains(out, "shell=bash") {
		t.Fatalf("expected bash output, got %q", out)
	}
}

func TestCheatsheetCommandUnknownModuleExits2(t *testing.T) {
	cheatsheetEnv(t)
	initAndEnable(t)

	code, _, errOut := runCheatsheetCLI("cheatsheet", "nope")
	if code != 2 {
		t.Fatalf("exit = %d, want 2 (stderr %q)", code, errOut)
	}
}

func TestCheatsheetCommandNoConfigExits2(t *testing.T) {
	cheatsheetEnv(t)

	code, _, errOut := runCheatsheetCLI("cheatsheet")
	if code != 2 {
		t.Fatalf("exit = %d, want 2 (stderr %q)", code, errOut)
	}
}

func TestCheatsheetCommandNoActiveModules(t *testing.T) {
	cheatsheetEnv(t)
	initAndEnable(t)

	code, out, errOut := runCheatsheetCLI("cheatsheet")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if out != "No active modules for zsh.\n" {
		t.Fatalf("got %q", out)
	}
}

func TestCheatsheetCommandRenderFailureWarnsAndExits1(t *testing.T) {
	configDir := cheatsheetEnv(t)
	writeCheatModule(t, configDir, "alpha", "Alpha", "First module", "{{ .Nope }}\n")
	writeCheatModule(t, configDir, "beta", "Beta", "Second module", "- `bb` does b\n")
	initAndEnable(t, "alpha", "beta")

	code, out, errOut := runCheatsheetCLI("cheatsheet")
	if code != 1 {
		t.Fatalf("exit = %d, want 1 (stderr %q)", code, errOut)
	}
	if !strings.Contains(out, "## Beta (beta)") {
		t.Fatalf("healthy module missing from output: %q", out)
	}
	if strings.Contains(out, "## Alpha (alpha)") {
		t.Fatalf("failed module must not be printed: %q", out)
	}
	if !strings.Contains(errOut, "warning: module alpha:") {
		t.Fatalf("missing warning, stderr %q", errOut)
	}
}

func TestCheatsheetCommandCleansControlSequences(t *testing.T) {
	configDir := cheatsheetEnv(t)
	writeCheatModule(t, configDir, "alpha", `Al\u001b[2Jpha`, `Desc\u001b]0;x\u0007`, "body \x1b[2J here\nline 2\n")
	initAndEnable(t, "alpha")

	code, out, errOut := runCheatsheetCLI("cheatsheet")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if strings.ContainsRune(out, '\x1b') || strings.ContainsRune(out, '\x07') {
		t.Fatalf("control characters leaked: %q", out)
	}
	if !strings.Contains(out, "line 2\n") {
		t.Fatalf("multi-line body lost its newlines: %q", out)
	}
}

func TestCheatsheetCommandFooterListsModulesWithoutFile(t *testing.T) {
	configDir := cheatsheetEnv(t)
	writeCheatModule(t, configDir, "alpha", "Alpha", "First module", "- `aa`\n")
	writeCheatModule(t, configDir, "beta", "Beta", "Second module", "")
	writeCheatModule(t, configDir, "gamma", "Gamma", "Third module", "")
	initAndEnable(t, "alpha", "beta", "gamma")

	code, out, errOut := runCheatsheetCLI("cheatsheet")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if !strings.HasSuffix(out, "\nNo cheatsheet: beta, gamma\n") {
		t.Fatalf("footer missing: %q", out)
	}
}
