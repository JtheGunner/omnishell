package tui

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/JtheGunner/omnishell/internal/modedit"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata/")

func sampleViews() []modedit.ModuleView {
	return []modedit.ModuleView{
		{
			ID: "completion", Name: "Completion", Description: "Tab completion tuning",
			Status: modedit.StatusEnabled, Packages: modedit.PackagesNA,
			Platforms: []string{"macos", "linux"}, Shells: []string{"zsh", "bash"},
			Origin: modedit.OriginBuiltin,
		},
		{
			ID: "fzf", Name: "FZF Fuzzy Finder", Description: "Ctrl+R history search",
			Homepage: "https://github.com/junegunn/fzf",
			Status:   modedit.StatusDisabled, Packages: modedit.PackagesMissing,
			Platforms: []string{"macos", "linux"}, Shells: []string{"zsh", "bash"},
			Origin: modedit.OriginBuiltin, OptionCount: 3,
		},
		{
			ID: "zshonly", Name: "Zsh only", Description: "A module for zsh alone",
			Status: modedit.StatusDisabled, Packages: modedit.PackagesNA,
			Platforms: []string{"linux"}, Shells: []string{"zsh"},
			Origin: modedit.OriginUser,
		},
	}
}

// manyViews returns n modules named mod00, mod01, ... for scrolling tests.
func manyViews(n int) []modedit.ModuleView {
	views := make([]modedit.ModuleView, n)
	for i := range views {
		id := "mod" + string(rune('0'+i/10)) + string(rune('0'+i%10))
		views[i] = modedit.ModuleView{ID: id, Name: id, Description: "module " + id, Status: modedit.StatusDisabled, Packages: modedit.PackagesNA}
	}
	return views
}

// key builds the key press a terminal would deliver for a key name such as
// "up", "esc", "ctrl+c", or a single typed character.
func key(name string) tea.KeyPressMsg {
	switch name {
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "ctrl+c":
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	default:
		r := []rune(name)
		return tea.KeyPressMsg{Code: r[0], Text: name}
	}
}

// press feeds keys to m one by one and returns the resulting model.
func press(t *testing.T, m Model, names ...string) Model {
	t.Helper()
	for _, name := range names {
		next, _ := m.Update(key(name))
		m = next.(Model)
	}
	return m
}

// sized returns m after a terminal of w x h cells has been reported.
func sized(m Model, w, h int) Model {
	next, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return next.(Model)
}

// plain renders m without styling, so assertions see only the text.
func plain(m Model) string {
	return ansi.Strip(m.View().Content)
}

// visibleIDs returns the ids of the modules that pass the filter, in order.
func visibleIDs(m Model) []string {
	ids := make([]string, len(m.visible))
	for i, idx := range m.visible {
		ids[i] = m.views[idx].ID
	}
	return ids
}

// assertFits fails unless out is exactly h lines, none wider than w cells.
func assertFits(t *testing.T, out string, w, h int) {
	t.Helper()
	lines := strings.Split(out, "\n")
	if len(lines) != h {
		t.Fatalf("view has %d lines, want %d:\n%s", len(lines), h, out)
	}
	for i, line := range lines {
		if got := ansi.StringWidth(line); got > w {
			t.Fatalf("line %d is %d cells wide, want at most %d: %q", i+1, got, w, line)
		}
	}
}

// assertGolden compares got with testdata/<name>.golden; run the tests with
// -update to (re)write the file after reviewing the change by eye.
func assertGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatalf("create testdata: %v", err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden (create it with -update): %v", err)
	}
	if string(want) != got {
		t.Fatalf("view differs from %s (re-run with -update if intended)\n--- got ---\n%s\n--- want ---\n%s", path, got, want)
	}
}
