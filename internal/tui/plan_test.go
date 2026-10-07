package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// planText returns n numbered lines, enough to need scrolling.
func planText(n int) string {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %03d", i+1)
	}
	return strings.Join(lines, "\n")
}

// withPlan returns a sized model over sampleViews whose backend answers Plan
// with preview, and the backend itself.
func withPlan(preview PlanPreview, err error) (Model, *fakeBackend) {
	m, b := newBackedModel(sampleViews())
	b.preview, b.planErr = preview, err
	return sized(m, 80, 20), b
}

// openPlan presses a and lets the plan finish computing.
func openPlan(m Model) Model {
	next, cmd := m.Update(key("a"))
	return settle(next.(Model), cmd)
}

func TestPressingAOpensTheWaitingScreenAndComputesThePlanInACommand(t *testing.T) {
	m, b := withPlan(PlanPreview{Text: "Plan (apt)\n  install  completion", NeedsApply: true}, nil)

	next, cmd := m.Update(key("a"))
	waiting := next.(Model)

	if waiting.screen != screenPlan || !waiting.plan.loading {
		t.Fatalf("screen=%v loading=%v, want the plan screen waiting for the plan", waiting.screen, waiting.plan.loading)
	}
	if cmd == nil {
		t.Fatal("the plan must be computed by a command")
	}
	if b.planCalls != 0 {
		t.Fatalf("Update computed the plan itself (%d calls); it must do no I/O", b.planCalls)
	}

	ready := settle(waiting, cmd)
	if b.planCalls != 1 || ready.plan.loading {
		t.Fatalf("planCalls=%d loading=%v, want the plan computed once and shown", b.planCalls, ready.plan.loading)
	}
	if want := []string{"Plan (apt)", "  install  completion"}; fmt.Sprint(ready.plan.lines) != fmt.Sprint(want) {
		t.Fatalf("lines = %q, want %q", ready.plan.lines, want)
	}
}

func TestYAndEnterLeadOnToApplyOnceThePlanIsShown(t *testing.T) {
	for _, name := range []string{"y", "enter"} {
		m, _ := withPlan(PlanPreview{Text: "Plan", NeedsApply: true}, nil)
		m = openPlan(m)

		next, cmd := m.Update(key(name))

		if cmd == nil {
			t.Fatalf("%s: expected the UI to close", name)
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Fatalf("%s: the command did not quit", name)
		}
		if !next.(Model).applyRequested {
			t.Fatalf("%s: the apply was not requested", name)
		}
	}
}

func TestYDoesNothingWhileThePlanIsStillBeingComputed(t *testing.T) {
	m, _ := withPlan(PlanPreview{Text: "Plan", NeedsApply: true}, nil)
	waiting, _ := m.Update(key("a"))

	next, cmd := waiting.(Model).Update(key("y"))

	if cmd != nil || next.(Model).applyRequested {
		t.Fatal("y before the plan has been seen must not lead to apply")
	}
}

func TestAPlanWithNothingToApplyOffersOnlyBack(t *testing.T) {
	m, _ := withPlan(PlanPreview{Text: "Plan (apt)\n\n0 modules", NeedsApply: false}, nil)
	m = openPlan(m)

	if got := m.plan.lines[len(m.plan.lines)-1]; got != nothingToApply {
		t.Fatalf("last line = %q, want the nothing-to-apply note", got)
	}
	for _, name := range []string{"y", "enter"} {
		next, cmd := m.Update(key(name))
		if cmd != nil || next.(Model).applyRequested {
			t.Fatalf("%s on a plan with nothing to apply must not lead to apply", name)
		}
	}
}

func TestAPlanErrorIsShownAndOffersOnlyBack(t *testing.T) {
	m, _ := withPlan(PlanPreview{}, errors.New("module \"fzf\": option ctrl_r: not a bool"))
	m = openPlan(m)

	if !strings.Contains(m.plan.err, `module "fzf"`) {
		t.Fatalf("err = %q, want the message", m.plan.err)
	}
	if next, cmd := m.Update(key("y")); cmd != nil || next.(Model).applyRequested {
		t.Fatal("y on a failed plan must not lead to apply")
	}
	if back := press(t, m, "esc"); back.screen != screenBrowser {
		t.Fatal("esc must leave a failed plan")
	}
}

func TestEscNAndBGoBackAndKeepTheBrowserState(t *testing.T) {
	for _, name := range []string{"esc", "n", "b"} {
		m, _ := withPlan(PlanPreview{Text: "Plan", NeedsApply: true}, nil)
		m = press(t, m, "/", "z", "enter", "down") // filter z, cursor on zshonly
		m = openPlan(m)

		back := press(t, m, name)

		if back.screen != screenBrowser {
			t.Fatalf("%s: still on the plan screen", name)
		}
		if v, _ := back.selected(); v.ID != "zshonly" || back.filter != "z" {
			t.Fatalf("%s: selected=%q filter=%q, want the selection and filter kept", name, v.ID, back.filter)
		}
	}
}

// The plan can take seconds. A user who has gone back must not be thrown onto
// the plan screen when the answer finally arrives, nor see it later.
func TestAPlanThatArrivesAfterGoingBackIsDropped(t *testing.T) {
	m, b := withPlan(PlanPreview{Text: "Plan", NeedsApply: true}, nil)
	waiting, cmd := m.Update(key("a"))
	back := press(t, waiting.(Model), "esc")

	late := settle(back, cmd)

	if late.screen != screenBrowser || late.plan.lines != nil {
		t.Fatalf("screen=%v lines=%v, want the late plan ignored", late.screen, late.plan.lines)
	}
	if b.planCalls != 1 {
		t.Fatalf("planCalls = %d, want the one request that was made", b.planCalls)
	}
}

func TestAnOldPlanDoesNotAnswerANewerRequest(t *testing.T) {
	m, _ := withPlan(PlanPreview{Text: "fresh", NeedsApply: true}, nil)
	first, firstCmd := m.Update(key("a"))
	back := press(t, first.(Model), "esc")
	second, secondCmd := back.Update(key("a"))

	afterOld := settle(second.(Model), firstCmd)
	if !afterOld.plan.loading {
		t.Fatal("the answer to the first request must not fill the second screen")
	}
	afterNew := settle(afterOld, secondCmd)
	if afterNew.plan.loading || len(afterNew.plan.lines) == 0 {
		t.Fatal("the answer to the second request must be shown")
	}
}

func TestThePlanIsComputedAgainEachTimeItIsOpened(t *testing.T) {
	m, b := withPlan(PlanPreview{Text: "Plan", NeedsApply: true}, nil)

	m = press(t, openPlan(m), "esc")
	m = space(m) // the config changes in between
	openPlan(m)

	if b.planCalls != 2 {
		t.Fatalf("planCalls = %d, want 2: a plan must reflect the config as it is now", b.planCalls)
	}
}

func TestAIsIgnoredWhileAToggleIsBeingWritten(t *testing.T) {
	m, b := withPlan(PlanPreview{Text: "Plan", NeedsApply: true}, nil)
	writing, _ := m.Update(key("space")) // the write has not finished

	next, cmd := writing.(Model).Update(key("a"))

	if cmd != nil || next.(Model).screen != screenBrowser {
		t.Fatal("a plan must not be requested while the config is being written")
	}
	if b.planCalls != 0 {
		t.Fatalf("planCalls = %d, want 0", b.planCalls)
	}
}

func TestAIsTextWhileTypingTheFilter(t *testing.T) {
	m, b := withPlan(PlanPreview{Text: "Plan", NeedsApply: true}, nil)
	m = press(t, m, "/", "a")

	if m.screen != screenBrowser || m.filter != "a" || b.planCalls != 0 {
		t.Fatalf("screen=%v filter=%q planCalls=%d, want a typed into the filter", m.screen, m.filter, b.planCalls)
	}
}

func TestQuitKeysOnThePlanScreenLeaveWithoutApplying(t *testing.T) {
	for _, name := range []string{"q", "ctrl+c"} {
		m, _ := withPlan(PlanPreview{Text: "Plan", NeedsApply: true}, nil)
		m = openPlan(m)

		next, cmd := m.Update(key(name))

		if cmd == nil || next.(Model).applyRequested {
			t.Fatalf("%s: want the UI to close without requesting an apply", name)
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Fatalf("%s: the command did not quit", name)
		}
	}
}

func TestScrollingMovesTheWindowAndStopsAtBothEnds(t *testing.T) {
	m, _ := withPlan(PlanPreview{Text: planText(100), NeedsApply: true}, nil)
	m = openPlan(m) // 80x20: 18 body rows, 100 lines, so the last offset is 82

	if m.plan.offset != 0 {
		t.Fatalf("offset = %d, want to start at the top", m.plan.offset)
	}
	if up := press(t, m, "up", "k"); up.plan.offset != 0 {
		t.Fatalf("offset above the top = %d, want 0", up.plan.offset)
	}
	if down := press(t, m, "down", "j"); down.plan.offset != 2 {
		t.Fatalf("offset after down, j = %d, want 2", down.plan.offset)
	}
	end := press(t, m, "end")
	if end.plan.offset != 82 {
		t.Fatalf("offset at the end = %d, want 82", end.plan.offset)
	}
	if past := press(t, end, "down", "pgdown"); past.plan.offset != 82 {
		t.Fatalf("offset past the end = %d, want 82", past.plan.offset)
	}
	if page := press(t, m, "pgdown"); page.plan.offset != 18 {
		t.Fatalf("offset after one page = %d, want 18", page.plan.offset)
	}
	if home := press(t, end, "home"); home.plan.offset != 0 {
		t.Fatalf("offset after home = %d, want 0", home.plan.offset)
	}
	if g := press(t, end, "g"); g.plan.offset != 0 {
		t.Fatalf("offset after g = %d, want 0", g.plan.offset)
	}
	if bigG := press(t, m, "G"); bigG.plan.offset != 82 {
		t.Fatalf("offset after G = %d, want 82", bigG.plan.offset)
	}
}

func TestAPlanShorterThanTheScreenDoesNotScroll(t *testing.T) {
	m, _ := withPlan(PlanPreview{Text: planText(5), NeedsApply: true}, nil)
	m = openPlan(m)

	if down := press(t, m, "down", "pgdown", "end"); down.plan.offset != 0 {
		t.Fatalf("offset = %d, want 0 for a plan that fits", down.plan.offset)
	}
}

func TestPlanTextAndErrorsAreNeutralisedBeforeTheyAreShown(t *testing.T) {
	m, _ := withPlan(PlanPreview{Text: "install bad\x1b]0;pwned\x07\x1b[2J\tname\nsecond", NeedsApply: true}, nil)
	m = openPlan(m)
	for _, line := range m.plan.lines {
		if strings.ContainsAny(line, "\x1b\x07\t") {
			t.Fatalf("a plan line still holds a control character: %q", line)
		}
	}

	failed, _ := withPlan(PlanPreview{}, errors.New("bad\x1b[2J\nnews"))
	failed = openPlan(failed)
	if strings.ContainsAny(failed.plan.err, "\x1b\n") {
		t.Fatalf("the error still holds a control character: %q", failed.plan.err)
	}
}

func TestResizingKeepsThePlanScrollPositionInRange(t *testing.T) {
	m, _ := withPlan(PlanPreview{Text: planText(100), NeedsApply: true}, nil)
	m = press(t, openPlan(m), "end") // offset 82 at 20 rows

	m = sized(m, 80, 50) // 48 rows: the largest sensible offset is now 52

	if rows := m.planRows(); m.plan.offset > m.maxPlanOffset() {
		t.Fatalf("offset %d is past the end (%d) after growing to %d rows", m.plan.offset, m.maxPlanOffset(), rows)
	}
}
