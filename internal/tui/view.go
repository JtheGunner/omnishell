package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/JtheGunner/omnishell/internal/modedit"
)

// View implements tea.Model.
func (m Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	return v
}

func (m Model) render() string {
	switch {
	case m.width == 0 || m.height == 0:
		return "" // the size is unknown until the first tea.WindowSizeMsg
	case m.width < minWidth || m.height < minHeight:
		return fmt.Sprintf("Terminal too small: need at least %dx%d, have %dx%d",
			minWidth, minHeight, m.width, m.height)
	}
	return m.renderList()
}

// renderList draws one line per visible module; the full layout replaces it.
func (m Model) renderList() string {
	lines := make([]string, 0, len(m.visible))
	for pos, idx := range m.visible {
		marker := "  "
		if pos == m.cursor {
			marker = "▸ "
		}
		lines = append(lines, marker+statusBox(m.views[idx].Status)+" "+m.views[idx].ID)
	}
	return strings.Join(lines, "\n")
}

func statusBox(s modedit.Status) string {
	switch s {
	case modedit.StatusEnabled:
		return "[x]"
	case modedit.StatusDisabled:
		return "[ ]"
	default:
		return "[?]"
	}
}
