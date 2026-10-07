package tui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// The two text fields (the filter and an option value) keep their text as a
// string and the cursor as a rune index into it, so the Model stays a plain
// value and tests can read the text directly.

// editText applies one key press to text, with the cursor at pos, and returns
// the new text and cursor. ok is false for keys it does not handle, so the
// caller can give them their own meaning (enter, esc, ctrl+c).
func editText(text string, pos int, msg tea.KeyPressMsg) (string, int, bool) {
	runes := []rune(text)
	pos = min(max(pos, 0), len(runes))
	switch msg.String() {
	case "left", "ctrl+b":
		pos = max(pos-1, 0)
	case "right", "ctrl+f":
		pos = min(pos+1, len(runes))
	case "home", "ctrl+a":
		pos = 0
	case "end", "ctrl+e":
		pos = len(runes)
	case "backspace":
		if pos > 0 {
			runes = append(runes[:pos-1], runes[pos:]...)
			pos--
		}
	case "delete", "ctrl+d":
		if pos < len(runes) {
			runes = append(runes[:pos], runes[pos+1:]...)
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
	pos = min(max(pos, 0), len(runes))
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
	pos = min(max(pos, 0), len(runes))
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
