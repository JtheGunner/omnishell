package tui

import (
	"reflect"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestNewShowsEveryModuleWithCursorOnTheFirst(t *testing.T) {
	m := New(sampleViews())

	if got, want := visibleIDs(m), []string{"completion", "fzf", "zshonly"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("visible = %v, want %v", got, want)
	}
	if v, ok := m.selected(); !ok || v.ID != "completion" {
		t.Fatalf("selected = %q ok=%v, want completion", v.ID, ok)
	}
}

func TestCursorMovesWithArrowsAndVimKeysAndStopsAtBothEnds(t *testing.T) {
	m := New(sampleViews())

	m = press(t, m, "up")
	if m.cursor != 0 {
		t.Fatalf("cursor above the top = %d, want 0", m.cursor)
	}
	m = press(t, m, "down", "j")
	if m.cursor != 2 {
		t.Fatalf("cursor after down, j = %d, want 2", m.cursor)
	}
	m = press(t, m, "down")
	if m.cursor != 2 {
		t.Fatalf("cursor below the bottom = %d, want 2", m.cursor)
	}
	m = press(t, m, "k", "up")
	if m.cursor != 0 {
		t.Fatalf("cursor after k, up = %d, want 0", m.cursor)
	}
}

func TestQuitKeys(t *testing.T) {
	for _, name := range []string{"q", "ctrl+c"} {
		_, cmd := New(sampleViews()).Update(key(name))
		if cmd == nil {
			t.Fatalf("%s: no command returned", name)
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Fatalf("%s: command did not quit", name)
		}
	}
}

func TestFilterNarrowsByIDOrDescriptionIgnoringCase(t *testing.T) {
	m := New(sampleViews())

	m = press(t, m, "/", "F", "Z")
	if got, want := visibleIDs(m), []string{"fzf"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("filter FZ: visible = %v, want %v", got, want)
	}

	m = press(t, m, "backspace", "backspace", "a", "l", "o", "n", "e")
	if got, want := visibleIDs(m), []string{"zshonly"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("filter alone (description match): visible = %v, want %v", got, want)
	}
}

func TestFilterBackspaceWidensAndEmptyBackspaceIsHarmless(t *testing.T) {
	m := press(t, New(sampleViews()), "/", "f", "z")
	m = press(t, m, "backspace", "backspace", "backspace")

	if len(m.visible) != 3 || m.filter != "" || !m.filtering {
		t.Fatalf("visible=%d filter=%q filtering=%v, want 3, empty, still typing", len(m.visible), m.filter, m.filtering)
	}
}

func TestFilterBackspaceRemovesAWholeMultiByteCharacter(t *testing.T) {
	m := press(t, New(sampleViews()), "/", "ä")
	if m.filter != "ä" {
		t.Fatalf("filter = %q, want ä", m.filter)
	}

	m = press(t, m, "backspace")
	if m.filter != "" {
		t.Fatalf("filter after backspace = %q, want empty (not a broken byte)", m.filter)
	}
}

func TestQDoesNotQuitWhileTypingAFilter(t *testing.T) {
	m := New(sampleViews())
	m = press(t, m, "/")

	next, cmd := m.Update(key("q"))
	if cmd != nil {
		t.Fatal("q while filtering must be typed into the filter, not quit")
	}
	if got := next.(Model).filter; got != "q" {
		t.Fatalf("filter = %q, want q", got)
	}
}

func TestCtrlCQuitsWhileTypingAFilter(t *testing.T) {
	m := press(t, New(sampleViews()), "/", "f")

	_, cmd := m.Update(key("ctrl+c"))
	if cmd == nil {
		t.Fatal("ctrl+c while filtering returned no command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("ctrl+c while filtering did not quit")
	}
}

func TestEnterKeepsTheFilterAndLeavesTypingMode(t *testing.T) {
	m := press(t, New(sampleViews()), "/", "f", "z", "enter")

	if m.filtering || m.filter != "fz" || len(m.visible) != 1 {
		t.Fatalf("filtering=%v filter=%q visible=%d, want false, fz, 1", m.filtering, m.filter, len(m.visible))
	}
	// Back in browsing mode, q quits again.
	if _, cmd := m.Update(key("q")); cmd == nil {
		t.Fatal("q after enter should quit")
	}
}

func TestEscCancelsTypingAndEscClearsAKeptFilter(t *testing.T) {
	m := press(t, New(sampleViews()), "/", "f", "z", "esc")
	if m.filtering || m.filter != "" || len(m.visible) != 3 {
		t.Fatalf("esc while typing: filtering=%v filter=%q visible=%d, want false, empty, 3", m.filtering, m.filter, len(m.visible))
	}

	m = press(t, m, "/", "f", "z", "enter", "esc")
	if m.filter != "" || len(m.visible) != 3 {
		t.Fatalf("esc on a kept filter: filter=%q visible=%d, want empty, 3", m.filter, len(m.visible))
	}
}

func TestFilteringMovesTheCursorBackToTheTopOfTheNewList(t *testing.T) {
	m := press(t, New(sampleViews()), "down", "down")
	if m.cursor != 2 {
		t.Fatalf("setup: cursor = %d, want 2", m.cursor)
	}

	m = press(t, m, "/", "f")
	if v, ok := m.selected(); m.cursor != 0 || !ok || v.ID != "fzf" {
		t.Fatalf("cursor=%d selected=%q ok=%v, want 0 on fzf", m.cursor, v.ID, ok)
	}
}

func TestEmptyAndUnmatchedListsAreSafe(t *testing.T) {
	m := New(nil)
	m = press(t, m, "down", "up", "j", "k", "/", "x", "esc")
	if _, ok := m.selected(); ok {
		t.Fatal("an empty list has no selection")
	}

	m = press(t, New(sampleViews()), "/", "n", "o", "p", "e", "x")
	if len(m.visible) != 0 {
		t.Fatalf("visible = %v, want none", visibleIDs(m))
	}
	m = press(t, m, "down", "enter")
	if _, ok := m.selected(); ok {
		t.Fatal("no match means no selection")
	}
}

func TestWindowSizeIsRecorded(t *testing.T) {
	m := sized(New(sampleViews()), 100, 30)

	if m.width != 100 || m.height != 30 {
		t.Fatalf("size = %dx%d, want 100x30", m.width, m.height)
	}
}

func TestViewRequestsTheAlternateScreen(t *testing.T) {
	if !New(sampleViews()).View().AltScreen {
		t.Fatal("the browser must run on the alternate screen")
	}
}
