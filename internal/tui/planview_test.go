package tui

import (
	"errors"
	"strings"
	"testing"
)

const samplePlan = `Plan (package manager: apt, shells: bash)

  install  completion          snippet: bash
  install  fzf                 snippet: bash   packages: fzf
  skip     macosonly           not supported on linux

2 to install, 0 to update, 1 skipped`

// planScreens returns the plan screen in each of its states at 80x20.
func planScreens(t *testing.T) map[string]Model {
	t.Helper()
	waiting, _ := withPlan(PlanPreview{Text: samplePlan, NeedsApply: true}, nil)
	next, _ := waiting.Update(key("a"))

	ready, _ := withPlan(PlanPreview{Text: samplePlan, NeedsApply: true}, nil)
	long, _ := withPlan(PlanPreview{Text: planText(100), NeedsApply: true}, nil)
	nothing, _ := withPlan(PlanPreview{Text: "Plan (package manager: apt, shells: bash)\n\n0 modules", NeedsApply: false}, nil)
	failed, _ := withPlan(PlanPreview{}, errors.New(`module "fzf": option ctrl_r: "yes please" is not a bool`))

	return map[string]Model{
		"plan-loading":  next.(Model),
		"plan-ready":    openPlan(ready),
		"plan-scrolled": press(t, openPlan(long), "pgdown", "down"),
		"plan-nothing":  openPlan(nothing),
		"plan-error":    openPlan(failed),
	}
}

func TestPlanScreenFillsTheTerminalExactlyInEveryState(t *testing.T) {
	for name, m := range planScreens(t) {
		assertFits(t, plain(m), 80, 20)
		big := sized(m, 120, 40)
		assertFits(t, plain(big), 120, 40)
		if t.Failed() {
			t.Fatalf("state %s did not fit", name)
		}
	}
}

func TestPlanScreenShowsTheWaitingNoteAndOnlyBack(t *testing.T) {
	out := plain(planScreens(t)["plan-loading"])

	if !strings.Contains(out, "plan preview") || !strings.Contains(out, "computing plan…") {
		t.Fatalf("waiting screen:\n%s", out)
	}
	if strings.Contains(out, "continue to apply") {
		t.Fatalf("the way forward must not be offered before the plan is shown:\n%s", out)
	}
}

func TestPlanScreenShowsThePlanAndOffersTheWayForward(t *testing.T) {
	out := plain(planScreens(t)["plan-ready"])

	for _, want := range []string{
		"plan preview", "install  fzf                 snippet: bash   packages: fzf",
		"skip     macosonly", "y/enter continue to apply", "esc back",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "lines ") {
		t.Fatalf("a plan that fits needs no line counter:\n%s", out)
	}
}

func TestPlanScreenCountsLinesWhileScrolling(t *testing.T) {
	long := planScreens(t)["plan-scrolled"] // one page and one line down: offset 19

	out := plain(long)

	if !strings.Contains(out, "lines 20-37 of 100") {
		t.Fatalf("header should show the window:\n%s", out)
	}
	if !strings.Contains(out, "line 020") || strings.Contains(out, "line 019") || strings.Contains(out, "line 038") {
		t.Fatalf("body should hold exactly lines 20 to 37:\n%s", out)
	}
}

func TestPlanScreenWithNothingToApplyExplainsAndOffersOnlyBack(t *testing.T) {
	out := plain(planScreens(t)["plan-nothing"])

	if !strings.Contains(out, nothingToApply) {
		t.Fatalf("missing the note:\n%s", out)
	}
	if strings.Contains(out, "continue to apply") {
		t.Fatalf("there is nothing to continue to:\n%s", out)
	}
}

func TestPlanScreenShowsTheErrorAndOffersOnlyBack(t *testing.T) {
	out := plain(planScreens(t)["plan-error"])

	if !strings.Contains(out, `Could not compute the plan: module "fzf"`) {
		t.Fatalf("missing the error:\n%s", out)
	}
	if strings.Contains(out, "continue to apply") {
		t.Fatalf("a failed plan must not offer apply:\n%s", out)
	}
}

func TestPlanScreenKeepsLongLinesAndLongErrorsInsideTheTerminal(t *testing.T) {
	long, _ := withPlan(PlanPreview{Text: "install " + strings.Repeat("x", 400), NeedsApply: true}, nil)
	assertFits(t, plain(openPlan(long)), 80, 20)

	failed, _ := withPlan(PlanPreview{}, errors.New(strings.Repeat("a very long reason ", 60)))
	out := plain(openPlan(failed))
	assertFits(t, out, 80, 20)
	if !strings.Contains(out, "Could not compute the plan: a very long reason") {
		t.Fatalf("the start of the error must stay readable:\n%s", out)
	}
}

func TestPlanScreenGivesWayToTheTooSmallMessage(t *testing.T) {
	m := sized(planScreens(t)["plan-ready"], 60, 10)

	if out := plain(m); !strings.Contains(out, "Terminal too small") || strings.Contains(out, "plan preview") {
		t.Fatalf("a small terminal must show the too-small message on this screen too:\n%s", out)
	}
}

func TestBrowserHelpMentionsThePlanKey(t *testing.T) {
	if out := plain(sized(newTestModel(sampleViews()), 80, 20)); !strings.Contains(out, "a plan") {
		t.Fatalf("the browser footer should list the plan key:\n%s", out)
	}
}

func TestPlanScreenGoldenFiles(t *testing.T) {
	for name, m := range planScreens(t) {
		assertGolden(t, name, plain(m))
	}
}
