# Recover Expired IMAP Sessions: Tasks

Plan: [reconnect-plan.md](reconnect-plan.md).

---

## Task 13: Probe and reconnect when a reused session answers with a server error

**Description:** In `imap.Client.do`, when `fn` fails with a `*goimap.Error` on a reused connection (and `ctx` is still live), run `LIST "" ""` on the same connection through `runLocked`. If the probe fails, drop the connection and let the existing loop reconnect and run `fn` once more; if it succeeds, return the original error unchanged. `finalError` and errors on a fresh connection keep today's behaviour. Add a test hook to the in-process server that expires every open session, so later commands on it get a tagged `BAD`/`NO` while the TCP connection stays open.

**Acceptance criteria:**
- [x] After the test server expires the client's session (connection left open), the next `Folders` call succeeds with `wantSeededFolders` and the server accepted exactly 2 connections.
- [x] A genuine server error on a healthy reused session (e.g. `Message` on a folder that doesn't exist) returns the same error as before (`errors.Is(err, mail.ErrFolderNotFound)`) without reconnecting (1 accepted connection).
- [x] `finalError` (e.g. `mail.ErrFolderChanged`) and a context that ended still return without a probe; existing reconnect/timeout/cancel tests pass unchanged.

**Verification:**
- [x] Tests pass: `go test -race ./internal/mail/imap`
- [x] Build succeeds: `go build -o bin/dk-email ./cmd/dk-email`
- [x] Manual check: none (covered by the in-process server).

**Dependencies:** None

**Files likely touched:**
- `internal/mail/imap/client.go`
- `internal/mail/imap/server_test.go`
- `internal/mail/imap/client_test.go`

**Estimated scope:** Small: 1-2 files (+ test hook)

---

## Task 14: Debug-log IMAP drops, expired sessions and reconnects

**Description:** Give `imap.Client` a `log *slog.Logger` (nil discards, as in `send.New`). `DialWithCredentials` takes it as a new last parameter and `cmd/dk-email` passes its debug logger; `New`, `NewWithCredentials` and `Dial` are unchanged. In `do`/`connectLocked`, log at debug level: a dropped connection (`account`, `op`, `err`), an expired session found by the probe (`account`, `op`, `err` = original server error), and a new connection (`account`, `replaces` = whether one was dropped before). Never log a password, token or server text from before login.

**Acceptance criteria:**
- [x] With `DK_EMAIL_DEBUG=1`, a dropped or expired connection produces debug lines in `debug.log` naming the account, the operation and the error, followed by a reconnect line.
- [x] A test with a captured `slog` handler sees the expired-session and reconnect records for the Task 13 scenario, and no record contains `testPassword`; an XOAUTH2 variant asserts no record contains the access token.
- [x] The debug log docs note that `debug.log` may contain folder names and addresses from server errors, so check it before sharing.
- [x] A nil logger is safe (`New`/`NewWithCredentials` callers and existing tests unchanged).

**Verification:**
- [x] Tests pass: `go test -race ./internal/mail/imap ./cmd/dk-email`
- [x] Build succeeds: `go build -o bin/dk-email ./cmd/dk-email`
- [x] Manual check: run with `DK_EMAIL_DEBUG=1`, quit Proton Bridge or toggle Wi-Fi, press `ctrl+r`, and read the new lines in the cache dir's `debug.log`.

**Dependencies:** Task 13

**Files likely touched:**
- `internal/mail/imap/client.go`
- `internal/mail/imap/client_test.go`
- `cmd/dk-email/main.go`

**Estimated scope:** Small: 1-2 files (+ one call site)

---

## Checkpoint: After Tasks 13-14
- [x] Full test suite passes: `go test -race ./...`
- [x] Lint and format pass: `go vet ./... && gofmt -l .`
- [x] Diff reviewed in the main session before commit

---

## Task 15: Docs, future plans, and an overnight run with the debug log

**Description:** Update `AGENTS.md` ("reconnect on dropped connection" → also on an expired session, found with a `LIST` probe) and the package comment in `client.go` if Task 13 didn't. In `tasks/future-plans.md`, move the overnight bug to a note that it's fixed pending field confirmation, keeping the IDLE and `password_cmd` cache entries. Then leave the app open overnight with `DK_EMAIL_DEBUG=1` and read the log.

**Acceptance criteria:**
- [ ] `AGENTS.md` and the `client.go` package comment describe the expired-session recovery.
- [ ] `tasks/future-plans.md` no longer lists the bug as open, or records what the overnight log showed if it's still failing.
- [ ] After an overnight run, every account still refreshes without a restart, or the log's error is recorded in [reconnect-plan.md](reconnect-plan.md#open-questions).

**Verification:**
- [ ] Tests pass: `go test -race ./...`
- [ ] Build succeeds: `go build -o bin/dk-email ./cmd/dk-email`
- [ ] Manual check: overnight run with `DK_EMAIL_DEBUG=1` (user).

**Dependencies:** Tasks 13-14

**Files likely touched:**
- `AGENTS.md`
- `internal/mail/imap/client.go`
- `tasks/future-plans.md`

**Estimated scope:** Small: 1-3 files

---

## Checkpoint: Complete (After Task 15)
- [ ] Full test suite passes: `go test -race ./...`
- [ ] Lint, format, build, and release config pass: `go vet ./... && gofmt -l . && go build -o bin/dk-email ./cmd/dk-email && goreleaser check`
- [ ] Overnight run confirms the fix (or the new evidence is recorded)
