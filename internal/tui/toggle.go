package tui

import (
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/JtheGunner/omnishell/internal/modedit"
)

// toggledMsg reports the outcome of flipping one module: either the error that
// stopped it, or every module's state as config.toml now has it.
type toggledMsg struct {
	statuses map[string]modedit.Status
	err      error
}

// toggle flips the selected module. The write happens in a command so Update
// itself stays free of I/O. Only one write runs at a time: a space pressed
// while one is pending is ignored, because it would be computed from a status
// the pending write is about to change.
func (m Model) toggle() (tea.Model, tea.Cmd) {
	v, ok := m.selected()
	if !ok || m.pending {
		return m, nil
	}
	m.pending = true
	return m, toggleCmd(m.backend, v.ID, v.Status != modedit.StatusEnabled)
}

// toggleCmd enables or disables id, then re-reads the modules' states so the
// screen shows what is now in config.toml rather than what it expects to be
// there. It reads only the states: everything else about a module is unchanged
// by a write, and re-probing the package manager takes seconds.
func toggleCmd(b Backend, id string, enable bool) tea.Cmd {
	return func() tea.Msg {
		var err error
		if enable {
			err = b.Enable(id)
		} else {
			err = b.Disable(id)
		}
		if err != nil {
			return toggledMsg{err: err}
		}
		statuses, err := b.Statuses()
		return toggledMsg{statuses: statuses, err: err}
	}
}

// applyToggled shows the error on the status line and leaves the list as it
// was, or copies the re-read states onto the modules. The cursor is not
// touched: the user may have moved while the write ran, and the filter does not
// depend on the status.
func (m Model) applyToggled(msg toggledMsg) Model {
	m.pending = false
	if msg.err != nil {
		m.status = sanitize(msg.err.Error())
		return m
	}
	m.views = slices.Clone(m.views)
	for i, v := range m.views {
		if status, ok := msg.statuses[v.ID]; ok {
			m.views[i].Status = status
		}
	}
	return m
}
