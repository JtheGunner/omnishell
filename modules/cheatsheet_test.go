package modules_test

import (
	"strings"
	"testing"

	"github.com/JtheGunner/omnishell/internal/module"
	"github.com/JtheGunner/omnishell/internal/render"
	"github.com/JtheGunner/omnishell/modules"
)

// renderCheatsheet renders a built-in module's cheatsheet.tmpl the way
// `omnishell cheatsheet` does (platform macos, no other module active).
func renderCheatsheet(t *testing.T, id, shell string, opts map[string]any) string {
	t.Helper()
	reg, err := module.LoadRegistry(modules.FS(), "")
	if err != nil {
		t.Fatal(err)
	}
	m, ok := reg.Get(id)
	if !ok {
		t.Fatalf("module %q not embedded", id)
	}
	body, has, err := m.Cheatsheet()
	if err != nil || !has {
		t.Fatalf("%s cheatsheet: has=%v err=%v", id, has, err)
	}
	norm, err := module.ValidateOptions(m.Manifest.Options, opts)
	if err != nil {
		t.Fatalf("options: %v", err)
	}
	out, err := render.Render(body, render.Context{
		Options: norm, Platform: "macos", Shell: shell,
		VendorDir: "/home/j/.config/omnishell/vendor",
		ConfigDir: "/home/j/.config/omnishell",
		Bin:       map[string]string{}, Active: map[string]bool{},
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	return out
}

// TestBuiltinCheatsheets renders every shipped cheatsheet for each shell its
// module supports, with default options, and compares it with a golden file.
func TestBuiltinCheatsheets(t *testing.T) {
	reg, err := module.LoadRegistry(modules.FS(), "")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range reg.All() {
		if _, has, err := m.Cheatsheet(); err != nil || !has {
			continue
		}
		for _, shell := range m.Manifest.Shells {
			t.Run(m.Manifest.Module.ID+"/"+shell, func(t *testing.T) {
				got := renderCheatsheet(t, m.Manifest.Module.ID, shell, nil)
				if strings.TrimSpace(got) == "" {
					t.Error("cheatsheet renders empty")
				}
				if strings.Contains(got, "\x1b") {
					t.Errorf("cheatsheet contains an escape byte:\n%q", got)
				}
				assertGoldenNamed(t, m.Manifest.Module.ID, "cheatsheet-"+shell, got)
			})
		}
	}
}

func TestSpellCorrectionCheatsheetFollowsOptions(t *testing.T) {
	def := renderCheatsheet(t, "spell-correction", "zsh", nil)
	all := renderCheatsheet(t, "spell-correction", "zsh", map[string]any{"scope": "all"})
	exempt := renderCheatsheet(t, "spell-correction", "zsh", map[string]any{"nocorrect_commands": []any{"git"}})

	if strings.Contains(def, "git") {
		t.Errorf("default cheatsheet must not mention git:\n%s", def)
	}
	if def == all {
		t.Error("scope = all must change the cheatsheet")
	}
	if !strings.Contains(exempt, "`git`") {
		t.Errorf("nocorrect_commands = [git] must be listed:\n%s", exempt)
	}
	assertGoldenNamed(t, "spell-correction", "cheatsheet-zsh-all", all)
	assertGoldenNamed(t, "spell-correction", "cheatsheet-zsh-nocorrect", exempt)
}

func TestHistoryCheatsheetFollowsSize(t *testing.T) {
	got := renderCheatsheet(t, "history", "zsh", map[string]any{"size": 1234})
	if !strings.Contains(got, "1234") {
		t.Errorf("history size must appear in the cheatsheet:\n%s", got)
	}
}

func TestDynamicCompletionCheatsheetFollowsCommands(t *testing.T) {
	none := renderCheatsheet(t, "dynamic-completion", "bash", nil)
	some := renderCheatsheet(t, "dynamic-completion", "bash",
		map[string]any{"commands": []any{"terraform", "aws=aws_completer"}})

	if strings.Contains(none, "asks the program") {
		t.Errorf("default cheatsheet must not list commands:\n%s", none)
	}
	for _, want := range []string{"`terraform`", "`aws=aws_completer`"} {
		if !strings.Contains(some, want) {
			t.Errorf("missing %s in:\n%s", want, some)
		}
	}
	assertGoldenNamed(t, "dynamic-completion", "cheatsheet-bash-commands", some)
}

func TestAutosuggestionsAndFzfTabCheatsheetsFollowOptions(t *testing.T) {
	style := renderCheatsheet(t, "autosuggestions", "zsh", map[string]any{"highlight_style": "fg=240"})
	if !strings.Contains(style, "fg=240") {
		t.Errorf("highlight_style must appear:\n%s", style)
	}
	on := renderCheatsheet(t, "fzf-tab", "zsh", nil)
	off := renderCheatsheet(t, "fzf-tab", "zsh", map[string]any{"cd_preview": false})
	if on == off {
		t.Error("cd_preview must change the fzf-tab cheatsheet")
	}
}

func TestFzfCheatsheetFollowsOptions(t *testing.T) {
	def := renderCheatsheet(t, "fzf", "zsh", nil)
	noR := renderCheatsheet(t, "fzf", "zsh", map[string]any{"ctrl_r": false})
	withT := renderCheatsheet(t, "fzf", "bash", map[string]any{"ctrl_t": true})

	if !strings.Contains(def, "Ctrl-R") {
		t.Errorf("default must list Ctrl-R:\n%s", def)
	}
	if strings.Contains(def, "Ctrl-T") {
		t.Errorf("default must not list Ctrl-T:\n%s", def)
	}
	if strings.Contains(noR, "Ctrl-R") {
		t.Errorf("ctrl_r = false must drop Ctrl-R:\n%s", noR)
	}
	if !strings.Contains(withT, "Ctrl-T") {
		t.Errorf("ctrl_t = true must list Ctrl-T:\n%s", withT)
	}
}

func TestAtuinCheatsheetFollowsOptions(t *testing.T) {
	def := renderCheatsheet(t, "atuin", "zsh", nil)
	off := renderCheatsheet(t, "atuin", "bash", map[string]any{"bind_ctrl_r": false, "bind_up_arrow": false})

	for _, want := range []string{"Ctrl-R", "`Up`"} {
		if !strings.Contains(def, want) {
			t.Errorf("default must list %s:\n%s", want, def)
		}
		if strings.Contains(off, want) {
			t.Errorf("disabled binding %s must be dropped:\n%s", want, off)
		}
	}
}

func TestZoxideCheatsheetFollowsCmd(t *testing.T) {
	got := renderCheatsheet(t, "zoxide", "zsh", map[string]any{"cmd": "j"})
	for _, want := range []string{"`j ", "`ji "} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in:\n%s", want, got)
		}
	}
	for _, bad := range []string{"`z ", "`zi ", "`z`", "`zi`"} {
		if strings.Contains(got, bad) {
			t.Errorf("cmd = j must not mention %s:\n%s", bad, got)
		}
	}
}

func TestBrootCheatsheetFollowsCmd(t *testing.T) {
	def := renderCheatsheet(t, "broot", "zsh", nil)
	got := renderCheatsheet(t, "broot", "bash", map[string]any{"cmd": "tree"})
	if strings.Contains(def, "alias") {
		t.Errorf("default must not mention an alias:\n%s", def)
	}
	if !strings.Contains(got, "`tree`") || !strings.Contains(got, "`br`") {
		t.Errorf("custom cmd must appear next to br:\n%s", got)
	}
}

func TestDirenvCheatsheetFollowsLogFormat(t *testing.T) {
	def := renderCheatsheet(t, "direnv", "zsh", nil)
	got := renderCheatsheet(t, "direnv", "zsh", map[string]any{"log_format": "[direnv] %s"})
	if strings.Contains(def, "DIRENV_LOG_FORMAT") {
		t.Errorf("default must not mention the log format:\n%s", def)
	}
	if !strings.Contains(got, "[direnv] %s") {
		t.Errorf("log_format must appear:\n%s", got)
	}
}

func TestMiseCheatsheetFollowsMode(t *testing.T) {
	act := renderCheatsheet(t, "mise", "zsh", nil)
	shims := renderCheatsheet(t, "mise", "bash", map[string]any{"mode": "shims"})
	if act == shims {
		t.Error("mode must change the mise cheatsheet")
	}
	if !strings.Contains(shims, "shims") {
		t.Errorf("shims mode must say so:\n%s", shims)
	}
}
