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
	return renderCheatsheetWith(t, id, shell, opts, nil)
}

// renderCheatsheetWith is renderCheatsheet with the given modules active.
func renderCheatsheetWith(t *testing.T, id, shell string, opts map[string]any, active []string) string {
	t.Helper()
	activeSet := map[string]bool{}
	for _, a := range active {
		activeSet[a] = true
	}
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
		Bin:       map[string]string{}, Active: activeSet,
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

// TestEveryBuiltinModuleHasACheatsheet fails for a built-in module that ships
// no cheatsheet.tmpl, or one that renders empty for a shell it supports.
func TestEveryBuiltinModuleHasACheatsheet(t *testing.T) {
	reg, err := module.LoadRegistry(modules.FS(), "")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range reg.All() {
		id := m.Manifest.Module.ID
		t.Run(id, func(t *testing.T) {
			if _, has, err := m.Cheatsheet(); err != nil || !has {
				t.Fatalf("module %q needs a cheatsheet: add modules/builtin/%s/cheatsheet.tmpl (has=%v err=%v)",
					id, id, has, err)
			}
			for _, shell := range m.Manifest.Shells {
				if strings.TrimSpace(renderCheatsheet(t, id, shell, nil)) == "" {
					t.Errorf("module %q: cheatsheet renders empty for %s", id, shell)
				}
			}
		})
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
	if !strings.Contains(got, "plain `cd` is unchanged") {
		t.Errorf("cmd = j leaves cd alone:\n%s", got)
	}

	cd := renderCheatsheet(t, "zoxide", "bash", map[string]any{"cmd": "cd"})
	if strings.Contains(cd, "unchanged") {
		t.Errorf("cmd = cd replaces cd, it must not be called unchanged:\n%s", cd)
	}
	if !strings.Contains(cd, "`cdi foo`") {
		t.Errorf("cmd = cd must list cdi:\n%s", cd)
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

func TestModernAliasesCheatsheetFollowsReplace(t *testing.T) {
	ls := renderCheatsheet(t, "modern-aliases", "zsh", map[string]any{"replace": []any{"ls"}})
	for _, want := range []string{"`ls`", "`ll`"} {
		if !strings.Contains(ls, want) {
			t.Errorf("replace = [ls] must mention %s:\n%s", want, ls)
		}
	}
	for _, bad := range []string{"`cat`", "`find`"} {
		if strings.Contains(ls, bad) {
			t.Errorf("replace = [ls] must not mention %s:\n%s", bad, ls)
		}
	}

	rest := renderCheatsheet(t, "modern-aliases", "bash", map[string]any{"replace": []any{"cat", "find"}})
	for _, want := range []string{"`cat`", "`find`"} {
		if !strings.Contains(rest, want) {
			t.Errorf("replace = [cat find] must mention %s:\n%s", want, rest)
		}
	}
	for _, bad := range []string{"`ls`", "`ll`"} {
		if strings.Contains(rest, bad) {
			t.Errorf("replace = [cat find] must not mention %s:\n%s", bad, rest)
		}
	}

	none := renderCheatsheet(t, "modern-aliases", "zsh", map[string]any{"replace": []any{}})
	for _, bad := range []string{"`ls`", "`ll`", "`cat`", "`find`"} {
		if strings.Contains(none, bad) {
			t.Errorf("replace = [] must not mention %s:\n%s", bad, none)
		}
	}
}

func TestPayRespectsCheatsheetFollowsAlias(t *testing.T) {
	def := renderCheatsheet(t, "pay-respects", "zsh", nil)
	got := renderCheatsheet(t, "pay-respects", "bash", map[string]any{"alias": "oops"})
	if !strings.Contains(def, "`f`") {
		t.Errorf("default alias f must be listed:\n%s", def)
	}
	if !strings.Contains(got, "`oops`") {
		t.Errorf("custom alias must be listed:\n%s", got)
	}
	if strings.Contains(got, "`f`") {
		t.Errorf("alias = oops must not mention f:\n%s", got)
	}
}

func TestYouShouldUseCheatsheetFollowsOptions(t *testing.T) {
	def := renderCheatsheet(t, "you-should-use", "zsh", nil)
	opt := renderCheatsheet(t, "you-should-use", "zsh", map[string]any{
		"message_position": "after", "mode": "ALL", "ignored_aliases": []any{"g", "ll"},
	})
	if !strings.Contains(def, "before the command") || strings.Contains(def, "after the command") {
		t.Errorf("default hint comes before the command:\n%s", def)
	}
	if !strings.Contains(opt, "after the command") || strings.Contains(opt, "before the command") {
		t.Errorf("message_position = after must say so:\n%s", opt)
	}
	if !strings.Contains(def, "closest") || !strings.Contains(opt, "every") {
		t.Errorf("mode must change the text:\ndef:\n%s\nopt:\n%s", def, opt)
	}
	if strings.Contains(def, "Never hinted") {
		t.Errorf("default has no ignored aliases:\n%s", def)
	}
	if !strings.Contains(opt, "Never hinted") {
		t.Errorf("ignored_aliases must produce the \"Never hinted\" line:\n%s", opt)
	}
	for _, want := range []string{"`g`", "`ll`"} {
		if !strings.Contains(opt, want) {
			t.Errorf("ignored alias %s must be listed:\n%s", want, opt)
		}
	}
}

func TestOmnishellPromptCheatsheetFollowsOptions(t *testing.T) {
	const userHost = "`user@host`"
	const duration = "Commands that ran for 1 second or longer show their duration"
	const bashNote = "`show_duration` has no effect in bash"

	def := renderCheatsheet(t, "omnishell-prompt", "zsh", nil)
	full := renderCheatsheet(t, "omnishell-prompt", "bash", map[string]any{"style": "full"})
	zshDur := renderCheatsheet(t, "omnishell-prompt", "zsh", map[string]any{"show_duration": true})
	bashDur := renderCheatsheet(t, "omnishell-prompt", "bash", map[string]any{"show_duration": true})

	if strings.Contains(def, userHost) || strings.Contains(def, duration) || strings.Contains(def, bashNote) {
		t.Errorf("default shows neither user@host nor duration:\n%s", def)
	}
	if !strings.Contains(full, userHost) {
		t.Errorf("style = full must list user@host:\n%s", full)
	}
	if !strings.Contains(zshDur, duration) {
		t.Errorf("zsh show_duration must be described:\n%s", zshDur)
	}
	if strings.Contains(bashDur, duration) || !strings.Contains(bashDur, bashNote) {
		t.Errorf("bash show_duration is a no-op and must say so:\n%s", bashDur)
	}
}

func TestTmuxCheatsheetFollowsOptions(t *testing.T) {
	const attach = "attaches to the tmux session `default`, or creates it"
	def := renderCheatsheet(t, "tmux", "zsh", nil)
	named := renderCheatsheet(t, "tmux", "bash", map[string]any{"session": "work"})
	off := renderCheatsheet(t, "tmux", "zsh", map[string]any{"auto_attach": false})

	if !strings.Contains(def, attach) {
		t.Errorf("default must describe the auto-attach to default:\n%s", def)
	}
	if !strings.Contains(named, "`work`") || strings.Contains(named, "`default`") {
		t.Errorf("session = work must replace default:\n%s", named)
	}
	if strings.Contains(off, "attaches to") || strings.Contains(off, "`default`") {
		t.Errorf("auto_attach = false must not promise an attach:\n%s", off)
	}
	if !strings.Contains(off, "Nothing starts automatically") {
		t.Errorf("auto_attach = false must say so:\n%s", off)
	}
	for _, s := range []string{def, named, off} {
		if !strings.Contains(s, "`Ctrl-b d`") {
			t.Errorf("tmux keys must be listed:\n%s", s)
		}
	}
}

func TestWelcomeCheatsheetFollowsOnlySSH(t *testing.T) {
	def := renderCheatsheet(t, "welcome", "zsh", nil)
	ssh := renderCheatsheet(t, "welcome", "bash", map[string]any{"only_ssh": true})
	if !strings.Contains(def, "on every interactive shell start") {
		t.Errorf("default shows the summary on every start:\n%s", def)
	}
	if !strings.Contains(ssh, "only when `SSH_CONNECTION` or `SSH_TTY` is set") ||
		strings.Contains(ssh, "every interactive shell start") {
		t.Errorf("only_ssh = true must restrict the summary:\n%s", ssh)
	}
}

func TestZshCompletionsCheatsheetFollowsCompletionModule(t *testing.T) {
	without := renderCheatsheetWith(t, "zsh-completions", "zsh", nil, nil)
	with := renderCheatsheetWith(t, "zsh-completions", "zsh", nil, []string{"completion"})

	if strings.Contains(without, "`Tab`") {
		t.Errorf("without the completion module Tab gains nothing:\n%s", without)
	}
	if !strings.Contains(without, "`completion` module") {
		t.Errorf("without the completion module the sheet must say it is needed:\n%s", without)
	}
	if !strings.Contains(with, "`Tab`") {
		t.Errorf("with the completion module Tab must be listed:\n%s", with)
	}
	assertGoldenNamed(t, "zsh-completions", "cheatsheet-zsh-with-completion", with)
}

func TestAutosuggestionsCheatsheetStatesAcceptConditions(t *testing.T) {
	got := renderCheatsheet(t, "autosuggestions", "zsh", nil)
	if strings.Contains(got, "grey") {
		t.Errorf("the suggestion colour is configurable, do not call it grey:\n%s", got)
	}
	if !strings.Contains(got, "cursor is at the end of the line") {
		t.Errorf("accepting only works at the end of the line:\n%s", got)
	}
}

func TestDynamicCompletionCheatsheetExplainsEntries(t *testing.T) {
	got := renderCheatsheet(t, "dynamic-completion", "bash",
		map[string]any{"commands": []any{"aws=aws_completer"}})
	if !strings.Contains(got, "You type `name`") {
		t.Errorf("must say which part of an entry is typed:\n%s", got)
	}
}

func TestRootLoopsCheatsheetFollowsAppearance(t *testing.T) {
	auto := renderCheatsheet(t, "root-loops", "zsh", nil)
	dark := renderCheatsheet(t, "root-loops", "bash", map[string]any{"appearance": "dark"})
	light := renderCheatsheet(t, "root-loops", "zsh", map[string]any{"appearance": "light"})
	if !strings.Contains(auto, "follows the system light/dark setting") {
		t.Errorf("auto must follow the system:\n%s", auto)
	}
	if !strings.Contains(dark, "Always applies the dark palette") || strings.Contains(dark, "follows the system") {
		t.Errorf("appearance = dark must force dark:\n%s", dark)
	}
	if !strings.Contains(light, "Always applies the light palette") || strings.Contains(light, "follows the system") {
		t.Errorf("appearance = light must force light:\n%s", light)
	}
}

func TestDeltaCheatsheetFollowsOptions(t *testing.T) {
	def := renderCheatsheet(t, "delta", "zsh", nil)
	for _, want := range []string{"`--navigate`", "`--line-numbers`", "`n` / `N`"} {
		if !strings.Contains(def, want) {
			t.Errorf("default must contain %s:\n%s", want, def)
		}
	}
	if strings.Contains(def, "--side-by-side") {
		t.Errorf("default must not mention side-by-side:\n%s", def)
	}

	got := renderCheatsheet(t, "delta", "bash", map[string]any{
		"navigate": false, "line_numbers": false, "side_by_side": true,
	})
	if !strings.Contains(got, "`--side-by-side`") {
		t.Errorf("side_by_side must be listed:\n%s", got)
	}
	for _, bad := range []string{"--navigate", "--line-numbers", "`n` / `N`"} {
		if strings.Contains(got, bad) {
			t.Errorf("must not mention %s when off:\n%s", bad, got)
		}
	}

	none := renderCheatsheet(t, "delta", "zsh", map[string]any{
		"navigate": false, "line_numbers": false,
	})
	if !strings.Contains(none, "Active flags: none") {
		t.Errorf("all flags off must say none:\n%s", none)
	}
}

func TestNaviCheatsheetFollowsKey(t *testing.T) {
	def := renderCheatsheet(t, "navi", "zsh", nil)
	if !strings.Contains(def, "`ctrl-g` — open navi") {
		t.Errorf("default key must be listed:\n%s", def)
	}
	if strings.Contains(def, "no longer bound") {
		t.Errorf("default key must not mention a rebind:\n%s", def)
	}

	got := renderCheatsheet(t, "navi", "bash", map[string]any{"key": "alt-n"})
	if !strings.Contains(got, "`alt-n` — open navi") {
		t.Errorf("custom key must be listed:\n%s", got)
	}
	if !strings.Contains(got, "`ctrl-g` is no longer bound") {
		t.Errorf("a custom key must say ctrl-g is released:\n%s", got)
	}
	if !strings.Contains(got, "needs bash 4.0 or newer") {
		t.Errorf("bash with a custom key must name the bash 4.0 requirement:\n%s", got)
	}
	zsh := renderCheatsheet(t, "navi", "zsh", map[string]any{"key": "alt-n"})
	if strings.Contains(zsh, "bash") {
		t.Errorf("zsh must not mention bash:\n%s", zsh)
	}
}
