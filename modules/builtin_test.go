package modules_test

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/JtheGunner/omnishell/internal/module"
	"github.com/JtheGunner/omnishell/internal/render"
	"github.com/JtheGunner/omnishell/modules"
)

var update = flag.Bool("update", false, "update golden files")

func renderModule(t *testing.T, id, shell string, opts map[string]any) string {
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

func assertGolden(t *testing.T, id, shell, got string) {
	t.Helper()
	p := filepath.Join("testdata", id, shell+".golden")
	if *update {
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(got), 0o644)
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
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(got), 0o644)
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

func TestModernAliases(t *testing.T) {
	full := map[string]any{"replace": []any{"ls", "cat", "find"}}
	subset := map[string]any{"replace": []any{"ls", "find"}}
	assertGolden(t, "modern-aliases", "zsh", renderModule(t, "modern-aliases", "zsh", full))
	assertGolden(t, "modern-aliases", "bash", renderModule(t, "modern-aliases", "bash", full))
	assertGoldenNamed(t, "modern-aliases", "zsh-subset", renderModule(t, "modern-aliases", "zsh", subset))
}
