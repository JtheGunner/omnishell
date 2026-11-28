package modules_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JtheGunner/omnishell/internal/module"
	"github.com/JtheGunner/omnishell/modules"
)

func dynamicCompletionModule(t *testing.T) module.Module {
	t.Helper()
	reg, err := module.LoadRegistry(modules.FS(), "")
	if err != nil {
		t.Fatal(err)
	}
	m, ok := reg.Get("dynamic-completion")
	if !ok {
		t.Fatal("module dynamic-completion not embedded")
	}
	return m
}

func TestDynamicCompletion(t *testing.T) {
	empty := renderModule(t, "dynamic-completion", "bash", nil)
	assertGoldenNamed(t, "dynamic-completion", "bash-default", empty)
	custom := renderModule(t, "dynamic-completion", "bash",
		map[string]any{"commands": []any{"terraform", "vault", "aws=aws_completer"}})
	assertGoldenNamed(t, "dynamic-completion", "bash-custom", custom)

	if strings.TrimSpace(empty) != "" {
		t.Errorf("an empty commands list must render nothing, got:\n%s", empty)
	}
	if !strings.Contains(custom, "'aws=aws_completer'") {
		t.Errorf("entries must be shell-quoted:\n%s", custom)
	}
}

func TestDynamicCompletionIsBashOnly(t *testing.T) {
	m := dynamicCompletionModule(t)
	if got := m.Manifest.Shells; len(got) != 1 || got[0] != "bash" {
		t.Errorf("shells = %v, want [bash]", got)
	}
	if _, has, err := m.Template("zsh"); err != nil || has {
		t.Errorf("zsh template: has=%v err=%v, want none", has, err)
	}
	if got := m.Manifest.Options["commands"].Type; got != "list<string>" {
		t.Errorf("commands type = %q, want list<string>", got)
	}
}

func TestDynamicCompletionRejectsUnsafeValues(t *testing.T) {
	m := dynamicCompletionModule(t)
	for _, bad := range []string{"x; rm -rf ~", "$" + "(id)", "a b", "-x", "a=", "=b", "a=b=c", "a=-b", ""} {
		_, err := module.ValidateOptions(m.Manifest.Options,
			map[string]any{"commands": []any{bad}})
		if err == nil {
			t.Errorf("value %q was accepted", bad)
		}
	}
	for _, good := range []string{"terraform", "aws=aws_completer", "g++", "a.b-c"} {
		if _, err := module.ValidateOptions(m.Manifest.Options,
			map[string]any{"commands": []any{good}}); err != nil {
			t.Errorf("value %q rejected: %v", good, err)
		}
	}
}

func TestDynamicCompletionRegistersOnlyCommandsOnPath(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not installed")
	}
	bin := filepath.Join(t.TempDir(), "my bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"fakecli", "fakecli_completer"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	rendered := renderModule(t, "dynamic-completion", "bash", map[string]any{
		"commands": []any{"fakecli", "fakealias=fakecli_completer", "missingcli", "gone=missing_completer"},
	})
	script := "PATH='" + bin + "':\"$PATH\"\n" + rendered +
		"\ncomplete -p fakecli fakealias\ncomplete -p missingcli gone 2>/dev/null\nset | grep -c '^__omnishell_dc'\n"
	out, _ := exec.Command(bash, "--norc", "--noprofile", "-c", script).CombinedOutput()
	got := string(out)
	for _, want := range []string{
		"complete -C '" + bin + "/fakecli' fakecli",
		"complete -C '" + bin + "/fakecli_completer' fakealias",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "missingcli") || strings.Contains(got, "gone") {
		t.Errorf("a missing command must not be registered:\n%s", got)
	}
	if !strings.HasSuffix(strings.TrimSpace(got), "0") {
		t.Errorf("temporary variables leaked:\n%s", got)
	}

	missing := renderModule(t, "dynamic-completion", "bash",
		map[string]any{"commands": []any{"missingcli", "gone=missing_completer"}})
	res, err := exec.Command(bash, "--norc", "--noprofile", "-c", missing+"\n").CombinedOutput()
	if err != nil || len(res) != 0 {
		t.Errorf("missing commands: err=%v output=%q, want none", err, res)
	}
}
