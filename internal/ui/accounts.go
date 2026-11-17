package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/dkouvdis/dk-email/internal/config"
	"github.com/dkouvdis/dk-email/internal/mail"
	"github.com/dkouvdis/dk-email/internal/ui/sidebar"
)

const (
	// connectTimeout covers password_cmd (which may wait up to 60s for a
	// 1Password prompt), login and listing folders.
	connectTimeout = 90 * time.Second
	// signInTimeout is how long an OAuth sign-in waits for the user; the
	// login test after it has connectTimeout.
	signInTimeout    = 5 * time.Minute
	summariesTimeout = 30 * time.Second
	pageSize         = 200
)

// Conn is what connecting an account yields. Sender is nil when sending
// isn't set up for the account (no smtp_host).
type Conn struct {
	Mailbox mail.Mailbox
	Sender  mail.Sender
}

// Connector opens an account's mailbox and, if sending is set up, its
// sender. It may block on password_cmd and login, so it always runs inside
// a tea.Cmd.
type Connector func(ctx context.Context, acct config.Account) (Conn, error)

type accountMsg struct {
	index int
	// id is the account's Model.ids entry; when set it wins over index.
	id      int
	conn    Conn
	folders []mail.Folder
	err     error
	took    time.Duration
}

// summariesMsg is a page of summaries; before is 0 for a folder's first page.
type summariesMsg struct {
	account   int
	folder    string
	before    mail.UID
	summaries []mail.Summary
	err       error
}

// openFolder is the folder whose threads are shown; account is -1 when none
// is. Its threads live in Model.list.
type openFolder struct {
	account int
	folder  string
	label   string
	loading bool
	// notice is shown in the status line once the first page arrives.
	notice string
}

func connectAccount(connect Connector, index, id int, acct config.Account) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), connectTimeout)
		defer cancel()
		start := time.Now()

		conn, err := connect(ctx, acct)
		if err == nil && conn.Mailbox == nil {
			err = errors.New("connector returned no mailbox")
		}
		if err != nil {
			return accountMsg{index: index, id: id, err: err, took: time.Since(start)}
		}
		folders, err := conn.Mailbox.Folders(ctx)
		return accountMsg{index: index, id: id, conn: conn, folders: folders, err: err, took: time.Since(start)}
	}
}

func loadSummaries(mb mail.Mailbox, account int, folder string, before mail.UID) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), summariesTimeout)
		defer cancel()
		sums, err := mb.Summaries(ctx, folder, before, pageSize)
		return summariesMsg{account: account, folder: folder, before: before, summaries: sums, err: err}
	}
}

func (m Model) handleAccount(msg accountMsg) (Model, tea.Cmd) {
	var ok bool
	if msg.index, ok = m.accountIndex(msg.index, msg.id); !ok {
		// The account was deleted or replaced while this was connecting.
		if msg.conn.Mailbox != nil {
			_ = msg.conn.Mailbox.Close()
		}
		return m, nil
	}
	name := m.accts[msg.index].Name
	if msg.conn.Mailbox != nil {
		m.boxes[msg.index] = msg.conn.Mailbox
		m.senders[msg.index] = msg.conn.Sender
	}
	m.side = m.side.SetFolders(msg.index, msg.folders, msg.err)
	m.log.Debug("account connected", "account", name, "ok", msg.err == nil, "folders", len(msg.folders), "took", msg.took.Round(time.Millisecond))
	if errors.Is(msg.err, ErrNeedsPassword) {
		m.needsPassword[msg.index] = true
		m.setError(fmt.Errorf("%s needs a password: press A to enter it", name))
		return m, nil
	}
	if errors.Is(msg.err, ErrNeedsSignIn) {
		text := fmt.Sprintf("%s needs you to sign in again", name)
		if m.onboard.UpdateAccount != nil {
			text += ": press E, choose OAuth 2.0 and Sign in & save"
		}
		m.setError(errors.New(text))
		return m, nil
	}
	if msg.err != nil {
		text := fmt.Sprintf("%s: %v", name, msg.err)
		if m.onboard.UpdateAccount != nil {
			text = strings.TrimSuffix(text, ".") + ". Press E to edit it."
		}
		m.setError(errors.New(text))
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
	if folder == "" || account < 0 || account >= len(m.boxes) || m.boxes[account] == nil {
		return m, nil
	}
	label, role := folder, mail.RoleNone
	if f, ok := m.side.Folder(account, folder); ok {
		label, role = sidebar.Label(f), f.Role
	}
	m.open = openFolder{account: account, folder: folder, label: label, loading: true}
	m.list = m.list.SetSummaries(nil, false)
	m.read = m.read.Close()
	m.readerPopup = false
	m.side = m.side.SetOpen(account, folder)
	m.setStatus(fmt.Sprintf("Loading %s…", label))
	m.log.Debug("open folder", "account", m.accts[account].Name, "role", role)
	return m, loadSummaries(m.boxes[account], account, folder, 0)
}

// reloadChanged reopens a folder whose UIDs were reset on the server.
func (m Model) reloadChanged(account int, folder string) (Model, tea.Cmd) {
	m.log.Debug("folder changed on server; reloading", "account", m.accts[account].Name)
	m, cmd := m.openFolder(account, folder)
	m.open.notice = fmt.Sprintf("%s changed on the server; reloaded.", m.open.label)
	return m, cmd
}

// loadMore fetches the page older than before for the open folder.
func (m Model) loadMore(before mail.UID) (Model, tea.Cmd) {
	if m.open.account < 0 || m.open.loading {
		return m, nil
	}
	m.setStatus("Loading older messages…")
	m.log.Debug("load page", "account", m.accts[m.open.account].Name, "before", before)
	return m, loadSummaries(m.boxes[m.open.account], m.open.account, m.open.folder, before)
}

func (m Model) handleSummaries(msg summariesMsg) (Model, tea.Cmd) {
	// A slow response for a folder that's no longer open, or a page nobody
	// is waiting for, is dropped.
	if m.open.account != msg.account || m.open.folder != msg.folder {
		return m, nil
	}
	paging := msg.before != 0
	if paging != m.list.LoadingMore() || (!paging && !m.open.loading) {
		return m, nil
	}
	name := m.accts[msg.account].Name
	m.open.loading = false
	more := len(msg.summaries) == pageSize

	switch {
	case paging && errors.Is(msg.err, mail.ErrFolderChanged):
		return m.reloadChanged(msg.account, msg.folder)
	case msg.err != nil:
		if paging {
			m.list = m.list.PageFailed()
		}
		m.setError(fmt.Errorf("load %s/%s: %w", name, m.open.label, msg.err))
		return m, nil
	case paging:
		m.list = m.list.AddPage(msg.summaries, more)
	default:
		m.list = m.list.SetSummaries(msg.summaries, more)
	}
	m.setStatus(m.open.notice)
	m.log.Debug("summaries loaded", "account", name, "count", len(msg.summaries), "page", paging)
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
