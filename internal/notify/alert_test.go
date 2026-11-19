package notify

import "testing"

func TestText(t *testing.T) {
	tests := []struct {
		name   string
		counts []Count
		want   string
	}{
		{"one account", []Count{{"Work", 3}}, "dk-email: 3 new in Work"},
		{"several", []Count{{"Work", 3}, {"Personal", 1}}, "dk-email: 3 new in Work, 1 in Personal"},
		{"zero entries skipped", []Count{{"Work", 0}, {"Personal", 2}, {"Other", -1}}, "dk-email: 2 new in Personal"},
		{"all zero", []Count{{"Work", 0}}, ""},
		{"none", nil, ""},
		{"control characters dropped", []Count{{"Wo\ark\x1b[31m\n", 1}}, "dk-email: 1 new in Work[31m"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Text(tt.counts); got != tt.want {
				t.Errorf("Text = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSummary(t *testing.T) {
	tests := []struct {
		name   string
		counts []Count
		want   string
	}{
		{"one account", []Count{{"Work", 3}}, "3 new in Work"},
		{"several", []Count{{"Work", 3}, {"Personal", 1}}, "3 new in Work, 1 in Personal"},
		{"zero entries skipped", []Count{{"Work", 0}, {"Personal", 2}}, "2 new in Personal"},
		{"none", nil, ""},
		{"all zero", []Count{{"Work", 0}}, ""},
		{"control characters dropped", []Count{{"Wo\ark\x1b[31m\n", 1}}, "1 new in Work[31m"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Summary(tt.counts); got != tt.want {
				t.Errorf("Summary = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSequence(t *testing.T) {
	const text = "dk-email: 1 new in Work"
	tests := []struct {
		name                string
		bell, desktop, tmux bool
		want                string
	}{
		{"bell only", true, false, false, "\a"},
		{"desktop only", false, true, false, "\x1b]9;" + text + "\x07"},
		{"both", true, true, false, "\x1b]9;" + text + "\x07\a"},
		{"neither", false, false, false, ""},
		{"tmux wraps the OSC 9", true, true, true, "\x1bPtmux;\x1b\x1b]9;" + text + "\x07\x1b\\\a"},
		{"tmux bell only", true, false, true, "\a"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Sequence(text, tt.bell, tt.desktop, tt.tmux); got != tt.want {
				t.Errorf("Sequence = %q, want %q", got, tt.want)
			}
		})
	}
}
