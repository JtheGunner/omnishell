package tui

import (
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/JtheGunner/omnishell/internal/modedit"
)

// toggledMsg reports the outcome of flipping one module: either the error that
// stopped it, or every module's state as config.toml now has it.
type toggledMsg struct {
	id       string // the module that was flipped
	enable   bool   // what it was flipped to
	written  bool   // true once config.toml holds the new state, even if err is set
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
			return toggledMsg{id: id, enable: enable, err: err}
		}
		statuses, err := b.Statuses()
		return toggledMsg{id: id, enable: enable, written: true, statuses: statuses, err: err}
	}
}

// applyToggled shows the error on the status line and leaves the list as it
// was, or copies the re-read states onto the modules. The cursor is not
// touched: the user may have moved while the write ran, and the filter does not
// depend on the status.
func (m Model) applyToggled(msg toggledMsg) Model {
	m.pending = false
	m.views = slices.Clone(m.views)
	if msg.err != nil {
		m.status = sanitize(msg.err.Error())
		if msg.written {
			// The write went through and only the re-read failed: show what
			// config.toml now holds rather than the state from before.
			next := modedit.StatusDisabled
			if msg.enable {
				next = modedit.StatusEnabled
			}
			for i, v := range m.views {
				if v.ID == msg.id {
					m.views[i].Status = next
				}
			}
		}
		return m
	}
	for i, v := range m.views {
		if status, ok := msg.statuses[v.ID]; ok {
			m.views[i].Status = status
		}
	}
	return m
}
