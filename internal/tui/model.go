package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/JtheGunner/omnishell/internal/modedit"
)

// The smallest terminal the layout is designed for.
const (
	minWidth  = 80
	minHeight = 20
)

// Model is the Bubble Tea model of the module browser. All state changes go
// through Update, which returns a new Model.
type Model struct {
	backend   Backend
	views     []modedit.ModuleView
	visible   []int // indexes into views that match the filter, in order
	cursor    int   // position within visible
	filter    string
	filtering bool                      // true while the user is typing into the filter
	status    string                    // the last error to show, cleared by the next key press
	pending   bool                      // true from pressing space until the write has finished
	initial   map[string]modedit.Status // each module's state when the browser started
	width     int                       // 0 until the first tea.WindowSizeMsg
	height    int
}

// New returns a browser over views, which must already be sorted for display,
// that changes modules through b. Module text is cleaned of control characters
// first (see sanitize.go).
func New(b Backend, views []modedit.ModuleView) Model {
	m := Model{backend: b, views: sanitizeViews(views)}
	m.initial = make(map[string]modedit.Status, len(m.views))
	for _, v := range m.views {
		m.initial[v.ID] = v.Status
	}
	m.applyFilter()
	return m
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd { return nil }

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case toggledMsg:
		return m.applyToggled(msg), nil
	case tea.KeyPressMsg:
		m.status = ""
		if m.filtering {
			return m.updateFiltering(msg)
		}
		return m.updateBrowsing(msg)
	}
	return m, nil
}

func (m Model) updateBrowsing(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "up", "k":
		m.move(-1)
	case "down", "j":
		m.move(1)
	case "space":
		return m.toggle()
	case "/":
		m.filtering = true
	case "esc":
		if m.filter != "" {
			m.filter = ""
			m.applyFilter()
		}
	}
	return m, nil
}

func (m Model) updateFiltering(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "enter":
		m.filtering = false
	case "esc":
		m.filtering = false
		m.filter = ""
		m.applyFilter()
	case "backspace":
		if runes := []rune(m.filter); len(runes) > 0 {
			m.filter = string(runes[:len(runes)-1])
			m.applyFilter()
		}
	default:
		if msg.Text != "" {
			m.filter += msg.Text
			m.applyFilter()
		}
	}
	return m, nil
}

// move shifts the cursor by delta, staying put at either end of the list.
func (m *Model) move(delta int) {
	next := m.cursor + delta
	if next < 0 || next >= len(m.visible) {
		return
	}
	m.cursor = next
}

// applyFilter recomputes visible from the filter (a case-insensitive substring
// of the module id or description) and moves the cursor back to the top.
func (m *Model) applyFilter() { m.applyFilterKeeping("") }

// applyFilterKeeping is applyFilter, but leaves the cursor on the module with
// the given id when it is still visible. An empty id means the top.
func (m *Model) applyFilterKeeping(id string) {
	query := strings.ToLower(m.filter)
	m.visible = make([]int, 0, len(m.views))
	for i, v := range m.views {
		if query == "" ||
			strings.Contains(strings.ToLower(v.ID), query) ||
			strings.Contains(strings.ToLower(v.Description), query) {
			m.visible = append(m.visible, i)
		}
	}
	m.cursor = 0
	for pos, idx := range m.visible {
		if id != "" && m.views[idx].ID == id {
			m.cursor = pos
		}
	}
}

// selected returns the module under the cursor, if the filtered list is not
// empty.
func (m Model) selected() (modedit.ModuleView, bool) {
	if len(m.visible) == 0 {
		return modedit.ModuleView{}, false
	}
	return m.views[m.visible[m.cursor]], true
}

// changes counts the modules whose enabled state differs from the state they
// had when the browser started, so toggling a module back counts as no change.
func (m Model) changes() int {
	n := 0
	for _, v := range m.views {
		if initial, ok := m.initial[v.ID]; ok && initial != v.Status {
			n++
		}
	}
	return n
}
