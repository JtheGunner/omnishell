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
	boxStyle    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	titleStyle  = lipgloss.NewStyle().Bold(true)
	dimStyle    = lipgloss.NewStyle().Faint(true)
	statusStyle = lipgloss.NewStyle().Bold(true).Inline(true)
)

// View implements tea.Model.
func (m Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.Cursor = m.textCursor()
	return v
}

func (m Model) render() string {
	switch {
	case m.width == 0 || m.height == 0:
		return "" // the size is unknown until the first tea.WindowSizeMsg
	case m.width < minWidth || m.height < minHeight:
		return ansi.Truncate(fmt.Sprintf("Terminal too small: need at least %dx%d, have %dx%d",
			minWidth, minHeight, m.width, m.height), m.width, "…")
	}

	switch m.screen {
	case screenPlan:
		return m.renderPlan()
	case screenOptions:
		return m.renderOptions()
	}

	bodyHeight := m.bodyRows()
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
	line := m.headerBase()
	filter := sanitize(m.filter)
	switch {
	case m.filtering:
		line += "  filter: " + filter
		if m.filterPos >= len([]rune(filter)) {
			line += "_" // the cursor is behind the text
		}
	case m.filter != "":
		line += fmt.Sprintf("  filter: %s (%d shown)", filter, len(m.visible))
	}
	return lipgloss.NewStyle().Inline(true).MaxWidth(m.width).Render(line)
}

// headerBase is the start of the browser's header line, before the filter.
func (m Model) headerBase() string {
	return titleStyle.Render("omnishell") +
		dimStyle.Render(fmt.Sprintf("  %d modules · %s", len(m.views), changesText(m.changes())))
}

// textCursor is where the terminal cursor belongs: in the filter or in the
// option value being typed, and nowhere else (nil hides it).
func (m Model) textCursor() *tea.Cursor {
	if m.width < minWidth || m.height < minHeight {
		return nil
	}
	switch {
	case m.screen == screenOptions:
		return m.optionCursor()
	case m.screen == screenBrowser && m.filtering:
		runes := []rune(sanitize(m.filter))
		before := string(runes[:min(m.filterPos, len(runes))])
		x := ansi.StringWidth(m.headerBase() + "  filter: " + before)
		if x >= m.width {
			return nil
		}
		return tea.NewCursor(x, 0)
	}
	return nil
}

func (m Model) renderFooter() string {
	if m.status != "" {
		return m.renderStatus()
	}
	if m.pending {
		return dimStyle.Inline(true).MaxWidth(m.width).Render("saving…")
	}
	help := "↑/↓ move · space toggle · o options · a plan · / filter · esc clear · q quit"
	if m.filtering {
		help = "type to filter · enter keep · esc cancel · ctrl+c quit"
	}
	return dimStyle.Inline(true).MaxWidth(m.width).Render(help)
}

// bodyRows is how many lines fit between the header and the footer.
func (m Model) bodyRows() int {
	return max(m.height-headerLines-m.footerHeight(), 1)
}

// maxStatusLines is how many lines an error message may take in the footer.
const maxStatusLines = 3

// statusLines is the status message, wrapped to the terminal width so a long
// reason stays readable; one that still does not fit in maxStatusLines ends
// with an ellipsis. It is nil when there is no message.
func (m Model) statusLines() []string {
	if m.status == "" {
		return nil
	}
	lines := strings.Split(ansi.Wrap("! "+m.status, max(m.width, 1), ""), "\n")
	if len(lines) > maxStatusLines {
		lines = lines[:maxStatusLines]
		last := strings.TrimRight(lines[maxStatusLines-1], " ")
		lines[maxStatusLines-1] = ansi.Truncate(last, max(m.width-1, 1), "") + "…"
	}
	return lines
}

// footerHeight is how many lines the footer takes: one, or more while a long
// message is shown.
func (m Model) footerHeight() int {
	if m.screen == screenPlan {
		return footerLines // the plan screen has no status line
	}
	return max(len(m.statusLines()), footerLines)
}

// renderStatus draws the wrapped status message.
func (m Model) renderStatus() string {
	lines := m.statusLines()
	for i, line := range lines {
		lines[i] = statusStyle.Render(line)
	}
	return strings.Join(lines, "\n")
}

// changesText says how many modules differ from the state the browser started
// with: "0 changes since start", "1 change since start".
func changesText(n int) string {
	if n == 1 {
		return "1 change since start"
	}
	return fmt.Sprintf("%d changes since start", n)
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
		style := lipgloss.NewStyle()
		if v.Unavailable != "" {
			style = dimStyle
		}
		if pos == m.cursor {
			style = style.Reverse(true)
		}
		lines = append(lines, style.Render(ansi.Truncate(marker+statusBox(v.Status)+" "+v.ID, width, "…")))
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
	lines := []string{
		titleStyle.Render(v.Name),
		dimStyle.Render(fmt.Sprintf("%s · %s", v.ID, v.Origin)),
		"",
		v.Description,
		"",
		field("Status", string(v.Status), width),
		field("Packages", string(v.Packages), width),
		field("Platforms", strings.Join(v.Platforms, ", "), width),
		field("Shells", strings.Join(v.Shells, ", "), width),
	}
	if v.Unavailable != "" {
		lines = append(lines, field("Host", v.Unavailable, width))
	}
	lines = append(lines,
		field("Options", fmt.Sprint(v.OptionCount), width),
		field("Homepage", homepage, width),
	)
	text := strings.Join(lines, "\n")

	return lipgloss.NewStyle().Width(width).MaxHeight(rows).Render(text)
}

// field renders one "Label:  value" entry of the detail pane, wrapped to width
// with the continuation lines under the value rather than under the label.
func field(label, value string, width int) string {
	return strings.Join(wrapValue(fmt.Sprintf("%-10s ", label+":"), value, width, maxFieldLines), "\n")
}

// maxFieldLines is a safety cap on how many lines one detail field may take;
// the pane clips to its own height anyway.
const maxFieldLines = 12

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
