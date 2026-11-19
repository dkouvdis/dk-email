package ui

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/dkouvdis/dk-email/internal/config"
	"github.com/dkouvdis/dk-email/internal/mail"
	"github.com/dkouvdis/dk-email/internal/ui/popup"
)

// editedMsg reports an edit being saved: tested first, or, with anyway, not
// tested at all. acct is the edited account with defaults applied. saveErr
// says err came from writing the config, not from the login test.
type editedMsg struct {
	gen     int
	index   int
	oldName string
	acct    config.Account
	anyway  bool
	conn    Conn
	folders []mail.Folder
	err     error
	saveErr bool
	// reconnect says conn was dialed under the account's old name, so its
	// token store is bound to a name the save has moved away from.
	reconnect bool
	took      time.Duration
}

// removedMsg reports an account being deleted from the config.
type removedMsg struct {
	gen   int
	index int
	acct  config.Account
	err   error
}

func (m *Model) newID() int {
	m.nextID++
	return m.nextID
}

// accountIndex finds the account a slow result is for. A result carrying an
// id is matched by it, so it survives deletions shifting indexes; without
// one (id 0) the index is trusted. It reports false when the account is
// gone or was replaced since.
func (m Model) accountIndex(index, id int) (int, bool) {
	if id != 0 {
		index = slices.Index(m.ids, id)
	}
	return index, index >= 0 && index < len(m.accts)
}

// editTarget is the account E acts on: the one under the sidebar cursor, as
// for c.
func (m Model) editTarget() int { return m.activeAccount() }

// openEdit opens the connect popup, prefilled, for the account under the
// sidebar cursor.
func (m Model) openEdit() (Model, tea.Cmd) {
	if m.onboard.UpdateAccount == nil || !m.onboard.ready() {
		m.setStatus("Editing accounts isn't available.")
		return m, nil
	}
	i := m.editTarget()
	if i < 0 || i >= len(m.accts) {
		m.setStatus("Select an account to edit it.")
		return m, nil
	}
	var others []string
	for j, a := range m.accts {
		if j != i {
			others = append(others, a.Name)
		}
	}
	m.connectFor, m.editFor = -1, i
	m.connectPop = popup.NewEditAccount(m.theme, m.accts[i], others)
	m.connectPop.SetSize(m.panes.Popup.W, m.panes.Popup.H)
	m.showConnect = true
	m.connectGen++
	m.log.Debug("popup opened", "popup", "edit account", "account", m.accts[i].Name)
	return m, nil
}

// handleEditResult acts on a submit, save-anyway or delete from the edit
// popup.
func (m Model) handleEditResult(r popup.ConnectResult) (Model, tea.Cmd) {
	i := m.editFor
	if i >= len(m.accts) {
		return m, nil
	}
	if r == popup.ConnectDelete {
		switch {
		case m.onboard.RemoveAccount == nil:
			m.connectPop.SetError("Deleting accounts isn't available.")
		case m.inflight > 0 || m.sending || m.saving:
			// Results of these name an account by index.
			m.connectPop.SetError("Wait for pending changes to finish.")
		default:
			m.connectPop.SetBusy(true)
			m.connectGen++
			return m, removeAccount(m.onboard, m.connectGen, i, m.accts[i], m.connectPop.DeletePassword())
		}
		return m, nil
	}
	req := m.connectPop.EditRequest()
	if r == popup.ConnectSaveAnyway {
		if editsOAuth(m.accts[i], req) {
			m.connectPop.SetError("Sign in first: Sign in & save stores the sign-in.")
			return m, nil
		}
		m.connectPop.SetBusy(true)
		m.connectGen++
		return m, saveEdit(m.onboard, m.connectGen, i, m.accts[i], req)
	}
	return m.startEditTest(false)
}

// startEditTest tests and saves the edit in the popup, signing in first when
// it chose OAuth 2.0.
func (m Model) startEditTest(useCode bool) (Model, tea.Cmd) {
	i := m.editFor
	if i < 0 || i >= len(m.accts) {
		return m, nil
	}
	req := m.connectPop.EditRequest()
	if editsOAuth(m.accts[i], req) && m.onboard.SignIn == nil {
		m.connectPop.SetError("OAuth sign-in isn't available.")
		return m, nil
	}
	m.connectPop.SetBusy(true)
	m.connectGen++
	ctx := m.beginTest()
	return m, testAndEdit(ctx, m.onboard, m.connectGen, i, m.accts[i], req, useCode)
}

// editsOAuth reports whether the edit asks for a new OAuth sign-in: OAuth 2.0
// was chosen, with no password_cmd supplying tokens.
func editsOAuth(old config.Account, req popup.EditRequest) bool {
	acct := editedAccount(old, req)
	return !req.KeepPassword && acct.OAuth() && acct.PasswordCmd == ""
}

// editedAccount is what the form's account becomes, from old (the loaded
// account). The login is tested with it before anything is saved.
func editedAccount(old config.Account, req popup.EditRequest) config.Account {
	return popup.ApplyEdit(old, req).WithDefaults()
}

// testAndEdit logs in with the edited account (IMAP, SMTP if it has a host,
// and the folder list) and only if all of that works saves it. On any
// failure nothing is saved. With OAuth chosen it signs in first and stores
// the sign-in under the edited name just before saving the account.
func testAndEdit(parent context.Context, o Onboarding, gen, index int, old config.Account, req popup.EditRequest, useCode bool) tea.Cmd {
	if o.SignIn != nil && editsOAuth(old, req) {
		return withProgress(gen, func(progress func(popup.SignIn)) tea.Msg {
			return editTest(parent, o, gen, index, old, req, true, useCode, progress)
		})
	}
	return func() tea.Msg {
		return editTest(parent, o, gen, index, old, req, false, false, nil)
	}
}

func editTest(parent context.Context, o Onboarding, gen, index int, old config.Account, req popup.EditRequest, oauth, useCode bool, progress func(popup.SignIn)) tea.Msg {
	start := time.Now()
	acct := editedAccount(old, req)
	res := editedMsg{gen: gen, index: index, oldName: old.Name, acct: acct}
	fail := func(err error) tea.Msg {
		closeConn(res.conn)
		res.conn = Conn{}
		res.err, res.took = err, time.Since(start)
		return res
	}

	var creds mail.Credentials
	var sess OAuthSession
	if oauth {
		signCtx, cancel := context.WithTimeout(parent, signInTimeout)
		var err error
		// Sign in under the old name: a saved client secret is still there
		// until the save moves the account.
		signAs := acct
		signAs.Name = old.Name
		sess, err = o.SignIn(signCtx, signAs, req.ClientSecret, useCode, progress)
		cancel()
		if err != nil {
			return fail(fmt.Errorf("sign-in failed: %w", err))
		}
		creds = sess.Creds
	} else if req.Password != "" {
		creds = mail.StaticPassword(req.Password)
	}

	ctx, cancel := context.WithTimeout(parent, connectTimeout)
	defer cancel()
	// The saved keychain password is still under the old name until the
	// save, which moves it, so dial under that name.
	dialAs := acct
	if req.KeepPassword && acct.PasswordCmd == "" {
		dialAs.Name = old.Name
		res.reconnect = acct.OAuth() && acct.Name != old.Name
	}
	conn, err := o.Dial(ctx, dialAs, creds)
	if err == nil && conn.Mailbox == nil {
		err = errors.New("connector returned no mailbox")
	}
	if err != nil {
		return fail(err)
	}
	res.conn = conn
	if o.VerifySMTP != nil && strings.TrimSpace(acct.SMTPHost) != "" {
		if err := o.VerifySMTP(ctx, dialAs, creds); err != nil {
			return fail(fmt.Errorf("IMAP login worked, but SMTP failed: %w", err))
		}
	}
	if res.folders, err = conn.Mailbox.Folders(ctx); err != nil {
		return fail(fmt.Errorf("list folders: %w", err))
	}
	if oauth {
		if err := sess.Save(acct.Name); err != nil {
			res.saveErr = true
			return fail(fmt.Errorf("connected, but saving failed: %w", err))
		}
	}
	if err := saveEditedAccount(o, index, old.Name, req, oauth); err != nil {
		if oauth && acct.Name != old.Name {
			sess.Forget(acct.Name) // under the same name the new token just replaced the old one
		}
		res.saveErr = true
		return fail(fmt.Errorf("connected, but saving failed: %w", err))
	}
	res.took = time.Since(start)
	return res
}

// saveEdit saves the edit without testing the login.
func saveEdit(o Onboarding, gen, index int, old config.Account, req popup.EditRequest) tea.Cmd {
	return func() tea.Msg {
		res := editedMsg{gen: gen, index: index, oldName: old.Name, acct: editedAccount(old, req), anyway: true}
		if err := saveEditedAccount(o, index, old.Name, req, false); err != nil {
			res.err, res.saveErr = err, true
		}
		return res
	}
}

func saveEditedAccount(o Onboarding, index int, oldName string, req popup.EditRequest, signedIn bool) error {
	edit := func(raw config.Account) config.Account { return popup.ApplyEdit(raw, req) }
	return o.UpdateAccount(index, oldName, edit, req.Password, signedIn)
}

func removeAccount(o Onboarding, gen, index int, acct config.Account, deletePassword bool) tea.Cmd {
	return func() tea.Msg {
		return removedMsg{gen: gen, index: index, acct: acct, err: o.RemoveAccount(index, acct, deletePassword)}
	}
}

func (m Model) handleEdited(msg editedMsg) (Model, tea.Cmd) {
	if msg.gen == m.connectGen {
		m.endTest()
	}
	live := msg.gen == m.connectGen && m.showConnect && m.editFor == msg.index
	if msg.err != nil {
		closeConn(msg.conn)
		m.log.Debug("edit", "account", msg.oldName, "ok", false, "anyway", msg.anyway, "took", msg.took.Round(time.Millisecond))
		if live {
			m.connectPop.SetBusy(false)
			if msg.saveErr {
				m.connectPop.SetError(msg.err.Error())
			} else {
				m.connectPop.SetTestFailed(msg.err.Error())
			}
		}
		return m, nil
	}
	// A saved edit is applied even if the popup was closed meanwhile: the
	// config has changed, and the next edit would otherwise find a stale name.
	i := msg.index
	if i < 0 || i >= len(m.accts) || m.accts[i].Name != msg.oldName {
		closeConn(msg.conn)
		return m, nil
	}
	m.log.Debug("edit", "account", msg.oldName, "ok", true, "anyway", msg.anyway, "took", msg.took.Round(time.Millisecond))
	if live || m.editFor == i {
		m.showConnect, m.editFor = false, -1
	}
	m.statusErr = false

	m.accts = slices.Clone(m.accts)
	m.accts[i] = msg.acct
	m.inbox.Forget(m.ids[i])
	m.stopWatch(m.ids[i])
	m.ids = slices.Clone(m.ids)
	m.ids[i] = m.newID() // results for the old connection are dropped
	if old := m.boxes[i]; old != nil {
		_ = old.Close()
	}
	m.boxes, m.senders = slices.Clone(m.boxes), slices.Clone(m.senders)
	m.boxes[i], m.senders[i] = nil, nil
	delete(m.needsPassword, i)
	m.side = m.side.Rename(i, msg.acct.Name)
	if m.open.account == i {
		m = m.clearOpen()
	}

	if msg.anyway || msg.reconnect {
		m.side = m.side.SetConnecting(i)
		if msg.anyway {
			m.setStatus(fmt.Sprintf("Saved %s without a working login.", msg.acct.Name))
		} else {
			// The tested connection saves tokens under the old name, so
			// drop it and connect again under the new one.
			closeConn(msg.conn)
			m.setStatus(fmt.Sprintf("Saved %s.", msg.acct.Name))
		}
		if m.connect == nil {
			return m, nil
		}
		return m, tea.Batch(m.side.Tick(), connectAccount(m.connect, i, m.ids[i], msg.acct))
	}
	m, cmd := m.handleAccount(accountMsg{index: i, id: m.ids[i], conn: msg.conn, folders: msg.folders, took: msg.took})
	done := fmt.Sprintf("Saved %s.", msg.acct.Name)
	if m.open.account == i && m.open.loading {
		m.open.notice = done // shown once its Inbox has loaded
	} else {
		m.setStatus(done)
	}
	return m, cmd
}

// clearOpen closes the open folder, its threads and the reader.
func (m Model) clearOpen() Model {
	m.open = openFolder{account: -1}
	m.resetSearch()
	m.list = m.list.SetSummaries(nil, false)
	m.read = m.read.Close()
	m.readerPopup = false
	m.side = m.side.SetOpen(-1, "")
	return m
}

func (m Model) handleRemoved(msg removedMsg) (Model, tea.Cmd) {
	if msg.err != nil {
		if msg.gen == m.connectGen && m.showConnect {
			m.connectPop.SetBusy(false)
			m.connectPop.SetError(msg.err.Error())
		}
		return m, nil
	}
	// The config no longer has the account, so drop it from the UI even if
	// the popup was closed meanwhile.
	i := msg.index
	if i < 0 || i >= len(m.accts) || m.accts[i].Name != msg.acct.Name {
		return m, nil
	}
	m.showConnect, m.editFor = false, -1
	m.log.Debug("account deleted", "account", msg.acct.Name)
	m, cmd := m.removeAccountAt(i)
	if len(m.accts) == 0 {
		var more tea.Cmd
		m, more = m.openConnect()
		cmd = tea.Batch(cmd, more)
	}
	m.statusErr = false
	m.setStatus(fmt.Sprintf("Deleted %s.", msg.acct.Name))
	return m, cmd
}

// removeAccountAt drops account i from every per-account slice and map, and
// shifts the indexes above it down by one: the mailbox is closed, the
// sidebar and the open folder follow, and so does needsPassword. Slow
// results are matched by account id (see accountIndex), so those for the
// removed account are dropped and the others still find theirs. The open
// view is cleared if it was on the account; if it was on a later one its
// first page is loaded again, since a result in flight names the old index.
func (m Model) removeAccountAt(i int) (Model, tea.Cmd) {
	if i < 0 || i >= len(m.accts) {
		return m, nil
	}
	if mb := m.boxes[i]; mb != nil {
		_ = mb.Close()
	}
	m.inbox.Forget(m.ids[i])
	m.stopWatch(m.ids[i])
	m.accts = slices.Delete(slices.Clone(m.accts), i, i+1)
	m.boxes = slices.Delete(slices.Clone(m.boxes), i, i+1)
	m.senders = slices.Delete(slices.Clone(m.senders), i, i+1)
	m.ids = slices.Delete(slices.Clone(m.ids), i, i+1)
	pw := map[int]bool{}
	for j := range m.needsPassword {
		switch {
		case j < i:
			pw[j] = m.needsPassword[j]
		case j > i:
			pw[j-1] = m.needsPassword[j]
		}
	}
	m.needsPassword = pw
	m.connectFor = -1
	m.side = m.side.RemoveAccount(i)

	switch {
	case m.open.account == i:
		return m.clearOpen(), nil
	case m.open.account > i:
		m.open.account--
		m.read = m.read.Close()
		m.readerPopup = false
		if m.list.LoadingMore() {
			m.list = m.list.PageFailed()
		}
		if m.open.loading && m.searching() {
			m.search.gen++
			return m, runSearch(m.boxes[m.open.account], m.open.account, m.open.folder, m.search.query, 0, m.search.gen)
		}
		if m.open.loading {
			return m, loadSummaries(m.boxes[m.open.account], m.open.account, m.open.folder, 0)
		}
	}
	return m, nil
}
