package module_test

import (
	"os"
	"testing"

	"github.com/JtheGunner/omnishell/internal/module"
)

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

func TestLoadRegistryRejectsFolderNameMismatch(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(dir+"/wrongname", 0o755)
	os.WriteFile(dir+"/wrongname/manifest.toml", []byte(
		"platforms=[\"linux\"]\nshells=[\"bash\"]\n[module]\nid=\"other\"\nname=\"x\"\ndescription=\"x\"\nversion=\"1\"\nschema=1\n"), 0o644)
	if _, err := module.LoadRegistry(os.DirFS(t.TempDir()), dir); err == nil {
		t.Fatal("want error when folder name != module.id")
	}
}

func ids(ms []module.Module) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.Manifest.Module.ID
	}
	return out
}
