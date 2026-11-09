package thread

import (
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dkouvdis/dk-email/internal/mail"
)

// thread is a pure core package: only the standard library and mail.
func TestThreadPackage_Imports(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", "-f", "{{if not .Standard}}{{.ImportPath}}{{end}}", ".").Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	allowed := []string{
		"github.com/dkouvdis/dk-email/internal/thread",
		"github.com/dkouvdis/dk-email/internal/mail",
	}
	for _, dep := range strings.Fields(string(out)) {
		if !slices.Contains(allowed, dep) {
			t.Errorf("thread imports disallowed package %q", dep)
		}
	}
}

var base = time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)

func msg(uid mail.UID, id, subject string, hour int, refs ...string) mail.Summary {
	s := mail.Summary{UID: uid, MessageID: id, Subject: subject, Date: base.Add(time.Duration(hour) * time.Hour)}
	if len(refs) > 0 {
		s.InReplyTo = refs[len(refs)-1]
		s.References = refs
	}
	return s
}

// uidsOf returns each thread's UIDs in thread order, for compact assertions.
func uidsOf(threads []Thread) [][]mail.UID {
	out := make([][]mail.UID, len(threads))
	for i, t := range threads {
		out[i] = t.UIDs()
	}
	return out
}

func assertThreads(t *testing.T, got []Thread, want [][]mail.UID) {
	t.Helper()
	g := uidsOf(got)
	if !slices.EqualFunc(g, want, slices.Equal) {
		t.Errorf("threads = %v, want %v", g, want)
	}
}

func TestBuild(t *testing.T) {
	tests := []struct {
		name string
		in   []mail.Summary
		want [][]mail.UID
	}{
		{
			name: "empty",
			in:   nil,
			want: [][]mail.UID{},
		},
		{
			name: "simple chain via references",
			in: []mail.Summary{
				msg(1, "<a@x>", "Plan", 0),
				msg(2, "<b@x>", "Re: Plan", 1, "<a@x>"),
				msg(3, "<c@x>", "Re: Plan", 2, "<a@x>", "<b@x>"),
			},
			want: [][]mail.UID{{1, 2, 3}},
		},
		{
			name: "out of order arrival",
			in: []mail.Summary{
				msg(3, "<c@x>", "Re: Plan", 2, "<a@x>", "<b@x>"),
				msg(1, "<a@x>", "Plan", 0),
				msg(2, "<b@x>", "Re: Plan", 1, "<a@x>"),
			},
			want: [][]mail.UID{{1, 2, 3}},
		},
		{
			name: "missing parent joins siblings",
			in: []mail.Summary{
				msg(5, "<b@x>", "Re: Gone", 1, "<root@x>"),
				msg(6, "<c@x>", "Re: Gone", 2, "<root@x>"),
			},
			want: [][]mail.UID{{5, 6}},
		},
		{
			name: "only in-reply-to set",
			in: []mail.Summary{
				msg(1, "<a@x>", "Plan", 0),
				{UID: 2, MessageID: "<b@x>", Subject: "Re: Plan", Date: base.Add(time.Hour), InReplyTo: "<a@x>"},
			},
			want: [][]mail.UID{{1, 2}},
		},
		{
			name: "in-reply-to with trailing comment",
			in: []mail.Summary{
				msg(1, "<a@x>", "Plan", 0),
				{UID: 2, MessageID: "<b@x>", Subject: "Re: Plan", Date: base.Add(time.Hour), InReplyTo: `<a@x> (Jane's message of Monday)`},
			},
			want: [][]mail.UID{{1, 2}},
		},
		{
			name: "ids without angle brackets match bracketed ones",
			in: []mail.Summary{
				msg(1, "a@x", "Plan", 0),
				msg(2, "<b@x>", "Re: Plan", 1, "<a@x>"),
			},
			want: [][]mail.UID{{1, 2}},
		},
		{
			name: "references packed in one string",
			in: []mail.Summary{
				msg(1, "<a@x>", "Plan", 0),
				msg(2, "<b@x>", "Re: Plan", 1),
				{UID: 3, MessageID: "<c@x>", Subject: "Re: Plan", Date: base.Add(2 * time.Hour), References: []string{"<a@x> <b@x>"}},
			},
			want: [][]mail.UID{{1, 2, 3}},
		},
		{
			name: "reference cycle",
			in: []mail.Summary{
				msg(1, "<a@x>", "Loop", 0, "<b@x>"),
				msg(2, "<b@x>", "Re: Loop", 1, "<a@x>"),
			},
			want: [][]mail.UID{{1, 2}},
		},
		{
			name: "self reference",
			in: []mail.Summary{
				msg(1, "<a@x>", "Me", 0, "<a@x>"),
			},
			want: [][]mail.UID{{1}},
		},
		{
			name: "duplicate message-ids land in one thread",
			in: []mail.Summary{
				msg(1, "<a@x>", "Copy", 0),
				msg(2, "<a@x>", "Copy", 0),
			},
			want: [][]mail.UID{{1, 2}},
		},
		{
			name: "no message-id stays alone",
			in: []mail.Summary{
				msg(1, "", "Note", 0),
				msg(2, "", "Note", 1),
			},
			want: [][]mail.UID{{2}, {1}},
		},
		{
			name: "no message-id but references still thread",
			in: []mail.Summary{
				msg(1, "<a@x>", "Plan", 0),
				msg(2, "", "Re: Plan", 1, "<a@x>"),
			},
			want: [][]mail.UID{{1, 2}},
		},
		{
			name: "generic subject never merges unrelated originals",
			in: []mail.Summary{
				msg(1, "<a@x>", "Hello", 0),
				msg(2, "<b@y>", "Hello", 1),
				msg(3, "<c@z>", "hello", 2),
			},
			want: [][]mail.UID{{3}, {2}, {1}},
		},
		{
			name: "subject fallback attaches an orphan reply",
			in: []mail.Summary{
				msg(1, "<a@x>", "Lunch?", 0),
				msg(2, "<b@x>", "RE: Lunch?", 1),
			},
			want: [][]mail.UID{{1, 2}},
		},
		{
			name: "subject fallback ignores list tags and stacked prefixes",
			in: []mail.Summary{
				msg(1, "<a@x>", "[golang-nuts] Generics", 0),
				msg(2, "<b@x>", "Fwd: Re: [golang-nuts] Generics", 1),
			},
			want: [][]mail.UID{{1, 2}},
		},
		{
			name: "orphan reply picks the latest original before it",
			in: []mail.Summary{
				msg(1, "<a@x>", "Hello", 0),
				msg(2, "<b@y>", "Hello", 5),
				msg(3, "<c@x>", "Re: Hello", 6),
				msg(4, "<d@x>", "Re: Hello", 3),
			},
			want: [][]mail.UID{{2, 3}, {1, 4}},
		},
		{
			name: "orphan reply older than every original joins the earliest",
			in: []mail.Summary{
				msg(1, "<a@x>", "Hello", 5),
				msg(2, "<b@y>", "Hello", 8),
				msg(3, "<c@x>", "Re: Hello", 1),
			},
			want: [][]mail.UID{{2}, {3, 1}},
		},
		{
			name: "orphan replies with no original group together",
			in: []mail.Summary{
				msg(1, "<a@x>", "Re: Lost", 0),
				msg(2, "<b@x>", "Re: Lost", 1),
			},
			want: [][]mail.UID{{1, 2}},
		},
		{
			name: "orphan reply joins a referenced thread with that subject",
			in: []mail.Summary{
				msg(1, "<a@x>", "Plan", 0),
				msg(2, "<b@x>", "Re: Plan", 1, "<a@x>"),
				msg(3, "<c@x>", "Re: Plan", 2),
			},
			want: [][]mail.UID{{1, 2, 3}},
		},
		{
			name: "empty normalized subject never falls back",
			in: []mail.Summary{
				msg(1, "<a@x>", "Re:", 0),
				msg(2, "<b@x>", "Re:", 1),
			},
			want: [][]mail.UID{{2}, {1}},
		},
		{
			name: "threads sorted by newest activity",
			in: []mail.Summary{
				msg(1, "<a@x>", "Old start", 0),
				msg(2, "<b@x>", "New start", 5),
				msg(3, "<c@x>", "Re: Old start", 9, "<a@x>"),
			},
			want: [][]mail.UID{{1, 3}, {2}},
		},
		{
			name: "same date breaks ties by uid",
			in: []mail.Summary{
				msg(7, "<a@x>", "A", 0),
				msg(9, "<b@x>", "B", 0),
				msg(8, "<c@x>", "Re: A", 0, "<a@x>"),
			},
			want: [][]mail.UID{{9}, {7, 8}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertThreads(t, Build(tt.in), tt.want)
		})
	}
}

func TestBuild_DoesNotModifyInput(t *testing.T) {
	in := []mail.Summary{
		msg(2, "<b@x>", "Re: Plan", 1, "<a@x>"),
		msg(1, "<a@x>", "Plan", 0),
	}
	orig := slices.Clone(in)
	Build(in)
	for i := range in {
		if in[i].UID != orig[i].UID {
			t.Fatalf("input reordered: %v", in)
		}
	}
}

func TestBuild_IsDeterministic(t *testing.T) {
	in := benchSummaries(500)
	want := uidsOf(Build(in))
	for range 5 {
		if got := uidsOf(Build(in)); !slices.EqualFunc(got, want, slices.Equal) {
			t.Fatal("Build returned different threads for the same input")
		}
	}
}

func TestThread_Accessors(t *testing.T) {
	a := msg(1, "<a@x>", "Plan", 0)
	a.Seen = true
	b := msg(2, "<b@x>", "Re: Plan", 1, "<a@x>")
	b.Flagged = true
	c := msg(3, "<c@x>", "Re: Plan", 2, "<a@x>")

	th := Build([]mail.Summary{c, a, b})[0]

	if got := th.Len(); got != 3 {
		t.Errorf("Len() = %d, want 3", got)
	}
	if got := th.Latest().UID; got != 3 {
		t.Errorf("Latest().UID = %d, want 3", got)
	}
	if got := th.Unread(); got != 2 {
		t.Errorf("Unread() = %d, want 2", got)
	}
	if !th.Flagged() {
		t.Error("Flagged() = false, want true")
	}
	if got := th.UIDs(); !slices.Equal(got, []mail.UID{1, 2, 3}) {
		t.Errorf("UIDs() = %v", got)
	}

	var empty Thread
	if empty.Latest().UID != 0 || empty.Flagged() || empty.Unread() != 0 || empty.Len() != 0 {
		t.Error("zero Thread accessors should return zero values")
	}
}

func TestNormalizeSubject(t *testing.T) {
	tests := []struct {
		in        string
		want      string
		wantReply bool
	}{
		{"Hello", "hello", false},
		{"  Hello   world ", "hello world", false},
		{"Re: Hello", "hello", true},
		{"RE:Hello", "hello", true},
		{"re : Hello", "hello", true},
		{"Fwd: Hello", "hello", true},
		{"FW: Hello", "hello", true},
		{"Re[2]: Hello", "hello", true},
		{"Re: Fwd: RE: Hello", "hello", true},
		{"[list] Hello", "hello", false},
		{"[list] Re: [list] Hello", "hello", true},
		{"Re[x]: Hello", "re[x]: hello", false},
		{"Re[]: Hello", "re[]: hello", false},
		{"Re[2 Hello", "re[2 hello", false},
		{"[urgent]", "[urgent]", false},
		{"[] Hello", "[] hello", false},
		{"Regarding: Hello", "regarding: hello", false},
		{"Rex", "rex", false},
		{"Re", "re", false},
		{"", "", false},
	}
	for _, tt := range tests {
		got, reply := normalizeSubject(tt.in)
		if got != tt.want || reply != tt.wantReply {
			t.Errorf("normalizeSubject(%q) = %q, %v; want %q, %v", tt.in, got, reply, tt.want, tt.wantReply)
		}
	}
}

// benchSummaries builds n messages in conversations of up to 5, with some
// orphan replies and missing ids, shuffled deterministically.
func benchSummaries(n int) []mail.Summary {
	out := make([]mail.Summary, 0, n)
	for i := range n {
		conv := i / 5
		pos := i % 5
		id := fmt.Sprintf("<m%d@bench>", i)
		subject := fmt.Sprintf("Topic %d", conv)
		var refs []string
		if pos > 0 {
			subject = "Re: " + subject
			for p := conv * 5; p < i; p++ {
				refs = append(refs, fmt.Sprintf("<m%d@bench>", p))
			}
		}
		switch {
		case i%37 == 0:
			id = ""
		case i%41 == 0:
			refs = nil
		}
		out = append(out, msg(mail.UID(i+1), id, subject, i, refs...))
	}
	// Deterministic shuffle so arrival order differs from thread order.
	for i := range out {
		j := (i * 7919) % len(out)
		out[i], out[j] = out[j], out[i]
	}
	return out
}

func BenchmarkBuild5000(b *testing.B) {
	in := benchSummaries(5000)
	b.ReportAllocs()
	for b.Loop() {
		Build(in)
	}
}

// Guards the spec's 50ms budget without relying on someone reading -bench output.
func TestBuild_5000UnderBudget(t *testing.T) {
	if testing.Short() || raceEnabled {
		t.Skip("timing check skipped in -short and -race runs")
	}
	in := benchSummaries(5000)
	res := testing.Benchmark(func(b *testing.B) {
		for b.Loop() {
			Build(in)
		}
	})
	if per := time.Duration(res.NsPerOp()); per > 50*time.Millisecond {
		t.Errorf("Build(5000) took %v per op, budget is 50ms", per)
	}
}
