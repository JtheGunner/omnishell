package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// The message modedit really produces when a module cannot run with the managed
// shells; it is longer than an 80-column terminal.
const longRejection = `module "zshonly" only supports zsh, but none of your managed shells (bash) do — install one of those shells first`

// footerText joins the lines below the body (everything after the last line
// that is part of the box or table) into one string with single spaces.
func footerText(out string, lines int) string {
	all := strings.Split(out, "\n")
	return strings.Join(strings.Fields(strings.Join(all[len(all)-lines:], " ")), " ")
}

func TestALongRejectionIsFullyReadableInTheBrowser(t *testing.T) {
	m, b := newBackedModel(sampleViews())
	b.toggleErr = errors.New(longRejection)
	m = space(press(t, sized(m, 80, 20), "down", "down"))

	out := plain(m)
	if got := footerText(out, 2); got != "! "+longRejection {
		t.Fatalf("the footer reads %q, want the whole message", got)
	}
	assertFits(t, out, 80, 20)
}

func TestALongRejectionIsFullyReadableOnTheOptionsScreen(t *testing.T) {
	m, b := withOptions()
	b.setOptionErr = errors.New(strings.Repeat("reason ", 20) + "end")
	m = onOption(t, openFzfOptions(t, m), "prefix")
	m = press(t, m, "enter", "x")
	m = settleKey(t, m, "enter")

	out := plain(m)
	if got := footerText(out, 3); !strings.HasSuffix(got, "end") || !strings.HasPrefix(got, "! reason") {
		t.Fatalf("the footer reads %q, want the whole message", got)
	}
	assertFits(t, out, 80, 20)
}

func TestAnAbsurdlyLongMessageIsCutWithAnEllipsisInsteadOfEatingTheScreen(t *testing.T) {
	m, b := newBackedModel(sampleViews())
	b.toggleErr = errors.New(strings.Repeat("word ", 200))
	m = space(press(t, sized(m, 80, 20), "down"))

	out := plain(m)
	lines := strings.Split(out, "\n")
	if !strings.HasSuffix(strings.TrimRight(lines[len(lines)-1], " "), "…") {
		t.Fatalf("the last footer line must end with an ellipsis:\n%s", out)
	}
	assertFits(t, out, 80, 20)
}

func TestTheTooSmallNoticeNeverExceedsTheTerminalWidth(t *testing.T) {
	for _, size := range [][2]int{{40, 10}, {20, 5}, {10, 3}, {79, 19}} {
		out := plain(sized(newTestModel(sampleViews()), size[0], size[1]))
		for _, line := range strings.Split(out, "\n") {
			if w := ansi.StringWidth(line); w > size[0] {
				t.Fatalf("%dx%d: the notice is %d cells wide: %q", size[0], size[1], w, line)
			}
		}
		if !strings.Contains(out, "Terminal too small") && size[0] >= 20 {
			t.Fatalf("%dx%d: the notice is missing: %q", size[0], size[1], out)
		}
	}
}
