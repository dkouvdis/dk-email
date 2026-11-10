package ui

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/dkouvdis/dk-email/internal/config"
	"github.com/dkouvdis/dk-email/internal/mail"
	"github.com/dkouvdis/dk-email/internal/mail/mailtest"
	"github.com/dkouvdis/dk-email/internal/ui/threadlist"
)

// fixture is two accounts backed by fakes: Personal (Gmail-like) and Work.
type fixture struct {
	personal, work *mailtest.Fake
	errs           map[string]error
}

func newFixture() *fixture {
	p := mailtest.New()
	p.AddFolder("INBOX", mail.RoleInbox)
	p.AddFolder("[Gmail]/Sent Mail", mail.RoleSent)
	p.AddFolder("[Gmail]/All Mail", mail.RoleAll)
	p.AddFolder("Receipts", mail.RoleNone)
	p.AddFolder("[Gmail]/Trash", mail.RoleTrash)
	p.AddMessage("INBOX", msg("Alice", "Plan", fixedNow.Add(-5*time.Hour), true, "p1"))
	p.AddMessage("INBOX", msg("Carol", "Lunch?", fixedNow.Add(-26*time.Hour), false, "l1"))
	p.AddMessage("INBOX", msg("Bob", "Re: Plan", fixedNow.Add(-2*time.Hour), false, "p2", "p1"))
	p.AddMessage("Receipts", msg("Shop", "Your receipt", fixedNow.Add(-72*time.Hour), false, "r1"))

	w := mailtest.New()
	w.AddFolder("INBOX", mail.RoleInbox)
	w.AddFolder("Archive", mail.RoleArchive)
	w.AddMessage("INBOX", msg("Boss", "Quarterly numbers", fixedNow.Add(-time.Hour), false, "w1"))
	return &fixture{personal: p, work: w, errs: map[string]error{}}
}

// fixedNow is the clock for list dates, so golden output never changes.
var fixedNow = time.Date(2026, 11, 9, 15, 30, 0, 0, time.Local)

func msg(from, subject string, at time.Time, seen bool, id string, refs ...string) mail.Message {
	s := mail.Summary{From: mail.Address{Name: from}, Subject: subject, Date: at, Seen: seen, MessageID: id}
	if len(refs) > 0 {
		s.InReplyTo, s.References = refs[len(refs)-1], refs
	}
	return mail.Message{Summary: s, Body: "Body of " + subject + " from " + from + "."}
}

func (f *fixture) connector() Connector {
	return func(ctx context.Context, acct config.Account) (mail.Mailbox, error) {
		if err := f.errs[acct.Name]; err != nil {
			return nil, err
		}
		if acct.Name == "Work" {
			return f.work, nil
		}
		return f.personal, nil
	}
}

// newModel is New with the refresh timer off: tests send refreshTickMsg
// themselves, since a real tick would block settle for minutes.
func newModel(connect Connector) Model {
	m := New(testConfig(), connect, nil)
	m.every = func(time.Duration) tea.Cmd { return nil }
	return m
}

// settle runs cmd and every command that follows from it, feeding each
// message back through Update. Spinner ticks are dropped so it terminates.
func settle(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	queue := []tea.Cmd{cmd}
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		switch msg := c().(type) {
		case nil, spinner.TickMsg:
		case tea.BatchMsg:
			queue = append(queue, msg...)
		default:
			next, nc := m.Update(msg)
			m = next.(Model)
			queue = append(queue, nc)
		}
	}
	return m
}

// started returns a model sized w×h whose accounts have all connected.
func started(t *testing.T, f *fixture, w, h int) Model {
	t.Helper()
	m := newModel(f.connector())
	m.list = threadlist.New(func() time.Time { return fixedNow })
	m = send(t, m, size(w, h))
	return settle(t, m, m.Init())
}

func TestAccounts_ConnectShowsBothWithUnreadCounts(t *testing.T) {
	m := started(t, newFixture(), 160, 30)

	v := plain(m)
	for _, want := range []string{"Personal", "Work", "Inbox", "Sent Mail", "All Mail", "Receipts", "Archive"} {
		if !strings.Contains(v, want) {
			t.Errorf("view missing %q:\n%s", want, v)
		}
	}
	var counts []string
	for _, l := range strings.Split(ansi.Strip(m.side.View(20, 30)), "\n") {
		if strings.Contains(l, "Inbox") {
			f := strings.Fields(l)
			counts = append(counts, f[len(f)-1])
		}
	}
	if !slices.Equal(counts, []string{"2", "1"}) {
		t.Errorf("Inbox unread counts = %v, want [2 1] (Personal, Work)", counts)
	}
	if strings.Contains(v, "connecting") {
		t.Errorf("an account still shows connecting:\n%s", v)
	}
}

func TestAccounts_OpensFirstInboxAndLoadsSummaries(t *testing.T) {
	f := newFixture()
	m := started(t, f, 160, 30)

	if m.open.account != 0 || m.open.folder != "INBOX" || m.list.Count() != 3 {
		t.Fatalf("open = %+v with %d summaries, want Personal INBOX with 3", m.open, m.list.Count())
	}
	if !strings.Contains(plain(m), "Re: Plan (2)") || !strings.Contains(plain(m), "▶ Inbox") {
		t.Errorf("view doesn't show the opened Inbox:\n%s", plain(m))
	}
	if calls := summaryCalls(f.work); len(calls) != 0 {
		t.Errorf("Work Summaries called %v; only one folder should open", calls)
	}
}

func summaryCalls(f *mailtest.Fake) []string {
	var out []string
	for _, c := range f.Calls() {
		if c.Method == mailtest.MethodSummaries {
			out = append(out, c.Folder)
		}
	}
	return out
}

func TestAccounts_FailingAccountDoesNotBlockTheOther(t *testing.T) {
	tests := []struct {
		name  string
		setup func(f *fixture)
		want  string
	}{
		{"connect fails", func(f *fixture) { f.errs["Work"] = errors.New("login failed: bad credentials") }, "login failed"},
		{"folders fail", func(f *fixture) { f.work.Fail(mailtest.MethodFolders, errors.New("LIST refused")) }, "LIST refused"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture()
			tt.setup(f)

			m := started(t, f, 160, 30)

			v := plain(m)
			if !strings.Contains(v, tt.want) {
				t.Errorf("view doesn't show Work's error %q:\n%s", tt.want, v)
			}
			if !m.statusErr || !strings.HasPrefix(m.status, "Work: ") {
				t.Errorf("status = %q (err %v), want a Work error", m.status, m.statusErr)
			}
			if !strings.Contains(v, "Receipts") || m.open.folder != "INBOX" {
				t.Errorf("Personal didn't load alongside the failing account:\n%s", v)
			}
		})
	}
}

func TestAccounts_KeysWorkWhileConnecting(t *testing.T) {
	f := newFixture()
	f.personal.SetDelay(300 * time.Millisecond)
	f.work.SetDelay(300 * time.Millisecond)
	m := send(t, newModel(f.connector()), size(160, 30))
	batch, ok := m.Init()().(tea.BatchMsg)
	if !ok {
		t.Fatal("Init didn't return a batch")
	}
	results := make(chan tea.Msg, len(batch))
	for _, c := range batch {
		go func() { results <- c() }()
	}

	start := time.Now()
	m = send(t, m, press("tab"), press("?"))
	if !m.showHelp || m.focus != PaneSidebar {
		t.Errorf("keys weren't handled while connecting: help %v focus %v", m.showHelp, m.focus)
	}
	if !strings.Contains(plain(m), "connecting") {
		t.Errorf("sidebar doesn't show connecting:\n%s", plain(m))
	}
	if took := time.Since(start); took > 100*time.Millisecond {
		t.Errorf("handling keys took %v while accounts connected", took)
	}

	m = send(t, m, press("esc"))
	for range batch {
		msg := <-results
		m = settle(t, m, func() tea.Msg { return msg })
	}
	if strings.Contains(plain(m), "connecting") {
		t.Errorf("still connecting after the commands finished:\n%s", plain(m))
	}
}

func TestAccounts_EnterOpensFolderUnderCursor(t *testing.T) {
	f := newFixture()
	m := started(t, f, 160, 30)
	m = send(t, m, press("h")) // focus the sidebar; cursor is on Personal's Inbox

	// Inbox, Sent Mail, All Mail, Trash, Receipts.
	m = send(t, m, press("j"), press("j"), press("j"), press("j"))
	next, cmd := m.Update(press("enter"))
	m = settle(t, next.(Model), cmd)

	if m.open.folder != "Receipts" || m.list.Count() != 1 || !strings.Contains(plain(m), "Your receipt") {
		t.Fatalf("open = %+v with %d summaries, want Receipts with 1", m.open, m.list.Count())
	}
	if got := summaryCalls(f.personal); !slices.Equal(got, []string{"INBOX", "Receipts"}) {
		t.Errorf("Summaries calls = %v, want [INBOX Receipts]", got)
	}

	m = send(t, m, size(70, 30))
	if !strings.Contains(plain(m), "Personal › Receipts") {
		t.Errorf("list-mode header doesn't name the open account and folder:\n%s", plain(m))
	}
}

func TestAccounts_SidebarKeysOnlyWhenFocused(t *testing.T) {
	m := started(t, newFixture(), 160, 30)

	m = send(t, m, press("j"))

	if _, f := m.side.Cursor(); f != "INBOX" {
		t.Errorf("j with the list focused moved the sidebar cursor to %q", f)
	}
}

func TestAccounts_StaleSummariesAreIgnored(t *testing.T) {
	m := started(t, newFixture(), 160, 30)

	next, _ := m.Update(summariesMsg{account: 0, folder: "Receipts", summaries: make([]mail.Summary, 9)})
	m = next.(Model)

	if m.open.folder != "INBOX" || m.list.Count() != 3 {
		t.Errorf("a response for a closed folder replaced the open one: %+v", m.open)
	}
}

func TestAccounts_SummariesErrorShowsInStatus(t *testing.T) {
	f := newFixture()
	f.personal.Fail(mailtest.MethodSummaries, errors.New("fetch timed out"))

	m := started(t, f, 160, 30)

	if !m.statusErr || !strings.Contains(m.status, "load Personal/Inbox: fetch timed out") {
		t.Errorf("status = %q, want the load error", m.status)
	}
	if m.open.loading {
		t.Error("open folder still loading after an error")
	}
}

func TestStatus_ErrorStaysUntilKeyPress(t *testing.T) {
	m := New(testConfig(), nil, nil)
	m.setError(errors.New("boom"))
	m.setStatus("Loading Inbox…")
	if m.status != "boom" || !m.statusErr {
		t.Fatalf("status = %q, progress replaced the error", m.status)
	}

	m = send(t, m, press("j"))

	if m.status != "" || m.statusErr {
		t.Errorf("status = %q (err %v) after a key, want it dismissed", m.status, m.statusErr)
	}
}

func TestAccounts_LoadingStatus(t *testing.T) {
	m := send(t, newModel(newFixture().connector()), size(160, 30))
	next, cmd := m.Update(accountMsg{index: 0, mb: newFixture().personal, folders: []mail.Folder{{Name: "INBOX", Role: mail.RoleInbox}}})
	m = next.(Model)

	if cmd == nil || m.status != "Loading Inbox…" || !strings.Contains(plain(m), "Loading…") {
		t.Errorf("status %q, view:\n%s\nwant Loading Inbox… and a loading list", m.status, plain(m))
	}
}

func TestAccounts_NilMailboxIsAnError(t *testing.T) {
	connect := func(context.Context, config.Account) (mail.Mailbox, error) { return nil, nil }
	m := send(t, newModel(connect), size(160, 30))

	m = settle(t, m, m.Init())

	if !strings.Contains(plain(m), "connector returned no mailbox") {
		t.Errorf("view doesn't show the connector error:\n%s", plain(m))
	}
}

func TestAccounts_CloseClosesMailboxes(t *testing.T) {
	f := newFixture()
	m := started(t, f, 160, 30)

	if err := m.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	for name, fake := range map[string]*mailtest.Fake{"Personal": f.personal, "Work": f.work} {
		if _, err := fake.Folders(context.Background()); !errors.Is(err, mail.ErrClosed) {
			t.Errorf("%s still open after Close (err = %v)", name, err)
		}
	}
}

func TestList_KeysMoveAndExpandWhenFocused(t *testing.T) {
	m := started(t, newFixture(), 160, 30)

	m = send(t, m, press("space"))
	if !strings.Contains(plain(m), "Alice") {
		t.Errorf("space didn't expand the Plan thread:\n%s", plain(m))
	}
	m = send(t, m, press("j"))
	if _, uid, _ := m.list.Selected(); uid == 0 {
		t.Error("j didn't move into the expanded thread")
	}
}

func bulkFixture(n int) *fixture {
	f := newFixture()
	f.personal.AddFolder("Bulk", mail.RoleNone)
	for i := range n {
		f.personal.AddMessage("Bulk", msg("S", fmt.Sprintf("bulk %03d", i), fixedNow.Add(time.Duration(i-n)*time.Minute), true, fmt.Sprint("b", i)))
	}
	return f
}

func TestList_PagesThroughAFolder(t *testing.T) {
	m := started(t, bulkFixture(450), 160, 30)
	opened, cmd := m.openFolder(0, "Bulk")
	m = settle(t, opened, cmd)
	if m.list.Count() != 200 {
		t.Fatalf("first page has %d messages, want 200", m.list.Count())
	}

	for _, want := range []int{400, 450, 450} {
		next, cmd := m.Update(press("G"))
		m = settle(t, next.(Model), cmd)
		if m.list.Count() != want {
			t.Fatalf("after G: %d messages, want %d", m.list.Count(), want)
		}
	}
	if m.status != "" {
		t.Errorf("status = %q after paging, want it cleared", m.status)
	}
}

func TestList_PageErrorShowsAndAllowsRetry(t *testing.T) {
	m := started(t, bulkFixture(250), 160, 30)
	opened, cmd := m.openFolder(0, "Bulk")
	m = settle(t, opened, cmd)
	next, _ := m.Update(press("G"))
	m = next.(Model)

	next, _ = m.Update(summariesMsg{account: 0, folder: "Bulk", before: 51, err: errors.New("fetch failed")})
	m = next.(Model)

	if !m.statusErr || !strings.Contains(m.status, "fetch failed") || m.list.LoadingMore() {
		t.Errorf("status %q, loading %v; want the error and no page loading", m.status, m.list.LoadingMore())
	}
}

func TestList_FolderChangedWhilePagingReloads(t *testing.T) {
	m := started(t, bulkFixture(250), 160, 30)
	opened, cmd := m.openFolder(0, "Bulk")
	m = settle(t, opened, cmd)
	next, _ := m.Update(press("G"))
	m = next.(Model)

	next, cmd = m.Update(summariesMsg{account: 0, folder: "Bulk", before: 51, err: fmt.Errorf("%q: %w", "Bulk", mail.ErrFolderChanged)})
	m = next.(Model)

	if cmd == nil || !m.open.loading || m.statusErr {
		t.Fatalf("status %q (err %v), loading %v, cmd %v; want a reload", m.status, m.statusErr, m.open.loading, cmd != nil)
	}
	m = settle(t, m, cmd)
	if m.status != "Bulk changed on the server; reloaded." {
		t.Errorf("status after reload = %q, want the notice", m.status)
	}
	if m.list.Count() != 200 || m.list.LoadingMore() {
		t.Errorf("after reload: %d messages, loading more %v; want a fresh first page", m.list.Count(), m.list.LoadingMore())
	}
}

func TestList_UnexpectedPageIsIgnored(t *testing.T) {
	m := started(t, newFixture(), 160, 30)

	next, _ := m.Update(summariesMsg{account: 0, folder: "INBOX", before: 3, summaries: make([]mail.Summary, 5)})
	m = next.(Model)

	if m.list.Count() != 3 {
		t.Errorf("a page nobody asked for changed the list to %d messages", m.list.Count())
	}
}
