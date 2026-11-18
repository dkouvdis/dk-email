package ui

import "testing"

func TestHelp_ComposerListsAttachKeys(t *testing.T) {
	m := New(testConfig(), nil, nil)
	want := map[string]string{
		"ctrl+o":     "attach a file",
		"drop files": "attach them",
		"←/→":        "choose attachment",
		"d":          "remove attachment",
	}
	for _, sec := range m.keys.HelpSections() {
		if sec.Title != "Composer" {
			continue
		}
		for _, b := range sec.Keys {
			if d, ok := want[b.Help().Key]; ok && d == b.Help().Desc {
				delete(want, b.Help().Key)
			}
		}
	}
	for k := range want {
		t.Errorf("Composer help lacks %q", k)
	}
}
