package tui

import (
	"flag"
	"os"
	"path/filepath"
	"slices"
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
			Origin:      modedit.OriginUser,
			Unavailable: "needs zsh, but your managed shells are bash",
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

// fakeBackend is an in-memory Backend that records every write. Enable and
// Disable flip the stored status, as the real config.toml would.
type fakeBackend struct {
	views      []modedit.ModuleView
	modulesErr error    // returned by Modules
	toggleErr  error    // returned by Enable and Disable, before anything changes
	calls      []string // "enable fzf", "disable fzf", in order
}

func (f *fakeBackend) Modules() ([]modedit.ModuleView, error) { return f.views, f.modulesErr }
func (f *fakeBackend) Enable(id string) error                 { return f.write(id, "enable", modedit.StatusEnabled) }
func (f *fakeBackend) Disable(id string) error                { return f.write(id, "disable", modedit.StatusDisabled) }

func (f *fakeBackend) write(id, verb string, status modedit.Status) error {
	f.calls = append(f.calls, verb+" "+id)
	if f.toggleErr != nil {
		return f.toggleErr
	}
	for i := range f.views {
		if f.views[i].ID == id {
			f.views[i].Status = status
		}
	}
	return nil
}

// newBackedModel returns a model over views together with the fake backend it
// writes through; the backend gets its own copy of views.
func newBackedModel(views []modedit.ModuleView) (Model, *fakeBackend) {
	b := &fakeBackend{views: slices.Clone(views)}
	return New(b, views), b
}

// newTestModel is newBackedModel for tests that never look at the backend.
func newTestModel(views []modedit.ModuleView) Model {
	m, _ := newBackedModel(views)
	return m
}

// settle runs cmd the way the Bubble Tea runtime would and feeds the message it
// produces back into m.
func settle(m Model, cmd tea.Cmd) Model {
	if cmd == nil {
		return m
	}
	next, _ := m.Update(cmd())
	return next.(Model)
}

// space presses the space bar and lets the resulting write finish.
func space(m Model) Model {
	next, cmd := m.Update(key("space"))
	return settle(next.(Model), cmd)
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
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
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
