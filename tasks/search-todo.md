# Server-Side Search (`UID SEARCH`): Tasks

Plan: [search-plan.md](search-plan.md).

---

## Task 9: Search domain types and filter query splitter

**Description:** Add provider-agnostic `mail.SearchQuery` and `Mailbox.Search` to `internal/mail`, and build a pure stdlib `internal/search` parser that splits Gmail-like filter tokens (`from:`, `to:`, `cc:`, `subject:`, `body:`, `is:unread`/`read`/`starred`, `since:`/`after:`, `before:`, `-` negation, `"phrases"`, bare words) into structured criteria.

**Acceptance criteria:**
- [ ] `internal/mail/mail.go` remains stdlib-only and defines `SearchQuery` plus `Mailbox.Search(ctx, folder, query, before, limit) ([]Summary, int, error)`.
- [ ] `search.Parse` handles field prefixes (`from:`, `to:`, `cc:`, `subject:`, `body:`), flag filters (`is:unread`, `is:read`, `is:starred`/`is:flagged`), `YYYY-MM-DD` date bounds (`since:`/`after:`, `before:`), `-` negation on tokens/phrases/prefixes, quoted phrases, and bare `TEXT` terms.
- [ ] Malformed dates or unclosed quotes degrade gracefully into literal search text rather than erroring.

**Verification:**
- [ ] Tests pass: `go test -race ./internal/search ./internal/mail/...`
- [ ] Build succeeds: `go build -o bin/dk-email ./cmd/dk-email`
- [ ] Manual check: None (pure table-driven parser tests).

**Dependencies:** OAuth Phase complete (avoids `internal/mail` / docs conflicts)

**Files likely touched:**
- `internal/mail/mail.go`
- `internal/search/parse.go`
- `internal/search/parse_test.go`

**Estimated scope:** Medium: 3-5 files

---

## Task 10: IMAP `UID SEARCH` adapter and `mailtest.Fake.Search`

**Description:** Implement `Client.Search` in `internal/mail/imap` using read-only `EXAMINE`, `*goimap.SearchCriteria` compilation, descending UID pagination (`before` + `limit`), and `fetchSummaries`, plus matching in-memory filtering on `mailtest.Fake`.

**Acceptance criteria:**
- [ ] `imap.Client.Search` opens the folder read-only (`EXAMINE`), excludes `\Deleted`, compiles `mail.SearchQuery` to `*goimap.SearchCriteria`, restricts UIDs `< before` when paging, returns `total` matches (when `before == 0`, or total overall matching UIDs) and newest `limit` `[]mail.Summary` sorted newest-first, and skips `UID FETCH` when 0 UIDs match.
- [ ] `mailtest.Fake.Search` evaluates the same `SearchQuery` fields, flags, dates, negations, and `before`/`limit` paging in memory for deterministic UI tests.
- [ ] `imapmemserver` tests cover text, header, flag, date, negation, empty results, and multi-page UID slicing.

**Verification:**
- [ ] Tests pass: `go test -race ./internal/mail/...`
- [ ] Build succeeds: `go build -o bin/dk-email ./cmd/dk-email`
- [ ] Manual check: None (verified via `imapmemserver` and `mailtest`).

**Dependencies:** Task 9

**Files likely touched:**
- `internal/mail/imap/search.go`
- `internal/mail/imap/search_test.go`
- `internal/mail/mailtest/fake.go`
- `internal/mail/mailtest/fake_test.go`

**Estimated scope:** Medium: 3-5 files

---

## Checkpoint: After Tasks 9-10 (Search Backend)
- [ ] All tests pass: `go test -race ./internal/search ./internal/mail/...`
- [ ] `go vet ./...` and `gofmt -l .` clean

---

## Task 11: Inline `/` list search bar, threaded results, paging, and refresh guard

**Description:** Bind `/` when `PaneList` is focused to open an inline search prompt in the list header, run `Mailbox.Search` asynchronously with threaded results and infinite-scroll paging, prevent background folder refreshes from overwriting active search results, and restore the folder view on `esc`.

**Acceptance criteria:**
- [ ] Pressing `/` in `PaneList` focuses an inline `/ ` input (`enter` runs `Mailbox.Search` guarded by a search generation counter; `esc` cancels the prompt or clears an active search back to the folder's summaries; `/` in `PaneSidebar` still filters folders; `?` help popup lists `/` search).
- [ ] Search results render threaded via `thread.Build` with a `search: <query>` header indicator, support all normal open/reply/flag/move/archive/delete actions, and paginate older matches via `LoadMoreMsg` (`before` UID).
- [ ] Periodic auto-refresh (`internal/ui/refresh.go`) does not overwrite active search results with unfiltered folder summaries, while manual `r` re-runs the active search.

**Verification:**
- [ ] Tests pass: `go test -race ./internal/ui/...`
- [ ] Build succeeds: `go build -o bin/dk-email ./cmd/dk-email`
- [ ] Manual check: UI tests verify `/` prompt, threaded results, paging, refresh guard, and `esc` restore.

**Dependencies:** Task 10

**Files likely touched:**
- `internal/ui/keys.go`
- `internal/ui/model.go`
- `internal/ui/accounts.go`
- `internal/ui/refresh.go`
- `internal/ui/search_test.go`

**Estimated scope:** Medium: 3-5 files

---

## Task 12: Search documentation and read-only live IMAP test

**Description:** Add a read-only `Search` test to `internal/mail/imap/live_test.go` (behind `-tags=live`) and document `/` server-side search keys and filter syntax in `README.md` and `SPEC.md`.

**Acceptance criteria:**
- [ ] `internal/mail/imap/live_test.go` exercises read-only `Client.Search` against `DK_EMAIL_LIVE_ACCOUNT` without mutating flags or folders.
- [ ] `README.md` and `SPEC.md` document `/` in the thread list, `esc` to clear, and supported search operators (`from:`, `to:`, `cc:`, `subject:`, `body:`, `is:unread`/`read`/`starred`, `since:`/`after:`, `before:`, `-`, quotes, free text).

**Verification:**
- [ ] Tests pass: `go test -race ./...`
- [ ] Build & release check succeed: `go build -o bin/dk-email ./cmd/dk-email && goreleaser check`
- [ ] Manual check: Verify `go test -tags=live -c ./internal/mail/imap` compiles cleanly even without `DK_EMAIL_LIVE_ACCOUNT` set.

**Dependencies:** Task 11

**Files likely touched:**
- `internal/mail/imap/live_test.go`
- `README.md`
- `SPEC.md`

**Estimated scope:** Medium: 3-5 files

---

## Checkpoint: Complete (After Tasks 11-12)
- [ ] Full test suite passes: `go test -race ./...`
- [ ] Lint, format, build, and release config pass: `go vet ./... && gofmt -l . && go build -o bin/dk-email ./cmd/dk-email && goreleaser check`
- [ ] `@architect` diff review passed
