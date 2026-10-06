package lockfile_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JtheGunner/omnishell/internal/lockfile"
)

func sample() lockfile.Lock {
	return lockfile.Lock{
		Schema:           lockfile.SchemaVersion,
		OmnishellVersion: "1.0.0",
		LastApply:        "2026-09-02T22:41:03Z",
		Platform:         "macos",
		PackageManager:   "brew",
		Modules: map[string]lockfile.ModuleState{
			"fzf": {
				ModuleVersion:  "1.0.0",
				Enabled:        true,
				OptionsHash:    "sha256:abc",
				ShellsRendered: []string{"zsh", "bash"},
				Packages: []lockfile.PackageState{
					{Name: "fzf", Manager: "brew", InstalledByOmnishell: true},
					{Name: "ncurses", Manager: "brew", InstalledByOmnishell: false},
				},
				Status: "ok",
			},
		},
		InitFiles: map[string]lockfile.FileState{
			"zsh": {Path: "~/.config/omnishell/init.zsh", ContentHash: "sha256:0f3a"},
		},
		RCFiles: map[string]lockfile.RCState{
			"zsh": {Path: "~/.zshrc", BlockPresent: true},
		},
	}
}

func TestWriteThenLoadRoundTrips(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.lock.json")
	if err := sample().Write(p); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, exists, err := lockfile.Load(p)
	if err != nil || !exists {
		t.Fatalf("Load: exists=%v err=%v", exists, err)
	}
	if got.Modules["fzf"].OptionsHash != "sha256:abc" || got.Modules["fzf"].Status != "ok" {
		t.Fatalf("round-trip mismatch: %+v", got.Modules["fzf"])
	}
}

func TestLoadMissingReturnsEmptyLock(t *testing.T) {
	got, exists, err := lockfile.Load(filepath.Join(t.TempDir(), "absent.json"))
	if err != nil {
		t.Fatalf("Load missing: %v", err)
	}
	if exists {
		t.Fatal("exists = true for missing file")
	}
	if got.Schema != lockfile.SchemaVersion || got.Modules == nil {
		t.Fatalf("empty lock not initialised: %+v", got)
	}
}

func TestInstalledPackagesFilters(t *testing.T) {
	got := sample().InstalledPackages("fzf")
	if len(got) != 1 || got[0].Name != "fzf" {
		t.Fatalf("InstalledPackages = %+v, want just fzf", got)
	}
}

func TestFallbackRefRoundTripsAndIsOptional(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.lock.json")
	l := sample()
	st := l.Modules["fzf"]
	st.FallbackRef = "v0.74.4"
	l.Modules["fzf"] = st
	if err := l.Write(path); err != nil {
		t.Fatal(err)
	}
	got, ok, err := lockfile.Load(path)
	if err != nil || !ok {
		t.Fatalf("Load: ok=%v err=%v", ok, err)
	}
	if got.Modules["fzf"].FallbackRef != "v0.74.4" {
		t.Fatalf("FallbackRef = %q, want v0.74.4", got.Modules["fzf"].FallbackRef)
	}

	// A lockfile written before this field existed must read as "unknown", and
	// an empty value must not be written at all.
	st.FallbackRef = ""
	l.Modules["fzf"] = st
	if err := l.Write(path); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "fallback_ref") {
		t.Fatalf("an empty FallbackRef must be omitted:\n%s", raw)
	}
	got, _, err = lockfile.Load(path)
	if err != nil || got.Modules["fzf"].FallbackRef != "" {
		t.Fatalf("legacy lockfile: FallbackRef=%q err=%v", got.Modules["fzf"].FallbackRef, err)
	}
}
