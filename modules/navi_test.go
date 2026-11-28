package modules_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/JtheGunner/omnishell/internal/graph"
	"github.com/JtheGunner/omnishell/internal/module"
	"github.com/JtheGunner/omnishell/modules"
)

func naviModule(t *testing.T) module.Module {
	t.Helper()
	reg, err := module.LoadRegistry(modules.FS(), "")
	if err != nil {
		t.Fatal(err)
	}
	m, ok := reg.Get("navi")
	if !ok {
		t.Fatal("module navi not embedded")
	}
	return m
}

func TestNaviGolden(t *testing.T) {
	custom := map[string]any{"key": "alt-n"}
	for _, shell := range []string{"zsh", "bash"} {
		assertGoldenNamed(t, "navi", shell+"-default", renderModule(t, "navi", shell, nil))
		assertGoldenNamed(t, "navi", shell+"-alt-n", renderModule(t, "navi", shell, custom))
	}
}

func TestNaviCustomKeyMapping(t *testing.T) {
	cases := []struct {
		key, shell, want string
	}{
		{"ctrl-x", "zsh", "bindkey '^x' _navi_widget"},
		{"alt-x", "zsh", "bindkey '^[x' _navi_widget"},
		{"ctrl-x", "bash", `bind -x '"\C-x": _navi_widget'`},
		{"alt-x", "bash", `bind -x '"\ex": _navi_widget'`},
	}
	for _, c := range cases {
		out := renderModule(t, "navi", c.shell, map[string]any{"key": c.key})
		if !strings.Contains(out, c.want) {
			t.Errorf("%s %s: missing %q in:\n%s", c.shell, c.key, c.want, out)
		}
	}
	zsh := renderModule(t, "navi", "zsh", map[string]any{"key": "alt-n"})
	if !strings.Contains(zsh, "bindkey '^g' send-break") {
		t.Errorf("zsh custom key must restore Ctrl+G:\n%s", zsh)
	}
	bash := renderModule(t, "navi", "bash", map[string]any{"key": "alt-n"})
	if !strings.Contains(bash, `bind '"\C-g": abort'`) {
		t.Errorf("bash custom key must restore Ctrl+G:\n%s", bash)
	}
}

func TestNaviManifest(t *testing.T) {
	m := naviModule(t)
	if got := m.Manifest.Requires; len(got) != 1 || got[0] != "fzf" {
		t.Errorf("requires = %v, want [fzf]", got)
	}
	if got := m.Manifest.Shells; len(got) != 2 {
		t.Errorf("shells = %v, want zsh and bash", got)
	}
	for _, mgr := range []string{"brew", "apt", "dnf", "pacman", "zypper", "apk"} {
		if got := m.Manifest.Packages.ForManager(mgr); len(got) != 1 || got[0] != "navi" {
			t.Errorf("%s packages = %v, want [navi]", mgr, got)
		}
	}
	fbs := m.Manifest.Packages.Fallback
	if len(fbs) != 1 || fbs[0].Type != "release" || fbs[0].Bin != "navi" || fbs[0].Ref != "v2.24.0" {
		t.Fatalf("fallbacks = %+v, want one release entry for navi v2.24.0", fbs)
	}
	for _, c := range []struct {
		arch, goarm string
		want        bool
	}{
		{"amd64", "", true},
		{"arm64", "", true},
		{"arm", "7", true},
		{"arm", "6", false},
		{"arm", "", false},
	} {
		asset, got := fbs[0].AssetFor("linux", c.arch, c.goarm)
		if got != c.want {
			t.Errorf("linux/%s goarm=%q: release asset = %v, want %v", c.arch, c.goarm, got, c.want)
			continue
		}
		if got && (len(asset.SHA256) != 64 || asset.Member != "navi") {
			t.Errorf("linux/%s: asset needs a sha256 and member navi, got %+v", c.arch, asset)
		}
	}
	if _, ok := fbs[0].AssetFor("darwin", "arm64", ""); ok {
		t.Error("navi has no macOS release asset; Homebrew provides it")
	}
}

func TestNaviLoadsAfterFzfAndAtuin(t *testing.T) {
	reg, err := module.LoadRegistry(modules.FS(), "")
	if err != nil {
		t.Fatal(err)
	}
	active := map[string]module.Manifest{}
	// atuin conflicts with fzf, so it cannot be active next to navi's required
	// fzf; its soft ordering is pinned through the manifest below.
	for _, id := range []string{"navi", "fzf", "completion"} {
		m, ok := reg.Get(id)
		if !ok {
			t.Fatalf("module %s missing", id)
		}
		active[id] = m.Manifest
	}
	order, err := graph.Order(active)
	if err != nil {
		t.Fatal(err)
	}
	pos := map[string]int{}
	for i, id := range order {
		pos[id] = i
	}
	for _, dep := range []string{"fzf", "completion"} {
		if pos["navi"] < pos[dep] {
			t.Errorf("navi loads before %s: %v", dep, order)
		}
	}
	after := strings.Join(active["navi"].After, ",")
	if !strings.Contains(after, "atuin") {
		t.Errorf("navi after = %q, want atuin included", after)
	}
}

func TestNaviRequiresFzfActive(t *testing.T) {
	m := naviModule(t)
	if _, err := graph.Order(map[string]module.Manifest{"navi": m.Manifest}); err == nil {
		t.Error("navi without fzf must be a dependency error")
	}
}

func TestNaviRejectsBadKeys(t *testing.T) {
	m := naviModule(t)
	for _, key := range []string{"ctrl-gg", "meta-g", "ctrl-1", "x; rm -rf ~", "", "Ctrl-g", "ctrl-G", "alt-"} {
		if _, err := module.ValidateOptions(m.Manifest.Options, map[string]any{"key": key}); err == nil {
			t.Errorf("key %q accepted, want rejection", key)
		}
	}
	for _, key := range []string{"ctrl-g", "ctrl-x", "alt-n"} {
		if _, err := module.ValidateOptions(m.Manifest.Options, map[string]any{"key": key}); err != nil {
			t.Errorf("key %q rejected: %v", key, err)
		}
	}
}

// The default key must not be bound by the modules navi sits next to. atuin's
// widget bindings come from `atuin init` at runtime and use Ctrl+R and the up
// arrow only, so its template must stay free of Ctrl+G as well.
func TestNaviDefaultKeyIsFree(t *testing.T) {
	taken := regexp.MustCompile(`(?i)\\C-g|\^g|"\\C-g"|\\x07|ctrl-g`)
	for _, id := range []string{"fzf", "atuin", "history"} {
		for _, shell := range []string{"zsh", "bash"} {
			opts := map[string]any{}
			if id == "fzf" {
				opts["ctrl_t"] = true
			}
			out := renderModule(t, id, shell, opts)
			if loc := taken.FindString(out); loc != "" {
				t.Errorf("%s/%s binds %q, which navi uses by default:\n%s", id, shell, loc, out)
			}
		}
	}
}

// Sources the real bash snippet with a stub navi that prints a minimal widget
// definition, and checks that the binding lands on the expected key.
func TestNaviBashSnippetInRealBash(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not installed")
	}
	if out, err := exec.Command(bash, "-c", "echo ${BASH_VERSINFO[0]}").Output(); err != nil || strings.TrimSpace(string(out)) < "4" {
		t.Skip("bash 4 or newer required for bind -x")
	}
	dir := t.TempDir()
	stub := "#!/bin/sh\n" +
		"if [ \"$1 $2\" = \"widget bash\" ]; then\n" +
		"  echo '_navi_widget() { :; }'\n" +
		"  echo 'bind -x \"\\\"\\C-g\\\": _navi_widget\"'\n" +
		"fi\n"
	if err := os.WriteFile(filepath.Join(dir, "navi"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		key  string
		want string
		not  string
	}{
		{"ctrl-g", `"\C-g"`, ""},
		{"alt-n", `"\en"`, `"\C-g"`},
	} {
		snippet := renderModule(t, "navi", "bash", map[string]any{"key": c.key})
		script := snippet + "\nbind -X\n"
		cmd := exec.Command(bash, "--norc", "--noprofile", "-i", "-c", script)
		cmd.Env = append(os.Environ(), "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"))
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s: bash failed: %v\n%s", c.key, err, out)
		}
		text := string(out)
		if !strings.Contains(text, c.want) {
			t.Errorf("%s: output lacks %q:\n%s", c.key, c.want, text)
		}
		if c.not != "" && strings.Contains(text, c.not) {
			t.Errorf("%s: output still has %q:\n%s", c.key, c.not, text)
		}
	}
}

func TestNaviSnippetsParse(t *testing.T) {
	for _, shell := range []string{"zsh", "bash"} {
		path, err := exec.LookPath(shell)
		if err != nil {
			continue
		}
		for _, key := range []string{"ctrl-g", "alt-n", "ctrl-x"} {
			snippet := renderModule(t, "navi", shell, map[string]any{"key": key})
			if out, err := exec.Command(path, "-n", "-c", snippet).CombinedOutput(); err != nil {
				t.Errorf("%s -n (%s): %v\n%s", shell, key, err, out)
			}
		}
	}
}
