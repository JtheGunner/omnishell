package tui

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/JtheGunner/omnishell/internal/modedit"
)

func selectedStatus(t *testing.T, m Model) modedit.Status {
	t.Helper()
	v, ok := m.selected()
	if !ok {
		t.Fatal("nothing is selected")
	}
	return v.Status
}

func TestSpaceEnablesADisabledModule(t *testing.T) {
	m, b := newBackedModel(sampleViews())
	m = press(t, m, "down") // fzf, disabled

	m = space(m)

	if want := []string{"enable fzf"}; !reflect.DeepEqual(b.calls, want) {
		t.Fatalf("backend calls = %v, want %v", b.calls, want)
	}
	if got := selectedStatus(t, m); got != modedit.StatusEnabled {
		t.Fatalf("fzf status = %q, want enabled", got)
	}
	if m.changes() != 1 {
		t.Fatalf("changes = %d, want 1", m.changes())
	}
}

func TestSpaceDisablesAnEnabledModule(t *testing.T) {
	m, b := newBackedModel(sampleViews()) // completion is first and enabled

	m = space(m)

	if want := []string{"disable completion"}; !reflect.DeepEqual(b.calls, want) {
		t.Fatalf("backend calls = %v, want %v", b.calls, want)
	}
	if got := selectedStatus(t, m); got != modedit.StatusDisabled {
		t.Fatalf("completion status = %q, want disabled", got)
	}
}

// Update must not do I/O itself: the write belongs to the command it returns.
func TestUpdateOnlyReturnsTheWriteAsACommand(t *testing.T) {
	m, b := newBackedModel(sampleViews())

	_, cmd := m.Update(key("space"))

	if cmd == nil {
		t.Fatal("space on a module must return a command")
	}
	if len(b.calls) != 0 {
		t.Fatalf("Update wrote %v before the command ran", b.calls)
	}
}

func TestTogglingBackLeavesNoChanges(t *testing.T) {
	m := space(space(newTestModel(sampleViews())))

	if m.changes() != 0 {
		t.Fatalf("changes = %d, want 0 after toggling the same module twice", m.changes())
	}
}

func TestChangesCountEveryModuleThatDiffersFromTheStart(t *testing.T) {
	m := newTestModel(sampleViews())
	m = space(m)                   // completion off
	m = space(press(t, m, "down")) // fzf on
	m = space(press(t, m, "down")) // zshonly on

	if m.changes() != 3 {
		t.Fatalf("changes = %d, want 3", m.changes())
	}
}

func TestRejectedToggleKeepsTheCheckboxAndShowsTheMessage(t *testing.T) {
	m, b := newBackedModel(sampleViews())
	b.toggleErr = errors.New(`module "zshonly" only supports zsh, but none of your managed shells (bash) do`)
	m = press(t, m, "down", "down") // zshonly, disabled

	m = space(m)

	if got := selectedStatus(t, m); got != modedit.StatusDisabled {
		t.Fatalf("zshonly status = %q, want it unchanged (disabled)", got)
	}
	if m.changes() != 0 {
		t.Fatalf("changes = %d, want 0 after a rejected toggle", m.changes())
	}
	if !strings.Contains(m.status, `module "zshonly" only supports zsh`) {
		t.Fatalf("status = %q, want the rejection message", m.status)
	}
}

func TestTheNextKeyPressClearsTheStatusMessage(t *testing.T) {
	m, b := newBackedModel(sampleViews())
	b.toggleErr = errors.New("nope")
	m = space(m)
	if m.status == "" {
		t.Fatal("setup: expected a status message")
	}

	m = press(t, m, "down")

	if m.status != "" {
		t.Fatalf("status = %q, want it cleared by the key press", m.status)
	}
}

func TestToggleKeepsTheCursorOnTheSameModuleUnderAFilter(t *testing.T) {
	m := press(t, newTestModel(sampleViews()), "/", "z", "enter") // fzf, zshonly
	m = press(t, m, "down")                                       // zshonly

	m = space(m)

	if v, _ := m.selected(); v.ID != "zshonly" {
		t.Fatalf("selected = %q, want zshonly to stay selected", v.ID)
	}
	if m.filter != "z" || len(m.visible) != 2 {
		t.Fatalf("filter=%q visible=%v, want the filter kept", m.filter, visibleIDs(m))
	}
}

func TestSpaceOnAnEmptyOrUnmatchedListDoesNothing(t *testing.T) {
	empty := newTestModel(nil)
	if _, cmd := empty.Update(key("space")); cmd != nil {
		t.Fatal("space on an empty list must not return a command")
	}

	m, b := newBackedModel(sampleViews())
	m = press(t, m, "/", "n", "o", "p", "e", "enter")
	if _, cmd := m.Update(key("space")); cmd != nil {
		t.Fatal("space with no match must not return a command")
	}
	if len(b.calls) != 0 {
		t.Fatalf("backend calls = %v, want none", b.calls)
	}
}

func TestSpaceWhileTypingTheFilterIsText(t *testing.T) {
	m, b := newBackedModel(sampleViews())
	m = press(t, m, "/", "a")

	next, cmd := m.Update(key("space"))

	if cmd != nil || len(b.calls) != 0 {
		t.Fatalf("space in the filter must not toggle: cmd=%v calls=%v", cmd != nil, b.calls)
	}
	if got := next.(Model).filter; got != "a " {
		t.Fatalf("filter = %q, want %q", got, "a ")
	}
}

func TestFailedRefreshAfterASuccessfulWriteShowsTheError(t *testing.T) {
	m, b := newBackedModel(sampleViews())
	b.modulesErr = errors.New("cannot re-read config")

	m = space(m)

	if !strings.Contains(m.status, "cannot re-read config") {
		t.Fatalf("status = %q, want the refresh error", m.status)
	}
	if want := []string{"disable completion"}; !reflect.DeepEqual(b.calls, want) {
		t.Fatalf("the write itself must still have happened once: %v", b.calls)
	}
}

func TestToggleOfAModuleThatIsUnavailableOnThisHostIsStillAttempted(t *testing.T) {
	// Dimming is advice, not a lock: modedit.Enable is the authority on what
	// is allowed, and its error reaches the status line.
	m, b := newBackedModel(sampleViews())
	m = press(t, m, "down", "down") // zshonly, Unavailable

	space(m)

	if want := []string{"enable zshonly"}; !reflect.DeepEqual(b.calls, want) {
		t.Fatalf("backend calls = %v, want %v", b.calls, want)
	}
}
