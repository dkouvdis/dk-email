package ui

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/dkouvdis/dk-email/internal/config"
	"github.com/dkouvdis/dk-email/internal/mail"
)

// fakeWatcher returns its queued results from Wait at once, and blocks on
// the context once the queue is empty.
type fakeWatcher struct {
	mu     sync.Mutex
	queue  []error
	closed int
}

func (w *fakeWatcher) Wait(ctx context.Context, folder string) error {
	w.mu.Lock()
	if len(w.queue) > 0 {
		err := w.queue[0]
		w.queue = w.queue[1:]
		w.mu.Unlock()
		return err
	}
	w.mu.Unlock()
	<-ctx.Done()
	return ctx.Err()
}

func (w *fakeWatcher) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closed++
	return nil
}

func (w *fakeWatcher) closes() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.closed
}

// runAll runs cmd, and each cmd of a batch, and returns the messages.
func runAll(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return []tea.Msg{msg}
	}
	var msgs []tea.Msg
	for _, c := range batch {
		msgs = append(msgs, runAll(c)...)
	}
	return msgs
}

// watched is the fixture, connected without watchers, then given Personal's
// watcher the way a connect result would: through Update.
func watched(t *testing.T, results ...error) (Model, *fixture, *fakeWatcher, tea.Cmd) {
	t.Helper()
	f := newFixture()
	m := started(t, f, 120, 30)
	w := &fakeWatcher{queue: results}
	folders, err := f.personal.Folders(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	next, cmd := m.Update(accountMsg{index: 0, id: m.ids[0], conn: Conn{Mailbox: f.personal, Watcher: w}, folders: folders})
	return next.(Model), f, w, cmd
}

func inboxOf(t *testing.T, msgs []tea.Msg) inboxMsg {
	t.Helper()
	for _, msg := range msgs {
		if in, ok := msg.(inboxMsg); ok {
			return in
		}
	}
	t.Fatalf("no inboxMsg in %v", msgs)
	return inboxMsg{}
}

func TestWatch_ConnectStartsAWaitAndAWakeRefreshesOnce(t *testing.T) {
	m, f, _, wait := watched(t, nil, nil)
	ws := m.watches[m.ids[0]]
	if ws == nil || wait == nil {
		t.Fatalf("watch %v, wait %v; want a started watch and a wait", ws, wait != nil)
	}
	in := inboxOf(t, runAll(wait))
	if in.ws != ws || in.id != m.ids[0] || in.err != nil {
		t.Fatalf("inboxMsg = %+v, want the watch's wake", in)
	}
	before := folderCalls(f.personal)

	next, cmd := m.Update(in)
	m = next.(Model)
	msgs := runAll(cmd)

	var refreshes int
	for _, msg := range msgs {
		if r, ok := msg.(refreshMsg); ok {
			refreshes++
			next, _ = m.Update(r)
			m = next.(Model)
		}
	}
	if refreshes != 1 || folderCalls(f.personal) != before+1 {
		t.Errorf("refreshes %d, Folders calls %d→%d; want exactly one", refreshes, before, folderCalls(f.personal))
	}
	if again := inboxOf(t, msgs); again.ws != ws {
		t.Error("the wake did not queue another wait on the same watch")
	}
}

func TestWatch_WakeDuringARefreshQueuesARerun(t *testing.T) {
	m, f, _, _ := watched(t)
	m, running := m.startRefresh(false)
	before := folderCalls(f.personal)

	next, cmd := m.Update(inboxMsg{id: m.ids[0], ws: m.watches[m.ids[0]]})
	m = next.(Model)
	if !m.refresh.rerun {
		t.Fatal("a wake during a refresh was dropped, want a rerun")
	}
	if cmd == nil {
		t.Fatal("no cmd, want the next wait")
	}
	m = settle(t, m, running)

	if got := folderCalls(f.personal) - before; got != 2 || m.refresh.rerun || m.refresh.pending != 0 {
		t.Errorf("Folders calls +%d, rerun %v, pending %d; want the running refresh then one more", got, m.refresh.rerun, m.refresh.pending)
	}
}

func TestWatch_ErrorsBackOffAndASuccessResets(t *testing.T) {
	m, _, w, _ := watched(t, nil)
	var delays []time.Duration
	m.after = func(d time.Duration, msg tea.Msg) tea.Cmd {
		delays = append(delays, d)
		return func() tea.Msg { return msg }
	}
	ws := m.watches[m.ids[0]]
	boom := errors.New("connection reset")
	fail := func() tea.Msg {
		next, cmd := m.Update(inboxMsg{id: m.ids[0], ws: ws, err: boom})
		m = next.(Model)
		return cmd()
	}

	for range 6 {
		if _, ok := fail().(watchRetryMsg); !ok {
			t.Fatal("a failed wait did not schedule a retry")
		}
	}
	want := []time.Duration{30 * time.Second, time.Minute, 2 * time.Minute, 4 * time.Minute, 5 * time.Minute, 5 * time.Minute}
	if !slices.Equal(delays, want) {
		t.Errorf("backoff = %v, want %v", delays, want)
	}

	next, _ := m.Update(inboxMsg{id: m.ids[0], ws: ws}) // a success
	m = next.(Model)
	delays = nil
	fail()
	if !slices.Equal(delays, []time.Duration{30 * time.Second}) {
		t.Errorf("backoff after a success = %v, want 30s again", delays)
	}

	_, cmd := m.Update(watchRetryMsg{id: m.ids[0], ws: ws})
	if in := inboxOf(t, runAll(cmd)); in.ws != ws || in.err != nil {
		t.Errorf("retry produced %+v, want a new wait on the watch (queue %v)", in, w.queue)
	}
}

func TestWatch_UnsupportedStopsTheWatch(t *testing.T) {
	m, f, w, _ := watched(t)
	ws := m.watches[m.ids[0]]
	before := folderCalls(f.personal)
	m.after = func(time.Duration, tea.Msg) tea.Cmd { t.Error("scheduled a retry"); return nil }

	next, cmd := m.Update(inboxMsg{id: m.ids[0], ws: ws, err: mail.ErrUnsupported})
	m = next.(Model)

	if cmd != nil || len(m.watches) != 0 || w.closes() != 1 || folderCalls(f.personal) != before {
		t.Errorf("cmd %v, watches %d, closes %d; want a stopped, closed watch and no follow-up", cmd != nil, len(m.watches), w.closes())
	}
	if _, cmd = m.Update(watchRetryMsg{id: m.ids[0], ws: ws}); cmd != nil {
		t.Error("a retry for a stopped watch re-armed it")
	}
}

// assertStale checks a late result of the stopped watch ws does nothing.
func assertStale(t *testing.T, m Model, f *fixture, id int, ws *watchState, w *fakeWatcher) {
	t.Helper()
	if w.closes() != 1 || m.watches[id] != nil {
		t.Errorf("closes %d, watch %v; want the old watch closed and gone", w.closes(), m.watches[id])
	}
	before := folderCalls(f.personal)
	next, cmd := m.Update(inboxMsg{id: id, ws: ws})
	m = next.(Model)
	_, retry := m.Update(watchRetryMsg{id: id, ws: ws})
	if cmd != nil || retry != nil || m.refresh.pending != 0 || folderCalls(f.personal) != before {
		t.Error("a late result of the old watch was acted on")
	}
}

func TestWatch_RemovingAnAccountStopsItsWatch(t *testing.T) {
	m, f, w, _ := watched(t)
	id, ws := m.ids[0], m.watches[m.ids[0]]

	m, _ = m.removeAccountAt(0)

	assertStale(t, m, f, id, ws, w)
}

func TestWatch_EditingAnAccountStopsItsWatch(t *testing.T) {
	m, f, w, _ := watched(t)
	id, ws := m.ids[0], m.watches[m.ids[0]]

	next, _ := m.Update(editedMsg{index: 0, oldName: "Personal", acct: config.Account{Name: "Personal"}, anyway: true})
	m = next.(Model)

	if m.ids[0] == id {
		t.Fatal("setup: the edit kept the account id")
	}
	assertStale(t, m, f, id, ws, w)
}

func TestWatch_RetryFailedStopsTheOldWatch(t *testing.T) {
	f := newFixture()
	f.errs["Work"] = errors.New("boom")
	m := started(t, f, 120, 30)
	w := &fakeWatcher{}
	id := m.ids[1]
	m.startWatch(id, w)
	ws := m.watches[id]

	m, _ = m.retryFailed()

	if w.closes() != 1 || m.watches[id] != nil {
		t.Errorf("closes %d, watch %v; want the old watch closed", w.closes(), m.watches[id])
	}
	if _, cmd := m.Update(inboxMsg{id: id, ws: ws}); cmd != nil {
		t.Error("a late result of the old watch was acted on")
	}
}

func TestWatch_FailedConnectClosesItsWatcher(t *testing.T) {
	m, f, _, _ := watched(t)
	m.stopWatch(m.ids[0])
	w2 := &fakeWatcher{}

	next, cmd := m.Update(accountMsg{index: 0, id: m.ids[0], conn: Conn{Mailbox: f.personal, Watcher: w2}, err: errors.New("list folders")})
	m = next.(Model)

	if w2.closes() != 1 || len(m.watches) != 0 || cmd != nil {
		t.Errorf("closes %d, watches %d, cmd %v; want the watcher closed and none started", w2.closes(), len(m.watches), cmd != nil)
	}
}

func TestWatch_CloseClosesEveryWatcher(t *testing.T) {
	m, _, w, _ := watched(t)
	w2 := &fakeWatcher{}
	m.startWatch(m.ids[1], w2)

	if err := m.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if w.closes() != 1 || w2.closes() != 1 || len(m.watches) != 0 {
		t.Errorf("closes %d and %d, watches %d; want every watcher closed", w.closes(), w2.closes(), len(m.watches))
	}
}
