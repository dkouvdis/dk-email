package ui

import (
	"context"
	"errors"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/dkouvdis/dk-email/internal/mail"
)

const refreshTimeout = 60 * time.Second

// refreshBefore asks Summaries for the newest page like before = 0, but as
// a non-zero before it also fails with mail.ErrFolderChanged if the
// folder's UIDs were reset, instead of silently accepting new ones.
const refreshBefore = ^mail.UID(0)

type refreshTickMsg struct{}

// refreshMsg is one account's refresh: its folders (with unread counts)
// and, for the account whose folder is open, that folder's newest page.
type refreshMsg struct {
	account    int
	folders    []mail.Folder
	foldersErr error
	folder     string // "" when this account's folders aren't open
	summaries  []mail.Summary
	sumsErr    error
}

// refreshState tracks the refresh in flight. writes is Model.writes when
// it started, and dirty records a write already in flight then: in either
// case the server may not reflect the UI's optimistic state, so the
// results are dropped and the next tick tries again.
type refreshState struct {
	pending int
	writes  int
	dirty   bool
	manual  bool
	// rerun asks for another refresh once this one finishes, because
	// something changed on the server after it started.
	rerun bool
}

func tick(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return refreshTickMsg{} })
}

func refreshAccount(mb mail.Mailbox, account int, folder string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), refreshTimeout)
		defer cancel()
		msg := refreshMsg{account: account, folder: folder}
		msg.folders, msg.foldersErr = mb.Folders(ctx)
		if folder != "" {
			msg.summaries, msg.sumsErr = mb.Summaries(ctx, folder, refreshBefore, pageSize)
		}
		return msg
	}
}

func (m Model) handleTick() (Model, tea.Cmd) {
	m, cmd := m.startRefresh(false)
	return m, tea.Batch(cmd, m.every(m.ui.RefreshInterval.Duration))
}

// startRefresh refreshes every connected account, unless a refresh is
// already running.
func (m Model) startRefresh(manual bool) (Model, tea.Cmd) {
	return m.refreshAccounts(manual, -1)
}

// refreshAfterDraft brings account up to date after a draft was saved or
// sent, which changes its Drafts folder. A refresh already running may have
// read the folder before that, so it is followed by another one.
func (m Model) refreshAfterDraft(account int) (Model, tea.Cmd) {
	if m.refresh.pending > 0 {
		m.refresh.rerun = true
		return m, nil
	}
	return m.refreshAccounts(false, account)
}

// refreshAccounts refreshes the connected accounts, or only the account
// numbered only when that isn't negative.
func (m Model) refreshAccounts(manual bool, only int) (Model, tea.Cmd) {
	if m.refresh.pending > 0 {
		return m, nil
	}
	var cmds []tea.Cmd
	for i, mb := range m.boxes {
		if mb == nil || (only >= 0 && i != only) {
			continue
		}
		folder := ""
		if i == m.open.account && !m.open.loading {
			folder = m.open.folder
		}
		cmds = append(cmds, refreshAccount(mb, i, folder))
	}
	if len(cmds) == 0 {
		return m, nil
	}
	m.refresh = refreshState{pending: len(cmds), writes: m.writes, dirty: m.inflight > 0, manual: manual}
	if manual {
		m.setStatus("Refreshing…")
	}
	m.log.Debug("refresh", "accounts", len(cmds), "manual", manual, "scoped", only >= 0)
	return m, tea.Batch(cmds...)
}

func (m Model) handleRefresh(msg refreshMsg) (Model, tea.Cmd) {
	if m.refresh.pending == 0 {
		return m, nil
	}
	m.refresh.pending--
	stale := m.refresh.dirty || m.writes != m.refresh.writes
	name := m.accts[msg.account].Name
	var cmd tea.Cmd

	switch {
	case msg.foldersErr != nil:
		m.setError(fmt.Errorf("refresh %s: %w", name, msg.foldersErr))
	case !stale:
		m.side = m.side.SetFolders(msg.account, msg.folders, nil)
	}

	openHere := msg.folder != "" && m.open.account == msg.account && m.open.folder == msg.folder && !m.open.loading
	switch {
	case !openHere:
	case errors.Is(msg.sumsErr, mail.ErrFolderChanged):
		m, cmd = m.reloadChanged(msg.account, msg.folder)
	case msg.sumsErr != nil:
		m.setError(fmt.Errorf("refresh %s/%s: %w", name, m.open.label, msg.sumsErr))
	case !stale:
		m.list = m.list.Merge(msg.summaries)
	}

	if m.refresh.pending == 0 {
		m.log.Debug("refresh done", "stale", stale)
		if m.refresh.manual && m.status == "Refreshing…" {
			m.setStatus("")
		}
		if m.refresh.rerun {
			var more tea.Cmd
			m.refresh.rerun = false
			m, more = m.startRefresh(false)
			cmd = tea.Batch(cmd, more)
		}
	}
	return m, cmd
}
