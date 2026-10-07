package modedit_test

import (
	"errors"
	"os"
	"reflect"
	"testing"
	"testing/fstest"

	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/JtheGunner/omnishell/internal/modedit"
	"github.com/JtheGunner/omnishell/internal/module"
	"github.com/JtheGunner/omnishell/internal/pkgmgr"
)

func viewByID(t *testing.T, views []modedit.ModuleView, id string) modedit.ModuleView {
	t.Helper()
	for _, v := range views {
		if v.ID == id {
			return v
		}
	}
	t.Fatalf("no view for %q in %+v", id, views)
	return modedit.ModuleView{}
}

func TestViewsAreSortedAndCarryManifestFields(t *testing.T) {
	ed, _ := newEditor(t, true, "bash")

	views, err := ed.Views()
	if err != nil {
		t.Fatalf("Views: %v", err)
	}
	ids := make([]string, len(views))
	for i, v := range views {
		ids[i] = v.ID
	}
	if want := []string{"fzf", "plain", "zshonly"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("ids = %v, want %v", ids, want)
	}

	fzf := viewByID(t, views, "fzf")
	if fzf.Name != "FZF Fuzzy Finder" || fzf.Description != "Ctrl+R history search" ||
		fzf.Homepage != "https://github.com/junegunn/fzf" {
		t.Fatalf("fzf text fields = %+v", fzf)
	}
	if !reflect.DeepEqual(fzf.Platforms, []string{"macos", "linux"}) ||
		!reflect.DeepEqual(fzf.Shells, []string{"zsh", "bash"}) {
		t.Fatalf("fzf platforms/shells = %v / %v", fzf.Platforms, fzf.Shells)
	}
	if fzf.OptionCount != 3 {
		t.Fatalf("fzf OptionCount = %d, want 3", fzf.OptionCount)
	}
	if fzf.Origin != modedit.OriginUser {
		t.Fatalf("fzf Origin = %q, want %q", fzf.Origin, modedit.OriginUser)
	}
}

func TestViewsStatusFollowsConfig(t *testing.T) {
	ed, _ := newEditor(t, true, "bash")
	if err := ed.Enable("fzf"); err != nil {
		t.Fatalf("Enable: %v", err)
	}

	views, err := ed.Views()
	if err != nil {
		t.Fatalf("Views: %v", err)
	}
	if got := viewByID(t, views, "fzf").Status; got != modedit.StatusEnabled {
		t.Fatalf("fzf status = %q, want enabled", got)
	}
	if got := viewByID(t, views, "plain").Status; got != modedit.StatusDisabled {
		t.Fatalf("plain status = %q, want disabled", got)
	}
}

// `list` works before `init`: a missing config is "no config", not an error.
func TestViewsWithoutConfigReportUnknownStatus(t *testing.T) {
	ed, _ := newEditor(t, false, "bash")

	views, err := ed.Views()
	if err != nil {
		t.Fatalf("Views: %v", err)
	}
	if len(views) == 0 {
		t.Fatal("expected the fixture modules")
	}
	for _, v := range views {
		if v.Status != modedit.StatusUnknown {
			t.Fatalf("%s status = %q, want %q", v.ID, v.Status, modedit.StatusUnknown)
		}
	}
}

// A broken config must surface as an error, never as an all-disabled list.
func TestViewsMalformedConfigIsAnError(t *testing.T) {
	ed, cfgPath := newEditor(t, true, "bash")
	if err := os.WriteFile(cfgPath, []byte("not = [toml"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	views, err := ed.Views()
	if err == nil || errors.Is(err, config.ErrNotFound) {
		t.Fatalf("err = %v, want a parse error that is not ErrNotFound", err)
	}
	if views != nil {
		t.Fatalf("views = %+v, want nil on error", views)
	}
}

func TestViewsPackageState(t *testing.T) {
	ed, _ := newEditor(t, true, "bash")
	mgr := ed.Engine.Manager.(*pkgmgr.MockManager)

	views, err := ed.Views()
	if err != nil {
		t.Fatalf("Views: %v", err)
	}
	if got := viewByID(t, views, "fzf").Packages; got != modedit.PackagesMissing {
		t.Fatalf("fzf packages = %q, want missing", got)
	}
	if got := viewByID(t, views, "plain").Packages; got != modedit.PackagesNA {
		t.Fatalf("plain packages = %q, want n/a", got)
	}

	mgr.Installed["fzf"] = true
	views, err = ed.Views()
	if err != nil {
		t.Fatalf("Views: %v", err)
	}
	if got := viewByID(t, views, "fzf").Packages; got != modedit.PackagesOK {
		t.Fatalf("fzf packages = %q, want ok", got)
	}
}

func TestViewsPackagesAreNAWithoutPackageManager(t *testing.T) {
	ed, _ := newEditor(t, true, "bash")
	ed.Engine.ManagerOK = false

	views, err := ed.Views()
	if err != nil {
		t.Fatalf("Views: %v", err)
	}
	if got := viewByID(t, views, "fzf").Packages; got != modedit.PackagesNA {
		t.Fatalf("fzf packages = %q, want n/a", got)
	}
}

func TestViewsMarksBuiltinOverriddenByUserModule(t *testing.T) {
	manifest, err := os.ReadFile("testdata/modules/fzf/manifest.toml")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	builtin := fstest.MapFS{"fzf/manifest.toml": {Data: manifest}, "zzz/manifest.toml": {Data: []byte(`
platforms = ["macos", "linux"]
shells    = ["zsh", "bash"]
requires  = []
after     = []

[module]
id          = "zzz"
name        = "Built-in only"
description = "Only in the built-in set"
version     = "1.0.0"
schema      = 1
`)}}
	reg, err := module.LoadRegistry(builtin, "testdata/modules")
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	ed, _ := newEditor(t, true, "bash")
	ed.Engine.Registry = reg

	views, err := ed.Views()
	if err != nil {
		t.Fatalf("Views: %v", err)
	}
	if got := viewByID(t, views, "fzf").Origin; got != modedit.OriginUserOverride {
		t.Fatalf("fzf origin = %q, want %q", got, modedit.OriginUserOverride)
	}
	if got := viewByID(t, views, "zzz").Origin; got != modedit.OriginBuiltin {
		t.Fatalf("zzz origin = %q, want %q", got, modedit.OriginBuiltin)
	}
	if got := viewByID(t, views, "plain").Origin; got != modedit.OriginUser {
		t.Fatalf("plain origin = %q, want %q", got, modedit.OriginUser)
	}
}

func TestViewsEmptyRegistryIsEmptyNotNil(t *testing.T) {
	reg, err := module.LoadRegistry(nil, "")
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	ed, _ := newEditor(t, true, "bash")
	ed.Engine.Registry = reg

	views, err := ed.Views()
	if err != nil {
		t.Fatalf("Views: %v", err)
	}
	if views == nil || len(views) != 0 {
		t.Fatalf("views = %#v, want empty non-nil slice", views)
	}
}
