package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// screen says which of the two screens is showing.
type screen int

const (
	screenBrowser screen = iota
	screenPlan
	screenOptions
)

// nothingToApply ends the plan text when applying would change nothing.
const nothingToApply = "Nothing to apply: no module or package changes are planned."

// planState is the plan screen: waiting for the plan, the error that replaced
// it, or the plan itself, scrolled to offset.
type planState struct {
	loading    bool
	err        string   // the reason the plan could not be computed, cleaned
	lines      []string // the plan, one cleaned line per element
	needsApply bool
	offset     int // index of the first visible line
}

// canApply reports whether y may hand over to apply: the plan is there and
// applying would do something.
func (p planState) canApply() bool {
	return !p.loading && p.err == "" && p.needsApply
}

// planMsg carries a computed plan back to Update. seq says which request it
// answers, so a plan the user has already walked away from can be dropped.
type planMsg struct {
	seq     int
	preview PlanPreview
	err     error
}

// planCmd computes the plan in the background; Update itself does no I/O.
func planCmd(b Backend, seq int) tea.Cmd {
	return func() tea.Msg {
		preview, err := b.Plan()
		return planMsg{seq: seq, preview: preview, err: err}
	}
}

// openPlan switches to the plan screen and starts computing the plan. It does
// nothing while a toggle is being written (the plan would describe a config
// that is about to change) or while an earlier computation is still running
// (it cannot be cancelled and queries the package manager).
func (m Model) openPlan() (tea.Model, tea.Cmd) {
	if m.pending || m.planRunning {
		return m, nil
	}
	m.screen = screenPlan
	m.planRunning = true
	m.planSeq++
	m.plan = planState{loading: true}
	return m, planCmd(m.backend, m.planSeq)
}

// applyPlan stores a computed plan, unless the user has left the plan screen
// (or asked for a newer plan) since it was requested.
func (m Model) applyPlan(msg planMsg) Model {
	m.planRunning = false
	if m.screen != screenPlan || msg.seq != m.planSeq {
		return m
	}
	if msg.err != nil {
		m.plan = planState{err: sanitize(msg.err.Error())}
		return m
	}
	text := strings.TrimRight(msg.preview.Text, "\n")
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = sanitize(line)
	}
	if !msg.preview.NeedsApply {
		lines = append(lines, "", nothingToApply)
	}
	m.plan = planState{lines: lines, needsApply: msg.preview.NeedsApply}
	return m
}

// updatePlan handles a key press on the plan screen. Only y and enter lead on
// to apply, and only when canApply says so; everything else scrolls or leaves.
func (m Model) updatePlan(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "esc", "n", "b":
		m.screen = screenBrowser
		m.planSeq++ // whatever is still being computed is no longer wanted
		m.plan = planState{}
	case "y", "enter":
		if m.plan.canApply() && m.planVisible() {
			m.applyRequested = true
			return m, tea.Quit
		}
	case "up", "k":
		m.scrollPlan(-1)
	case "down", "j":
		m.scrollPlan(1)
	case "pgup":
		m.scrollPlan(-m.planRows())
	case "pgdown":
		m.scrollPlan(m.planRows())
	case "home", "g":
		m.plan.offset = 0
	case "end", "G":
		m.plan.offset = m.maxPlanOffset()
	}
	return m, nil
}

// planVisible reports whether the plan is on screen: a terminal below the
// minimum size shows only the too-small notice, and confirming something that
// cannot be seen would be blind.
func (m Model) planVisible() bool {
	return m.width >= minWidth && m.height >= minHeight
}

// planRows is how many plan lines fit on the screen.
func (m Model) planRows() int { return m.bodyRows() }

func (m Model) maxPlanOffset() int {
	return max(len(m.planDisplay())-m.planRows(), 0)
}

// scrollPlan moves the visible window by delta lines, stopping at both ends.
func (m *Model) scrollPlan(delta int) {
	m.plan.offset = min(max(m.plan.offset+delta, 0), m.maxPlanOffset())
}
