package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/JtheGunner/omnishell/internal/modedit"
)

// toggledMsg reports the outcome of flipping one module: either the error that
// stopped it, or the module list re-read after the write.
type toggledMsg struct {
	id    string
	views []modedit.ModuleView
	err   error
}

// toggle flips the selected module. The write happens in a command so Update
// itself stays free of I/O.
func (m Model) toggle() (tea.Model, tea.Cmd) {
	v, ok := m.selected()
	if !ok {
		return m, nil
	}
	return m, toggleCmd(m.backend, v.ID, v.Status != modedit.StatusEnabled)
}

// toggleCmd enables or disables id, then re-reads the modules so the screen
// shows what is now in config.toml rather than what it expects to be there.
func toggleCmd(b Backend, id string, enable bool) tea.Cmd {
	return func() tea.Msg {
		var err error
		if enable {
			err = b.Enable(id)
		} else {
			err = b.Disable(id)
		}
		if err != nil {
			return toggledMsg{id: id, err: err}
		}
		views, err := b.Modules()
		return toggledMsg{id: id, views: views, err: err}
	}
}

// applyToggled shows the error on the status line and leaves the list as it
// was, or swaps in the re-read modules and keeps the cursor on the module.
func (m Model) applyToggled(msg toggledMsg) Model {
	if msg.err != nil {
		m.status = sanitize(msg.err.Error())
		return m
	}
	m.views = sanitizeViews(msg.views)
	m.applyFilterKeeping(msg.id)
	return m
}
