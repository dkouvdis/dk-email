# Implementation Plan: Server-Side Search (`UID SEARCH`)

Tasks, acceptance criteria, and verification steps: [search-todo.md](search-todo.md).

## Overview

Add server-side IMAP folder search (`/` when the thread list is focused) so users can search across an entire mailbox beyond the 200 loaded summaries without leaving `dk-email`. Queries use familiar Gmail-style filter tokens (`from:`, `to:`, `cc:`, `subject:`, `body:`, `is:unread`/`read`/`starred`, `since:`/`after:`, `before:`, `-` negation, `"phrases"`, and bare `TEXT` terms) compiled to standard `go-imap/v2` `UID SEARCH` criteria so search works identically across Microsoft 365, Gmail, Fastmail, Proton Bridge, and Dovecot with no `go-imap/v2` fork.

## Architecture Decisions

- **Provider-agnostic `Mailbox.Search` seam (`internal/mail`):** `internal/mail` stays standard-library-only. Task 9 adds the query types only; Task 10 adds `Mailbox.Search(ctx, folder, query, before, limit) (summaries []Summary, total int, err error)` together with both implementations (`imap.Client`, `mailtest.Fake`), so every commit builds. `Search` mirrors `Summaries` pagination (`before` UID ceiling + newest-first `limit`). The query is one flat AND list:

  ```go
  type SearchKind int // SearchText, SearchFrom, SearchTo, SearchCc, SearchSubject,
                      // SearchBody, SearchUnread, SearchRead, SearchStarred,
                      // SearchSince, SearchBefore
  type SearchTerm struct {
  	Kind  SearchKind
  	Value string    // text kinds
  	Date  time.Time // SearchSince, SearchBefore
  	Not   bool
  }
  type SearchQuery struct {
  	Raw   string       // as typed: shown in the list header, kept for a later X-GM-RAW
  	Terms []SearchTerm // all must match
  }
  ```
- **Pure filter splitter (`internal/search`):** ~60–100 lines of stdlib Go split filter operators, flags, `YYYY-MM-DD` date bounds, `-` negations, quoted phrases, and bare words. Malformed dates or unclosed quotes degrade gracefully into literal `TEXT` terms instead of failing user input.
- **Read-only `EXAMINE` + shared UID paging (`internal/mail/imap`):** `newestUIDs` (`summaries.go`) already runs `UID SEARCH NOT \Deleted` with the `before` ceiling and newest-`limit` slice. It gains a `*goimap.SearchCriteria` parameter and returns the total match count, so `Summaries` (empty criteria) and `Search` (compiled query) share one path, then both reuse `fetchSummaries` so search results thread identically to normal folder views.
- **Threaded, non-destructive `/` list search UX (`internal/ui`):** `/` in `PaneList` opens an inline `/ ` prompt in the list header (`/ ` in `PaneSidebar` continues filtering folders). Results thread via `thread.Build`, preserve all triage/open/reply/paging operations, guard periodic 120s `refresh` (`internal/ui/refresh.go`) from overwriting active search results (`ctrl+r` re-runs the active search; `r` is reply), and clear back to the folder's summaries on `esc`.

## Task List

### Phase 1: Search Query & IMAP Adapter
- [x] Task 9: Search domain types and filter query splitter
- [x] Task 10: IMAP `UID SEARCH` adapter and `mailtest.Fake.Search`

### Checkpoint: Search Backend
- [x] `go test -race ./internal/search ./internal/mail/...` passes against `imapmemserver` and `mailtest.Fake`

### Phase 2: Search TUI & Documentation
- [x] Task 11: Inline `/` list search bar, threaded results, paging, and refresh guard
- [ ] Task 12: Search guide (`docs/search.md`), README/SPEC, and read-only Gmail live test

### Checkpoint: Complete
- [ ] `go test -race ./...`, `go vet ./...`, `gofmt -l .`, `go build -o bin/dk-email ./cmd/dk-email`, and `goreleaser check` pass
- [ ] All acceptance criteria met; reviewed with `agent-skills:code-reviewer`

## Dependency Graph

```
Task 9 (mail.SearchQuery + internal/search) ──► Task 10 (IMAP Search + Fake.Search) ──► Task 11 (UI `/` search bar & paging) ──► Task 12 (docs/search.md & Gmail live test)
```

## Risks and Mitigations

| Risk | Impact | Mitigation |
|------|--------|------------|
| Periodic 120s folder auto-refresh overwrites active search results | Med | `refresh.go` skips unfiltered `list.Merge` while a search is active; manual `ctrl+r` re-runs the active search (Task 11). |
| Stale slow `UID SEARCH` response arrives after user cleared search or changed folders | Med | Guard `searchMsg` with a search generation counter + account/folder match, matching `fetchGen` (Task 11). |
| Zero matching UIDs triggers invalid empty `UID FETCH` | Med | Short-circuit immediately when matched UID slice is empty and return `nil, 0, nil` (Task 10). |
| IMAP `SINCE`/`BEFORE` compare dates only, in the server's time zone | Low | Pinned by a parser/adapter test; documented in `docs/search.md` (`before:2026-11-17` excludes that day). |
| Microsoft 365's IMAP search is weaker (`BODY`/`TEXT` substring and non-ASCII matching) | Low | Not promised: `docs/search.md` lists what is verified (Gmail, Dovecot via `imapmemserver`). |
| Non-ASCII search terms on servers without `UTF8=ACCEPT` | Low | `go-imap/v2` `UIDSearch` automatically handles `CHARSET UTF-8` negotiation when non-ASCII strings appear in `SearchCriteria`. |

## Assumptions to Validate

- [ ] Gmail's `UID SEARCH` with `TEXT`, `FROM` and `-` negation returns the same messages as Gmail's web search for a few sample queries (Task 12 live test, read-only).
- [ ] Date-only `SINCE`/`BEFORE` semantics are acceptable once documented.

## Not Doing (and Why)

- **Account-wide search:** needs one `SEARCH` per folder and a mixed-folder results view. On Gmail, open All Mail and search there.
- **Gmail `X-GM-RAW` passthrough:** `SearchQuery.Raw` keeps the door open; the operator parser works everywhere first.
- **OR and grouping:** the flat AND list covers almost every real query.
- **Search-as-you-type / client-side pre-filter:** a second code path; search runs on `enter`.
- **Saved searches as sidebar folders:** later, if at all.
- **Microsoft 365 live verification:** not part of this phase.

## Open Questions

None.
