package tui

import (
	"strings"
	"testing"

	"github.com/JtheGunner/omnishell/internal/modedit"
)

// hostileViews returns one module whose manifest text tries to reach the
// terminal: a window-title OSC, a clear-screen CSI, a colour change, a bell, a
// carriage return, a newline and a right-to-left override.
func hostileViews() []modedit.ModuleView {
	return []modedit.ModuleView{{
		ID:          "evil\x1b]0;pwned\x07",
		Name:        "Evil\x1b[2J name",
		Description: "line one\nline two\r\x1b[31mred\u202etxt",
		Homepage:    "https://example.com/\x1b[1;1H",
		Platforms:   []string{"linux\x1b[2J"},
		Shells:      []string{"zsh\x07"},
		Status:      modedit.StatusDisabled,
		Packages:    modedit.PackagesNA,
		Origin:      modedit.OriginUser,
	}}
}

// Module text comes from manifests that users write or copy from strangers, so
// it must never reach the terminal as a control sequence.
func TestViewNeutralisesControlSequencesInModuleText(t *testing.T) {
	out := sized(New(hostileViews()), 100, 30).View().Content

	for _, bad := range []string{"\x1b]", "\x1b[2J", "\x1b[31m", "\x1b[1;1H", "\x07", "\r", "\u202e"} {
		if strings.Contains(out, bad) {
			t.Fatalf("the view contains %q straight from the manifest:\n%q", bad, out)
		}
	}
	assertFits(t, plain(sized(New(hostileViews()), 100, 30)), 100, 30)
}

func TestFilterSeesTheNeutralisedText(t *testing.T) {
	m := press(t, New(hostileViews()), "/", "p", "w", "n", "e", "d")

	if len(m.visible) != 1 {
		t.Fatalf("visible = %v, want the module whose id contains pwned", visibleIDs(m))
	}
}

func TestNewDoesNotMutateTheCallersViews(t *testing.T) {
	views := hostileViews()
	want := views[0].ID

	_ = New(views)

	if views[0].ID != want || views[0].Platforms[0] != "linux\x1b[2J" {
		t.Fatalf("New must copy: caller's view changed to %+v", views[0])
	}
}
