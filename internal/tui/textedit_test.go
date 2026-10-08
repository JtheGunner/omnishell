package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestEditTextMovesTheCursorAndEditsAtIt(t *testing.T) {
	text, pos := "abcd", 4
	step := func(name string) {
		t.Helper()
		var ok bool
		text, pos, ok = editText(text, pos, key(name))
		if !ok {
			t.Fatalf("%s must be handled", name)
		}
	}

	step("left")
	step("left")
	if pos != 2 {
		t.Fatalf("pos = %d after two lefts, want 2", pos)
	}
	text, pos, _ = editText(text, pos, key("X"))
	if text != "abXcd" || pos != 3 {
		t.Fatalf("text=%q pos=%d, want X inserted at the cursor", text, pos)
	}
	step("backspace")
	if text != "abcd" || pos != 2 {
		t.Fatalf("text=%q pos=%d, want the character before the cursor removed", text, pos)
	}
	step("delete")
	if text != "abd" || pos != 2 {
		t.Fatalf("text=%q pos=%d, want the character under the cursor removed", text, pos)
	}
	step("home")
	step("delete")
	if text != "bd" || pos != 0 {
		t.Fatalf("text=%q pos=%d after home and delete", text, pos)
	}
	step("backspace") // at the start: nothing to remove
	if text != "bd" || pos != 0 {
		t.Fatalf("backspace at the start changed text=%q pos=%d", text, pos)
	}
	step("end")
	step("right") // at the end: stays
	if pos != 2 {
		t.Fatalf("pos = %d at the end, want 2", pos)
	}
	step("ctrl+a")
	if pos != 0 {
		t.Fatalf("ctrl+a: pos = %d, want 0", pos)
	}
	step("ctrl+e")
	if pos != 2 {
		t.Fatalf("ctrl+e: pos = %d, want 2", pos)
	}
}

func TestEditTextCountsRunesNotBytes(t *testing.T) {
	text, pos, _ := editText("äö日", 3, key("left"))
	text, pos, _ = editText(text, pos, key("backspace"))
	if text != "ä日" || pos != 1 {
		t.Fatalf("text=%q pos=%d, want the rune before the cursor removed", text, pos)
	}
}

func TestEditTextLeavesOtherKeysAlone(t *testing.T) {
	for _, name := range []string{"up", "down", "enter", "esc", "ctrl+c"} {
		if _, _, ok := editText("abc", 1, key(name)); ok {
			t.Fatalf("%s must be left to the caller", name)
		}
	}
	_ = tea.KeyPressMsg{}
}

func TestPasteTextInsertsAtTheCursorAndCleansIt(t *testing.T) {
	text, pos := pasteText("ad", 1, "b\x1b[31mc\n")
	if strings.ContainsAny(text, "\x1b\n") {
		t.Fatalf("pasted control characters must be cleaned: %q", text)
	}
	if !strings.HasPrefix(text, "ab") || !strings.HasSuffix(text, "d") || pos != len([]rune(text))-1 {
		t.Fatalf("text=%q pos=%d, want the paste between a and d with the cursor behind it", text, pos)
	}
}

func TestEditWindowKeepsTheCursorInsideTheCell(t *testing.T) {
	long := strings.Repeat("0123456789", 6)
	for _, pos := range []int{0, 1, 10, 30, 59, 60} {
		cell, col := editWindow(long, pos, 23)
		if w := ansi.StringWidth(cell); w > 23 {
			t.Fatalf("pos %d: cell %q is %d cells wide, want at most 23", pos, cell, w)
		}
		if col < 0 || col >= 23 {
			t.Fatalf("pos %d: cursor column %d outside the cell", pos, col)
		}
	}
}

func TestEditWindowShowsTheWholeShortText(t *testing.T) {
	cell, col := editWindow("abc", 3, 23)
	if cell != "abc_" || col != 3 {
		t.Fatalf("cell=%q col=%d, want abc_ with the cursor on the underscore", cell, col)
	}
	cell, col = editWindow("abc", 1, 23)
	if cell != "abc" || col != 1 {
		t.Fatalf("cell=%q col=%d, want abc with the cursor on b", cell, col)
	}
}

func TestEditWindowWithWideCharacters(t *testing.T) {
	long := strings.Repeat("日本語", 10)
	for _, pos := range []int{0, 7, 15, 30} {
		cell, col := editWindow(long, pos, 23)
		if ansi.StringWidth(cell) > 23 || col >= 23 {
			t.Fatalf("pos %d: cell %q (%d cells) col %d does not fit", pos, cell, ansi.StringWidth(cell), col)
		}
	}
}

func TestOptionInputCursorMovesAndEditsInTheMiddle(t *testing.T) {
	m, b := withOptions()
	m = onOption(t, openFzfOptions(t, m), "prefix") // value "abc"

	m = press(t, m, "enter", "left", "left", "X")
	if m.options.input != "aXbc" {
		t.Fatalf("input = %q, want X inserted before b", m.options.input)
	}
	m = press(t, m, "home", "Y", "end", "Z")
	if m.options.input != "YaXbcZ" {
		t.Fatalf("input = %q, want Y at the start and Z at the end", m.options.input)
	}
	m = press(t, m, "left", "left", "delete")
	if m.options.input != "YaXbZ" {
		t.Fatalf("input = %q after delete, want the character under the cursor gone", m.options.input)
	}
	if len(b.optionCalls) != 0 {
		t.Fatal("moving and typing must not write")
	}
}

func TestOptionInputShowsARealCursorAtTheEditPosition(t *testing.T) {
	m, _ := withOptions()
	m = onOption(t, openFzfOptions(t, m), "prefix")

	if m.View().Cursor != nil {
		t.Fatal("no cursor expected before editing starts")
	}
	m = press(t, m, "enter")
	cur := m.View().Cursor
	if cur == nil {
		t.Fatal("the editing cell needs a visible cursor")
	}
	lines := strings.Split(plain(m), "\n")
	line := lines[cur.Y]
	if !strings.Contains(line, "prefix") {
		t.Fatalf("the cursor is on line %d %q, want the row being edited", cur.Y, line)
	}
	// "[abc_]": the cursor sits on the underscore behind the text.
	if got := ansi.Cut(line, cur.X, cur.X+1); got != "_" {
		t.Fatalf("the cursor is on %q at column %d of %q, want the underscore", got, cur.X, line)
	}

	m = press(t, m, "left")
	cur = m.View().Cursor
	if got := ansi.Cut(strings.Split(plain(m), "\n")[cur.Y], cur.X, cur.X+1); got != "c" {
		t.Fatalf("after left the cursor is on %q, want c", got)
	}
}

func TestOptionInputCursorStaysVisibleWhileScrolled(t *testing.T) {
	m, b := withOptions()
	b.optionRows["fzf"][2].Value, b.optionRows["fzf"][2].Set = strings.Repeat("0123456789", 6), true
	m = onOption(t, openFzfOptions(t, m), "prefix")
	m = press(t, m, "enter", "home")

	cur := m.View().Cursor
	if cur == nil {
		t.Fatal("cursor missing")
	}
	line := strings.Split(plain(m), "\n")[cur.Y]
	if got := ansi.Cut(line, cur.X, cur.X+1); got != "0" {
		t.Fatalf("at the start the cursor is on %q, want the first character:\n%s", got, plain(m))
	}
	assertFits(t, plain(m), 80, 20)
}

func TestPasteIntoAnOptionValueGoesInAtTheCursor(t *testing.T) {
	m, _ := withOptions()
	m = onOption(t, openFzfOptions(t, m), "prefix")
	m = press(t, m, "enter", "left")

	m = paste(m, "XY\x1b")
	if strings.ContainsRune(m.options.input, '\x1b') || !strings.HasPrefix(m.options.input, "abXY") || !strings.HasSuffix(m.options.input, "c") {
		t.Fatalf("input = %q, want the cleaned paste between b and c", m.options.input)
	}
}

func TestPasteIsIgnoredOutsideATextField(t *testing.T) {
	m, _ := withOptions()
	m = openFzfOptions(t, m)
	before := m
	m = paste(m, "abc")
	if m.options.input != before.options.input || m.options.editing || m.filter != "" {
		t.Fatal("a paste on the options list must not start editing or filtering")
	}

	browser := newTestModel(sampleViews())
	browser = paste(sized(browser, 80, 20), "abc")
	if browser.filter != "" || browser.filtering {
		t.Fatal("a paste into the browser outside the filter must be ignored")
	}
}

func TestFilterCursorMovesAndPasteWorks(t *testing.T) {
	m := sized(newTestModel(sampleViews()), 80, 20)
	m = press(t, m, "/", "f", "z")
	m = press(t, m, "left", "x")
	if m.filter != "fxz" {
		t.Fatalf("filter = %q, want x inserted before z", m.filter)
	}
	m = press(t, m, "backspace", "backspace")
	if m.filter != "z" {
		t.Fatalf("filter = %q, want z", m.filter)
	}

	m = paste(m, "f")
	if m.filter != "fz" && m.filter != "zf" {
		t.Fatalf("filter = %q, want the paste inserted", m.filter)
	}
	cur := m.View().Cursor
	if cur == nil || cur.Y != 0 {
		t.Fatalf("the filter needs a cursor on the header line, got %+v", cur)
	}
}

func TestFilterPasteIsCleanedAndFilters(t *testing.T) {
	m := sized(newTestModel(sampleViews()), 80, 20)
	m = press(t, m, "/")
	m = paste(m, "fz\x1b[2J\n")
	if strings.ContainsAny(m.filter, "\x1b\n") {
		t.Fatalf("filter = %q, control characters must be cleaned", m.filter)
	}
	if len(m.visible) != 0 {
		t.Fatalf("visible = %v, a cleaned multi-word paste matches nothing", visibleIDs(m))
	}
	m.filter = ""
	m.applyFilter()
	m = paste(m, "fzf")
	if got := visibleIDs(m); len(got) != 1 || got[0] != "fzf" {
		t.Fatalf("visible = %v, want only fzf after pasting fzf", got)
	}
}

func TestNoCursorOutsideTextFields(t *testing.T) {
	m := sized(newTestModel(sampleViews()), 80, 20)
	if m.View().Cursor != nil {
		t.Fatal("the browser has no text cursor while not filtering")
	}
	m = press(t, m, "/")
	if m.View().Cursor == nil {
		t.Fatal("filtering needs a cursor")
	}
	m = press(t, m, "enter")
	if m.View().Cursor != nil {
		t.Fatal("a kept filter has no cursor")
	}
}

// Clusters made of several runes, written as escapes so the source stays
// unambiguous: e + combining acute, a ZWJ family emoji, and a flag.
const (
	clusterAccent = "e\u0301"
	clusterFamily = "\U0001F468\u200d\U0001F469\u200d\U0001F467"
	clusterFlag   = "\U0001F1E9\U0001F1EA"
)

func TestEditTextMovesOverWholeClusters(t *testing.T) {
	for name, cluster := range map[string]string{"accent": clusterAccent, "family": clusterFamily, "flag": clusterFlag} {
		text := "a" + cluster + "z"
		end := len([]rune(text))
		afterCluster := end - 1

		_, pos, _ := editText(text, afterCluster, key("left"))
		if pos != 1 {
			t.Errorf("%s: left from behind the cluster gave pos %d, want 1", name, pos)
		}
		_, pos, _ = editText(text, 1, key("right"))
		if pos != afterCluster {
			t.Errorf("%s: right over the cluster gave pos %d, want %d", name, pos, afterCluster)
		}
	}
}

func TestEditTextDeletesWholeClusters(t *testing.T) {
	for name, cluster := range map[string]string{"accent": clusterAccent, "family": clusterFamily, "flag": clusterFlag} {
		text := "a" + cluster + "z"
		afterCluster := len([]rune(text)) - 1

		got, pos, _ := editText(text, afterCluster, key("backspace"))
		if got != "az" || pos != 1 {
			t.Errorf("%s: backspace gave text=%q pos=%d, want az / 1", name, got, pos)
		}
		got, pos, _ = editText(text, 1, key("delete"))
		if got != "az" || pos != 1 {
			t.Errorf("%s: delete gave text=%q pos=%d, want az / 1", name, got, pos)
		}
	}
}

func TestEditTextSnapsAMidClusterCursorToABoundary(t *testing.T) {
	text := "a" + clusterFamily + "z"
	_, pos, _ := editText(text, 3, key("right")) // 3 is inside the family emoji
	if pos != len([]rune(text))-1 {
		t.Fatalf("pos = %d, want the cursor behind the cluster", pos)
	}
	got, pos, _ := editText(text, 3, key("delete"))
	if got != "az" || pos != 1 {
		t.Fatalf("text=%q pos=%d, want the whole cluster removed", got, pos)
	}
}

func TestPasteTextSnapsAMidClusterCursorToABoundary(t *testing.T) {
	got, pos := pasteText("a"+clusterAccent+"z", 2, "X") // between e and the accent
	if got != "aXe\u0301z" || pos != 2 {
		t.Fatalf("text=%q pos=%d, want X before the cluster, never inside it", got, pos)
	}
}

func TestEditWindowNeverCutsACluster(t *testing.T) {
	for name, cluster := range map[string]string{"accent": clusterAccent, "family": clusterFamily, "flag": clusterFlag} {
		text := strings.Repeat("ab"+cluster, 12)
		runes := []rune(text)
		for pos := 0; pos <= len(runes); pos++ {
			cell, col := editWindow(text, pos, 9)
			if w := ansi.StringWidth(cell); w > 9 || col < 0 || col >= 9 {
				t.Fatalf("%s pos %d: cell %q is %d wide with cursor at %d", name, pos, cell, w, col)
			}
			for _, bad := range []string{"\u0301", "\u200d"} {
				if strings.HasPrefix(strings.TrimPrefix(cell, "…"), bad) {
					t.Fatalf("%s pos %d: cell %q starts inside a cluster", name, pos, cell)
				}
			}
			if strings.Count(cell, "\U0001F1E9")+strings.Count(cell, "\U0001F1EA") == 1 {
				t.Fatalf("%s pos %d: cell %q holds half a flag", name, pos, cell)
			}
		}
	}
}

func TestEditWindowPlacesTheCursorOnAClusterColumn(t *testing.T) {
	text := "a" + clusterAccent + "z"
	cell, col := editWindow(text, 2, 23) // mid-cluster input snaps to the boundary
	if cell != text || col != 1 {
		t.Fatalf("cell=%q col=%d, want the cursor on the accent cluster at column 1", cell, col)
	}
}
