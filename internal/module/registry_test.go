package module_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/JtheGunner/omnishell/internal/module"
)

func TestTemplateSurfacesNonNotExistReadError(t *testing.T) {
	// "zsh.tmpl" exists as a directory, so fs.ReadFile fails with an error
	// that is not fs.ErrNotExist. Template must return that error, not (,,nil).
	m := module.Module{FS: fstest.MapFS{
		"zsh.tmpl/inner": {Data: []byte("x")},
	}}
	body, has, err := m.Template("zsh")
	if err == nil {
		t.Fatalf("want a read error, got body=%q has=%v err=nil", body, has)
	}
	if has || body != "" {
		t.Fatalf("want empty result on error, got body=%q has=%v", body, has)
	}
}

func TestTemplateMissingIsNotAnError(t *testing.T) {
	m := module.Module{FS: fstest.MapFS{}}
	body, has, err := m.Template("zsh")
	if err != nil || has || body != "" {
		t.Fatalf("missing template: body=%q has=%v err=%v", body, has, err)
	}
}

func TestLoadRegistryMergesBuiltinAndUser(t *testing.T) {
	reg, err := module.LoadRegistry(os.DirFS("testdata/builtin"), "testdata/user")
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	all := reg.All()
	if len(all) != 3 {
		t.Fatalf("modules = %d, want 3 (completion, fzf, mymod)", len(all))
	}
	if all[0].Manifest.Module.ID != "completion" || all[1].Manifest.Module.ID != "fzf" || all[2].Manifest.Module.ID != "mymod" {
		t.Fatalf("not sorted by id: %v", ids(all))
	}

	fzf, ok := reg.Get("fzf")
	if !ok {
		t.Fatal("fzf missing")
	}
	if fzf.Source != module.SourceUser || fzf.Manifest.Module.Version != "9.9.9" {
		t.Fatalf("fzf not overridden by user module: %+v", fzf.Source)
	}
	body, has, err := fzf.Template("zsh")
	if err != nil || !has {
		t.Fatalf("fzf zsh template: has=%v err=%v", has, err)
	}
	if body != "# fzf zsh (user override)\n" {
		t.Fatalf("template body = %q", body)
	}

	comp, _ := reg.Get("completion")
	if comp.Source != module.SourceBuiltin {
		t.Fatalf("completion source = %v", comp.Source)
	}
	if _, has, _ := comp.Template("zsh"); !has {
		t.Fatal("completion should have a zsh template")
	}
	if comp.HasHook("check") {
		t.Fatal("completion should have no check hook")
	}

	if got := reg.Overrides(); len(got) != 1 || got[0] != "fzf" {
		t.Fatalf("overrides = %v, want [fzf]", got)
	}
}

func TestLoadRegistrySkipsMalformedUserModule(t *testing.T) {
	dir := t.TempDir()
	// A dir with no manifest.toml.
	if err := os.MkdirAll(filepath.Join(dir, "brokennomanifest"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A dir whose folder name != module.id.
	if err := os.MkdirAll(filepath.Join(dir, "wrongname"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "wrongname", "manifest.toml"), []byte(
		"platforms=[\"linux\"]\nshells=[\"bash\"]\n[module]\nid=\"other\"\nname=\"x\"\ndescription=\"x\"\nversion=\"1\"\nschema=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A perfectly good user module.
	if err := os.MkdirAll(filepath.Join(dir, "good"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "good", "manifest.toml"), []byte(
		"platforms=[\"linux\"]\nshells=[\"bash\"]\n[module]\nid=\"good\"\nname=\"x\"\ndescription=\"x\"\nversion=\"1\"\nschema=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	reg, err := module.LoadRegistry(nil, dir)
	if err != nil {
		t.Fatalf("LoadRegistry should not fail on a malformed user module: %v", err)
	}
	if _, ok := reg.Get("good"); !ok {
		t.Fatal("the good user module did not load")
	}
	mal := reg.Malformed()
	if len(mal) != 2 {
		t.Fatalf("Malformed() = %v, want 2 entries", mal)
	}
	joined := strings.Join(mal, "\n")
	if !strings.Contains(joined, "brokennomanifest") || !strings.Contains(joined, "missing manifest.toml") {
		t.Fatalf("Malformed() missing the no-manifest dir: %v", mal)
	}
	if !strings.Contains(joined, "wrongname") {
		t.Fatalf("Malformed() missing the folder/id mismatch dir: %v", mal)
	}
}

func TestLoadRegistryBuiltinMismatchIsHardError(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "wrongname"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "wrongname", "manifest.toml"), []byte(
		"platforms=[\"linux\"]\nshells=[\"bash\"]\n[module]\nid=\"other\"\nname=\"x\"\ndescription=\"x\"\nversion=\"1\"\nschema=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := module.LoadRegistry(os.DirFS(dir), ""); err == nil {
		t.Fatal("want a hard error for a malformed built-in module")
	}
}

func ids(ms []module.Module) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.Manifest.Module.ID
	}
	return out
}
