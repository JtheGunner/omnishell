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

// planBody returns at most rows lines: a waiting note, the error, or the
// visible window of the plan.
func (m Model) planBody(rows int) []string {
	switch {
	case m.plan.loading:
		return []string{dimStyle.Render("computing plan…")}
	case m.plan.err != "":
		wrapped := lipgloss.NewStyle().Width(m.width).Render("Could not compute the plan: " + m.plan.err)
		return clip(strings.Split(wrapped, "\n"), rows)
	}
	end := min(m.plan.offset+rows, len(m.plan.lines))
	return append([]string(nil), m.plan.lines[m.plan.offset:end]...)
}

func clip(lines []string, n int) []string {
	if len(lines) > n {
		return lines[:n]
	}
	return lines
}

func (m Model) renderPlanHeader(rows int) string {
	line := titleStyle.Render("omnishell") + dimStyle.Render("  plan preview")
	if len(m.plan.lines) > rows {
		last := min(m.plan.offset+rows, len(m.plan.lines))
		line += dimStyle.Render(fmt.Sprintf("  lines %d-%d of %d", m.plan.offset+1, last, len(m.plan.lines)))
	}
	return lipgloss.NewStyle().Inline(true).MaxWidth(m.width).Render(line)
}

func (m Model) renderPlanFooter() string {
	help := "↑/↓ scroll · esc back · q quit"
	switch {
	case m.plan.loading:
		help = "esc back · q quit"
	case m.plan.err != "":
		help = "esc back · q quit"
	case m.plan.canApply():
		help = "↑/↓ scroll · y/enter continue to apply · esc back · q quit"
	}
	return dimStyle.Inline(true).MaxWidth(m.width).Render(help)
}
