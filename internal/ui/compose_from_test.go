package ui

import (
	"strings"
	"testing"

	"github.com/dkouvdis/dk-email/internal/config"
	"github.com/dkouvdis/dk-email/internal/mail"
	"github.com/dkouvdis/dk-email/internal/mail/mailtest"
)

var salesAddr = mail.Address{Name: "Sales Team", Email: "sales@example.com"}

// aliasComposer is a new message on an account with one alias, with a
// recipient typed in and the focus back on the From row.
func aliasComposer(t *testing.T) (Model, *mailtest.DraftSender) {
	t.Helper()
	m, s := composeFixture(t)
	m.accts[0].Aliases = []config.Alias{{Email: salesAddr.Email, Name: salesAddr.Name}}
	m = written(t, m, "alice@example.com", "Hi", "Body")
	// Body -> From.
	m = send(t, m, press("tab"))
	return m, s
}

func TestComposeFrom_SendUsesTheChosenAlias(t *testing.T) {
	m, s := aliasComposer(t)
	if view := plain(m); !strings.Contains(view, "From") || !strings.Contains(view, "Me Myself <me@example.com>") {
		t.Fatalf("no From row:\n%s", view)
	}
	m = send(t, m, press("right"))
	if view := plain(m); !strings.Contains(view, "New message · sales@example.com") {
		t.Errorf("title should follow the selection:\n%s", view)
	}
	m = sendKey(t, m)

	sent := s.Sent()
	if len(sent) != 1 || sent[0].From != salesAddr {
		t.Fatalf("sent %+v, want From %v", sent, salesAddr)
	}
}

func TestComposeFrom_SaveUsesTheChosenAlias(t *testing.T) {
	m, s := aliasComposer(t)
	m = send(t, m, press("right"))
	m, _ = keyCmd(t, m, "esc")
	m, cmd := keyCmd(t, m, "s")
	if cmd == nil {
		t.Fatal("s started no save")
	}
	m = send(t, m, cmd())

	saved := s.Saved()
	if len(saved) != 1 || saved[0].From != salesAddr {
		t.Fatalf("saved %+v, want From %v", saved, salesAddr)
	}
}

func TestComposeFrom_NoAliasesNoRow(t *testing.T) {
	m, _ := composeFixture(t)
	m, _ = keyCmd(t, m, "c")
	if view := plain(m); strings.Contains(view, "From ") {
		t.Errorf("From row without aliases:\n%s", view)
	}
}

func TestComposeFrom_ReplyPreselectsTheAlias(t *testing.T) {
	m := replied(t, aliasFixture(t), "r")
	if got := m.draft.pop.FromIndex(); got != 1 {
		t.Errorf("FromIndex = %d, want 1 (the alias)", got)
	}
	if view := plain(m); !strings.Contains(view, "Sales Team <sales@example.com>") {
		t.Errorf("From row should show the alias:\n%s", view)
	}
	if m.draft.pop.Dirty() {
		t.Error("the preselected From is the baseline, not a change")
	}
}

func TestComposeFrom_ReopenedDraftKeepsAnUnconfiguredFrom(t *testing.T) {
	m, _, _ := draftsFixture(t, &mailtest.DraftSender{})
	m = openedDraft(t, m)
	// The stored From is me@example.com, so set the account to something else.
	m.draft = nil
	m.accts[0].Email = "other@example.com"
	f := mail.Address{Name: "Old Alias", Email: "gone@example.com"}
	m, _ = m.startDraft(0, mail.Draft{From: f, To: []mail.Address{{Email: "a@b.c"}}, Subject: "S"}, composeDraft)

	if len(m.draft.from) != 2 || m.draft.pop.FromIndex() != 1 {
		t.Fatalf("from = %v, index %d", m.draft.from, m.draft.pop.FromIndex())
	}
	if view := plain(m); !strings.Contains(view, "Old Alias <gone@example.com>") {
		t.Errorf("From row should show the draft's From:\n%s", view)
	}
	if got := m.draft.selectedFrom(); got != f {
		t.Errorf("selectedFrom = %v", got)
	}
}
