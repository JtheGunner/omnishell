package modedit_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/JtheGunner/omnishell/internal/config"
	"github.com/JtheGunner/omnishell/internal/engine"
	"github.com/JtheGunner/omnishell/internal/modedit"
	"github.com/JtheGunner/omnishell/internal/module"
	"github.com/JtheGunner/omnishell/internal/pkgmgr"
	"github.com/JtheGunner/omnishell/internal/platform"
)

// cheatModule describes one fixture module for the cheatsheet tests.
type cheatModule struct {
	platforms  []string
	shells     []string
	extra      string // extra manifest TOML (options, packages)
	cheatsheet string // body of cheatsheet.tmpl; "" means no file
}

func (m cheatModule) manifest(id string) string {
	quote := func(ss []string) string {
		q := make([]string, len(ss))
		for i, s := range ss {
			q[i] = `"` + s + `"`
		}
		return "[" + strings.Join(q, ", ") + "]"
	}
	platforms, shells := m.platforms, m.shells
	if platforms == nil {
		platforms = []string{"macos", "linux"}
	}
	if shells == nil {
		shells = []string{"zsh", "bash"}
	}
	return "platforms = " + quote(platforms) + "\nshells = " + quote(shells) +
		"\nrequires = []\nafter = []\n\n[module]\nid = \"" + id + "\"\nname = \"Name " + id +
		"\"\ndescription = \"Desc " + id + "\"\nversion = \"1.0.0\"\nschema = 1\n" + m.extra
}

// cheatFS builds the files of the given modules as a fake module tree.
func cheatFS(mods map[string]cheatModule) fstest.MapFS {
	fsys := fstest.MapFS{}
	for id, m := range mods {
		fsys[id+"/manifest.toml"] = &fstest.MapFile{Data: []byte(m.manifest(id))}
		if m.cheatsheet != "" {
			fsys[id+"/cheatsheet.tmpl"] = &fstest.MapFile{Data: []byte(m.cheatsheet)}
		}
	}
	return fsys
}

// newCheatEditor builds an Editor over the given built-in modules with cfg as
// config.toml (no file when cfg is empty) on a fake Linux host that has the
// named shells present.
func newCheatEditor(t *testing.T, mods map[string]cheatModule, cfg string, shells ...string) (modedit.Editor, string) {
	t.Helper()
	reg, err := module.LoadRegistry(cheatFS(mods), "")
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	present := map[string]bool{}
	for _, s := range shells {
		present[s] = true
	}
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	if cfg != "" {
		if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
			t.Fatalf("write config: %v", err)
		}
	}
	return modedit.Editor{
		Engine: engine.Engine{
			Platform: platform.Info{
				OS:        platform.Linux,
				HomeDir:   t.TempDir(),
				ConfigDir: t.TempDir(),
				Shells: []platform.ShellInfo{
					{Name: "zsh", RCPath: "/x/.zshrc", Present: present["zsh"]},
					{Name: "bash", RCPath: "/x/.bashrc", Present: present["bash"]},
				},
			},
			Registry:  reg,
			Manager:   &pkgmgr.MockManager{NameV: "apt", DetectV: true, Installed: map[string]bool{}},
			ManagerOK: true,
			Runner:    &pkgmgr.MockRunner{},
		},
		CfgPath: cfgPath,
	}, cfgPath
}

// enabled returns a config enabling the given module ids.
func enabled(ids ...string) string {
	var b strings.Builder
	b.WriteString(baseConfig)
	for _, id := range ids {
		b.WriteString("\n[modules." + id + "]\nenabled = true\n")
	}
	return b.String()
}

func entryIDs(c modedit.Cheatsheet) []string {
	ids := make([]string, len(c.Entries))
	for i, e := range c.Entries {
		ids[i] = e.ID
	}
	return ids
}

func entryByID(t *testing.T, c modedit.Cheatsheet, id string) modedit.CheatsheetEntry {
	t.Helper()
	for _, e := range c.Entries {
		if e.ID == id {
			return e
		}
	}
	t.Fatalf("no entry %q in %+v", id, c.Entries)
	return modedit.CheatsheetEntry{}
}

func TestCheatsheetListsOnlyActiveModulesForShell(t *testing.T) {
	mods := map[string]cheatModule{
		"on":        {cheatsheet: "on\n"},
		"also":      {cheatsheet: "also\n"},
		"off":       {cheatsheet: "off\n"},
		"macosonly": {platforms: []string{"macos"}, cheatsheet: "mac\n"},
		"zshonly":   {shells: []string{"zsh"}, cheatsheet: "zsh\n"},
	}
	ed, _ := newCheatEditor(t, mods, enabled("on", "also", "macosonly", "zshonly"), "bash")

	got, err := ed.Cheatsheet(modedit.CheatsheetOptions{})
	if err != nil {
		t.Fatalf("Cheatsheet: %v", err)
	}
	if want := []string{"also", "on"}; !reflect.DeepEqual(entryIDs(got), want) {
		t.Fatalf("entries = %v, want %v", entryIDs(got), want)
	}
	if got.Shell != "bash" {
		t.Fatalf("Shell = %q, want bash", got.Shell)
	}
	e := entryByID(t, got, "on")
	if e.Name != "Name on" || e.Description != "Desc on" || e.Body != "on\n" {
		t.Fatalf("entry = %+v", e)
	}
}

func TestCheatsheetUsesEffectiveOptions(t *testing.T) {
	mods := map[string]cheatModule{"aliases": {
		extra:      "\n[options.replace]\ntype = \"list<string>\"\ndefault = [\"ls\"]\nhelp = \"x\"\n",
		cheatsheet: `{{if has_item .Options.replace "ls"}}ls-on{{else}}ls-off{{end}}`,
	}}

	ed, _ := newCheatEditor(t, mods, enabled("aliases"), "bash")
	got, err := ed.Cheatsheet(modedit.CheatsheetOptions{})
	if err != nil {
		t.Fatalf("Cheatsheet: %v", err)
	}
	if body := entryByID(t, got, "aliases").Body; body != "ls-on\n" {
		t.Fatalf("default body = %q, want ls-on", body)
	}

	cfg := enabled("aliases") + "[modules.aliases.options]\nreplace = [\"cat\"]\n"
	ed, _ = newCheatEditor(t, mods, cfg, "bash")
	got, err = ed.Cheatsheet(modedit.CheatsheetOptions{})
	if err != nil {
		t.Fatalf("Cheatsheet: %v", err)
	}
	if body := entryByID(t, got, "aliases").Body; body != "ls-off\n" {
		t.Fatalf("configured body = %q, want ls-off", body)
	}
}

func TestCheatsheetShellSelection(t *testing.T) {
	mods := map[string]cheatModule{"m": {cheatsheet: `{{.Shell}}`}}
	ed, _ := newCheatEditor(t, mods, enabled("m"), "zsh", "bash")

	cases := []struct {
		name string
		opts modedit.CheatsheetOptions
		want string
	}{
		{"flag wins", modedit.CheatsheetOptions{Shell: "bash", EnvShell: "zsh"}, "bash"},
		{"env shell when managed", modedit.CheatsheetOptions{EnvShell: "bash"}, "bash"},
		{"first managed shell", modedit.CheatsheetOptions{}, "zsh"},
		{"unmanaged env shell falls back", modedit.CheatsheetOptions{EnvShell: "fish"}, "zsh"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ed.Cheatsheet(tc.opts)
			if err != nil {
				t.Fatalf("Cheatsheet: %v", err)
			}
			if body := entryByID(t, got, "m").Body; got.Shell != tc.want || body != tc.want+"\n" {
				t.Fatalf("Shell = %q body = %q, want %q", got.Shell, body, tc.want)
			}
		})
	}

	_, err := ed.Cheatsheet(modedit.CheatsheetOptions{Shell: "fish"})
	var cfgErr config.Error
	if !errors.As(err, &cfgErr) || !strings.Contains(err.Error(), "fish") ||
		!strings.Contains(err.Error(), "zsh, bash") {
		t.Fatalf("err = %v, want a config.Error naming fish and the managed shells", err)
	}

	noShell, _ := newCheatEditor(t, mods, enabled("m"))
	if _, err := noShell.Cheatsheet(modedit.CheatsheetOptions{}); !errors.As(err, &cfgErr) {
		t.Fatalf("err = %v, want a config.Error when no shell is managed", err)
	}
}

func TestCheatsheetMissingConfigIsAConfigError(t *testing.T) {
	ed, _ := newCheatEditor(t, map[string]cheatModule{"m": {}}, "", "bash")

	_, err := ed.Cheatsheet(modedit.CheatsheetOptions{})
	var cfgErr config.Error
	if !errors.As(err, &cfgErr) || !strings.Contains(err.Error(), "omnishell init") {
		t.Fatalf("err = %v, want a config.Error hinting at omnishell init", err)
	}
}

func TestCheatsheetInvalidOptionIsAConfigError(t *testing.T) {
	mods := map[string]cheatModule{"m": {
		extra:      "\n[options.n]\ntype = \"int\"\ndefault = 1\nhelp = \"x\"\n",
		cheatsheet: "x",
	}}
	ed, _ := newCheatEditor(t, mods, enabled("m")+"[modules.m.options]\nn = \"many\"\n", "bash")

	_, err := ed.Cheatsheet(modedit.CheatsheetOptions{})
	var ce engine.ConfigError
	if !errors.As(err, &ce) || !strings.Contains(err.Error(), `module "m"`) {
		t.Fatalf("err = %v, want an engine.ConfigError naming the module", err)
	}
}

func TestCheatsheetIgnoresUnknownModuleIDInConfig(t *testing.T) {
	ed, _ := newCheatEditor(t, map[string]cheatModule{"m": {cheatsheet: "m"}}, enabled("m", "ghost"), "bash")

	got, err := ed.Cheatsheet(modedit.CheatsheetOptions{})
	if err != nil {
		t.Fatalf("Cheatsheet: %v", err)
	}
	if want := []string{"m"}; !reflect.DeepEqual(entryIDs(got), want) {
		t.Fatalf("entries = %v, want %v", entryIDs(got), want)
	}
}

func TestCheatsheetIDsMustBeActive(t *testing.T) {
	mods := map[string]cheatModule{
		"on":  {cheatsheet: "on"},
		"off": {cheatsheet: "off"},
		"zsh": {shells: []string{"zsh"}, cheatsheet: "z"},
	}
	ed, _ := newCheatEditor(t, mods, enabled("on", "zsh"), "bash")

	got, err := ed.Cheatsheet(modedit.CheatsheetOptions{IDs: []string{"on"}})
	if err != nil || !reflect.DeepEqual(entryIDs(got), []string{"on"}) {
		t.Fatalf("got %v err %v, want only on", entryIDs(got), err)
	}

	for _, id := range []string{"off", "zsh", "nope"} {
		_, err := ed.Cheatsheet(modedit.CheatsheetOptions{IDs: []string{"on", id}})
		var cfgErr config.Error
		if !errors.As(err, &cfgErr) || !strings.Contains(err.Error(), `"`+id+`"`) {
			t.Fatalf("id %q: err = %v, want a config.Error naming it", id, err)
		}
	}
}

func TestCheatsheetModuleWithoutFileLandsInNoCheatsheet(t *testing.T) {
	mods := map[string]cheatModule{"b": {}, "a": {}, "c": {cheatsheet: "c"}}
	ed, _ := newCheatEditor(t, mods, enabled("a", "b", "c"), "bash")

	got, err := ed.Cheatsheet(modedit.CheatsheetOptions{})
	if err != nil {
		t.Fatalf("Cheatsheet: %v", err)
	}
	if want := []string{"a", "b"}; !reflect.DeepEqual(got.NoCheatsheet, want) {
		t.Fatalf("NoCheatsheet = %v, want %v", got.NoCheatsheet, want)
	}
	if want := []string{"c"}; !reflect.DeepEqual(entryIDs(got), want) {
		t.Fatalf("entries = %v, want %v", entryIDs(got), want)
	}
}

func TestCheatsheetRenderFailureIsCollectedNotFatal(t *testing.T) {
	mods := map[string]cheatModule{
		"bad":  {cheatsheet: "{{.Options.nope}}"},
		"good": {cheatsheet: "fine"},
	}
	ed, _ := newCheatEditor(t, mods, enabled("bad", "good"), "bash")

	got, err := ed.Cheatsheet(modedit.CheatsheetOptions{})
	if err != nil {
		t.Fatalf("Cheatsheet: %v", err)
	}
	if len(got.Failed) != 1 || got.Failed[0].ID != "bad" || got.Failed[0].Err == nil {
		t.Fatalf("Failed = %+v, want one failure for bad", got.Failed)
	}
	if want := []string{"good"}; !reflect.DeepEqual(entryIDs(got), want) {
		t.Fatalf("entries = %v, want %v", entryIDs(got), want)
	}
}

func TestCheatsheetUserOverrideWins(t *testing.T) {
	builtin := cheatFS(map[string]cheatModule{"m": {cheatsheet: "builtin"}})
	userDir := t.TempDir()
	user := cheatModule{cheatsheet: "user"}
	if err := os.MkdirAll(filepath.Join(userDir, "m"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{"manifest.toml": user.manifest("m"), "cheatsheet.tmpl": user.cheatsheet}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(userDir, "m", name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	reg, err := module.LoadRegistry(builtin, userDir)
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	ed, _ := newCheatEditor(t, nil, enabled("m"), "bash")
	ed.Engine.Registry = reg

	got, err := ed.Cheatsheet(modedit.CheatsheetOptions{})
	if err != nil {
		t.Fatalf("Cheatsheet: %v", err)
	}
	if body := entryByID(t, got, "m").Body; body != "user\n" {
		t.Fatalf("body = %q, want the user module's", body)
	}
}

func TestCheatsheetReportsMissingPackages(t *testing.T) {
	mods := map[string]cheatModule{
		"needs": {extra: "\n[packages]\napt = [\"tool\"]\n", cheatsheet: "n"},
		"free":  {cheatsheet: "f"},
	}
	ed, _ := newCheatEditor(t, mods, enabled("needs", "free"), "bash")

	got, err := ed.Cheatsheet(modedit.CheatsheetOptions{})
	if err != nil {
		t.Fatalf("Cheatsheet: %v", err)
	}
	if !entryByID(t, got, "needs").PackagesMissing {
		t.Fatal("needs: PackagesMissing = false, want true")
	}
	if entryByID(t, got, "free").PackagesMissing {
		t.Fatal("free: PackagesMissing = true, want false")
	}

	ed.Engine.Manager.(*pkgmgr.MockManager).Installed["tool"] = true
	got, err = ed.Cheatsheet(modedit.CheatsheetOptions{})
	if err != nil {
		t.Fatalf("Cheatsheet: %v", err)
	}
	if entryByID(t, got, "needs").PackagesMissing {
		t.Fatal("needs installed: PackagesMissing = true, want false")
	}
}

func TestCheatsheetWritesNothing(t *testing.T) {
	mods := map[string]cheatModule{"m": {cheatsheet: "m"}, "n": {}}
	ed, cfgPath := newCheatEditor(t, mods, enabled("m", "n"), "bash")

	snapshot := func() (string, []string) {
		data, err := os.ReadFile(cfgPath)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		roots := []string{filepath.Dir(cfgPath), ed.Engine.Platform.ConfigDir, ed.Engine.Platform.HomeDir}
		for _, root := range roots {
			err := filepath.WalkDir(root, func(p string, _ os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				names = append(names, p)
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		}
		sort.Strings(names)
		return string(data), names
	}
	beforeData, beforeNames := snapshot()

	if _, err := ed.Cheatsheet(modedit.CheatsheetOptions{}); err != nil {
		t.Fatalf("Cheatsheet: %v", err)
	}

	afterData, afterNames := snapshot()
	if beforeData != afterData || !reflect.DeepEqual(beforeNames, afterNames) {
		t.Fatalf("Cheatsheet wrote to disk: config %q -> %q, files %v -> %v",
			beforeData, afterData, beforeNames, afterNames)
	}
}
