package tui

import (
	"strings"
	"testing"
)

func TestViewIsEmptyUntilTheTerminalSizeIsKnown(t *testing.T) {
	if got := New(sampleViews()).View().Content; got != "" {
		t.Fatalf("view before the first size message = %q, want empty", got)
	}
}

func TestViewShowsATooSmallMessageBelowTheMinimumSize(t *testing.T) {
	cases := []struct{ w, h int }{{79, 20}, {80, 19}, {10, 5}}
	for _, c := range cases {
		out := plain(sized(New(sampleViews()), c.w, c.h))
		if !strings.Contains(out, "Terminal too small: need at least 80x20") {
			t.Fatalf("%dx%d: view = %q, want the too-small message", c.w, c.h, out)
		}
		if strings.Contains(out, "fzf") {
			t.Fatalf("%dx%d: the module list must not render when too small", c.w, c.h)
		}
	}
}
