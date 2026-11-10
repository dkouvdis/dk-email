package ui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/dkouvdis/dk-email/internal/mail"
	"github.com/dkouvdis/dk-email/internal/ui/popup"
	"github.com/dkouvdis/dk-email/internal/ui/sidebar"
)

// pick identifies a folder chosen in the account/folder picker.
type pick struct {
	account int
	folder  string
}

// openPicker shows every account's folders, with the open one current. In
// narrow mode the sidebar already lists them, so a closes the reader popup
// and focuses it.
func (m Model) openPicker() (Model, tea.Cmd) {
	if m.panes.Kind == KindNarrow {
		m.readerPopup = false
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

// openMovePicker lists the open account's other folders; choosing one moves
// targets there.
func (m Model) openMovePicker(targets []mail.Summary) (Model, tea.Cmd) {
	account := m.open.account
	a := m.side.Accounts()[account]
	g := popup.Group[pick]{Name: a.Name}
	for _, f := range a.Folders {
		if f.Name == m.open.folder {
			continue
		}
		g.Items = append(g.Items, popup.Item[pick]{Label: sidebar.Label(f), Count: f.Unread, Value: pick{account, f.Name}})
	}
	if len(g.Items) == 0 {
		m.setStatus("No other folder to move to.")
		return m, nil
	}
	m.picker = popup.NewPicker(fmt.Sprintf("Move %s to", plural(len(targets), "message")), []popup.Group[pick]{g})
	m.picker.SetSize(m.panes.Popup.W, m.panes.Popup.H)
	m.showPicker = true
	m.moveTargets = targets
	m.log.Debug("popup opened", "popup", "move picker")
	return m, nil
}

func (m Model) handlePickerKey(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	var r popup.Result
	m.picker, r = m.picker.Update(msg)
	targets := m.moveTargets
	switch r {
	case popup.Cancelled:
		m.showPicker, m.moveTargets = false, nil
		m.log.Debug("popup closed", "popup", "picker")
	case popup.Chosen:
		m.showPicker, m.moveTargets = false, nil
		p, _ := m.picker.Selected()
		m.log.Debug("popup closed", "popup", "picker", "chose", true)
		if targets != nil {
			label := p.folder
			if f, ok := m.side.Folder(p.account, p.folder); ok {
				label = sidebar.Label(f)
			}
			return m.startMove(targets, p.folder, label, "move")
		}
		if p.account == m.open.account && p.folder == m.open.folder {
			return m, nil
		}
		return m.openFolder(p.account, p.folder)
	}
	return m, nil
}
