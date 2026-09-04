package module_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/JtheGunner/omnishell/internal/module"
)

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParseManifestFzf(t *testing.T) {
	m, err := module.ParseManifest(loadFixture(t, "fzf-manifest.toml"))
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	if m.Module.ID != "fzf" || m.Module.Version != "1.0.0" || m.Module.Schema != 1 {
		t.Fatalf("meta = %+v", m.Module)
	}
	if m.Module.Homepage != "https://github.com/junegunn/fzf" {
		t.Fatalf("homepage = %q", m.Module.Homepage)
	}
	if len(m.Platforms) != 2 || len(m.Shells) != 2 {
		t.Fatalf("platforms/shells = %v / %v", m.Platforms, m.Shells)
	}
	if len(m.After) != 1 || m.After[0] != "completion" {
		t.Fatalf("after = %v", m.After)
	}
	if len(m.Packages.Brew) != 1 || m.Packages.Brew[0] != "fzf" {
		t.Fatalf("brew packages = %v", m.Packages.Brew)
	}
	if len(m.Packages.Fallback) != 1 || m.Packages.Fallback[0].Type != "git" {
		t.Fatalf("fallback = %+v", m.Packages.Fallback)
	}
	if m.Options["ctrl_r"].Type != "bool" || m.Options["ctrl_r"].Default != true {
		t.Fatalf("ctrl_r schema = %+v", m.Options["ctrl_r"])
	}
	if m.Options["replace"].Type != "list<enum>" || len(m.Options["replace"].Values) != 3 {
		t.Fatalf("replace schema = %+v", m.Options["replace"])
	}
	if err := module.ValidateManifest(m); err != nil {
		t.Fatalf("ValidateManifest: %v", err)
	}
}

func TestValidateManifestRejectsBadID(t *testing.T) {
	m, _ := module.ParseManifest(loadFixture(t, "fzf-manifest.toml"))
	m.Module.ID = "Fzf_Bad"
	if err := module.ValidateManifest(m); err == nil {
		t.Fatal("want error for bad id")
	}
}

func TestValidateManifestRejectsUnknownOptionType(t *testing.T) {
	m, _ := module.ParseManifest(loadFixture(t, "fzf-manifest.toml"))
	opt := m.Options["ctrl_r"]
	opt.Type = "float"
	m.Options["ctrl_r"] = opt
	if err := module.ValidateManifest(m); err == nil {
		t.Fatal("want error for unknown option type")
	}
}

func TestValidateManifestRejectsWrongSchema(t *testing.T) {
	m, _ := module.ParseManifest(loadFixture(t, "fzf-manifest.toml"))
	m.Module.Schema = 2
	if err := module.ValidateManifest(m); err == nil {
		t.Fatal("want error for schema 2")
	}
}

func TestParseManifestRejectsUnknownKey(t *testing.T) {
	_, err := module.ParseManifest([]byte("[module]\nid=\"x\"\nversion=\"1\"\nschema=1\nbogus=true\n"))
	if err == nil {
		t.Fatal("want error for unknown key")
	}
}
