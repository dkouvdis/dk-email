package ui

import tea "charm.land/bubbletea/v2"

// paneLine is the inner line of a bordered pane at screen row y.
func paneLine(r Rect, y int) int { return y - r.Y - 1 }

// handleClick selects what's under a left click. A folder opens at once; a
// thread is selected, and opens when it's clicked again. Clicks do nothing
// while a popup is open.
func (m Model) handleClick(ms tea.Mouse) (Model, tea.Cmd) {
	if ms.Button != tea.MouseLeft || m.popupOpen() || m.showConnect || m.showConfirm {
		return m, nil
	}
	p := m.panes
	switch {
	case p.Sidebar.contains(ms.X, ms.Y):
		m.focus = PaneSidebar
		var cmd tea.Cmd
		m.side, cmd = m.side.Click(paneLine(p.Sidebar, ms.Y))
		return m, cmd
	case p.List.contains(ms.X, ms.Y):
		m.focus = PaneList
		if m.open.account < 0 || m.open.loading {
			return m, nil
		}
		var hit, again bool
		m.list, hit, again = m.list.Click(paneLine(p.List, ms.Y) - listHeaderRows)
		if hit && again {
			return m.openThread()
		}
	}
	return m, nil
}

// handleWheel scrolls like ↑/↓: in the open popup, else in the pane under
// the pointer. It never opens anything. While a popup is open the mouse is
// released to the terminal (see View), so wheel events rarely arrive then.
func (m Model) handleWheel(ms tea.Mouse) (Model, tea.Cmd) {
	k := tea.KeyPressMsg{Code: tea.KeyDown}
	switch ms.Button {
	case tea.MouseWheelDown:
	case tea.MouseWheelUp:
		k = tea.KeyPressMsg{Code: tea.KeyUp}
	default:
		return m, nil
	}
	var cmd tea.Cmd
	switch {
	case m.showConnect || m.showConfirm:
	case m.popupOpen():
		next, c := m.handleKey(k)
		return next.(Model), c
	case m.panes.Sidebar.contains(ms.X, ms.Y):
		m.focus = PaneSidebar
		m.side, cmd = m.side.Update(k)
	case m.panes.List.contains(ms.X, ms.Y) && m.open.account >= 0 && !m.open.loading:
		m.focus = PaneList
		m.list, cmd = m.list.Update(k)
	}
	return m, cmd
}
