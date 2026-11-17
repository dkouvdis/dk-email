# Implementation Plan: Server-Side Search (`UID SEARCH`)

Tasks, acceptance criteria, and verification steps: [search-todo.md](search-todo.md).

## Overview

Add server-side IMAP folder search (`/` when the thread list is focused) so users can search across an entire mailbox beyond the 200 loaded summaries without leaving `dk-email`. Queries use familiar Gmail-style filter tokens (`from:`, `to:`, `cc:`, `subject:`, `body:`, `is:unread`/`read`/`starred`, `since:`/`after:`, `before:`, `-` negation, `"phrases"`, and bare `TEXT` terms) compiled to standard `go-imap/v2` `UID SEARCH` criteria so search works identically across Microsoft 365, Gmail, Fastmail, Proton Bridge, and Dovecot with no `go-imap/v2` fork.

## Architecture Decisions

- **Provider-agnostic `Mailbox.Search` seam (`internal/mail`):** `internal/mail` stays standard-library-only, adding `mail.SearchQuery` and `Mailbox.Search(ctx, folder, query, before, limit) (summaries []Summary, total int, err error)` mirroring `Summaries` pagination (`before` UID ceiling + newest-first `limit`).
- **Pure filter splitter (`internal/search`):** ~60–100 lines of stdlib Go split filter operators, flags, `YYYY-MM-DD` date bounds, `-` negations, quoted phrases, and bare words. Malformed dates or unclosed quotes degrade gracefully into literal `TEXT` terms instead of failing user input.
- **Read-only `EXAMINE` + `fetchSummaries` reuse (`internal/mail/imap`):** `Client.Search` opens the folder read-only (`EXAMINE`), excludes `\Deleted`, runs `UIDSearch`, applies `before` UID paging, slices the newest `limit` UIDs, and reuses `fetchSummaries` so search results thread identically to normal folder views.
- **Threaded, non-destructive `/` list search UX (`internal/ui`):** `/` in `PaneList` opens an inline `/ ` prompt in the list header (`/ ` in `PaneSidebar` continues filtering folders). Results thread via `thread.Build`, preserve all triage/open/reply/paging operations, guard periodic 120s `refresh` (`internal/ui/refresh.go`) from overwriting active search results (`r`/`ctrl+r` re-runs the active search), and clear back to the folder's summaries on `esc`.

## Task List

### Phase 1: Search Query & IMAP Adapter
- [ ] Task 9: Search domain types and filter query splitter
- [ ] Task 10: IMAP `UID SEARCH` adapter and `mailtest.Fake.Search`

### Checkpoint: Search Backend
- [ ] `go test -race ./internal/search ./internal/mail/...` passes against `imapmemserver` and `mailtest.Fake`

### Phase 2: Search TUI & Documentation
- [ ] Task 11: Inline `/` list search bar, threaded results, paging, and refresh guard
- [ ] Task 12: Search documentation and read-only live IMAP test

### Checkpoint: Complete
- [ ] `go test -race ./...`, `go vet ./...`, `gofmt -l .`, `go build -o bin/dk-email ./cmd/dk-email`, and `goreleaser check` pass
- [ ] All acceptance criteria met; reviewed by `@architect`

## Dependency Graph

```
Task 9 (mail.SearchQuery + internal/search) ──► Task 10 (IMAP Search + Fake.Search) ──► Task 11 (UI `/` search bar & paging) ──► Task 12 (docs & live test)
```

## Risks and Mitigations

| Risk | Impact | Mitigation |
|------|--------|------------|
| Periodic 120s folder auto-refresh overwrites active search results | Med | `refresh.go` skips unfiltered `list.Merge` while a search is active; manual `r`/`ctrl+r` re-runs the active search (Task 11). |
| Stale slow `UID SEARCH` response arrives after user cleared search or changed folders | Med | Guard `searchMsg` with a search generation counter + account/folder match, matching `fetchGen` (Task 11). |
| Zero matching UIDs triggers invalid empty `UID FETCH` | Med | Short-circuit immediately when matched UID slice is empty and return `nil, 0, nil` (Task 10). |
| Non-ASCII search terms on servers without `UTF8=ACCEPT` | Low | `go-imap/v2` `UIDSearch` automatically handles `CHARSET UTF-8` negotiation when non-ASCII strings appear in `SearchCriteria`. |

## Open Questions

None.
