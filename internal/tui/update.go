package tui

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	nm, cmd := m.update(msg)
	return nm, cmd
}

// update is the internal handler. Returning the concrete Model type makes
// it easier to write synthetic tests without constant type assertions.
func (m Model) update(msg tea.Msg) (Model, tea.Cmd) {
	keys := DefaultKeyMap()

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tea.KeyMsg:
		if m.mode != modeBrowse {
			return m, nil // modal handling lands in later tasks
		}
		switch {
		case key.Matches(msg, keys.Up):
			if m.cursor > 0 {
				m.cursor--
			}
		case key.Matches(msg, keys.Down):
			if m.cursor < len(m.visibleRows())-1 {
				m.cursor++
			}
		case key.Matches(msg, keys.Expand):
			rows := m.visibleRows()
			if len(rows) == 0 {
				return m, nil
			}
			r := rows[m.cursor]
			if r.memberIdx == -1 {
				m.findings[r.findingIdx].Expanded = !m.findings[r.findingIdx].Expanded
				if m.cursor >= len(m.visibleRows()) {
					m.cursor = len(m.visibleRows()) - 1
				}
			}
		case key.Matches(msg, keys.Toggle):
			rows := m.visibleRows()
			if len(rows) == 0 {
				return m, nil
			}
			r := rows[m.cursor]
			if r.memberIdx >= 0 {
				mem := &m.findings[r.findingIdx].Members[r.memberIdx]
				if mem.Selectable() {
					mem.Selected = !mem.Selected
				}
			}
		case key.Matches(msg, keys.Quit):
			return m, tea.Quit
		}
	}
	return m, nil
}
