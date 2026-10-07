package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// renderPlan draws the plan screen: a header, exactly planRows lines of body
// and a footer, so it fills the terminal the way the browser does.
func (m Model) renderPlan() string {
	rows := m.planRows()
	body := m.planBody(rows)
	for len(body) < rows {
		body = append(body, "")
	}
	for i, line := range body {
		body[i] = ansi.Truncate(line, m.width, "…")
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		m.renderPlanHeader(rows),
		strings.Join(body, "\n"),
		m.renderPlanFooter(),
	)
}

// planBody returns at most rows lines: the visible window of what planDisplay
// shows.
func (m Model) planBody(rows int) []string {
	lines := m.planDisplay()
	start := min(m.plan.offset, len(lines))
	end := min(start+rows, len(lines))
	return append([]string(nil), lines[start:end]...)
}

// planDisplay is everything the plan screen has to show, wrapped to the
// terminal width: a waiting note, the error, or the plan. Long lines wrap
// instead of being cut, and the error scrolls like the plan does.
func (m Model) planDisplay() []string {
	switch {
	case m.plan.loading:
		return []string{dimStyle.Render("computing plan…")}
	case m.plan.err != "":
		return strings.Split(ansi.Wrap("Could not compute the plan: "+m.plan.err, max(m.width, 1), ""), "\n")
	}
	var out []string
	for _, line := range m.plan.lines {
		out = append(out, wrapIndented(line, max(m.width, 1))...)
	}
	return out
}

// wrapIndented wraps line to width cells. A line that starts with spaces keeps
// them, and its continuation lines are indented two cells further.
func wrapIndented(line string, width int) []string {
	trimmed := strings.TrimLeft(line, " ")
	indent := len(line) - len(trimmed)
	if ansi.StringWidth(line) <= width || indent >= width-10 {
		return strings.Split(ansi.Wrap(line, width, ""), "\n")
	}
	avail := width - indent - 2
	parts := strings.Split(ansi.Wrap(trimmed, avail, ""), "\n")
	for i, part := range parts {
		if i == 0 {
			parts[i] = strings.Repeat(" ", indent) + part
		} else {
			parts[i] = strings.Repeat(" ", indent+2) + part
		}
	}
	return parts
}

func clip(lines []string, n int) []string {
	if len(lines) > n {
		return lines[:n]
	}
	return lines
}

func (m Model) renderPlanHeader(rows int) string {
	line := titleStyle.Render("omnishell") + dimStyle.Render("  plan preview")
	if all := len(m.planDisplay()); all > rows {
		last := min(m.plan.offset+rows, all)
		line += dimStyle.Render(fmt.Sprintf("  lines %d-%d of %d", m.plan.offset+1, last, all))
	}
	return lipgloss.NewStyle().Inline(true).MaxWidth(m.width).Render(line)
}

func (m Model) renderPlanFooter() string {
	help := "↑/↓ scroll · esc back · q quit"
	switch {
	case m.plan.loading:
		help = "esc back · q quit"
	case m.plan.err != "":
		help = "↑/↓ scroll · esc back · q quit"
	case m.plan.canApply():
		help = "↑/↓ scroll · y/enter continue to apply (apply asks again) · esc back · q quit"
	}
	return dimStyle.Inline(true).MaxWidth(m.width).Render(help)
}
