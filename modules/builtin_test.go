package modules_test

import (
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/JtheGunner/omnishell/internal/graph"
	"github.com/JtheGunner/omnishell/internal/module"
	"github.com/JtheGunner/omnishell/internal/render"
	"github.com/JtheGunner/omnishell/modules"
)

var update = flag.Bool("update", false, "update golden files")

func renderModule(t *testing.T, id, shell string, opts map[string]any) string {
	t.Helper()
	return renderModuleOn(t, id, shell, "macos", opts)
}

func renderModuleOn(t *testing.T, id, shell, platform string, opts map[string]any) string {
	t.Helper()
	reg, err := module.LoadRegistry(modules.FS(), "")
	if err != nil {
		t.Fatal(err)
	}
	m, ok := reg.Get(id)
	if !ok {
		t.Fatalf("module %q not embedded", id)
	}
	body, has, err := m.Template(shell)
	if err != nil || !has {
		t.Fatalf("%s/%s template: has=%v err=%v", id, shell, has, err)
	}
	norm, err := module.ValidateOptions(m.Manifest.Options, opts)
	if err != nil {
		t.Fatalf("options: %v", err)
	}
	out, err := render.Render(body, render.Context{
		Options: norm, Platform: platform, Shell: shell,
		VendorDir: "/home/j/.config/omnishell/vendor",
		ConfigDir: "/home/j/.config/omnishell",
		Bin:       map[string]string{}, Active: map[string]bool{},
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	return out
}

func assertGolden(t *testing.T, id, shell, got string) {
	t.Helper()
	p := filepath.Join("testdata", id, shell+".golden")
	if *update {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("%s/%s golden mismatch:\n--- got ---\n%s\n--- want ---\n%s", id, shell, got, string(want))
	}
}

func assertGoldenNamed(t *testing.T, id, name, got string) {
	t.Helper()
	p := filepath.Join("testdata", id, name+".golden")
	if *update {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("%s/%s golden mismatch:\n--- got ---\n%s\n--- want ---\n%s", id, name, got, string(want))
	}
}

func TestCompletion(t *testing.T) {
	assertGolden(t, "completion", "zsh", renderModule(t, "completion", "zsh", nil))
	assertGolden(t, "completion", "bash", renderModule(t, "completion", "bash", nil))
}

func TestHistory(t *testing.T) {
	opts := map[string]any{"size": int64(50000)}
	assertGolden(t, "history", "zsh", renderModule(t, "history", "zsh", opts))
	assertGolden(t, "history", "bash", renderModule(t, "history", "bash", opts))
}

func TestAutosuggestions(t *testing.T) {
	assertGolden(t, "autosuggestions", "zsh",
		renderModule(t, "autosuggestions", "zsh", map[string]any{"highlight_style": "fg=8"}))
}

func TestSyntaxHighlighting(t *testing.T) {
	assertGolden(t, "syntax-highlighting", "zsh",
		renderModule(t, "syntax-highlighting", "zsh", nil))
}

func TestFzf(t *testing.T) {
	on := map[string]any{"ctrl_r": true, "ctrl_t": false, "default_opts": "--height 40% --reverse --border"}
	off := map[string]any{"ctrl_r": false, "ctrl_t": false, "default_opts": "--height 40% --reverse --border"}
	assertGoldenNamed(t, "fzf", "zsh", renderModule(t, "fzf", "zsh", on))
	assertGoldenNamed(t, "fzf", "bash", renderModule(t, "fzf", "bash", on))
	assertGoldenNamed(t, "fzf", "zsh-noctrlr", renderModule(t, "fzf", "zsh", off))
	assertGoldenNamed(t, "fzf", "bash-noctrlr", renderModule(t, "fzf", "bash", off))
}

func TestZoxide(t *testing.T) {
	opts := map[string]any{"cmd": "z"}
	assertGolden(t, "zoxide", "zsh", renderModule(t, "zoxide", "zsh", opts))
	assertGolden(t, "zoxide", "bash", renderModule(t, "zoxide", "bash", opts))
}

// TestNoUnquotedStringOptionsInCommandPosition is the guard for the zoxide
// shell-injection class of bug: any string- or list-typed option that a builtin
// template interpolates MUST be piped through shellquote, because the generated
// init file is shell code sourced at every shell startup. Numbers and bools are
// safe unquoted.
func TestNoUnquotedStringOptionsInCommandPosition(t *testing.T) {
	reg, err := module.LoadRegistry(modules.FS(), "")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range reg.All() {
		id := m.Manifest.Module.ID
		var risky []string
		for name, spec := range m.Manifest.Options {
			switch spec.Type {
			case "string", "list<string>", "list<enum>":
				risky = append(risky, name)
			}
		}
		if len(risky) == 0 {
			continue
		}
		for _, shell := range []string{"zsh", "bash"} {
			body, has, err := m.Template(shell)
			if err != nil {
				t.Fatalf("%s/%s template read: %v", id, shell, err)
			}
			if !has {
				continue
			}
			for _, name := range risky {
				// Only value-emitting actions matter: the pipeline begins with
				// the .Options.<name> field access, e.g. {{ .Options.cmd }} or
				// {{ .Options.cmd | shellquote }}. Control actions that merely
				// read the option (`{{ if has_item .Options.replace "ls" }}`,
				// `{{ if not .Options.ctrl_r }}`) never reach command position.
				emit := regexp.MustCompile(`\{\{-?\s*\.Options\.` + regexp.QuoteMeta(name) + `\b[^}]*\}\}`)
				for _, action := range emit.FindAllString(body, -1) {
					if !regexp.MustCompile(`\|\s*shellquote\b`).MatchString(action) {
						t.Errorf("%s/%s.tmpl interpolates string option %q without | shellquote: %s",
							id, shell, name, action)
					}
				}
			}
		}
	}
	// Sanity: this test would be vacuous if no builtin used a string option.
	sawStringOpt := false
	for _, m := range reg.All() {
		for _, spec := range m.Manifest.Options {
			if spec.Type == "string" {
				sawStringOpt = true
			}
		}
	}
	if !sawStringOpt {
		t.Fatal("no builtin module declares a string option; guard is vacuous")
	}
}

func TestWelcome(t *testing.T) {
	def := map[string]any{"only_ssh": false}
	ssh := map[string]any{"only_ssh": true}
	assertGoldenNamed(t, "welcome", "zsh", renderModule(t, "welcome", "zsh", def))
	assertGoldenNamed(t, "welcome", "bash", renderModule(t, "welcome", "bash", def))
	assertGoldenNamed(t, "welcome", "zsh-ssh", renderModule(t, "welcome", "zsh", ssh))
	assertGoldenNamed(t, "welcome", "bash-ssh", renderModule(t, "welcome", "bash", ssh))
}

func TestBroot(t *testing.T) {
	def := map[string]any{"cmd": "br"}
	custom := map[string]any{"cmd": "tree"}
	assertGoldenNamed(t, "broot", "zsh", renderModule(t, "broot", "zsh", def))
	assertGoldenNamed(t, "broot", "bash", renderModule(t, "broot", "bash", def))
	assertGoldenNamed(t, "broot", "zsh-cmd", renderModule(t, "broot", "zsh", custom))
	assertGoldenNamed(t, "broot", "bash-cmd", renderModule(t, "broot", "bash", custom))
}

func TestOmnishellPrompt(t *testing.T) {
	minimal := map[string]any{"style": "minimal", "show_duration": false, "char": "❯"}
	full := map[string]any{"style": "full", "show_duration": false, "char": "❯"}
	dur := map[string]any{"style": "minimal", "show_duration": true, "char": "❯"}
	assertGoldenNamed(t, "omnishell-prompt", "zsh-minimal", renderModule(t, "omnishell-prompt", "zsh", minimal))
	assertGoldenNamed(t, "omnishell-prompt", "zsh-full", renderModule(t, "omnishell-prompt", "zsh", full))
	assertGoldenNamed(t, "omnishell-prompt", "zsh-duration", renderModule(t, "omnishell-prompt", "zsh", dur))
	assertGoldenNamed(t, "omnishell-prompt", "bash-minimal", renderModule(t, "omnishell-prompt", "bash", minimal))
	assertGoldenNamed(t, "omnishell-prompt", "bash-full", renderModule(t, "omnishell-prompt", "bash", full))
}

func TestFzfTab(t *testing.T) {
	on := map[string]any{"cd_preview": true}
	off := map[string]any{"cd_preview": false}
	assertGoldenNamed(t, "fzf-tab", "zsh", renderModule(t, "fzf-tab", "zsh", on))
	assertGoldenNamed(t, "fzf-tab", "zsh-nopreview", renderModule(t, "fzf-tab", "zsh", off))
}

func TestFzfTabLoadOrder(t *testing.T) {
	reg, err := module.LoadRegistry(modules.FS(), "")
	if err != nil {
		t.Fatal(err)
	}
	active := map[string]module.Manifest{}
	for _, id := range []string{"completion", "fzf", "fzf-tab", "autosuggestions", "syntax-highlighting"} {
		m, ok := reg.Get(id)
		if !ok {
			t.Fatalf("module %q not embedded", id)
		}
		active[id] = m.Manifest
	}
	order, err := graph.Order(active)
	if err != nil {
		t.Fatalf("graph.Order: %v", err)
	}
	pos := map[string]int{}
	for i, id := range order {
		pos[id] = i
	}
	if pos["fzf-tab"] < pos["completion"] || pos["fzf-tab"] < pos["fzf"] {
		t.Fatalf("fzf-tab must load after completion and fzf: %v", order)
	}
	if pos["fzf-tab"] > pos["autosuggestions"] || pos["fzf-tab"] > pos["syntax-highlighting"] {
		t.Fatalf("fzf-tab must load before autosuggestions and syntax-highlighting: %v", order)
	}
}

func TestAtuin(t *testing.T) {
	on := map[string]any{"bind_ctrl_r": true, "bind_up_arrow": true}
	off := map[string]any{"bind_ctrl_r": false, "bind_up_arrow": false}
	assertGoldenNamed(t, "atuin", "zsh", renderModule(t, "atuin", "zsh", on))
	assertGoldenNamed(t, "atuin", "bash", renderModule(t, "atuin", "bash", on))
	assertGoldenNamed(t, "atuin", "zsh-nobinds", renderModule(t, "atuin", "zsh", off))
	assertGoldenNamed(t, "atuin", "bash-nobinds", renderModule(t, "atuin", "bash", off))
}

func TestAtuinConflictsWithFzf(t *testing.T) {
	reg, err := module.LoadRegistry(modules.FS(), "")
	if err != nil {
		t.Fatal(err)
	}
	m, ok := reg.Get("atuin")
	if !ok {
		t.Fatal("atuin not embedded")
	}
	found := false
	for _, c := range m.Manifest.Conflicts {
		if c == "fzf" {
			found = true
		}
	}
	if !found {
		t.Fatalf("atuin manifest conflicts = %v, want it to contain \"fzf\"", m.Manifest.Conflicts)
	}
}

func TestStarship(t *testing.T) {
	assertGoldenNamed(t, "starship", "zsh", renderModule(t, "starship", "zsh", nil))
	assertGoldenNamed(t, "starship", "bash", renderModule(t, "starship", "bash", nil))
}

func TestStarshipLoadOrder(t *testing.T) {
	reg, err := module.LoadRegistry(modules.FS(), "")
	if err != nil {
		t.Fatal(err)
	}
	active := map[string]module.Manifest{}
	for _, id := range []string{"completion", "fzf-tab", "fzf", "starship"} {
		m, ok := reg.Get(id)
		if !ok {
			t.Fatalf("module %q not embedded", id)
		}
		active[id] = m.Manifest
	}
	order, err := graph.Order(active)
	if err != nil {
		t.Fatalf("graph.Order: %v", err)
	}
	pos := map[string]int{}
	for i, id := range order {
		pos[id] = i
	}
	// The prompt must initialise after the completion / tab machinery.
	if pos["starship"] < pos["completion"] || pos["starship"] < pos["fzf-tab"] {
		t.Fatalf("starship must load after completion and fzf-tab: %v", order)
	}
}

func TestStarshipConflictsWithOmnishellPrompt(t *testing.T) {
	reg, err := module.LoadRegistry(modules.FS(), "")
	if err != nil {
		t.Fatal(err)
	}
	m, ok := reg.Get("starship")
	if !ok {
		t.Fatal("starship not embedded")
	}
	found := false
	for _, c := range m.Manifest.Conflicts {
		if c == "omnishell-prompt" {
			found = true
		}
	}
	if !found {
		t.Fatalf("starship manifest conflicts = %v, want it to contain \"omnishell-prompt\"", m.Manifest.Conflicts)
	}
}

func TestPayRespects(t *testing.T) {
	def := map[string]any{"alias": "f"}
	custom := map[string]any{"alias": "oops"}
	assertGoldenNamed(t, "pay-respects", "zsh", renderModule(t, "pay-respects", "zsh", def))
	assertGoldenNamed(t, "pay-respects", "bash", renderModule(t, "pay-respects", "bash", def))
	assertGoldenNamed(t, "pay-respects", "zsh-alias", renderModule(t, "pay-respects", "zsh", custom))
	assertGoldenNamed(t, "pay-respects", "bash-alias", renderModule(t, "pay-respects", "bash", custom))
}

func TestMise(t *testing.T) {
	act := map[string]any{"mode": "activate"}
	shims := map[string]any{"mode": "shims"}
	assertGoldenNamed(t, "mise", "zsh", renderModule(t, "mise", "zsh", act))
	assertGoldenNamed(t, "mise", "bash", renderModule(t, "mise", "bash", act))
	assertGoldenNamed(t, "mise", "zsh-shims", renderModule(t, "mise", "zsh", shims))
	assertGoldenNamed(t, "mise", "bash-shims", renderModule(t, "mise", "bash", shims))
}

func TestDirenv(t *testing.T) {
	def := map[string]any{"log_format": "", "whitelist": []any{}}
	opts := map[string]any{"log_format": "kv", "whitelist": []any{"/home/j/work", "/opt"}}
	assertGoldenNamed(t, "direnv", "zsh", renderModule(t, "direnv", "zsh", def))
	assertGoldenNamed(t, "direnv", "bash", renderModule(t, "direnv", "bash", def))
	assertGoldenNamed(t, "direnv", "zsh-opts", renderModule(t, "direnv", "zsh", opts))
	assertGoldenNamed(t, "direnv", "bash-opts", renderModule(t, "direnv", "bash", opts))
}

func TestColorizedMan(t *testing.T) {
	assertGolden(t, "colorized-man", "zsh", renderModule(t, "colorized-man", "zsh", nil))
	assertGolden(t, "colorized-man", "bash", renderModule(t, "colorized-man", "bash", nil))
}

func TestLsColors(t *testing.T) {
	assertGoldenNamed(t, "ls-colors", "zsh-macos", renderModuleOn(t, "ls-colors", "zsh", "macos", nil))
	assertGoldenNamed(t, "ls-colors", "zsh-linux", renderModuleOn(t, "ls-colors", "zsh", "linux", nil))
	assertGoldenNamed(t, "ls-colors", "bash-macos", renderModuleOn(t, "ls-colors", "bash", "macos", nil))
	assertGoldenNamed(t, "ls-colors", "bash-linux", renderModuleOn(t, "ls-colors", "bash", "linux", nil))
}

func TestRootLoops(t *testing.T) {
	assertGolden(t, "root-loops", "zsh", renderModule(t, "root-loops", "zsh", nil))
	assertGolden(t, "root-loops", "bash", renderModule(t, "root-loops", "bash", nil))
	assertGoldenNamed(t, "root-loops", "zsh-dark",
		renderModule(t, "root-loops", "zsh", map[string]any{"appearance": "dark"}))
	assertGoldenNamed(t, "root-loops", "zsh-light",
		renderModule(t, "root-loops", "zsh", map[string]any{"appearance": "light"}))
}

func TestPagerDefaults(t *testing.T) {
	assertGolden(t, "pager-defaults", "zsh", renderModule(t, "pager-defaults", "zsh", nil))
	assertGolden(t, "pager-defaults", "bash", renderModule(t, "pager-defaults", "bash", nil))
}

func TestWindowTitle(t *testing.T) {
	assertGolden(t, "window-title", "zsh", renderModule(t, "window-title", "zsh", nil))
	assertGolden(t, "window-title", "bash", renderModule(t, "window-title", "bash", nil))
}

func TestModernAliases(t *testing.T) {
	full := map[string]any{"replace": []any{"ls", "cat", "find"}}
	subset := map[string]any{"replace": []any{"ls", "find"}}
	assertGolden(t, "modern-aliases", "zsh", renderModule(t, "modern-aliases", "zsh", full))
	assertGolden(t, "modern-aliases", "bash", renderModule(t, "modern-aliases", "bash", full))
	assertGoldenNamed(t, "modern-aliases", "zsh-subset", renderModule(t, "modern-aliases", "zsh", subset))
}
