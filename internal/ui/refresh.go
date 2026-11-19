package ui

import (
	"context"
	"errors"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/dkouvdis/dk-email/internal/mail"
	"github.com/dkouvdis/dk-email/internal/notify"
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
	id         int // the account's Model.ids entry; when set it wins over account
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
	rerun   bool
	newMail map[int]int // new INBOX mail this round, by account id
}

func tick(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return refreshTickMsg{} })
}

func refreshAccount(mb mail.Mailbox, account, id int, folder string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), refreshTimeout)
		defer cancel()
		msg := refreshMsg{account: account, id: id, folder: folder}
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

// refreshSoon brings account up to date because something changed on the
// server (a draft saved or sent, new mail pushed). A refresh already running
// may have read the account before that, so it is followed by another one.
func (m Model) refreshSoon(account int) (Model, tea.Cmd) {
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
		if i == m.open.account && !m.open.loading && !m.searching() {
			folder = m.open.folder
		}
		cmds = append(cmds, refreshAccount(mb, i, m.ids[i], folder))
	}
	if len(cmds) == 0 {
		return m, nil
	}
	m.refresh = refreshState{pending: len(cmds), writes: m.writes, dirty: m.inflight > 0, manual: manual, newMail: map[int]int{}}
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
	var cmd tea.Cmd
	var ok bool
	if msg.account, ok = m.accountIndex(msg.account, msg.id); !ok {
		// Its account was deleted or replaced meanwhile.
		return m.finishRefresh(cmd, true)
	}
	stale := m.refresh.dirty || m.writes != m.refresh.writes
	name := m.accts[msg.account].Name

	if msg.foldersErr == nil && !stale {
		// Only a non-stale round has every recorded move done on the server;
		// a skipped round keeps the baseline, so the next one counts the gap.
		id := m.ids[msg.account]
		if n := m.inbox.Observe(id, msg.folders); n > 0 {
			m.refresh.newMail[id] += n
		}
	}

	switch {
	case msg.foldersErr != nil:
		m.setError(fmt.Errorf("refresh %s: %w", name, msg.foldersErr))
	case !stale:
		m.side = m.side.SetFolders(msg.account, msg.folders, nil)
	}

	openHere := msg.folder != "" && m.open.account == msg.account && m.open.folder == msg.folder && !m.open.loading && !m.searching()
	switch {
	case !openHere:
	case errors.Is(msg.sumsErr, mail.ErrFolderChanged):
		m, cmd = m.reloadChanged(msg.account, msg.folder)
	case msg.sumsErr != nil:
		m.setError(fmt.Errorf("refresh %s/%s: %w", name, m.open.label, msg.sumsErr))
	case !stale:
		m.list = m.list.Merge(msg.summaries)
	}

	return m.finishRefresh(cmd, stale)
}

// finishRefresh ends the refresh once every account has reported.
func (m Model) finishRefresh(cmd tea.Cmd, stale bool) (Model, tea.Cmd) {
	if m.refresh.pending == 0 {
		m.log.Debug("refresh done", "stale", stale)
		if m.refresh.manual && m.status == "Refreshing…" {
			m.setStatus("")
		}
		cmd = tea.Batch(cmd, m.announce())
		if m.refresh.rerun {
			var more tea.Cmd
			m.refresh.rerun = false
			m, more = m.startRefresh(false)
			cmd = tea.Batch(cmd, more)
		}
	}
	return m, cmd
}

// announce rings the bell and sends a desktop notification for the new INBOX
// mail this refresh found, or returns nil when there is none or both are off.
func (m Model) announce() tea.Cmd {
	var counts []notify.Count
	for i, a := range m.accts {
		if n := m.refresh.newMail[m.ids[i]]; n > 0 {
			counts = append(counts, notify.Count{Account: a.Name, New: n})
		}
	}
	if len(counts) == 0 {
		return nil
	}
	m.log.Debug("new mail", "accounts", len(counts), "focused", m.focused)
	if m.focused || (!m.notify.Bell && !m.notify.Desktop) {
		return nil
	}
	return tea.Raw(notify.Sequence(notify.Text(counts), m.notify.Bell, m.notify.Desktop, m.tmux))
}
