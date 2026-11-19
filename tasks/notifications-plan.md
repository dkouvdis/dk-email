# Implementation Plan: New Mail Notifications

Spec: [notifications-spec.md](notifications-spec.md). Tasks, acceptance criteria and verification steps: [notifications-todo.md](notifications-todo.md).

## Overview

Tell the user when new mail reaches an INBOX: a bell, an OSC 9 desktop notification and an unread count in the window title, all count only and skipped while the window has focus. Step 1 hooks into the existing `refresh_interval` poll. Step 2 adds an IMAP `IDLE` watcher per account, so the same refresh runs within seconds of delivery.

## Investigation Summary

- `internal/ui/refresh.go` already fetches every account's folders (STATUS `MESSAGES`/`UNSEEN` via `countOptions` in `internal/mail/imap/folders.go`) on each tick and on `ctrl+r`. Adding `UIDNEXT`/`UIDVALIDITY` to `countOptions` costs no extra round trip.
- `handleRefresh` drops *stale* results (a write was in flight). The new-mail check skips them too: a move into INBOX is recorded when it starts, so only a non-stale round is sure to see every recorded move on the server. A skipped round keeps the baseline, so the next one counts the whole gap.
- `refreshAccounts` returns nothing while a refresh is running; `refreshAfterDraft` shows the fix (`refresh.rerun`). An IDLE wake-up must use the same path, or a burst of mail would be lost.
- Bubble Tea v2.1.1 has everything for step 1: `tea.View.ReportFocus` with `tea.FocusMsg`/`tea.BlurMsg`, `tea.View.WindowTitle`, and `tea.Raw` for the bell and OSC 9.
- go-imap v2.0.0-beta.8 has `Client.Idle()` and `Options.UnilateralDataHandler.Mailbox` (reports `NumMessages`), and `imapmemserver` supports IDLE, so the watcher can be tested in-process with `server_test.go`.
- `imap.Client` is one serialized connection; IDLE would block it. The watcher gets its own connection and never shares `Client.mu`.

## Architecture Decisions

- **New mail = rise in INBOX `UIDNEXT`.** It counts deliveries whether they're read or not (the spec says any new INBOX message), and needs no extra command. dk-email subtracts the messages it moved into INBOX since the last result for that account, so its own moves and undo don't alert. A changed `UIDVALIDITY`, a lower `UIDNEXT` or a missing value reset the baseline silently. The first result after connecting is only a baseline.
- **Pure logic in a new `internal/notify` package** (standard library only): the per-account tracker, the alert text, and the escape sequences (`\a`; OSC 9 as `ESC ] 9 ; text BEL`, wrapped as `ESC P tmux; … ESC \` with inner `ESC`s doubled when `$TMUX` is set). The text always starts with `dk-email:`, so it never begins with `4;`, which Ghostty and ConEmu read as OSC 9;4 progress. The UI only feeds it folder lists and writes what it returns via `tea.Raw`.
- **One alert per refresh round.** Counts are collected in `refreshState` and sent once in `finishRefresh`, so three accounts don't ring three times.
- **Focus.** `View.ReportFocus` is on whenever bell or desktop is on. `focused` starts false (a terminal without focus reports must still alert), becomes true on `FocusMsg` or any key or click, and false on `BlurMsg`.
- **Title** is the sum of INBOX `Unread` from the sidebar (so it follows optimistic marks), `dk-email (N)` or `dk-email`, set on every view while `title` is on.
- **Watcher is a separate interface.** `mail.Watcher` has `Wait(ctx, folder) error`: it returns nil once the folder gained messages since the last call, or an error. Each `Wait` is one blocking `tea.Cmd`, re-armed by `Update` (the existing I/O-in-commands pattern, no channels). `imap.Watcher` keeps a lazily dialed connection with INBOX selected, remembers the last `EXISTS`, returns at once if it grew between calls, and re-issues `IDLE` every 25 minutes (Gmail ends IDLE at 29). Without the `IDLE` capability it returns `mail.ErrUnsupported`, and the UI stops watching that account for the session.
- **Errors back off, polling continues.** A failed `Wait` is logged at debug level and retried after 30s, doubling to 5 minutes, and reset by a success. Contexts are cancelled when an account is removed or replaced and on quit.
- **No new settings for push**: `IDLE` is used whenever the server supports it.

## Dependency Graph

```
T1 config + title ──┬─> T3 alerts ──> T4 focus ──> T5 docs (step 1)
T2 detect (imap+notify) ┘                              │
                                                       v
                         T6 imap.Watcher ──> T7 UI wiring ──> T8 docs + overnight
```

## Task List

### Phase 1: Notify on poll
- [x] Task 1: `[notify]` config and unread count in the window title
- [x] Task 2: Detect new INBOX mail from refresh results
- [x] Task 3: Bell and desktop alerts after a refresh round
- [x] Task 4: No alerts while the window has focus

### Checkpoint: Step 1 works
- [ ] `go test -race ./...`, `go vet ./...`, `gofmt -l .` clean
- [ ] Manual check in Ghostty and iTerm2: send yourself mail, switch apps, see a banner and hear or see the bell

- [x] Task 5: User docs for step 1

### Phase 2: IDLE push
- [ ] Task 6: `imap.Watcher` waits for new INBOX mail over its own connection
- [ ] Task 7: The UI refreshes an account when its watcher wakes

### Checkpoint: Push works
- [ ] `go test -race ./...`, `go vet ./...`, `gofmt -l .` clean
- [ ] Mail sent to yourself alerts within ~10 s

- [ ] Task 8: Docs for push and an overnight run

### Checkpoint: Complete
- [ ] All acceptance criteria met; diff reviewed

## Risks and Mitigations

| Risk | Impact | Mitigation |
|------|--------|------------|
| A second connection per account hits a server limit (Gmail allows 15) or Bridge rejects it | Medium | One extra per account only; on failure the watcher backs off and polling is unaffected. The overnight run in Task 8 checks all three providers. |
| The watcher connection dies overnight like the main one did | Medium | The same backoff and fresh dial each time; debug-logged; polling still runs. |
| OSC 9 shows nothing in some terminals (Terminal.app) or under tmux without passthrough | Low | The bell and title still work; docs list per-terminal setup and the tmux options. |
| False alerts from moves into INBOX by another client | Low | Accepted: that's new mail as far as this INBOX is concerned. dk-email's own moves are subtracted. |
| Bubble Tea's title reset on exit leaves an empty title | Low | Documented; revisit if it annoys. |

## Open Questions

- None blocking. Native notifications for Terminal.app and a `notify_cmd` hook are listed under Not Doing in the spec.
