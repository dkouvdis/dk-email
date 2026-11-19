package ui

import (
	"context"
	"errors"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/dkouvdis/dk-email/internal/mail"
)

const (
	watchBackoff    = 30 * time.Second
	watchBackoffMax = 5 * time.Minute
)

// watchState is one account's IDLE watch. Its pointer identifies the watch,
// so results from a stopped one are ignored.
type watchState struct {
	w       mail.Watcher
	ctx     context.Context
	cancel  context.CancelFunc
	backoff time.Duration // wait before the next try after a failure; 0 after a success
}

// inboxMsg reports that a Wait returned: err is nil when INBOX gained mail.
type inboxMsg struct {
	id  int
	ws  *watchState
	err error
}

// watchRetryMsg reports that the backoff after a failed Wait is over.
type watchRetryMsg struct {
	id int
	ws *watchState
}

// closeConn closes what a discarded connection holds.
func closeConn(c Conn) {
	if c.Mailbox != nil {
		_ = c.Mailbox.Close()
	}
	if c.Watcher != nil {
		_ = c.Watcher.Close()
	}
}

func waitInbox(ws *watchState, ctx context.Context, id int) tea.Cmd {
	return func() tea.Msg {
		// INBOX is the reserved IMAP name for every account's inbox.
		return inboxMsg{id: id, ws: ws, err: ws.w.Wait(ctx, "INBOX")}
	}
}

// startWatch begins watching account id's INBOX, replacing any watch it had.
func (m Model) startWatch(id int, w mail.Watcher) tea.Cmd {
	if w == nil {
		return nil
	}
	m.stopWatch(id)
	ctx, cancel := context.WithCancel(context.Background())
	ws := &watchState{w: w, ctx: ctx, cancel: cancel}
	m.watches[id] = ws
	if i, ok := m.accountIndex(-1, id); ok {
		m.log.Debug("watch start", "account", m.accts[i].Name)
	}
	return waitInbox(ws, ctx, id)
}

// stopWatch ends account id's watch, if it has one.
func (m Model) stopWatch(id int) {
	ws, ok := m.watches[id]
	if !ok {
		return
	}
	ws.cancel()
	_ = ws.w.Close()
	delete(m.watches, id)
}

func (m Model) handleInbox(msg inboxMsg) (Model, tea.Cmd) {
	ws := msg.ws
	if m.watches[msg.id] != ws {
		return m, nil
	}
	i, ok := m.accountIndex(-1, msg.id)
	if !ok {
		return m, nil
	}
	name := m.accts[i].Name
	switch {
	case msg.err == nil:
		ws.backoff = 0
		m.log.Debug("push refresh", "account", name)
		m, cmd := m.refreshSoon(i)
		return m, tea.Batch(cmd, waitInbox(ws, ws.ctx, msg.id))
	case errors.Is(msg.err, mail.ErrUnsupported):
		m.log.Debug("watch unsupported", "account", name)
		m.stopWatch(msg.id)
		return m, nil
	}
	if ws.backoff == 0 {
		ws.backoff = watchBackoff
	} else {
		ws.backoff = min(2*ws.backoff, watchBackoffMax)
	}
	m.log.Debug("watch failed", "account", name, "err", msg.err, "backoff", ws.backoff)
	return m, m.after(ws.backoff, watchRetryMsg{id: msg.id, ws: ws})
}

func (m Model) handleWatchRetry(msg watchRetryMsg) (Model, tea.Cmd) {
	if m.watches[msg.id] != msg.ws {
		return m, nil
	}
	return m, waitInbox(msg.ws, msg.ws.ctx, msg.id)
}
