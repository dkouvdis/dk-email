package ui

import (
	"context"
	"errors"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/dkouvdis/dk-email/internal/config"
	"github.com/dkouvdis/dk-email/internal/mail"
	"github.com/dkouvdis/dk-email/internal/ui/sidebar"
)

const (
	// connectTimeout covers password_cmd (which may wait up to 60s for a
	// 1Password prompt), login and listing folders.
	connectTimeout   = 90 * time.Second
	summariesTimeout = 30 * time.Second
	pageSize         = 200
)

// Connector opens an account's mailbox. It may block on password_cmd and
// login, so it always runs inside a tea.Cmd.
type Connector func(ctx context.Context, acct config.Account) (mail.Mailbox, error)

type accountMsg struct {
	index   int
	mb      mail.Mailbox
	folders []mail.Folder
	err     error
	took    time.Duration
}

type summariesMsg struct {
	account   int
	folder    string
	summaries []mail.Summary
	err       error
}

// openFolder is the folder whose threads are shown; account is -1 when none is.
type openFolder struct {
	account   int
	folder    string
	label     string
	loading   bool
	summaries []mail.Summary
}

func connectAccount(connect Connector, index int, acct config.Account) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), connectTimeout)
		defer cancel()
		start := time.Now()

		mb, err := connect(ctx, acct)
		if err == nil && mb == nil {
			err = errors.New("connector returned no mailbox")
		}
		if err != nil {
			return accountMsg{index: index, err: err, took: time.Since(start)}
		}
		folders, err := mb.Folders(ctx)
		return accountMsg{index: index, mb: mb, folders: folders, err: err, took: time.Since(start)}
	}
}

func loadSummaries(mb mail.Mailbox, account int, folder string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), summariesTimeout)
		defer cancel()
		sums, err := mb.Summaries(ctx, folder, 0, pageSize)
		return summariesMsg{account: account, folder: folder, summaries: sums, err: err}
	}
}

func (m Model) handleAccount(msg accountMsg) (Model, tea.Cmd) {
	if msg.index < 0 || msg.index >= len(m.accts) {
		return m, nil
	}
	name := m.accts[msg.index].Name
	if msg.mb != nil {
		m.boxes[msg.index] = msg.mb
	}
	m.side = m.side.SetFolders(msg.index, msg.folders, msg.err)
	m.log.Debug("account connected", "account", name, "ok", msg.err == nil, "folders", len(msg.folders), "took", msg.took.Round(time.Millisecond))
	if msg.err != nil {
		m.setError(fmt.Errorf("%s: %w", name, msg.err))
		return m, nil
	}
	if m.open.account < 0 {
		for _, f := range msg.folders {
			if f.Role == mail.RoleInbox {
				return m.openFolder(msg.index, f.Name)
			}
		}
	}
	return m, nil
}

func (m Model) openFolder(account int, folder string) (Model, tea.Cmd) {
	if account < 0 || account >= len(m.boxes) || m.boxes[account] == nil {
		return m, nil
	}
	label, role := folder, mail.RoleNone
	if f, ok := m.side.Folder(account, folder); ok {
		label, role = sidebar.Label(f), f.Role
	}
	m.open = openFolder{account: account, folder: folder, label: label, loading: true}
	m.side = m.side.SetOpen(account, folder)
	m.setStatus(fmt.Sprintf("Loading %s…", label))
	m.log.Debug("open folder", "account", m.accts[account].Name, "role", role)
	return m, loadSummaries(m.boxes[account], account, folder)
}

func (m Model) handleSummaries(msg summariesMsg) (Model, tea.Cmd) {
	// A slow response for a folder that's no longer open is dropped.
	if m.open.account != msg.account || m.open.folder != msg.folder {
		return m, nil
	}
	m.open.loading = false
	if msg.err != nil {
		m.setError(fmt.Errorf("load %s/%s: %w", m.accts[msg.account].Name, m.open.label, msg.err))
		return m, nil
	}
	m.open.summaries = msg.summaries
	m.setStatus("")
	m.log.Debug("summaries loaded", "account", m.accts[msg.account].Name, "count", len(msg.summaries))
	return m, nil
}

// Close closes every connected mailbox. Call it after the program exits.
func (m Model) Close() error {
	var errs []error
	for _, mb := range m.boxes {
		if mb != nil {
			errs = append(errs, mb.Close())
		}
	}
	return errors.Join(errs...)
}
