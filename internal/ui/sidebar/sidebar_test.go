package sidebar

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/dkouvdis/dk-email/internal/mail"
	"github.com/dkouvdis/dk-email/internal/ui/theme"
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
	m := New(theme.Default(), []string{"Personal"})
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
	m := New(theme.Default(), []string{"Personal", "Work"})
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
	m := New(theme.Default(), []string{"Personal", "Work"})
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
	m := New(theme.Default(), []string{"Personal", "Work"})
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
	m := New(theme.Default(), []string{"Personal"})

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
	m := New(theme.Default(), []string{"Personal"})
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
	m := New(theme.Default(), []string{"Personal"})
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
	m := New(theme.Default(), []string{"Personal"})
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
	m := New(theme.Default(), []string{"Personal"})
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
	m := New(theme.Default(), []string{"Personal"}).SetFolders(0, gmailFolders(), nil)

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
	m := New(theme.Default(), []string{"Personal", "Work", "Empty"})
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
	m := New(theme.Default(), []string{"Personal"})

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

func TestView_CursorFolderIsAccentNotReversed(t *testing.T) {
	m := New(theme.Default(), []string{"Personal"}).SetFolders(0, gmailFolders(), nil)

	var row string
	for _, l := range strings.Split(m.View(22, 40), "\n") {
		if strings.Contains(ansi.Strip(l), "Inbox") {
			row = l
		}
	}

	if !strings.Contains(row, "38;5;16;45m") {
		t.Errorf("cursor folder should be on_accent (black) on accent (magenta): %q", row)
	}
	if strings.Contains(row, "\x1b[7m") || strings.Contains(row, ";7m") {
		t.Errorf("cursor folder should not use reverse video: %q", row)
	}
}

// rawLines is the view with styles, split into lines, each with its plain text.
func rawLines(m Model) (raw, plain []string) {
	raw = strings.Split(m.View(22, 40), "\n")
	for _, l := range raw {
		plain = append(plain, ansi.Strip(l))
	}
	return raw, plain
}

func TestView_DimsAccountsOtherThanTheCursors(t *testing.T) {
	th := theme.Default()
	m := New(th, []string{"Personal", "Work"})
	m = m.SetFolders(0, gmailFolders(), nil)
	m = m.SetFolders(1, []mail.Folder{{Name: "INBOX", Role: mail.RoleInbox, Unread: 1}, {Name: "Clients"}}, nil)

	check := func(active string) {
		t.Helper()
		raw, plain := rawLines(m)
		account := ""
		for i, l := range plain {
			if l == "Personal" || l == "Work" {
				account = l
			}
			if l == "" {
				continue
			}
			dimmed := raw[i] == th.Dim(l)
			if dimmed != (account != active) {
				t.Errorf("with %s active, %s row %q dimmed = %v", active, account, l, dimmed)
			}
		}
	}

	check("Personal")
	m = m.SetOpen(1, "INBOX")
	check("Work")
}

func TestView_FilterDoesNotDim(t *testing.T) {
	th := theme.Default()
	m := New(th, []string{"Personal", "Work"})
	m = m.SetFolders(0, gmailFolders(), nil)
	m = m.SetFolders(1, []mail.Folder{{Name: "INBOX", Role: mail.RoleInbox, Unread: 1}}, nil)

	m = typeKeys(m, "/inbox")

	raw, plain := rawLines(m)
	for i, l := range plain {
		if l != "" && raw[i] == th.Dim(l) {
			t.Errorf("filtered row %q is dimmed", l)
		}
	}
	if !slices.Contains(plain, "Work") {
		t.Errorf("filter should show Work's Inbox:\n%s", strings.Join(plain, "\n"))
	}
}

func TestView_TrashShowsNoUnreadCount(t *testing.T) {
	folders := gmailFolders()
	for i := range folders {
		if folders[i].Role == mail.RoleTrash {
			folders[i].Unread = 5
		}
	}
	m := New(theme.Default(), []string{"Personal"}).SetFolders(0, folders, nil)

	if hasLine(strings.Split(view(m), "\n"), "Trash", "5") {
		t.Errorf("Trash shows its unread count:\n%s", view(m))
	}
	for _, e := range m.Accounts()[0].Tree {
		if e.Name == "[Gmail]/Trash" && e.Unread != 0 {
			t.Errorf("Trash entry Unread = %d, want 0 so pickers hide it too", e.Unread)
		}
	}
}

func TestFilter_SlashFiltersFoldersAndEnterOpens(t *testing.T) {
	m := New(theme.Default(), []string{"Personal", "Work"})
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
	m := New(theme.Default(), []string{"Personal"}).SetFolders(0, gmailFolders(), nil)

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
	m := New(theme.Default(), []string{"Personal"}).SetFolders(0, gmailFolders(), nil)

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
	m := New(theme.Default(), []string{"Personal", "Work", "Proton"})
	m = m.SetFolders(0, folders("a1", "a2", "a3", "a4", "a5"), nil)
	m = m.SetFolders(1, folders("b1", "b2", "b3", "b4", "b5"), nil)
	m = m.SetFolders(2, folders("c1", "c2", "c3", "c4", "c5"), nil)
	m = m.SetSize(22, 6)
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

func nestedFolders() []mail.Folder {
	return []mail.Folder{
		{Name: "Work/Clients/Acme", Delimiter: "/", Unread: 2},
		{Name: "INBOX", Delimiter: "/", Role: mail.RoleInbox},
		{Name: "Receipts", Delimiter: "/"},
		{Name: "Work/Clients", Delimiter: "/"},
		{Name: "INBOX/Later", Delimiter: "/"},
		{Name: "[Gmail]/Sent Mail", Delimiter: "/", Role: mail.RoleSent},
		{Name: "[Gmail]/Starred", Delimiter: "/"},
		{Name: "Work/Admin", Delimiter: "/"},
	}
}

func TestTree_NestsByPathWithSpecialFoldersFlatOnTop(t *testing.T) {
	got := Tree(nestedFolders())

	want := []Entry{
		{Name: "INBOX", Label: "Inbox", Path: "Inbox", Depth: 0, Folder: true},
		{Name: "INBOX/Later", Label: "Later", Path: "INBOX/Later", Depth: 1, Folder: true},
		{Name: "[Gmail]/Sent Mail", Label: "Sent Mail", Path: "Sent Mail", Depth: 0, Folder: true},
		{Name: "[Gmail]", Label: "[Gmail]", Path: "[Gmail]", Depth: 0},
		{Name: "[Gmail]/Starred", Label: "Starred", Path: "[Gmail]/Starred", Depth: 1, Folder: true},
		{Name: "Receipts", Label: "Receipts", Path: "Receipts", Depth: 0, Folder: true},
		{Name: "Work", Label: "Work", Path: "Work", Depth: 0},
		{Name: "Work/Admin", Label: "Admin", Path: "Work/Admin", Depth: 1, Folder: true},
		{Name: "Work/Clients", Label: "Clients", Path: "Work/Clients", Depth: 1, Folder: true},
		{Name: "Work/Clients/Acme", Label: "Acme", Path: "Work/Clients/Acme", Depth: 2, Folder: true, Unread: 2},
	}
	if len(got) != len(want) {
		t.Fatalf("Tree = %d entries, want %d:\n%+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestTree_DotDelimiterAndNoDelimiter(t *testing.T) {
	got := Tree([]mail.Folder{{Name: "Lists.Go", Delimiter: "."}, {Name: "Lists", Delimiter: "."}, {Name: "a/b"}})

	if len(got) != 3 || got[0].Label != "a/b" || got[0].Depth != 0 || got[1].Name != "Lists" || got[2].Label != "Go" || got[2].Depth != 1 || got[2].Path != "Lists/Go" {
		t.Errorf("Tree = %+v", got)
	}
}

func TestView_ShowsNestedFoldersIndentedAndSkipsParentsThatArentFolders(t *testing.T) {
	m := New(theme.Default(), []string{"Personal"}).SetFolders(0, nestedFolders(), nil)

	lines := strings.Split(view(m), "\n")
	for _, want := range []string{"    Later", "    Starred", "  Work", "    Admin", "      Acme"} {
		if !slices.ContainsFunc(lines, func(l string) bool { return strings.HasPrefix(l, want) }) {
			t.Errorf("no line starting %q:\n%s", want, strings.Join(lines, "\n"))
		}
	}

	m, _ = m.Update(press("G"))
	for _, want := range []string{"Work/Clients", "Work/Admin", "Receipts"} {
		m, _ = m.Update(press("k"))
		if _, f := m.Cursor(); f != want {
			t.Fatalf("k moved to %q, want %q (the Work parent isn't a folder)", f, want)
		}
	}
}

func TestFilter_ShowsMatchesWithTheirPath(t *testing.T) {
	m := New(theme.Default(), []string{"Personal"}).SetFolders(0, nestedFolders(), nil)

	m = typeKeys(m, "/acme")

	if v := view(m); !strings.Contains(v, "Work/Clients/Acme") || strings.Contains(v, "  Work\n") {
		t.Errorf("want the match with its path and no parents:\n%s", v)
	}
	m = typeKeys(New(theme.Default(), []string{"Personal"}).SetFolders(0, nestedFolders(), nil), "/work")
	if v := view(m); !strings.Contains(v, "No folders match") {
		t.Errorf("a parent that isn't a folder shouldn't match:\n%s", v)
	}
}

func TestView_ScrollingUpMovesOneRowAtATime(t *testing.T) {
	var fs []mail.Folder
	for i := range 15 {
		fs = append(fs, mail.Folder{Name: fmt.Sprintf("f%02d", i)})
	}
	m := New(theme.Default(), []string{"Personal"}).SetFolders(0, fs, nil).SetSize(22, 8)

	m, _ = m.Update(press("G"))
	prev := m.offset
	for range 14 {
		m, _ = m.Update(press("k"))
		if _, f := m.Cursor(); f != "f00" && prev-m.offset > 1 {
			t.Fatalf("k on %s scrolled up %d rows, want at most 1", f, prev-m.offset)
		}
		prev = m.offset
	}
	if top := strings.Split(ansi.Strip(m.View(22, 8)), "\n")[0]; strings.TrimSpace(top) != "Personal" {
		t.Errorf("on the first folder, top row = %q, want the account name", top)
	}
}

func TestClick_OpensTheFolderOnThatLineAndClearsAFilter(t *testing.T) {
	m := New(theme.Default(), []string{"Personal"}).SetFolders(0, gmailFolders(), nil).SetSize(22, 20)

	if _, cmd := m.Click(0); cmd != nil {
		t.Error("clicking the account name should do nothing")
	}
	m = typeKeys(m, "/spam") // line 0 is the filter, 1 the account, 2 Spam
	m, cmd := m.Click(2)
	if cmd == nil {
		t.Fatal("clicking a filtered folder returned no command")
	}
	if sel := cmd().(SelectedMsg); sel.Folder != "[Gmail]/Spam" || m.Filtering() {
		t.Errorf("opened %q, filtering %v; want Spam and the filter cleared", sel.Folder, m.Filtering())
	}
}

func TestInboxUnread(t *testing.T) {
	m := New(theme.Default(), []string{"Personal", "Work", "Empty"})
	m = m.SetFolders(0, gmailFolders(), nil) // INBOX 12; All Mail and Spam don't count
	m = m.SetFolders(1, []mail.Folder{{Name: "INBOX", Delimiter: "/", Role: mail.RoleInbox, Unread: 5}}, nil)
	m = m.SetFolders(2, nil, nil)

	if got := m.InboxUnread(); got != 17 {
		t.Errorf("InboxUnread() = %d, want 17", got)
	}
}
