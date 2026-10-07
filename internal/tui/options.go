package tui

import (
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/JtheGunner/omnishell/internal/modedit"
)

// optionsState is the options screen: the options of one module, the one under
// the cursor, and the text being typed while a string or int option is edited.
type optionsState struct {
	id      string // the module whose options are shown
	rows    []modedit.OptionView
	cursor  int
	editing bool   // true while the user types a new value
	input   string // the value typed so far
	stale   bool   // true when a write went through but re-reading the rows failed
	pos     int    // the cursor within input, as a rune index
}

// optionsMsg carries the options of a module back to Update after `o`.
type optionsMsg struct {
	id   string
	rows []modedit.OptionView
	err  error
}

// optionWrittenMsg reports the outcome of writing one option: the error that
// stopped it, or the module's options re-read after the write.
type optionWrittenMsg struct {
	id      string
	key     string // the option that was written
	raw     string // the value that was written, as typed or chosen
	rows    []modedit.OptionView
	written bool // true once config.toml holds the new value, even if err is set
	err     error
}

// optionsCmd reads the options of a module in the background.
func optionsCmd(b Backend, id string) tea.Cmd {
	return func() tea.Msg {
		rows, err := b.Options(id)
		return optionsMsg{id: id, rows: rows, err: err}
	}
}

// optionWriteCmd writes one option and then re-reads the module's options, so
// the screen shows what config.toml now holds.
func optionWriteCmd(b Backend, id, key, raw string) tea.Cmd {
	return func() tea.Msg {
		if err := b.SetOption(id, key, raw); err != nil {
			return optionWrittenMsg{id: id, err: err}
		}
		rows, err := b.Options(id)
		return optionWrittenMsg{id: id, key: key, raw: raw, rows: rows, written: true, err: err}
	}
}

// optionRereadCmd reads a module's options again after a write whose re-read
// failed; it answers like a write, so the screen unlocks when it succeeds.
func optionRereadCmd(b Backend, id string) tea.Cmd {
	return func() tea.Msg {
		rows, err := b.Options(id)
		return optionWrittenMsg{id: id, rows: rows, written: true, err: err}
	}
}

// openOptions asks for the options of the selected module. It does nothing
// while another write is pending, and says so when the module has no options.
func (m Model) openOptions() (tea.Model, tea.Cmd) {
	v, ok := m.selected()
	if !ok || m.pending {
		return m, nil
	}
	if v.OptionCount == 0 {
		m.status = v.ID + " has no options"
		return m, nil
	}
	m.pending = true
	return m, optionsCmd(m.backend, v.ID)
}

// applyOptions shows the options screen, or the reason it cannot be shown.
func (m Model) applyOptions(msg optionsMsg) Model {
	m.pending = false
	if m.screen != screenBrowser {
		return m // the user is somewhere else now; the answer is no longer wanted
	}
	if msg.err != nil {
		m.status = sanitize(msg.err.Error())
		return m
	}
	rows := sanitizeOptions(msg.rows)
	m.options = optionsState{id: msg.id, rows: rows}
	m.screen = screenOptions
	// A / pressed while the options were loading started the filter; do not
	// bring that typing mode back with the browser.
	m.filtering = false
	m.rememberOptions(msg.id, rows)
	return m
}

// applyOptionWritten shows a rejected value on the status line and keeps the
// text being typed, so it can be corrected; or swaps in the re-read options and
// leaves the editing mode.
func (m Model) applyOptionWritten(msg optionWrittenMsg) Model {
	m.pending = false
	if m.screen != screenOptions || msg.id != m.options.id {
		return m // the write finished, but this screen is not the one it was for
	}
	if msg.err != nil {
		m.status = sanitize(msg.err.Error())
		if msg.written {
			// The value is in config.toml but the rows are older than it: lock
			// the keys that compute from them until a read succeeds.
			m.options.stale = true
			m.options.editing = false
			m.options.input, m.options.pos = "", 0
			if msg.key != "" {
				// Show and count what was written, as far as it is known.
				rows := slices.Clone(m.options.rows)
				for i := range rows {
					if rows[i].Key == msg.key {
						rows[i].Value, rows[i].Set = sanitize(msg.raw), true
					}
				}
				m.options.rows = rows
				m.rememberOptions(msg.id, rows)
			}
		}
		return m
	}
	rows := sanitizeOptions(msg.rows)
	m.options.stale = false
	m.options.rows = rows
	m.options.cursor = min(m.options.cursor, max(len(rows)-1, 0))
	m.options.editing = false
	m.options.input, m.options.pos = "", 0
	m.rememberOptions(msg.id, rows)
	return m
}

// rememberOptions records the current values of a module's options, and their
// values the first time they were seen, so changes() can tell what changed.
func (m *Model) rememberOptions(id string, rows []modedit.OptionView) {
	now := make(map[string]string, len(rows))
	for _, r := range rows {
		now[r.Key] = r.Value
	}
	if m.optionBase == nil {
		m.optionBase = map[string]map[string]string{}
	}
	if _, seen := m.optionBase[id]; !seen {
		m.optionBase[id] = now
	}
	if m.optionNow == nil {
		m.optionNow = map[string]map[string]string{}
	}
	m.optionNow[id] = now
}

// changedOptions counts the options whose value differs from the first value
// the options screen showed, so setting one back counts as no change.
func (m Model) changedOptions() int {
	n := 0
	for id, now := range m.optionNow {
		for key, value := range now {
			if m.optionBase[id][key] != value {
				n++
			}
		}
	}
	return n
}

// sanitizeOptions returns a cleaned copy of rows; see sanitize.go.
func sanitizeOptions(rows []modedit.OptionView) []modedit.OptionView {
	out := make([]modedit.OptionView, len(rows))
	for i, r := range rows {
		r.Key = sanitize(r.Key)
		r.Type = sanitize(r.Type)
		r.Help = sanitize(r.Help)
		r.Pattern = sanitize(r.Pattern)
		r.Default = sanitize(r.Default)
		r.Value = sanitize(r.Value)
		r.Values = sanitizeAll(r.Values)
		out[i] = r
	}
	return out
}

// selectedOption returns the option under the cursor, if there is one.
func (m Model) selectedOption() (modedit.OptionView, bool) {
	if m.options.cursor < 0 || m.options.cursor >= len(m.options.rows) {
		return modedit.OptionView{}, false
	}
	return m.options.rows[m.options.cursor], true
}

// updateOptions handles a key press on the options screen. While a write is
// pending only ctrl+c works: the keys would act on values about to change.
func (m Model) updateOptions(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.pending {
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		return m, nil
	}
	if m.options.editing {
		return m.updateOptionInput(msg)
	}
	if m.options.stale {
		return m.updateStaleOptions(msg)
	}
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "esc", "b":
		m.screen = screenBrowser
		m.options = optionsState{}
	case "up", "k":
		m.options.cursor = max(m.options.cursor-1, 0)
	case "down", "j":
		m.options.cursor = min(m.options.cursor+1, max(len(m.options.rows)-1, 0))
	case "space":
		if row, ok := m.selectedOption(); ok && row.Editable && row.Type == "bool" {
			next := "true"
			if row.Value == "true" {
				next = "false"
			}
			return m.writeOption(row.Key, next)
		}
	case "left":
		return m.cycleEnum(-1)
	case "right":
		return m.cycleEnum(1)
	case "enter":
		if row, ok := m.selectedOption(); ok && row.Editable && (row.Type == "string" || row.Type == "int") {
			m.options.editing = true
			m.options.input = row.Value
			m.options.pos = len([]rune(row.Value))
		}
	}
	return m, nil
}

// updateOptionInput handles a key press while a string or int value is typed.
func (m Model) updateOptionInput(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.options.editing = false
		m.options.input, m.options.pos = "", 0
	case "enter":
		if row, ok := m.selectedOption(); ok {
			return m.writeOption(row.Key, m.options.input)
		}
	default:
		m.options.input, m.options.pos, _ = editText(m.options.input, m.options.pos, msg)
	}
	return m, nil
}

// pasteOption inserts pasted text into the value being typed.
func (m Model) pasteOption(content string) Model {
	if m.pending || !m.options.editing {
		return m
	}
	m.options.input, m.options.pos = pasteText(m.options.input, m.options.pos, content)
	return m
}

// cycleEnum moves an enum option to the next (delta 1) or previous (delta -1)
// allowed value, wrapping around, and writes it.
func (m Model) cycleEnum(delta int) (tea.Model, tea.Cmd) {
	row, ok := m.selectedOption()
	if !ok || !row.Editable || row.Type != "enum" || len(row.Values) == 0 {
		return m, nil
	}
	n := len(row.Values)
	idx := slices.Index(row.Values, row.Value)
	var next int
	switch {
	case idx < 0 && delta > 0:
		next = 0
	case idx < 0:
		next = n - 1
	default:
		next = (idx + delta + n) % n
	}
	return m.writeOption(row.Key, row.Values[next])
}

// writeOption starts writing one option; the write itself runs in a command.
func (m Model) writeOption(key, raw string) (tea.Model, tea.Cmd) {
	m.pending = true
	return m, optionWriteCmd(m.backend, m.options.id, key, raw)
}

// updateStaleOptions handles a key press while the rows are older than the file:
// moving, leaving and reading again work; everything that would write is off.
func (m Model) updateStaleOptions(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "esc", "b":
		m.screen = screenBrowser
		m.options = optionsState{}
	case "up", "k":
		m.options.cursor = max(m.options.cursor-1, 0)
	case "down", "j":
		m.options.cursor = min(m.options.cursor+1, max(len(m.options.rows)-1, 0))
	case "r":
		m.pending = true
		return m, optionRereadCmd(m.backend, m.options.id)
	}
	return m, nil
}
