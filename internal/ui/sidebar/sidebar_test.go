package sidebar

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/dkouvdis/dk-email/internal/mail"
)

func press(k string) tea.KeyPressMsg {
	switch k {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	}
	return tea.KeyPressMsg{Code: []rune(k)[0], Text: k}
}

func typeKeys(m Model, text string) Model {
	for _, r := range text {
		m, _ = m.Update(press(string(r)))
	}
	return m
}

func gmailFolders() []mail.Folder {
	return []mail.Folder{
		{Name: "Receipts", Delimiter: "/"},
		{Name: "[Gmail]/Trash", Delimiter: "/", Role: mail.RoleTrash},
		{Name: "INBOX", Delimiter: "/", Role: mail.RoleInbox, Unread: 12, Total: 40},
		{Name: "[Gmail]/All Mail", Delimiter: "/", Role: mail.RoleAll, Unread: 30},
		{Name: "archive-old", Delimiter: "/"},
		{Name: "[Gmail]/Sent Mail", Delimiter: "/", Role: mail.RoleSent},
		{Name: "[Gmail]/Spam", Delimiter: "/", Role: mail.RoleJunk, Unread: 3},
		{Name: "[Gmail]/Drafts", Delimiter: "/", Role: mail.RoleDrafts},
	}
}

func view(m Model) string { return ansi.Strip(m.View(22, 40)) }

func TestSetFolders_OrdersByRoleThenName(t *testing.T) {
	m := New([]string{"Personal"})
	m = m.SetFolders(0, gmailFolders(), nil)

	var got []string
	for _, f := range m.accounts[0].folders {
		got = append(got, f.Name)
	}
	want := []string{"INBOX", "[Gmail]/Drafts", "[Gmail]/Sent Mail", "[Gmail]/All Mail", "[Gmail]/Trash", "[Gmail]/Spam", "archive-old", "Receipts"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("order = %v, want %v", got, want)
	}
}

func TestView_ShowsAccountsFoldersAndUnreadCounts(t *testing.T) {
	m := New([]string{"Personal", "Work"})
	m = m.SetFolders(0, gmailFolders(), nil)
	m = m.SetFolders(1, []mail.Folder{{Name: "INBOX", Role: mail.RoleInbox, Unread: 1}}, nil)

	v := view(m)

	for _, want := range []string{"Personal", "Work", "Inbox", "Sent Mail", "All Mail", "Spam", "Receipts"} {
		if !strings.Contains(v, want) {
			t.Errorf("view missing %q:\n%s", want, v)
		}
	}
	lines := strings.Split(v, "\n")
	if !hasLine(lines, "Inbox", "12") || !hasLine(lines, "Spam", "3") {
		t.Errorf("unread counts not on their folder rows:\n%s", v)
	}
	if hasLine(lines, "Trash", "0") {
		t.Errorf("zero unread count should be blank:\n%s", v)
	}
	for _, l := range lines {
		if w := ansi.StringWidth(l); w > 22 {
			t.Errorf("line %q is %d wide, want ≤ 22", l, w)
		}
	}
}

func hasLine(lines []string, label, count string) bool {
	for _, l := range lines {
		if strings.Contains(l, label) && strings.HasSuffix(strings.TrimRight(l, " "), " "+count) {
			return true
		}
	}
	return false
}

func TestView_ConnectingAndFailedAccounts(t *testing.T) {
	m := New([]string{"Personal", "Work"})
	m = m.SetFolders(1, nil, errors.New("login to account \"Work\" failed: bad credentials"))

	v := view(m)

	if !strings.Contains(v, "connecting") {
		t.Errorf("connecting account has no spinner text:\n%s", v)
	}
	if !strings.Contains(v, "login to account") {
		t.Errorf("failed account doesn't show its error:\n%s", v)
	}
	if !m.Connecting() {
		t.Error("Connecting() = false while Personal connects")
	}
	m = m.SetFolders(0, []mail.Folder{{Name: "INBOX", Role: mail.RoleInbox}}, nil)
	if m.Connecting() {
		t.Error("Connecting() = true after every account finished")
	}
}

func TestUpdate_CursorSkipsHeadersAndEnterSelects(t *testing.T) {
	m := New([]string{"Personal", "Work"})
	m = m.SetFolders(0, []mail.Folder{{Name: "INBOX", Role: mail.RoleInbox}, {Name: "Receipts"}}, nil)
	m = m.SetFolders(1, []mail.Folder{{Name: "INBOX", Role: mail.RoleInbox}}, nil)

	steps := []struct {
		key     string
		account int
		folder  string
	}{
		{"j", 0, "Receipts"},
		{"down", 1, "INBOX"}, // crosses the Work header
		{"j", 1, "INBOX"},    // stays at the bottom
		{"g", 0, "INBOX"},
		{"G", 1, "INBOX"},
		{"k", 0, "Receipts"},
	}
	for _, s := range steps {
		m, _ = m.Update(press(s.key))
		if a, f := m.Cursor(); a != s.account || f != s.folder {
			t.Fatalf("after %s: cursor on %d/%q, want %d/%q", s.key, a, f, s.account, s.folder)
		}
	}

	_, cmd := m.Update(press("enter"))
	if cmd == nil {
		t.Fatal("enter returned no command")
	}
	sel, ok := cmd().(SelectedMsg)
	if !ok || sel.Account != 0 || sel.Folder != "Receipts" {
		t.Errorf("enter sent %+v, want SelectedMsg{0, Receipts}", sel)
	}
}

func TestUpdate_NoFoldersYet(t *testing.T) {
	m := New([]string{"Personal"})

	m, cmd := m.Update(press("enter"))
	if cmd != nil {
		t.Error("enter with no folders returned a command")
	}
	m, _ = m.Update(press("j"))
	if a, f := m.Cursor(); a != -1 || f != "" {
		t.Errorf("Cursor() = %d/%q, want -1/\"\" with no folders", a, f)
	}
}

func TestSetOpen_MarksOpenFolderAndMovesCursor(t *testing.T) {
	m := New([]string{"Personal"})
	m = m.SetFolders(0, gmailFolders(), nil)

	m = m.SetOpen(0, "Receipts")

	if a, f := m.Cursor(); a != 0 || f != "Receipts" {
		t.Errorf("cursor = %d/%q, want 0/Receipts", a, f)
	}
	if !strings.Contains(view(m), "▶ Receipts") {
		t.Errorf("open folder not marked:\n%s", view(m))
	}
}

func TestSetFolders_KeepsCursorOnSameFolder(t *testing.T) {
	m := New([]string{"Personal"})
	m = m.SetFolders(0, gmailFolders(), nil)
	m = m.SetOpen(0, "Receipts")

	m = m.SetFolders(0, append([]mail.Folder{{Name: "Aardvark"}}, gmailFolders()...), nil)

	if _, f := m.Cursor(); f != "Receipts" {
		t.Errorf("cursor moved to %q after a refresh, want Receipts", f)
	}
}

func TestView_ScrollsToKeepCursorVisible(t *testing.T) {
	var folders []mail.Folder
	for i := range 30 {
		folders = append(folders, mail.Folder{Name: "f" + string(rune('a'+i%26)) + string(rune('a'+i/26))})
	}
	m := New([]string{"Personal"})
	m = m.SetFolders(0, folders, nil)

	m, _ = m.Update(press("G"))
	v := ansi.Strip(m.View(22, 8))

	if got := len(strings.Split(v, "\n")); got > 8 {
		t.Errorf("view has %d lines, want ≤ 8", got)
	}
	_, f := m.Cursor()
	if !strings.Contains(v, f) {
		t.Errorf("cursor folder %q scrolled out of view:\n%s", f, v)
	}
}

func TestSpinnerTicksOnlyWhileConnecting(t *testing.T) {
	m := New([]string{"Personal"})
	tick := m.Tick()
	if tick == nil {
		t.Fatal("Tick() = nil while connecting")
	}
	m, next := m.Update(tick())
	if next == nil {
		t.Error("spinner stopped while still connecting")
	}

	m = m.SetFolders(0, nil, nil)
	if _, next = m.Update(tick()); next != nil {
		t.Error("spinner keeps ticking after every account finished")
	}
}

func TestAdjustUnread(t *testing.T) {
	m := New([]string{"Personal"}).SetFolders(0, gmailFolders(), nil)

	m = m.AdjustUnread(0, "INBOX", -2).AdjustUnread(0, "[Gmail]/Spam", -10).AdjustUnread(5, "INBOX", -1)

	if f, _ := m.Folder(0, "INBOX"); f.Unread != 10 {
		t.Errorf("INBOX unread = %d, want 10", f.Unread)
	}
	if f, _ := m.Folder(0, "[Gmail]/Spam"); f.Unread != 0 {
		t.Errorf("Spam unread = %d, want clamped to 0", f.Unread)
	}
	if _, ok := m.Folder(0, "nope"); ok {
		t.Error("Folder found a folder that doesn't exist")
	}
}

func TestAccounts(t *testing.T) {
	m := New([]string{"Personal", "Work", "Empty"})
	m = m.SetFolders(0, gmailFolders(), nil)
	m = m.SetFolders(2, nil, nil)

	got := m.Accounts()

	if len(got) != 3 || got[0].Name != "Personal" || got[0].Folders[0].Name != "INBOX" || got[0].Note != "" {
		t.Errorf("Personal = %+v", got[0])
	}
	if got[1].Note != "connecting…" || got[2].Note != "no folders" {
		t.Errorf("notes = %q, %q", got[1].Note, got[2].Note)
	}
	m = m.SetFolders(1, nil, errors.New("login failed\nHint: use an app password"))
	if note := m.Accounts()[1].Note; note != "login failed" {
		t.Errorf("error note = %q, want its first line", note)
	}
}

func TestAddAccount(t *testing.T) {
	m := New([]string{"Personal"})

	m, i := m.AddAccount("Work", []mail.Folder{{Name: "INBOX", Role: mail.RoleInbox, Unread: 4}})

	if i != 1 || m.Accounts()[1].Name != "Work" {
		t.Fatalf("AddAccount = %d, accounts %+v", i, m.Accounts())
	}
	if f, ok := m.Folder(1, "INBOX"); !ok || f.Unread != 4 {
		t.Errorf("Work INBOX = %+v, %v", f, ok)
	}
	if a, f := m.Cursor(); a != 1 || f != "INBOX" {
		t.Errorf("cursor = %d/%q, want the new account's Inbox when nothing else was selectable", a, f)
	}
}

func TestView_CursorFolderIsPurpleNotReversed(t *testing.T) {
	m := New([]string{"Personal"}).SetFolders(0, gmailFolders(), nil)

	var row string
	for _, l := range strings.Split(m.View(22, 40), "\n") {
		if strings.Contains(ansi.Strip(l), "Inbox") {
			row = l
		}
	}

	if !strings.Contains(row, "48;5;63") || !strings.Contains(row, "38;5;231") {
		t.Errorf("cursor folder should be white on purple 63: %q", row)
	}
	if strings.Contains(row, "\x1b[7m") || strings.Contains(row, ";7m") {
		t.Errorf("cursor folder should not use reverse video: %q", row)
	}
}

func TestFilter_SlashFiltersFoldersAndEnterOpens(t *testing.T) {
	m := New([]string{"Personal", "Work"})
	m = m.SetFolders(0, gmailFolders(), nil)
	m = m.SetFolders(1, []mail.Folder{{Name: "INBOX", Role: mail.RoleInbox}, {Name: "Clients"}}, nil)

	m = typeKeys(m, "/MAIL")

	if !m.Filtering() {
		t.Fatal("/ didn't start filtering")
	}
	v := view(m)
	for _, want := range []string{"/ MAIL", "Personal", "Sent Mail", "All Mail"} {
		if !strings.Contains(v, want) {
			t.Errorf("filtered view missing %q:\n%s", want, v)
		}
	}
	for _, gone := range []string{"Inbox", "Receipts", "Work", "Clients"} {
		if strings.Contains(v, gone) {
			t.Errorf("filtered view still shows %q:\n%s", gone, v)
		}
	}
	if a, f := m.Cursor(); a != 0 || f != "[Gmail]/Sent Mail" {
		t.Errorf("cursor on %d/%q, want the first match", a, f)
	}

	m, _ = m.Update(press("down"))
	m, cmd := m.Update(press("enter"))
	if cmd == nil {
		t.Fatal("enter while filtering returned no command")
	}
	if sel, _ := cmd().(SelectedMsg); sel.Folder != "[Gmail]/All Mail" {
		t.Errorf("enter opened %+v, want All Mail", sel)
	}
	if m.Filtering() || !strings.Contains(view(m), "Clients") {
		t.Errorf("enter should clear the filter and show every folder again:\n%s", view(m))
	}
	if _, f := m.Cursor(); f != "[Gmail]/All Mail" {
		t.Errorf("cursor on %q after opening, want All Mail", f)
	}
}

func TestFilter_TakesEveryKeyAndEscClears(t *testing.T) {
	m := New([]string{"Personal"}).SetFolders(0, gmailFolders(), nil)

	m = typeKeys(m, "/jkzz")

	if v := view(m); !strings.Contains(v, "No folders match") || !strings.Contains(v, "/ jkzz") {
		t.Errorf("want the typed filter and a no-match note:\n%s", v)
	}
	if _, cmd := m.Update(press("enter")); cmd != nil {
		t.Error("enter with no matches should do nothing")
	}
	m, _ = m.Update(press("esc"))
	if m.Filtering() || !strings.Contains(view(m), "Receipts") || strings.Contains(view(m), "/ ") {
		t.Errorf("esc should clear the filter:\n%s", view(m))
	}
}

func TestFilter_Paste(t *testing.T) {
	m := New([]string{"Personal"}).SetFolders(0, gmailFolders(), nil)

	m = m.Paste(tea.PasteMsg{Content: "spam"})
	if m.Filtering() {
		t.Error("paste started a filter without /")
	}
	m, _ = m.Update(press("/"))
	m = m.Paste(tea.PasteMsg{Content: "spam"})

	if _, f := m.Cursor(); f != "[Gmail]/Spam" || strings.Contains(view(m), "Inbox") {
		t.Errorf("paste didn't filter to Spam (cursor %q):\n%s", f, view(m))
	}
}

func TestView_ScrollingUpShowsTheAccountName(t *testing.T) {
	folders := func(names ...string) []mail.Folder {
		var out []mail.Folder
		for _, n := range names {
			out = append(out, mail.Folder{Name: n})
		}
		return out
	}
	m := New([]string{"Personal", "Work", "Proton"})
	m = m.SetFolders(0, folders("a1", "a2", "a3", "a4", "a5"), nil)
	m = m.SetFolders(1, folders("b1", "b2", "b3", "b4", "b5"), nil)
	m = m.SetFolders(2, folders("c1", "c2", "c3", "c4", "c5"), nil)
	m = m.SetHeight(6)
	top := func() string { return strings.TrimSpace(strings.Split(ansi.Strip(m.View(22, 6)), "\n")[0]) }

	m, _ = m.Update(press("G"))
	m, _ = m.Update(press("g"))
	if got := top(); got != "Personal" {
		t.Errorf("after G then g, top row = %q, want the Personal header:\n%s", got, ansi.Strip(m.View(22, 6)))
	}

	m, _ = m.Update(press("G"))
	for range 9 { // c5 → b1
		m, _ = m.Update(press("k"))
	}
	if got := top(); got != "Work" {
		t.Errorf("on Work's first folder, top row = %q, want the Work header:\n%s", got, ansi.Strip(m.View(22, 6)))
	}
}
