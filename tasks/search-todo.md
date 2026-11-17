# Server-Side Search (`UID SEARCH`): Tasks

Plan: [search-plan.md](search-plan.md).

---

## Task 9: Search domain types and filter query splitter

**Description:** Add provider-agnostic `mail.SearchQuery`, `mail.SearchTerm` and `mail.SearchKind` (shape in [search-plan.md](search-plan.md#architecture-decisions)) to `internal/mail`, without touching the `Mailbox` interface, and build a pure stdlib `internal/search` parser that splits Gmail-like filter tokens (`from:`, `to:`, `cc:`, `subject:`, `body:`, `is:unread`/`read`/`starred`, `since:`/`after:`, `before:`, `-` negation, `"phrases"`, bare words) into structured criteria.

**Acceptance criteria:**
- [x] `internal/mail/mail.go` remains stdlib-only and defines `SearchKind`, `SearchTerm` and `SearchQuery` (with `Raw` set to the input). `Mailbox` is unchanged until Task 10.
- [x] `search.Parse` handles field prefixes (`from:`, `to:`, `cc:`, `subject:`, `body:`), flag filters (`is:unread`, `is:read`, `is:starred`/`is:flagged`), `YYYY-MM-DD` date bounds (`since:`/`after:`, `before:`), `-` negation on tokens/phrases/prefixes, quoted phrases, and bare `TEXT` terms.
- [x] Malformed dates or unclosed quotes degrade gracefully into literal search text rather than erroring.

**Verification:**
- [x] Tests pass: `go test -race ./internal/search ./internal/mail/...`
- [x] Build succeeds: `go build -o bin/dk-email ./cmd/dk-email`
- [x] Manual check: None (pure table-driven parser tests).

**Dependencies:** OAuth Phase complete (avoids `internal/mail` / docs conflicts)

**Files likely touched:**
- `internal/mail/mail.go`
- `internal/search/parse.go`
- `internal/search/parse_test.go`

**Estimated scope:** Medium: 3-5 files

---

## Task 10: IMAP `UID SEARCH` adapter and `mailtest.Fake.Search`

**Description:** Add `Mailbox.Search(ctx, folder, query, before, limit) ([]Summary, int, error)` to `internal/mail` and implement `Client.Search` in `internal/mail/imap` using read-only `EXAMINE`, `*goimap.SearchCriteria` compilation, descending UID pagination (`before` + `limit`), and `fetchSummaries`, plus matching in-memory filtering on `mailtest.Fake`.

**Acceptance criteria:**
- [x] `newestUIDs` takes a `*goimap.SearchCriteria` (always plus `NOT \Deleted`) and returns the total match count; `Summaries` and `Search` both use it.
- [x] `imap.Client.Search` opens the folder read-only (`EXAMINE`), excludes `\Deleted`, compiles `mail.SearchQuery` to `*goimap.SearchCriteria`, restricts UIDs `< before` when paging, returns `total` matches (when `before == 0`, or total overall matching UIDs) and newest `limit` `[]mail.Summary` sorted newest-first, and skips `UID FETCH` when 0 UIDs match.
- [x] `mailtest.Fake.Search` evaluates the same `SearchQuery` fields, flags, dates, negations, and `before`/`limit` paging in memory for deterministic UI tests.
- [x] `imapmemserver` tests cover text, header, flag, date, negation, empty results, and multi-page UID slicing.

**Verification:**
- [x] Tests pass: `go test -race ./internal/mail/...`
- [x] Build succeeds: `go build -o bin/dk-email ./cmd/dk-email`
- [x] Manual check: None (verified via `imapmemserver` and `mailtest`).

**Dependencies:** Task 9

**Files likely touched:**
- `internal/mail/mail.go`
- `internal/mail/imap/summaries.go`
- `internal/mail/imap/search.go`
- `internal/mail/imap/search_test.go`
- `internal/mail/mailtest/fake.go`
- `internal/mail/mailtest/fake_test.go`

**Estimated scope:** Medium: 3-5 files

---

## Checkpoint: After Tasks 9-10 (Search Backend)
- [x] All tests pass: `go test -race ./internal/search ./internal/mail/...`
- [x] `go vet ./...` and `gofmt -l .` clean

---

## Task 11: Inline `/` list search bar, threaded results, paging, and refresh guard

**Description:** Bind `/` when `PaneList` is focused to open an inline search prompt in the list header, run `Mailbox.Search` asynchronously with threaded results and infinite-scroll paging, prevent background folder refreshes from overwriting active search results, and restore the folder view on `esc`.

**Acceptance criteria:**
- [ ] Pressing `/` in `PaneList` focuses an inline `/ ` input (`enter` runs `Mailbox.Search` guarded by a search generation counter; `esc` cancels the prompt or clears an active search back to the folder's summaries; `/` in `PaneSidebar` still filters folders; `?` help popup lists `/` search).
- [ ] Search results render threaded via `thread.Build` with a `search: <query>` header indicator, support all normal open/reply/flag/move/archive/delete actions, and paginate older matches via `LoadMoreMsg` (`before` UID).
- [ ] Periodic auto-refresh (`internal/ui/refresh.go`) does not overwrite active search results with unfiltered folder summaries, while manual `ctrl+r` re-runs the active search.

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

## Task 12: Search guide (`docs/search.md`), README/SPEC, and read-only Gmail live test

**Description:** Add a read-only `Search` test to `internal/mail/imap/live_test.go` (behind `-tags=live`), run it against a Gmail account, and write the user guide `docs/search.md`, linked from `README.md` and `SPEC.md`.

**Acceptance criteria:**
- [ ] `internal/mail/imap/live_test.go` exercises read-only `Client.Search` against `DK_EMAIL_LIVE_ACCOUNT` without mutating flags or folders.
- [ ] The live test passes against Gmail and its sample queries match Gmail's web search.
- [ ] `docs/search.md` (same style as `docs/accounts.md`) covers: `/` in the thread list, `enter` to run, `esc` to clear, `ctrl+r` to re-run, paging, and that search covers the open folder only (on Gmail, search All Mail for everything); every operator with an example (`from:`, `to:`, `cc:`, `subject:`, `body:`, `is:unread`/`read`/`starred`/`flagged`, `since:`/`after:`, `before:`, `-`, quotes, free text); that terms combine with AND and there is no OR; date-only `SINCE`/`BEFORE` behaviour; how malformed input falls back to text; and which providers are verified (Gmail, Dovecot), with Microsoft 365 caveats.
- [ ] `README.md` has a short Search section linking to `docs/search.md`; `SPEC.md` lists the `/` key and the `Mailbox.Search` seam; `AGENTS.md` mentions `internal/search` under supporting modules.

**Verification:**
- [ ] Tests pass: `go test -race ./...`
- [ ] Build & release check succeed: `go build -o bin/dk-email ./cmd/dk-email && goreleaser check`
- [ ] Manual check: Verify `go test -tags=live -c ./internal/mail/imap` compiles cleanly even without `DK_EMAIL_LIVE_ACCOUNT` set.

**Dependencies:** Task 11

**Files likely touched:**
- `internal/mail/imap/live_test.go`
- `docs/search.md`
- `README.md`
- `SPEC.md`
- `AGENTS.md`

**Estimated scope:** Medium: 3-5 files

---

## Checkpoint: Complete (After Tasks 11-12)
- [ ] Full test suite passes: `go test -race ./...`
- [ ] Lint, format, build, and release config pass: `go vet ./... && gofmt -l . && go build -o bin/dk-email ./cmd/dk-email && goreleaser check`
- [ ] `agent-skills:code-reviewer` diff review passed
