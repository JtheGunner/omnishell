package tui

import (
	"fmt"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/JtheGunner/omnishell/internal/modedit"
)

const (
	// optionDetailLines is how many lines under the option table describe the
	// option under the cursor.
	optionDetailLines = 8
	keyColumn         = 22 // width of the option name column
	valueColumn       = 26 // width of the value column
)

// renderOptions draws the options screen: a header, exactly bodyRows lines of
// body (the option table, then the selected option's description) and a footer.
func (m Model) renderOptions() string {
	rows := m.bodyRows()
	tableRows := max(rows-optionDetailLines, 1)

	// The description follows the table directly, however few options there are.
	body := m.optionTable(tableRows)
	body = append(body, "")
	body = append(body, m.optionDetail(max(rows-len(body), 0))...)
	for len(body) < rows {
		body = append(body, "")
	}
	for i, line := range body[:rows] {
		body[i] = ansi.Truncate(line, m.width, "…")
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		m.renderOptionsHeader(),
		strings.Join(body[:rows], "\n"),
		m.renderOptionsFooter(),
	)
}

func (m Model) renderOptionsHeader() string {
	line := titleStyle.Render("omnishell") +
		dimStyle.Render(fmt.Sprintf("  options · %s · %s", m.options.id, changesText(m.changes())))
	return lipgloss.NewStyle().Inline(true).MaxWidth(m.width).Render(line)
}

func (m Model) renderOptionsFooter() string {
	switch {
	case m.status != "":
		return statusStyle.Render(ansi.Truncate("! "+m.status, m.width, "…"))
	case m.pending:
		return dimStyle.Inline(true).MaxWidth(m.width).Render("saving…")
	case m.options.editing:
		return dimStyle.Inline(true).MaxWidth(m.width).Render("type a value · enter save · esc cancel · ctrl+c quit")
	}
	return dimStyle.Inline(true).MaxWidth(m.width).
		Render("↑/↓ move · space toggle · ←/→ change · enter edit · esc back · q quit")
}

// optionTable draws at most n option lines, scrolled so the cursor is visible.
func (m Model) optionTable(n int) []string {
	rows := m.options.rows
	if len(rows) == 0 {
		return []string{dimStyle.Render("No options")}
	}
	start := 0
	if m.options.cursor >= n {
		start = m.options.cursor - n + 1
	}
	end := min(start+n, len(rows))

	lines := make([]string, 0, end-start)
	for pos := start; pos < end; pos++ {
		row := rows[pos]
		marker := "  "
		if pos == m.options.cursor {
			marker = "▸ "
		}
		line := marker + pad(row.Key, keyColumn) + pad(m.optionValueCell(pos, row), valueColumn) + row.Type

		style := lipgloss.NewStyle()
		if !row.Editable {
			style = dimStyle
		}
		if pos == m.options.cursor {
			style = style.Reverse(true)
		}
		lines = append(lines, style.Render(ansi.Truncate(line, m.width, "…")))
	}
	return lines
}

// optionValueCell is what the value column shows for one option: the text being
// typed for the row under edit, otherwise the value with a note where it is not
// a plain, explicitly set value.
func (m Model) optionValueCell(pos int, row modedit.OptionView) string {
	switch {
	case m.options.editing && pos == m.options.cursor:
		return "[" + m.options.input + "_]"
	case row.Invalid:
		return row.Value + " (invalid)"
	case !row.Set:
		return row.Value + " (default)"
	}
	return row.Value
}

// pad cuts s to width cells (with an ellipsis) or pads it with spaces.
func pad(s string, width int) string {
	s = ansi.Truncate(s, width-1, "…")
	return s + strings.Repeat(" ", max(width-ansi.StringWidth(s), 0))
}

// optionDetail describes the option under the cursor in at most n lines.
func (m Model) optionDetail(n int) []string {
	row, ok := m.selectedOption()
	if !ok {
		return nil
	}

	var lines []string
	if row.Help != "" {
		wrapped := lipgloss.NewStyle().Width(max(m.width-2, 1)).Render(row.Help)
		lines = append(lines, clip(strings.Split(wrapped, "\n"), 3)...)
	}
	lines = append(lines, "Type:    "+row.Type+" · "+optionHint(row))
	lines = append(lines, "Default: "+row.Default)
	if len(row.Values) > 0 {
		lines = append(lines, "Allowed: "+strings.Join(row.Values, ", "))
	}
	if row.Pattern != "" {
		lines = append(lines, "Pattern: "+row.Pattern)
	}
	if !m.moduleEnabled(m.options.id) {
		lines = append(lines, "Note:    this module is not enabled; its options apply once it is")
	}
	return clip(lines, n)
}

// optionHint says how the option under the cursor is changed.
func optionHint(row modedit.OptionView) string {
	switch {
	case !row.Editable:
		return "a list; set it with omnishell set"
	case row.Type == "bool":
		return "space toggles"
	case row.Type == "enum":
		return "←/→ changes"
	}
	return "enter edits"
}

// moduleEnabled reports whether the module with the given id is enabled.
func (m Model) moduleEnabled(id string) bool {
	i := slices.IndexFunc(m.views, func(v modedit.ModuleView) bool { return v.ID == id })
	return i >= 0 && m.views[i].Status == modedit.StatusEnabled
}
