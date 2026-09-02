package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/JtheGunner/omnishell/internal/initfile"
	"github.com/JtheGunner/omnishell/internal/lockfile"
	"github.com/JtheGunner/omnishell/internal/module"
	"github.com/JtheGunner/omnishell/internal/platform"
)

func TestHomeRelative(t *testing.T) {
	e := Engine{Platform: platform.Info{HomeDir: "/home/j"}}
	cases := map[string]string{
		"/home/j/.bashrc":                  "$HOME/.bashrc",
		"/home/j/.config/omnishell/init.b": "$HOME/.config/omnishell/init.b",
		"/home/j":                          "$HOME",
		"/etc/profile":                     "/etc/profile",
		"/home/jsmith/.bashrc":             "/home/jsmith/.bashrc", // no false prefix match
	}
	for in, want := range cases {
		if got := e.homeRelative(in); got != want {
			t.Errorf("homeRelative(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestInitOrRCDrift(t *testing.T) {
	reg, err := module.LoadRegistry(nil, "testdata/modules")
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	e := Engine{
		Platform: platform.Info{
			OS:        platform.Linux,
			HomeDir:   home,
			ConfigDir: filepath.Join(home, ".config", "omnishell"),
			Shells:    []platform.ShellInfo{{Name: "bash", RCPath: filepath.Join(home, ".bashrc"), Present: true}},
		},
		Registry: reg,
	}
	m, _ := reg.Get("completion")

	plan := Plan{
		Order:         []string{"completion"},
		ManagedShells: []string{"bash"},
		Modules: map[string]ModulePlan{
			"completion": {
				ID:       "completion",
				Action:   ActionUnchanged,
				Shells:   []string{"bash"},
				Manifest: m.Manifest,
			},
		},
	}

	rendered := e.renderAll(plan, map[string]string{})
	sections := e.buildSections(plan, rendered, map[string]string{}, "bash")
	wantHash := initfile.ContentHash(sections)

	// Lock matches init hash but rc block is absent → drift.
	lock := lockfile.Lock{InitFiles: map[string]lockfile.FileState{"bash": {ContentHash: wantHash}}}
	if !e.initOrRCDrift(plan, lock) {
		t.Fatal("expected drift when rc block missing")
	}

	// Add the rc block → no drift.
	if err := os.WriteFile(filepath.Join(home, ".bashrc"),
		[]byte("# >>> omnishell >>>\nx\n# <<< omnishell <<<\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if e.initOrRCDrift(plan, lock) {
		t.Fatal("expected no drift when hash matches and rc block present")
	}

	// Wrong lock hash → drift.
	bad := lockfile.Lock{InitFiles: map[string]lockfile.FileState{"bash": {ContentHash: "sha256:deadbeef"}}}
	if !e.initOrRCDrift(plan, bad) {
		t.Fatal("expected drift when init hash differs from lock")
	}
}
