package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/JtheGunner/omnishell/internal/modedit"
)

const (
	listWidth = 30 // outer width of the left box, borders included
	// boxChrome is the horizontal space a box spends on its border (2) and
	// padding (2); the vertical cost is the border alone (2).
	boxChrome   = 4
	boxChromeV  = 2
	headerLines = 1
	footerLines = 1
)

var (
	boxStyle      = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	titleStyle    = lipgloss.NewStyle().Bold(true)
	selectedStyle = lipgloss.NewStyle().Reverse(true)
	dimStyle      = lipgloss.NewStyle().Faint(true)
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

	bodyHeight := m.height - headerLines - footerLines
	rows := bodyHeight - boxChromeV
	rightWidth := m.width - listWidth

	left := boxStyle.Width(listWidth).Height(bodyHeight).
		Render(m.renderList(rows, listWidth-boxChrome))
	right := boxStyle.Width(rightWidth).Height(bodyHeight).
		Render(m.renderDetail(rows, rightWidth-boxChrome))

	return lipgloss.JoinVertical(lipgloss.Left,
		m.renderHeader(),
		lipgloss.JoinHorizontal(lipgloss.Top, left, right),
		m.renderFooter(),
	)
}

func (m Model) renderHeader() string {
	line := titleStyle.Render("omnishell") + dimStyle.Render(fmt.Sprintf("  %d modules", len(m.views)))
	switch {
	case m.filtering:
		line += "  filter: " + m.filter + "_"
	case m.filter != "":
		line += fmt.Sprintf("  filter: %s (%d shown)", m.filter, len(m.visible))
	}
	return lipgloss.NewStyle().Inline(true).MaxWidth(m.width).Render(line)
}

func (m Model) renderFooter() string {
	help := "↑/↓ move · / filter · esc clear filter · q quit"
	if m.filtering {
		help = "type to filter · enter keep · esc cancel · ctrl+c quit"
	}
	return dimStyle.Inline(true).MaxWidth(m.width).Render(help)
}

// renderList draws at most rows module lines, scrolled so the cursor is
// always on screen.
func (m Model) renderList(rows, width int) string {
	if len(m.visible) == 0 {
		if len(m.views) == 0 {
			return dimStyle.Render("No modules")
		}
		return dimStyle.Render("No matches")
	}

	start := 0
	if m.cursor >= rows {
		start = m.cursor - rows + 1
	}
	end := min(start+rows, len(m.visible))

	lines := make([]string, 0, end-start)
	for pos := start; pos < end; pos++ {
		v := m.views[m.visible[pos]]
		marker := "  "
		if pos == m.cursor {
			marker = "▸ "
		}
		line := ansi.Truncate(marker+statusBox(v.Status)+" "+v.ID, width, "…")
		if pos == m.cursor {
			line = selectedStyle.Render(line)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// renderDetail describes the selected module, wrapped to width and clipped to
// rows lines so a long description can never push the layout out of shape.
func (m Model) renderDetail(rows, width int) string {
	v, ok := m.selected()
	if !ok {
		return ""
	}

	homepage := v.Homepage
	if homepage == "" {
		homepage = "—"
	}
	text := strings.Join([]string{
		titleStyle.Render(v.Name),
		dimStyle.Render(fmt.Sprintf("%s · %s", v.ID, v.Origin)),
		"",
		v.Description,
		"",
		field("Status", string(v.Status)),
		field("Packages", string(v.Packages)),
		field("Platforms", strings.Join(v.Platforms, ", ")),
		field("Shells", strings.Join(v.Shells, ", ")),
		field("Options", fmt.Sprint(v.OptionCount)),
		field("Homepage", homepage),
	}, "\n")

	return lipgloss.NewStyle().Width(width).MaxHeight(rows).Render(text)
}

// field renders one "Label:  value" line of the detail pane.
func field(label, value string) string {
	return fmt.Sprintf("%-10s %s", label+":", value)
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
