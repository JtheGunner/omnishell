package tui

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/JtheGunner/omnishell/internal/modedit"
)

// optionRow returns the option with the given key from the open options screen.
func optionRow(t *testing.T, m Model, key string) modedit.OptionView {
	t.Helper()
	for _, r := range m.options.rows {
		if r.Key == key {
			return r
		}
	}
	t.Fatalf("no option %q on the screen: %+v", key, m.options.rows)
	return modedit.OptionView{}
}

// onOption moves the cursor to the option with the given key.
func onOption(t *testing.T, m Model, key string) Model {
	t.Helper()
	for i, r := range m.options.rows {
		if r.Key == key {
			m.options.cursor = i
			return m
		}
	}
	t.Fatalf("no option %q", key)
	return m
}

// settleKey presses a key and lets any resulting write finish.
func settleKey(t *testing.T, m Model, name string) Model {
	t.Helper()
	next, cmd := m.Update(key(name))
	return settle(next.(Model), cmd)
}

func TestOOpensTheOptionsOfTheSelectedModuleInACommand(t *testing.T) {
	m, b := withOptions()
	m = press(t, m, "down") // fzf

	next, cmd := m.Update(key("o"))

	if cmd == nil || !next.(Model).pending {
		t.Fatal("o must start loading the options in a command")
	}
	if b.optionsRead != 0 {
		t.Fatalf("Update read the options itself (%d reads); it must do no I/O", b.optionsRead)
	}
	opened := settle(next.(Model), cmd)
	if opened.screen != screenOptions || opened.options.id != "fzf" || len(opened.options.rows) != 5 || opened.pending {
		t.Fatalf("screen=%v id=%q rows=%d pending=%v, want the options of fzf", opened.screen, opened.options.id, len(opened.options.rows), opened.pending)
	}
}

func TestOOnAModuleWithoutOptionsSaysSo(t *testing.T) {
	m, b := withOptions() // completion is first and has none

	next, cmd := m.Update(key("o"))

	if cmd != nil || next.(Model).screen != screenBrowser {
		t.Fatal("a module without options must not open the options screen")
	}
	if got := next.(Model).status; got != "completion has no options" {
		t.Fatalf("status = %q", got)
	}
	if b.optionsRead != 0 {
		t.Fatal("nothing should be read")
	}
}

func TestOIsIgnoredWhileAWriteIsPendingAndTypedIntoAFilter(t *testing.T) {
	m, _ := withOptions()
	m = press(t, m, "down")
	writing, _ := m.Update(key("space")) // a toggle is being written
	if next, cmd := writing.(Model).Update(key("o")); cmd != nil || next.(Model).screen != screenBrowser {
		t.Fatal("o must be ignored while a write is pending")
	}

	typing := press(t, m, "/", "o")
	if typing.screen != screenBrowser || typing.filter != "o" {
		t.Fatalf("screen=%v filter=%q, want o typed into the filter", typing.screen, typing.filter)
	}
}

func TestAnOptionsLoadFailureShowsTheMessageAndStaysInTheBrowser(t *testing.T) {
	m, b := withOptions()
	b.optionsErr = errors.New("cannot read config.toml")
	m = press(t, m, "down")

	next, cmd := m.Update(key("o"))
	failed := settle(next.(Model), cmd)

	if failed.screen != screenBrowser || failed.status != "cannot read config.toml" || failed.pending {
		t.Fatalf("screen=%v status=%q pending=%v", failed.screen, failed.status, failed.pending)
	}
}

func TestSpaceTogglesABoolOptionThroughTheBackend(t *testing.T) {
	m, b := withOptions()
	m = openFzfOptions(t, m)

	next, cmd := m.Update(key("space"))
	if len(b.optionCalls) != 0 {
		t.Fatal("Update wrote the option itself; it must do no I/O")
	}
	m = settle(next.(Model), cmd)

	if want := []string{"set fzf ctrl_r false"}; !reflect.DeepEqual(b.optionCalls, want) {
		t.Fatalf("backend calls = %v, want %v", b.optionCalls, want)
	}
	if r := optionRow(t, m, "ctrl_r"); r.Value != "false" || !r.Set {
		t.Fatalf("ctrl_r = %+v, want false and set", r)
	}
	if again := settleKey(t, m, "space"); optionRow(t, again, "ctrl_r").Value != "true" {
		t.Fatal("a second space must switch it back on")
	}
}

func TestSpaceDoesNothingOnOtherOptionTypes(t *testing.T) {
	m, b := withOptions()
	m = openFzfOptions(t, m)
	for _, key := range []string{"extras", "prefix", "retries", "theme"} {
		on := onOption(t, m, key)
		if _, cmd := on.Update(keyMsg("space")); cmd != nil {
			t.Fatalf("space on %s must not write", key)
		}
	}
	if len(b.optionCalls) != 0 {
		t.Fatalf("backend calls = %v, want none", b.optionCalls)
	}
}

func keyMsg(name string) tea.KeyPressMsg { return key(name) }

func TestLeftAndRightCycleAnEnumAndWrapAround(t *testing.T) {
	m, b := withOptions()
	m = onOption(t, openFzfOptions(t, m), "theme") // dark of dark, light, solarized

	m = settleKey(t, m, "right")
	m = settleKey(t, m, "right")
	m = settleKey(t, m, "right") // wraps to dark
	m = settleKey(t, m, "left")  // wraps back to solarized

	want := []string{"set fzf theme light", "set fzf theme solarized", "set fzf theme dark", "set fzf theme solarized"}
	if !reflect.DeepEqual(b.optionCalls, want) {
		t.Fatalf("backend calls = %v, want %v", b.optionCalls, want)
	}
	if got := optionRow(t, m, "theme").Value; got != "solarized" {
		t.Fatalf("theme = %q, want solarized", got)
	}
}

func TestAnEnumWithAnUnknownValueStartsAtTheEdgeInTheDirectionPressed(t *testing.T) {
	m, b := withOptions()
	b.optionRows["fzf"][4].Value = "weird"
	m = onOption(t, openFzfOptions(t, m), "theme")

	right := settleKey(t, m, "right")
	left := settleKey(t, m, "left")

	if got := optionRow(t, right, "theme").Value; got != "dark" {
		t.Fatalf("right from an unknown value = %q, want the first (dark)", got)
	}
	if got := optionRow(t, left, "theme").Value; got != "solarized" {
		t.Fatalf("left from an unknown value = %q, want the last (solarized)", got)
	}
}

func TestLeftAndRightDoNothingOnOtherOptionTypes(t *testing.T) {
	m, _ := withOptions()
	m = openFzfOptions(t, m)
	for _, k := range []string{"ctrl_r", "extras", "prefix", "retries"} {
		on := onOption(t, m, k)
		for _, dir := range []string{"left", "right"} {
			if _, cmd := on.Update(key(dir)); cmd != nil {
				t.Fatalf("%s on %s must not write", dir, k)
			}
		}
	}
}

func TestEnterEditsAStringStartingFromItsValueAndSavesOnEnter(t *testing.T) {
	m, b := withOptions()
	m = onOption(t, openFzfOptions(t, m), "prefix")

	m = press(t, m, "enter")
	if !m.options.editing || m.options.input != "abc" {
		t.Fatalf("editing=%v input=%q, want editing with the current value", m.options.editing, m.options.input)
	}
	m = press(t, m, "backspace", "x", "y")
	if m.options.input != "abxy" {
		t.Fatalf("input = %q, want abxy", m.options.input)
	}
	if len(b.optionCalls) != 0 {
		t.Fatal("typing must not write")
	}

	m = settleKey(t, m, "enter")

	if want := []string{"set fzf prefix abxy"}; !reflect.DeepEqual(b.optionCalls, want) {
		t.Fatalf("backend calls = %v, want %v", b.optionCalls, want)
	}
	if m.options.editing || optionRow(t, m, "prefix").Value != "abxy" {
		t.Fatalf("editing=%v value=%q, want the saved value shown and editing over", m.options.editing, optionRow(t, m, "prefix").Value)
	}
}

func TestAnIntIsEditedLikeAString(t *testing.T) {
	m, b := withOptions()
	m = onOption(t, openFzfOptions(t, m), "retries")

	m = press(t, m, "enter", "backspace", "7")
	settleKey(t, m, "enter")

	if want := []string{"set fzf retries 7"}; !reflect.DeepEqual(b.optionCalls, want) {
		t.Fatalf("backend calls = %v, want %v", b.optionCalls, want)
	}
}

// A rejected value must not be written, must be explained, and must leave the
// typed text in place so it can be corrected rather than retyped.
func TestARejectedValueKeepsTheTextAndShowsTheMessage(t *testing.T) {
	m, b := withOptions()
	b.setOptionErr = errors.New(`invalid value for fzf.prefix: "ABC" does not match required pattern ^[a-z]+$`)
	m = onOption(t, openFzfOptions(t, m), "prefix")
	m = press(t, m, "enter", "backspace", "backspace", "backspace", "A", "B", "C")

	m = settleKey(t, m, "enter")

	if !m.options.editing || m.options.input != "ABC" {
		t.Fatalf("editing=%v input=%q, want the text kept for correction", m.options.editing, m.options.input)
	}
	if !strings.Contains(m.status, "does not match required pattern") {
		t.Fatalf("status = %q, want the reason", m.status)
	}
	if got := optionRow(t, m, "prefix").Value; got != "abc" {
		t.Fatalf("prefix = %q, want the old value untouched", got)
	}

	b.setOptionErr = nil
	m = press(t, m, "backspace", "backspace", "backspace", "x", "y", "z")
	m = settleKey(t, m, "enter")
	if m.options.editing || optionRow(t, m, "prefix").Value != "xyz" || m.status != "" {
		t.Fatalf("editing=%v value=%q status=%q, want the corrected value saved", m.options.editing, optionRow(t, m, "prefix").Value, m.status)
	}
}

func TestEscCancelsEditingWithoutWriting(t *testing.T) {
	m, b := withOptions()
	m = onOption(t, openFzfOptions(t, m), "prefix")

	m = press(t, m, "enter", "x", "esc")

	if m.options.editing || m.options.input != "" || m.screen != screenOptions {
		t.Fatalf("editing=%v input=%q screen=%v, want back on the option list", m.options.editing, m.options.input, m.screen)
	}
	if len(b.optionCalls) != 0 {
		t.Fatalf("backend calls = %v, want none", b.optionCalls)
	}
}

func TestWhileEditingQAndSpaceAreTextButCtrlCStillQuits(t *testing.T) {
	m, _ := withOptions()
	m = onOption(t, openFzfOptions(t, m), "prefix")
	m = press(t, m, "enter", "backspace", "backspace", "backspace", "q", "space", "j")

	if m.options.input != "q j" || m.screen != screenOptions {
		t.Fatalf("input=%q screen=%v, want q, space and j typed", m.options.input, m.screen)
	}
	_, cmd := m.Update(key("ctrl+c"))
	if cmd == nil {
		t.Fatal("ctrl+c must quit even while typing")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("ctrl+c did not quit")
	}
}

func TestBackspaceRemovesAWholeMultiByteCharacterFromTheInput(t *testing.T) {
	m, _ := withOptions()
	m = onOption(t, openFzfOptions(t, m), "prefix")

	m = press(t, m, "enter", "backspace", "backspace", "backspace", "ä")
	if m.options.input != "ä" {
		t.Fatalf("input = %q, want ä", m.options.input)
	}
	if m = press(t, m, "backspace"); m.options.input != "" {
		t.Fatalf("input = %q, want empty (not a broken byte)", m.options.input)
	}
}

func TestEnterDoesNotStartEditingABoolEnumOrListOption(t *testing.T) {
	m, _ := withOptions()
	m = openFzfOptions(t, m)
	for _, k := range []string{"ctrl_r", "extras", "theme"} {
		if on := press(t, onOption(t, m, k), "enter"); on.options.editing {
			t.Fatalf("enter on %s must not start text editing", k)
		}
	}
}

func TestEscLeavesTheOptionsAndKeepsTheBrowserSelectionAndFilter(t *testing.T) {
	m, _ := withOptions()
	m = press(t, m, "/", "z", "enter", "up") // filter z: fzf, zshonly; cursor on fzf
	next, cmd := m.Update(key("o"))
	m = settle(next.(Model), cmd)

	back := press(t, m, "esc")

	if back.screen != screenBrowser || back.filter != "z" {
		t.Fatalf("screen=%v filter=%q", back.screen, back.filter)
	}
	if v, _ := back.selected(); v.ID != "fzf" {
		t.Fatalf("selected = %q, want fzf", v.ID)
	}
}

func TestQuitsFromTheOptionsList(t *testing.T) {
	m, _ := withOptions()
	m = openFzfOptions(t, m)
	_, cmd := m.Update(key("q"))
	if cmd == nil {
		t.Fatal("q must quit from the option list")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("q did not quit")
	}
}

func TestTheOptionCursorMovesAndStopsAtBothEnds(t *testing.T) {
	m, _ := withOptions()
	m = openFzfOptions(t, m)

	if up := press(t, m, "up", "k"); up.options.cursor != 0 {
		t.Fatalf("cursor above the top = %d", up.options.cursor)
	}
	if down := press(t, m, "down", "j"); down.options.cursor != 2 {
		t.Fatalf("cursor after down, j = %d, want 2", down.options.cursor)
	}
	if end := press(t, m, "down", "down", "down", "down", "down", "down"); end.options.cursor != 4 {
		t.Fatalf("cursor past the bottom = %d, want 4", end.options.cursor)
	}
}

func TestKeysAreIgnoredWhileAnOptionIsBeingWritten(t *testing.T) {
	m, b := withOptions()
	m = openFzfOptions(t, m)
	writing, cmd := m.Update(key("space"))
	if cmd == nil {
		t.Fatal("setup: expected a write")
	}

	for _, k := range []string{"space", "down", "esc", "enter", "q"} {
		next, again := writing.(Model).Update(key(k))
		if again != nil || next.(Model).screen != screenOptions || next.(Model).options.cursor != 0 {
			t.Fatalf("%s while writing must be ignored", k)
		}
	}
	if _, quit := writing.(Model).Update(key("ctrl+c")); quit == nil {
		t.Fatal("ctrl+c must still quit")
	}
	settle(writing.(Model), cmd)
	if len(b.optionCalls) != 1 {
		t.Fatalf("backend calls = %v, want exactly the one write", b.optionCalls)
	}
}

func TestOptionTextIsNeutralisedBeforeItIsShownOrTyped(t *testing.T) {
	m, b := withOptions()
	b.optionRows["fzf"][2].Help = "evil\x1b]0;pwned\x07\x1b[2J\nhelp"
	b.optionRows["fzf"][2].Value = "v\x1b[31m"
	m = onOption(t, openFzfOptions(t, m), "prefix")
	for _, r := range m.options.rows {
		for _, s := range []string{r.Key, r.Help, r.Value, r.Default} {
			if strings.ContainsAny(s, "\x1b\x07\n") {
				t.Fatalf("option text still holds a control character: %q", s)
			}
		}
	}

	m = press(t, m, "enter")
	typed, _ := m.Update(tea.KeyPressMsg{Code: 'x', Text: "x\x1b[2J"})
	if strings.ContainsAny(typed.(Model).options.input, "\x1b") {
		t.Fatalf("typed text still holds an escape: %q", typed.(Model).options.input)
	}
}

func TestTheChangeCounterCountsOptionChangesNetAndWithModuleChanges(t *testing.T) {
	m, _ := withOptions()
	m = openFzfOptions(t, m)
	if m.changes() != 0 {
		t.Fatalf("changes = %d before touching anything, want 0", m.changes())
	}

	m = settleKey(t, m, "space")
	if m.changes() != 1 {
		t.Fatalf("changes = %d after one option, want 1", m.changes())
	}
	m = settleKey(t, onOption(t, m, "theme"), "right")
	if m.changes() != 2 {
		t.Fatalf("changes = %d after two options, want 2", m.changes())
	}
	m = settleKey(t, onOption(t, m, "ctrl_r"), "space") // back to the first value
	if m.changes() != 1 {
		t.Fatalf("changes = %d after setting one back, want 1", m.changes())
	}

	m = press(t, m, "esc") // browser: fzf is selected; toggle it on
	m = space(m)
	if m.changes() != 2 {
		t.Fatalf("changes = %d with one option and one module change, want 2", m.changes())
	}
}

func TestAFailedRereadAfterASuccessfulWriteShowsTheErrorAndKeepsTheScreen(t *testing.T) {
	m, b := withOptions()
	m = openFzfOptions(t, m)
	b.optionsErr = errors.New("cannot re-read")

	m = settleKey(t, m, "space")

	if m.screen != screenOptions || !strings.Contains(m.status, "cannot re-read") || m.pending {
		t.Fatalf("screen=%v status=%q pending=%v", m.screen, m.status, m.pending)
	}
	if len(b.optionCalls) != 1 {
		t.Fatalf("the write itself must have happened once: %v", b.optionCalls)
	}
}

func TestTheStatusMessageClearsOnTheNextKeyOnTheOptionsScreen(t *testing.T) {
	m, b := withOptions()
	b.setOptionErr = errors.New("nope")
	m = openFzfOptions(t, m)
	m = settleKey(t, m, "space")
	if m.status == "" {
		t.Fatal("setup: expected a message")
	}

	if m = press(t, m, "down"); m.status != "" {
		t.Fatalf("status = %q, want it cleared by the key press", m.status)
	}
}

// The browser still takes keys while the options of a module are loading, so a
// `/` pressed in that moment starts the filter. The options screen must not
// carry that typing mode across: back in the browser, q must quit again.
func TestFilterTypingStartedWhileTheOptionsLoadDoesNotSurviveTheOptionsScreen(t *testing.T) {
	m, _ := withOptions()
	m = press(t, m, "down") // fzf
	loading, cmd := m.Update(key("o"))
	typing := press(t, loading.(Model), "/")
	if !typing.filtering {
		t.Fatal("setup: the browser should still take / while the options load")
	}

	opened := settle(typing, cmd)
	back := press(t, opened, "esc")

	if back.screen != screenBrowser || back.filtering {
		t.Fatalf("screen=%v filtering=%v, want the browser in browsing mode", back.screen, back.filtering)
	}
	if _, quit := back.Update(key("q")); quit == nil {
		t.Fatal("q must quit from the browser again, not be typed into a filter")
	}
}

// Results that arrive for a screen or module that is no longer the one shown
// must not touch it. Today the pending flag makes that impossible; these tests
// keep it impossible when a key is added to the browser later.
func TestAnOptionWriteResultForAnotherModuleOrScreenIsDropped(t *testing.T) {
	m, _ := withOptions()
	m = openFzfOptions(t, m)
	browser := press(t, m, "esc") // keys are ignored while a write is pending, so leave first
	m.pending = true
	elsewhere := []modedit.OptionView{{Key: "other", Type: "bool", Value: "true", Editable: true}}

	other := m.applyOptionWritten(optionWrittenMsg{id: "some-other-module", rows: elsewhere})
	if other.pending || len(other.options.rows) != 5 || other.options.id != "fzf" {
		t.Fatalf("pending=%v rows=%d id=%q, want fzf's rows untouched and the wait over", other.pending, len(other.options.rows), other.options.id)
	}
	if _, ok := other.optionNow["some-other-module"]; ok {
		t.Fatal("a dropped result must not enter the change count")
	}

	browser.pending = true
	late := browser.applyOptionWritten(optionWrittenMsg{id: "fzf", rows: elsewhere})
	if late.pending || late.screen != screenBrowser || len(late.options.rows) != 0 {
		t.Fatalf("pending=%v screen=%v rows=%d, want the browser untouched", late.pending, late.screen, len(late.options.rows))
	}
}

func TestAnOptionsLoadResultThatArrivesOnAnotherScreenIsDropped(t *testing.T) {
	m, _ := withOptions()
	m.screen = screenPlan
	m.pending = true

	got := m.applyOptions(optionsMsg{id: "fzf", rows: sampleOptions()})

	if got.screen != screenPlan || got.pending || len(got.options.rows) != 0 {
		t.Fatalf("screen=%v pending=%v rows=%d, want the plan screen untouched and the wait over", got.screen, got.pending, len(got.options.rows))
	}
}
