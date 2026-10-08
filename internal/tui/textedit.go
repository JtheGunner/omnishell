package tui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"
)

// The two text fields (the filter and an option value) keep their text as a
// string and the cursor as a rune index into it, so the Model stays a plain
// value and tests can read the text directly. The cursor always rests on a
// grapheme-cluster boundary: a visible character made of several runes (a
// combining accent, a ZWJ emoji, a flag) is moved over and deleted as one.

// clusterStarts returns the rune index at which each grapheme cluster of runes
// starts, followed by len(runes) as the final boundary.
func clusterStarts(runes []rune) []int {
	starts := make([]int, 0, len(runes)+1)
	rest, offset := string(runes), 0
	for rest != "" {
		cluster, remainder, _, _ := uniseg.FirstGraphemeClusterInString(rest, -1)
		starts = append(starts, offset)
		offset += len([]rune(cluster))
		rest = remainder
	}
	return append(starts, len(runes))
}

// snapToBoundary returns the cluster boundary at or before pos, which it first
// clamps into the text.
func snapToBoundary(starts []int, pos int) int {
	pos = min(max(pos, 0), starts[len(starts)-1])
	for i := len(starts) - 1; i >= 0; i-- {
		if starts[i] <= pos {
			return starts[i]
		}
	}
	return 0
}

// boundaryIndex returns the index into starts of the boundary pos (already
// snapped).
func boundaryIndex(starts []int, pos int) int {
	for i, s := range starts {
		if s == pos {
			return i
		}
	}
	return len(starts) - 1
}

// editText applies one key press to text, with the cursor at pos, and returns
// the new text and cursor. ok is false for keys it does not handle, so the
// caller can give them their own meaning (enter, esc, ctrl+c).
func editText(text string, pos int, msg tea.KeyPressMsg) (string, int, bool) {
	runes := []rune(text)
	starts := clusterStarts(runes)
	pos = snapToBoundary(starts, pos)
	at := boundaryIndex(starts, pos)
	switch msg.String() {
	case "left", "ctrl+b":
		pos = starts[max(at-1, 0)]
	case "right", "ctrl+f":
		pos = starts[min(at+1, len(starts)-1)]
	case "home", "ctrl+a":
		pos = 0
	case "end", "ctrl+e":
		pos = len(runes)
	case "backspace":
		if at > 0 {
			runes = append(runes[:starts[at-1]], runes[pos:]...)
			pos = starts[at-1]
		}
	case "delete", "ctrl+d":
		if at < len(starts)-1 {
			runes = append(runes[:pos], runes[starts[at+1]:]...)
		}
	default:
		if msg.Text == "" {
			return text, pos, false
		}
		text, pos = pasteText(text, pos, msg.Text)
		return text, pos, true
	}
	return string(runes), pos, true
}

// pasteText inserts content, cleaned of control characters, at pos and returns
// the new text and the cursor behind the inserted part.
func pasteText(text string, pos int, content string) (string, int) {
	runes := []rune(text)
	pos = snapToBoundary(clusterStarts(runes), pos)
	add := []rune(sanitize(content))
	out := make([]rune, 0, len(runes)+len(add))
	out = append(out, runes[:pos]...)
	out = append(out, add...)
	out = append(out, runes[pos:]...)
	return string(out), pos + len(add)
}

// editWindow returns what fits into room cells of text with the cursor at pos,
// and the cell column of the cursor. When the cursor is behind the text, an
// underscore marks it. A text that does not fit is cut on the left, and on the
// right when much of it follows the cursor, so the cursor is always inside.
func editWindow(text string, pos, room int) (string, int) {
	runes := []rune(text)
	pos = snapToBoundary(clusterStarts(runes), pos)
	before, after := string(runes[:pos]), string(runes[pos:])
	atEnd := after == ""

	if ansi.StringWidth(before)+max(ansi.StringWidth(after), 1) <= room {
		if atEnd {
			after = "_"
		}
		return before + after, ansi.StringWidth(before)
	}

	// Not everything fits: give the part after the cursor at most a third of
	// the cell, and the rest to the part before it.
	rightBudget := min(max(ansi.StringWidth(after), 1), max(room/3, 1))
	right := after
	if ansi.StringWidth(right) > rightBudget {
		right = ansi.Truncate(right, rightBudget, "…")
	}
	if atEnd {
		right = "_"
	}
	leftBudget := max(room-max(ansi.StringWidth(right), 1), 1)
	if w := ansi.StringWidth(before); w > leftBudget {
		// TruncateLeft keeps a wide character that is cut in the middle, so the
		// result can be a cell too wide: cut a little more until it fits.
		for n := w - (leftBudget - 1); ; n++ {
			if cut := ansi.TruncateLeft(before, n, "…"); ansi.StringWidth(cut) <= leftBudget {
				before = cut
				break
			}
		}
	}
	return before + right, ansi.StringWidth(before)
}
