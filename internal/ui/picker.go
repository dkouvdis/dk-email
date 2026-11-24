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
	// The attachment picker also sets the message, the attachment's index in
	// it and its name, to spot a message that changed since it was listed.
	uid  mail.UID
	part int
	name string
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
		for _, e := range a.Tree {
			it := treeItem(i, e)
			it.Current = e.Folder && i == m.open.account && e.Name == m.open.folder
			g.Items = append(g.Items, it)
		}
		groups = append(groups, g)
	}
	m.picker = popup.NewPicker(m.theme, "Go to folder", groups)
	m.picker.SetSize(m.panes.Popup.W, m.panes.Popup.H)
	m.showPicker = true
	m.log.Debug("popup opened", "popup", "picker")
	return m, nil
}

// treeItem is a picker item for one row of an account's folder tree.
func treeItem(account int, e sidebar.Entry) popup.Item[pick] {
	return popup.Item[pick]{
		Label:    e.Label,
		Path:     e.Path,
		Depth:    e.Depth,
		Count:    e.Unread,
		Value:    pick{account: account, folder: e.Name},
		Disabled: !e.Folder,
	}
}

// openMovePicker lists the open account's folders; choosing one moves
// targets there. The open folder is shown dimmed, keeping the tree's shape.
func (m Model) openMovePicker(targets []mail.Summary) (Model, tea.Cmd) {
	account := m.open.account
	a := m.side.Accounts()[account]
	g := popup.Group[pick]{Name: a.Name}
	others := 0
	for _, e := range a.Tree {
		it := treeItem(account, e)
		if e.Name == m.open.folder {
			it.Disabled = true
		}
		if !it.Disabled {
			others++
		}
		g.Items = append(g.Items, it)
	}
	if others == 0 {
		m.setStatus("No other folder to move to.")
		return m, nil
	}
	m.picker = popup.NewPicker(m.theme, fmt.Sprintf("Move %s to", plural(len(targets), "message")), []popup.Group[pick]{g})
	m.picker.SetSize(m.panes.Popup.W, m.panes.Popup.H)
	m.showPicker = true
	m.moveTargets = targets
	m.log.Debug("popup opened", "popup", "move picker")
	return m, nil
}

func (m Model) handlePickerKey(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	// In the folder picker, 1–9 move to that account's INBOX.
	if n, ok := sidebar.AccountKey(msg); ok && !m.picker.Filtering() && m.moveTargets == nil && !m.pickingAttachment {
		m.picker.JumpToGroup(n, func(p pick) bool { return m.side.Role(p.account, p.folder) == mail.RoleInbox })
		return m, nil
	}
	var r popup.Result
	m.picker, r = m.picker.Update(msg)
	targets, attaching := m.moveTargets, m.pickingAttachment
	switch r {
	case popup.Cancelled:
		m.showPicker, m.moveTargets, m.pickingAttachment = false, nil, false
		m.log.Debug("popup closed", "popup", "picker")
	case popup.Chosen:
		m.showPicker, m.moveTargets, m.pickingAttachment = false, nil, false
		p, _ := m.picker.Selected()
		m.log.Debug("popup closed", "popup", "picker", "chose", true)
		if attaching {
			return m.saveAttachment(p)
		}
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
