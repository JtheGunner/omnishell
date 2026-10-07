package tui

import (
	"errors"
	"strings"
	"testing"
)

func TestViewIsEmptyUntilTheTerminalSizeIsKnown(t *testing.T) {
	if got := newTestModel(sampleViews()).View().Content; got != "" {
		t.Fatalf("view before the first size message = %q, want empty", got)
	}
}

func TestViewShowsATooSmallMessageBelowTheMinimumSize(t *testing.T) {
	cases := []struct{ w, h int }{{79, 20}, {80, 19}, {10, 5}}
	for _, c := range cases {
		out := plain(sized(newTestModel(sampleViews()), c.w, c.h))
		if !strings.Contains(out, "Terminal too small: need at least 80x20") {
			t.Fatalf("%dx%d: view = %q, want the too-small message", c.w, c.h, out)
		}
		if strings.Contains(out, "fzf") {
			t.Fatalf("%dx%d: the module list must not render when too small", c.w, c.h)
		}
	}
}

func TestViewAtTheMinimumSizeRendersTheBrowser(t *testing.T) {
	out := plain(sized(newTestModel(sampleViews()), 80, 20))

	if strings.Contains(out, "too small") {
		t.Fatalf("80x20 must be big enough:\n%s", out)
	}
	assertFits(t, out, 80, 20)
}

func TestViewFitsExactlyAtSeveralSizes(t *testing.T) {
	for _, c := range []struct{ w, h int }{{80, 20}, {100, 30}, {200, 50}} {
		assertFits(t, plain(sized(newTestModel(sampleViews()), c.w, c.h)), c.w, c.h)
	}
}

func TestViewListsModulesWithStatusBoxesAndMarksTheCursor(t *testing.T) {
	out := plain(sized(newTestModel(sampleViews()), 80, 20))

	for _, want := range []string{"▸ [x] completion", "  [ ] fzf", "  [ ] zshonly"} {
		if !strings.Contains(out, want) {
			t.Fatalf("view is missing %q:\n%s", want, out)
		}
	}
}

func TestViewDetailFollowsTheCursor(t *testing.T) {
	m := sized(newTestModel(sampleViews()), 100, 30)
	m = press(t, m, "down") // fzf

	out := plain(m)
	for _, want := range []string{
		"FZF Fuzzy Finder", "fzf · builtin", "Ctrl+R history search",
		"Status:    disabled", "Packages:  missing", "Platforms: macos, linux",
		"Shells:    zsh, bash", "Options:   3", "Homepage:  https://github.com/junegunn/fzf",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("detail is missing %q:\n%s", want, out)
		}
	}
}

func TestViewDetailShowsADashWhenThereIsNoHomepage(t *testing.T) {
	out := plain(sized(newTestModel(sampleViews()), 100, 30)) // completion has none

	if !strings.Contains(out, "Homepage:  —") {
		t.Fatalf("want a dash for the missing homepage:\n%s", out)
	}
}

func TestViewScrollsTheListToKeepTheCursorVisible(t *testing.T) {
	m := sized(newTestModel(manyViews(30)), 80, 20)
	for range 29 {
		m = press(t, m, "down")
	}

	out := plain(m)
	if !strings.Contains(out, "▸ [ ] mod29") {
		t.Fatalf("the cursor row mod29 must be visible:\n%s", out)
	}
	if strings.Contains(out, "mod00") {
		t.Fatalf("mod00 must have scrolled out of view:\n%s", out)
	}
	assertFits(t, out, 80, 20)
}

func TestViewKeepsTheLayoutWhenDescriptionAndHomepageAreVeryLong(t *testing.T) {
	views := sampleViews()
	views[0].Description = strings.Repeat("a very long description ", 40)
	views[0].Homepage = "https://example.com/" + strings.Repeat("x", 300)

	assertFits(t, plain(sized(newTestModel(views), 80, 20)), 80, 20)
}

func TestViewTruncatesALongModuleIDInTheList(t *testing.T) {
	views := sampleViews()
	views[0].ID = strings.Repeat("long-module-id-", 10)

	assertFits(t, plain(sized(newTestModel(views), 80, 20)), 80, 20)
}

func TestViewSaysSoWhenThereAreNoModulesOrNoMatches(t *testing.T) {
	if out := plain(sized(newTestModel(nil), 80, 20)); !strings.Contains(out, "No modules") {
		t.Fatalf("empty registry view:\n%s", out)
	}
	assertFits(t, plain(sized(newTestModel(nil), 80, 20)), 80, 20)

	m := press(t, sized(newTestModel(sampleViews()), 80, 20), "/", "n", "o", "p", "e")
	out := plain(m)
	if !strings.Contains(out, "No matches") {
		t.Fatalf("unmatched filter view:\n%s", out)
	}
	assertFits(t, out, 80, 20)
}

func TestViewHeaderAndFooterReflectTheFilterState(t *testing.T) {
	m := sized(newTestModel(sampleViews()), 80, 20)

	typing := plain(press(t, m, "/", "f"))
	if !strings.Contains(typing, "filter: f_") || !strings.Contains(typing, "enter keep") {
		t.Fatalf("typing view:\n%s", typing)
	}

	kept := plain(press(t, m, "/", "f", "z", "enter"))
	if !strings.Contains(kept, "filter: fz (1 shown)") || !strings.Contains(kept, "q quit") {
		t.Fatalf("kept-filter view:\n%s", kept)
	}
}

// Shrinking the terminal below the minimum hides the browser but must not
// lose the user's place: growing it again shows the same selection and filter.
func TestResizingBelowTheMinimumAndBackKeepsTheState(t *testing.T) {
	m := sized(newTestModel(sampleViews()), 100, 30)
	m = press(t, m, "/", "z", "enter")

	m = sized(m, 40, 10)
	if !strings.Contains(plain(m), "Terminal too small") {
		t.Fatal("expected the too-small message at 40x10")
	}

	m = sized(m, 100, 30)
	out := plain(m)
	if !strings.Contains(out, "▸ [ ] fzf") || !strings.Contains(out, "filter: z (2 shown)") {
		t.Fatalf("state lost after the resize round-trip:\n%s", out)
	}
	assertFits(t, out, 100, 30)
}

func TestViewHeaderCountsChangesSinceStart(t *testing.T) {
	m := sized(newTestModel(sampleViews()), 100, 30)
	if out := plain(m); !strings.Contains(out, "3 modules · 0 changes since start") {
		t.Fatalf("header before any toggle:\n%s", out)
	}

	m = space(m)
	if out := plain(m); !strings.Contains(out, "3 modules · 1 change since start") {
		t.Fatalf("header after one toggle:\n%s", out)
	}

	m = space(press(t, m, "down"))
	if out := plain(m); !strings.Contains(out, "3 modules · 2 changes since start") {
		t.Fatalf("header after two toggles:\n%s", out)
	}
}

func TestViewFooterShowsTheStatusMessageInsteadOfTheHelp(t *testing.T) {
	m, b := newBackedModel(sampleViews())
	b.toggleErr = errors.New("module \"zshonly\" only supports zsh")
	m = space(sized(m, 100, 30))

	out := plain(m)
	if !strings.Contains(out, `! module "zshonly" only supports zsh`) {
		t.Fatalf("status line missing:\n%s", out)
	}
	if strings.Contains(out, "esc clear filter") {
		t.Fatalf("the help must give way to the status line:\n%s", out)
	}
	assertFits(t, out, 100, 30)

	if out := plain(press(t, m, "down")); !strings.Contains(out, "space toggle") {
		t.Fatalf("the help must come back after the next key:\n%s", out)
	}
}

// Error text from a backend is untrusted like manifest text: it must be shown,
// but never as a terminal command or as extra lines.
func TestViewShowsAStatusMessageButNeutralisesControlSequences(t *testing.T) {
	m, b := newBackedModel(sampleViews())
	b.toggleErr = errors.New("bad\x1b]0;pwned\x07\x1b[2J\nsecond line")
	m = space(sized(m, 100, 30))

	out := m.View().Content
	for _, bad := range []string{"\x1b]", "\x1b[2J", "\x07"} {
		if strings.Contains(out, bad) {
			t.Fatalf("view contains %q from the error text:\n%q", bad, out)
		}
	}
	if !strings.Contains(plain(m), "! bad ]0;pwned") {
		t.Fatalf("the message must still be readable:\n%s", plain(m))
	}
	assertFits(t, plain(m), 100, 30)
}

func TestViewTruncatesALongStatusMessageToTheWidth(t *testing.T) {
	m, b := newBackedModel(sampleViews())
	b.toggleErr = errors.New(strings.Repeat("a very long message ", 20))
	m = space(sized(m, 80, 20))

	out := plain(m)
	if !strings.Contains(out, "! a very long message") || !strings.Contains(out, "…") {
		t.Fatalf("expected a visibly truncated status line:\n%s", out)
	}
	assertFits(t, out, 80, 20)
}

func TestViewHelpMentionsTheSpaceKey(t *testing.T) {
	if out := plain(sized(newTestModel(sampleViews()), 80, 20)); !strings.Contains(out, "space toggle") {
		t.Fatalf("footer help should list the space key:\n%s", out)
	}
}

func TestViewDimsModulesThatCannotRunOnThisHost(t *testing.T) {
	m := sized(newTestModel(sampleViews()), 80, 20)
	rows := strings.Split(m.renderList(10, 26), "\n")

	const faint = "\x1b[2m"
	if strings.Contains(rows[1], faint) { // fzf runs here
		t.Fatalf("fzf must not be dimmed: %q", rows[1])
	}
	if !strings.Contains(rows[2], faint) { // zshonly does not
		t.Fatalf("zshonly must be dimmed: %q", rows[2])
	}
}

func TestViewDetailExplainsWhyAModuleCannotRunHere(t *testing.T) {
	m := press(t, sized(newTestModel(sampleViews()), 100, 30), "down", "down") // zshonly

	if out := plain(m); !strings.Contains(out, "Host:      needs zsh, but your managed shells are bash") {
		t.Fatalf("detail is missing the host line:\n%s", out)
	}
}

func TestViewDetailHasNoHostLineForAModuleThatRunsHere(t *testing.T) {
	m := press(t, sized(newTestModel(sampleViews()), 100, 30), "down") // fzf

	if out := plain(m); strings.Contains(out, "Host:") {
		t.Fatalf("a usable module must not get a host line:\n%s", out)
	}
}

func TestViewGoldenFiles(t *testing.T) {
	base := sized(newTestModel(sampleViews()), 80, 20)
	rejectedModel, rejectedBackend := newBackedModel(sampleViews())
	rejectedBackend.toggleErr = errors.New(`module "zshonly" only supports zsh, but none of your managed shells (bash) do`)
	rejected := space(press(t, sized(rejectedModel, 80, 20), "down", "down"))
	cases := map[string]Model{
		"browser-80x20":       base,
		"browser-second-row":  press(t, base, "down"),
		"browser-filtered":    press(t, base, "/", "f", "z", "enter"),
		"browser-typing":      press(t, base, "/", "z"),
		"browser-no-modules":  sized(newTestModel(nil), 80, 20),
		"browser-too-small":   sized(newTestModel(sampleViews()), 60, 10),
		"browser-toggled":     space(press(t, base, "down")),
		"browser-unavailable": press(t, base, "down", "down"),
		"browser-rejected":    rejected,
	}
	for name, m := range cases {
		assertGolden(t, name, plain(m))
	}
}
