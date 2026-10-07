package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/JtheGunner/omnishell/internal/modedit"
)

// optionScreens returns the options screen of fzf in the states worth a
// snapshot, at 80x20.
func optionScreens(t *testing.T) map[string]Model {
	t.Helper()
	open := func() (Model, *fakeBackend) {
		m, b := withOptions()
		return openFzfOptions(t, m), b
	}

	bool_, _ := open()
	enum, _ := open()
	editing, _ := open()
	list, _ := open()
	failing, failingBackend := open()
	failingBackend.setOptionErr = errors.New(`invalid value for fzf.prefix: "ABC" does not match required pattern ^[a-z]+$`)

	return map[string]Model{
		"options-bool":    bool_,
		"options-enum":    onOption(t, enum, "theme"),
		"options-editing": press(t, onOption(t, editing, "prefix"), "enter", "x"),
		"options-list":    onOption(t, list, "extras"),
		"options-error": settleKey(t, press(t, onOption(t, failing, "prefix"), "enter", "backspace", "backspace", "backspace", "A", "B", "C"),
			"enter"),
	}
}

func TestOptionsScreenFillsTheTerminalExactlyInEveryState(t *testing.T) {
	for name, m := range optionScreens(t) {
		assertFits(t, plain(m), 80, 20)
		assertFits(t, plain(sized(m, 120, 40)), 120, 40)
		if t.Failed() {
			t.Fatalf("state %s did not fit", name)
		}
	}
}

func TestOptionsScreenHeaderNamesTheModuleAndCountsChanges(t *testing.T) {
	m := optionScreens(t)["options-bool"]
	if out := plain(m); !strings.Contains(out, "options · fzf · 0 changes since start") {
		t.Fatalf("header before a change:\n%s", out)
	}

	m = settleKey(t, m, "space")
	if out := plain(m); !strings.Contains(out, "options · fzf · 1 change since start") {
		t.Fatalf("header after a change:\n%s", out)
	}
}

func TestOptionsTableShowsKeysValuesTypesAndWhereAValueComesFrom(t *testing.T) {
	m, b := withOptions()
	b.optionRows["fzf"][3].Value, b.optionRows["fzf"][3].Set = "9", true         // retries: set
	b.optionRows["fzf"][4].Value, b.optionRows["fzf"][4].Invalid = "weird", true // theme: invalid
	b.optionRows["fzf"][4].Set = true
	out := plain(openFzfOptions(t, m))

	for _, want := range []string{
		"▸ ctrl_r", "true (default)", "bool",
		"  extras", "a,b (default)", "list<string>",
		"  retries", " 9 ", "int",
		"weird (invalid)", "enum",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "9 (default)") {
		t.Fatalf("an option that is set must not be labelled default:\n%s", out)
	}
}

func TestOptionsDetailExplainsTheSelectedOptionByType(t *testing.T) {
	m := openFzfOptions(t, mustWithOptions())
	cases := []struct {
		key  string
		want []string
	}{
		{"ctrl_r", []string{"Bind Ctrl+R to the fzf history widget", "Type:    bool · space toggles", "Default: true"}},
		{"theme", []string{"Colour theme", "Type:    enum · ←/→ changes", "Allowed: dark, light, solarized"}},
		{"prefix", []string{"Key prefix", "Type:    string · enter edits", "Pattern: ^[a-z]+$"}},
		{"retries", []string{"Type:    int · enter edits", "Default: 3"}},
		{"extras", []string{"Type:    list<string> · a list; set it with omnishell set"}},
	}
	for _, c := range cases {
		out := plain(onOption(t, m, c.key))
		for _, want := range c.want {
			if !strings.Contains(out, want) {
				t.Fatalf("%s: missing %q:\n%s", c.key, want, out)
			}
		}
	}
}

func mustWithOptions() Model {
	m, _ := withOptions()
	return m
}

func TestOptionsDetailWarnsWhenTheModuleIsNotEnabledAndStopsOnceItIs(t *testing.T) {
	m, _ := withOptions()
	disabled := plain(openFzfOptions(t, m))
	if !strings.Contains(disabled, "this module is not enabled") {
		t.Fatalf("a disabled module needs the note:\n%s", disabled)
	}

	m, _ = withOptions()
	m = space(press(t, m, "down")) // enable fzf in the browser first
	next, cmd := m.Update(key("o"))
	enabled := plain(settle(next.(Model), cmd))
	if strings.Contains(enabled, "not enabled") {
		t.Fatalf("an enabled module must not get the note:\n%s", enabled)
	}
}

func TestOptionsEditShowsTheTypedTextInPlaceOfTheValue(t *testing.T) {
	out := plain(optionScreens(t)["options-editing"])

	if !strings.Contains(out, "[abcx_]") {
		t.Fatalf("the input cell is missing:\n%s", out)
	}
	if !strings.Contains(out, "type a value · enter save · esc cancel") {
		t.Fatalf("the editing footer is missing:\n%s", out)
	}
}

func TestOptionsErrorReplacesTheFooterAndKeepsTheInput(t *testing.T) {
	out := plain(optionScreens(t)["options-error"])

	if !strings.Contains(out, `! invalid value for fzf.prefix: "ABC" does not match required pattern`) {
		t.Fatalf("the status line is missing:\n%s", out)
	}
	if !strings.Contains(out, "[ABC_]") {
		t.Fatalf("the typed text must stay visible for correction:\n%s", out)
	}
}

func TestOptionsFooterShowsSavingWhileAWriteIsPending(t *testing.T) {
	m := optionScreens(t)["options-bool"]
	writing, cmd := m.Update(key("space"))

	pending := plain(writing.(Model))
	if !strings.Contains(pending, "saving…") {
		t.Fatalf("a pending write must be visible:\n%s", pending)
	}
	assertFits(t, pending, 80, 20)
	if done := plain(settle(writing.(Model), cmd)); strings.Contains(done, "saving…") {
		t.Fatalf("the marker must disappear once the write is done:\n%s", done)
	}
}

func TestOptionsListOptionsAreDimmedBecauseTheyCannotBeEditedHere(t *testing.T) {
	m := optionScreens(t)["options-bool"]
	rows := m.optionTable(10)

	const faint = "\x1b[2m"
	if strings.Contains(rows[0], faint) { // ctrl_r is editable (and selected: reverse, not faint)
		t.Fatalf("an editable option must not be dimmed: %q", rows[0])
	}
	if !strings.Contains(rows[1], faint) { // extras is a list
		t.Fatalf("a list option must be dimmed: %q", rows[1])
	}
}

func TestOptionsScreenScrollsTheTableToKeepTheCursorVisible(t *testing.T) {
	m, b := withOptions()
	many := make([]modedit.OptionView, 30)
	for i := range many {
		many[i] = modedit.OptionView{Key: fmt.Sprintf("opt%02d", i), Type: "bool", Default: "false", Value: "false", Editable: true}
	}
	b.optionRows["fzf"] = many
	m = openFzfOptions(t, m)
	for range 29 {
		m = press(t, m, "down")
	}

	out := plain(m)
	if !strings.Contains(out, "▸ opt29") || strings.Contains(out, "opt00") {
		t.Fatalf("the cursor row opt29 must be visible and opt00 scrolled out:\n%s", out)
	}
	assertFits(t, out, 80, 20)
}

func TestOptionsScreenKeepsLongValuesAndHelpInsideTheTerminal(t *testing.T) {
	m, b := withOptions()
	b.optionRows["fzf"][2].Value = strings.Repeat("v", 300)
	b.optionRows["fzf"][2].Key = strings.Repeat("k", 100)
	b.optionRows["fzf"][2].Help = strings.Repeat("a very long help text ", 40)
	m = onOption(t, openFzfOptions(t, m), strings.Repeat("k", 100))

	assertFits(t, plain(m), 80, 20)
	assertFits(t, plain(press(t, m, "enter")), 80, 20)
}

func TestOptionsScreenGivesWayToTheTooSmallMessage(t *testing.T) {
	m := sized(optionScreens(t)["options-bool"], 60, 10)

	if out := plain(m); !strings.Contains(out, "Terminal too small") || strings.Contains(out, "options ·") {
		t.Fatalf("a small terminal must show the too-small message:\n%s", out)
	}
}

func TestBrowserHelpMentionsTheOptionsKey(t *testing.T) {
	if out := plain(sized(newTestModel(sampleViews()), 80, 20)); !strings.Contains(out, "o options") {
		t.Fatalf("the browser footer should list the options key:\n%s", out)
	}
}

func TestOptionsScreenGoldenFiles(t *testing.T) {
	for name, m := range optionScreens(t) {
		assertGolden(t, name, plain(m))
	}
}
