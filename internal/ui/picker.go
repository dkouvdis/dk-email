package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/dkouvdis/dk-email/internal/ui/popup"
	"github.com/dkouvdis/dk-email/internal/ui/sidebar"
)

// pick identifies a folder chosen in the account/folder picker.
type pick struct {
	account int
	folder  string
}

// openPicker shows every account's folders, with the open one current. In
// wide mode the sidebar already lists them, so a just focuses it.
func (m Model) openPicker() (Model, tea.Cmd) {
	if m.panes.Kind == KindWide {
		m.focus = PaneSidebar
		return m, nil
	}
	var groups []popup.Group[pick]
	for i, a := range m.side.Accounts() {
		g := popup.Group[pick]{Name: a.Name, Note: a.Note}
		for _, f := range a.Folders {
			g.Items = append(g.Items, popup.Item[pick]{
				Label:   sidebar.Label(f),
				Count:   f.Unread,
				Value:   pick{i, f.Name},
				Current: i == m.open.account && f.Name == m.open.folder,
			})
		}
		groups = append(groups, g)
	}
	m.picker = popup.NewPicker("Go to folder", groups)
	m.picker.SetSize(m.panes.Popup.W, m.panes.Popup.H)
	m.showPicker = true
	m.log.Debug("popup opened", "popup", "picker")
	return m, nil
}

func (m Model) handlePickerKey(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	var r popup.Result
	m.picker, r = m.picker.Update(msg)
	switch r {
	case popup.Cancelled:
		m.showPicker = false
		m.log.Debug("popup closed", "popup", "picker")
	case popup.Chosen:
		m.showPicker = false
		p, _ := m.picker.Selected()
		m.log.Debug("popup closed", "popup", "picker", "chose", true)
		if p.account == m.open.account && p.folder == m.open.folder {
			return m, nil
		}
		return m.openFolder(p.account, p.folder)
	}
	return m, nil
}
