# Implementation Plan: Recover Expired IMAP Sessions

Tasks, acceptance criteria, and verification steps: [reconnect-todo.md](reconnect-todo.md).

## Overview

Fix the "authentication lost overnight" bug from [future-plans.md](future-plans.md#bugs): with the app left open overnight an account stops working until the app is restarted. `imap.Client.do` (`internal/mail/imap/client.go`) already reconnects when a connection breaks or a call times out, but it treats any server status response (`*goimap.Error`, i.e. tagged `NO`/`BAD`) as final and keeps the connection. A server that expires the session while leaving the TCP connection open (Proton Bridge after sleep, Gmail, Microsoft 365) answers every later command with `NO`/`BAD`, so the dead session is reused forever. This plan makes `do` detect that case and reconnect, and adds debug logging so the next field failure leaves a trail.

## Investigation Summary

- Every UI `tea.Cmd` has a 15–90s context timeout; `runLocked` closes the TCP connection when it ends and drops it, so half-open connections (laptop sleep, NAT timeout) recover on the next call. Covered by `TestFolders_ReconnectsAfterCallTimesOut`.
- go-imap v2.0.0-beta.8 sets no read deadline while idle, and closes `Closed()` on EOF; a server `BYE` + close is picked up by `connectLocked`. Covered by `TestFolders_ReconnectsAfterServerDropsConnection`.
- Password accounts reuse the password resolved at connect (`mail.StaticPassword`), so reconnects need no `password_cmd`; OAuth accounts fetch a fresh token per dial.
- The refresh tick reschedules itself regardless of errors, so the UI keeps retrying; the gap is only in `do`.
- The user's accounts are all password accounts: Proton Bridge (`127.0.0.1`), Gmail (app password), Fastmail. The exact overnight error text is unknown; the fix covers the identified hole whatever the trigger.

## Architecture Decisions

- **Probe before trusting a server error on a reused connection.** When `fn` returns a `*goimap.Error` on a connection that predates the call (`reused`), `do` runs `LIST "" ""` on the same connection. `LIST` is only valid in the authenticated state (unlike `NOOP`/`CAPABILITY`, which a logged-out session still accepts) and is the cheapest such command (it returns only the hierarchy delimiter). If the probe fails in any way, the session is dead: drop, reconnect, and run `fn` once more through the existing loop. If the probe succeeds, the original error is genuine and is returned unchanged.
- **No probe for `finalError`, fresh connections, or ended contexts.** `finalError` (e.g. `mail.ErrFolderChanged`) is our own verdict on a healthy connection. A server error on a fresh connection is returned as today (the retry loop already stops at `!reused`). If `ctx` ended, `do` returns the context error before any probe. The probe runs through `runLocked`, so it honours `ctx` cancellation like any call.
- **At most one extra round trip per genuine server error**, only on reused connections. Genuine `NO`s (missing folder, over-quota APPEND) are rare in normal use, so the cost is negligible next to an extra reconnect.
- **Debug logging inside `internal/mail/imap`.** `Client` gains a `log *slog.Logger` (nil discards, like `send.New`). `DialWithCredentials` gains a `log` parameter; `New`/`NewWithCredentials`/`Dial` are unchanged (tests set the field directly). Logged at debug level: a dropped connection (op, error), an expired session detected by the probe (op, original server error), and each new connection (account, whether it replaces a dropped one). Server text never carries credentials after login, and login errors are already redacted by `loginError`; nothing else from the session is logged.
- **Out of scope:** IMAP `IDLE` (now planned in [notifications-plan.md](notifications-plan.md)) and caching `password_cmd` (in [future-plans.md](future-plans.md)); the fix keeps both possible (the probe and logging live in `do`, which IDLE would also go through).

## Task List

### Phase 1: Fix
- [x] Task 13: Probe and reconnect when a reused session answers with a server error

### Phase 2: Diagnostics
- [x] Task 14: Debug-log IMAP drops, expired sessions and reconnects

### Checkpoint: Fix + Diagnostics
- [x] `go test -race ./...`, `go vet ./...`, `gofmt -l .` clean

### Phase 3: Docs and field check
- [ ] Task 15: Docs, future plans, and an overnight run with the debug log

### Checkpoint: Complete
- [ ] All acceptance criteria met; diff reviewed

## Risks and Mitigations

| Risk | Impact | Mitigation |
|------|--------|------------|
| The overnight failure has a different cause (e.g. server refuses re-login, or Bridge is unreachable after wake) | High | Task 14's logging records the error and every reconnect; Task 15 runs overnight with `DK_EMAIL_DEBUG=1` before the bug is closed. |
| A server rejects `LIST "" ""` even on a healthy session | Low | RFC 3501 §6.3.8 requires it; on such a server the only cost is one extra reconnect per genuine error, never a wrong result. |
| Repeating `fn` after a reconnect | Low | `do` already requires `fn` to be safe to repeat; the retry is the same single retry used for broken connections. |
| Retry loops on a server that fails every fresh login | Low | A fresh connection is never retried (`!reused`), so at most one reconnect per call. |

## Open Questions

- Which account failed and what the status line said. If the overnight run shows a timeout or login failure rather than an expired session, revisit before closing the bug.
