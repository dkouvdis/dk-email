// Package thread groups a folder's message summaries into conversations.
// It is pure: no I/O, no UI, and it never modifies its input.
//
// Messages are linked through Message-ID, In-Reply-To and References using
// union-find, which tolerates out-of-order arrival, missing parents, cycles
// and duplicate ids. A message with no references but a Re:/Fwd: subject is
// then attached by normalized subject. Messages that aren't replies are never
// merged by subject, so two unrelated "Hello" mails stay apart.
package thread

import (
	"cmp"
	"slices"
	"strings"
	"time"

	"github.com/dkouvdis/dk-email/internal/mail"
)

// Thread is one conversation. Messages are ordered oldest first.
type Thread struct {
	Messages []mail.Summary
}

// Latest returns the newest message, which a collapsed thread row shows.
func (t Thread) Latest() mail.Summary {
	if len(t.Messages) == 0 {
		return mail.Summary{}
	}
	return t.Messages[len(t.Messages)-1]
}

func (t Thread) Len() int { return len(t.Messages) }

// Unread counts messages without the Seen flag.
func (t Thread) Unread() int {
	n := 0
	for _, m := range t.Messages {
		if !m.Seen {
			n++
		}
	}
	return n
}

// Flagged reports whether any message in the thread is starred.
func (t Thread) Flagged() bool {
	return slices.ContainsFunc(t.Messages, func(m mail.Summary) bool { return m.Flagged })
}

// UIDs lists the thread's message UIDs, oldest first.
func (t Thread) UIDs() []mail.UID {
	out := make([]mail.UID, len(t.Messages))
	for i, m := range t.Messages {
		out[i] = m.UID
	}
	return out
}

// Build groups summaries into threads, newest activity first.
func Build(summaries []mail.Summary) []Thread {
	n := len(summaries)
	// Nodes 0..n-1 are messages; ids seen only in references get extra nodes.
	uf := newUnionFind(n)
	idNode := make(map[string]int, n)
	nodeFor := func(id string) int {
		if node, ok := idNode[id]; ok {
			return node
		}
		node := uf.add()
		idNode[id] = node
		return node
	}

	hasRefs := make([]bool, n)
	for i, s := range summaries {
		if id := firstID(s.MessageID); id != "" {
			uf.union(i, nodeFor(id))
		}
		for _, raw := range append([]string{s.InReplyTo}, s.References...) {
			for _, id := range parseIDs(raw) {
				uf.union(i, nodeFor(id))
				hasRefs[i] = true
			}
		}
	}

	attachOrphanReplies(summaries, hasRefs, uf)

	groups := make(map[int][]mail.Summary)
	var roots []int
	for i, s := range summaries {
		r := uf.find(i)
		if _, ok := groups[r]; !ok {
			roots = append(roots, r)
		}
		groups[r] = append(groups[r], s)
	}

	threads := make([]Thread, 0, len(roots))
	for _, r := range roots {
		msgs := groups[r]
		slices.SortFunc(msgs, compareMessages)
		threads = append(threads, Thread{Messages: msgs})
	}
	slices.SortFunc(threads, func(a, b Thread) int {
		return compareMessages(b.Latest(), a.Latest())
	})
	return threads
}

// attachOrphanReplies links each reply that has no references to the thread
// of an original with the same normalized subject: the latest original sent
// at or before the reply, else the earliest one. Replies with no original
// share a thread with each other.
func attachOrphanReplies(summaries []mail.Summary, hasRefs []bool, uf *unionFind) {
	type candidate struct {
		node int
		date time.Time
	}
	originals := make(map[string][]candidate)
	var orphans []int
	subjects := make([]string, len(summaries))

	for i, s := range summaries {
		norm, isReply := normalizeSubject(s.Subject)
		if norm == "" {
			continue
		}
		subjects[i] = norm
		if isReply && !hasRefs[i] {
			orphans = append(orphans, i)
			continue
		}
		originals[norm] = append(originals[norm], candidate{i, s.Date})
	}
	if len(orphans) == 0 {
		return
	}
	for _, cands := range originals {
		slices.SortStableFunc(cands, func(a, b candidate) int { return a.date.Compare(b.date) })
	}

	orphanGroup := make(map[string]int)
	for _, i := range orphans {
		norm := subjects[i]
		cands := originals[norm]
		if len(cands) == 0 {
			if first, ok := orphanGroup[norm]; ok {
				uf.union(i, first)
			} else {
				orphanGroup[norm] = i
			}
			continue
		}
		// Index of the first original dated after the reply.
		after, _ := slices.BinarySearchFunc(cands, summaries[i].Date, func(c candidate, d time.Time) int {
			if c.date.After(d) {
				return 1
			}
			return -1
		})
		pick := cands[0]
		if after > 0 {
			pick = cands[after-1]
		}
		uf.union(i, pick.node)
	}
}

// compareMessages orders by date, then UID, so results are deterministic.
func compareMessages(a, b mail.Summary) int {
	if c := a.Date.Compare(b.Date); c != 0 {
		return c
	}
	return cmp.Compare(a.UID, b.UID)
}

// parseIDs extracts message ids from a header value. Bracketed ids are
// preferred so trailing comments are ignored; otherwise whitespace splits.
func parseIDs(raw string) []string {
	if !strings.Contains(raw, "<") {
		ids := strings.Fields(raw)
		for i := range ids {
			ids[i] = strings.Trim(ids[i], "<>")
		}
		return slices.DeleteFunc(ids, func(s string) bool { return s == "" })
	}
	var ids []string
	for {
		start := strings.IndexByte(raw, '<')
		if start < 0 {
			return ids
		}
		end := strings.IndexByte(raw[start:], '>')
		if end < 0 {
			return ids
		}
		if id := strings.TrimSpace(raw[start+1 : start+end]); id != "" {
			ids = append(ids, id)
		}
		raw = raw[start+end+1:]
	}
}

func firstID(raw string) string {
	if ids := parseIDs(raw); len(ids) > 0 {
		return ids[0]
	}
	return ""
}

type unionFind struct{ parent []int }

func newUnionFind(n int) *unionFind {
	uf := &unionFind{parent: make([]int, n)}
	for i := range uf.parent {
		uf.parent[i] = i
	}
	return uf
}

func (u *unionFind) add() int {
	u.parent = append(u.parent, len(u.parent))
	return len(u.parent) - 1
}

func (u *unionFind) find(x int) int {
	for u.parent[x] != x {
		u.parent[x] = u.parent[u.parent[x]]
		x = u.parent[x]
	}
	return x
}

func (u *unionFind) union(a, b int) {
	ra, rb := u.find(a), u.find(b)
	if ra == rb {
		return
	}
	// Keep the smaller index as root so message nodes win over phantom ids.
	if rb < ra {
		ra, rb = rb, ra
	}
	u.parent[rb] = ra
}
