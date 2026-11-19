# New Mail Notifications: Tasks

Plan: [notifications-plan.md](notifications-plan.md) · Spec: [notifications-spec.md](notifications-spec.md)

---

## Task 1: `[notify]` config and unread count in the window title

**Description:** Add an optional `[notify]` section to `config.Config` with `bell`, `desktop` and `title` (bools, all `true` when absent). It stays strict: unknown keys under `[notify]` fail validation like everywhere else. In `ui.Model.View`, when `title` is on, set `v.WindowTitle` to `dk-email (N)`, where N is the sum of `Unread` over every connected account's INBOX (`mail.RoleInbox`) as the sidebar holds it, or `dk-email` when N is 0. When `title` is off, leave `WindowTitle` empty so the terminal's title is untouched.

**Acceptance criteria:**
- [x] A config without `[notify]` loads with all three on; `[notify] bell = false` turns only the bell off; `[notify] sound = true` fails with an unknown-key error.
- [x] With two accounts whose INBOXes have 2 and 1 unread, `View().WindowTitle` is `dk-email (3)`; after marking one read it's `dk-email (2)`; with none unread it's `dk-email`.
- [x] With `title = false`, `View().WindowTitle` is empty.

**Verification:**
- [x] Tests pass: `go test -race ./internal/config ./internal/ui`
- [x] Build succeeds: `go build -o bin/dk-email ./cmd/dk-email`
- [x] Manual check: the Ghostty tab shows `dk-email (N)` and follows marking read.

**Dependencies:** None

**Files likely touched:**
- `internal/config/config.go`, `internal/config/config_test.go`
- `internal/ui/model.go`, `internal/ui/model_test.go`

**Estimated scope:** Small: 2-4 files

---

## Task 2: Detect new INBOX mail from refresh results

**Description:** Add `UIDNext` and `UIDValidity` to `mail.Folder` and to `countOptions` in `internal/mail/imap/folders.go` (filled by `newFolder`, both on the LIST-STATUS and plain STATUS paths). Create `internal/notify` (standard library plus `internal/mail` only) with a `Tracker`: `Observe(account int, folders []mail.Folder) int` returns how many messages INBOX gained since the last observation for that account, and `Moved(account, n int)` records messages dk-email moved into INBOX. Rules: the first observation is a baseline (0); a `UIDVALIDITY` change, a lower `UIDNEXT` or a missing value resets the baseline (0); otherwise new = `UIDNext` rise minus moved, never below 0, and moved is cleared. `Forget(account)` drops an account. Feed it from `handleAccount` (baseline) and `handleRefresh` (non-stale rounds only; see the plan), record moves to INBOX in the move path in `internal/ui/actions.go`, and forget accounts that are removed or replaced. No alert yet; keep the per-round sum in `refreshState`.

**Acceptance criteria:**
- [x] `imap` folder tests show INBOX `UIDNext`/`UIDValidity` set, both with and without LIST-STATUS.
- [x] Tracker table tests cover: baseline, +3 delivered, a delivery after a move into INBOX (moves subtracted), a `UIDVALIDITY` change, a lower `UIDNEXT`, a missing INBOX, and two accounts that don't affect each other.
- [x] A UI test that runs a refresh after the server gained 2 INBOX messages leaves `refreshState` with 2 new for that account; a refresh that only reflects a dk-email move into INBOX leaves 0.
- [x] A refresh that runs while a move into INBOX is in flight doesn't later count that move as new mail; mail delivered during such a round is counted in the next one.

**Verification:**
- [x] Tests pass: `go test -race ./internal/mail/imap ./internal/notify ./internal/ui`
- [x] Build succeeds: `go build -o bin/dk-email ./cmd/dk-email`
- [x] Manual check: none (no visible change yet).

**Dependencies:** None

**Files likely touched:**
- `internal/mail/mail.go`
- `internal/mail/imap/folders.go`, `internal/mail/imap/folders_test.go`
- `internal/notify/tracker.go`, `internal/notify/tracker_test.go`
- `internal/ui/refresh.go`, `internal/ui/accounts.go`, `internal/ui/actions.go`, tests

**Estimated scope:** Medium: 5-7 files

---

## Task 3: Bell and desktop alerts after a refresh round

**Description:** In `internal/notify`, add `Text(counts)` that builds `dk-email: 3 new in Work, 1 in Personal` from per-account counts in account order (`dk-email: 1 new in Work` for one), and `Sequence(text string, bell, desktop bool, tmux bool) string`, which returns `\a` and/or OSC 9 (`ESC ] 9 ; text BEL`), wrapped for tmux passthrough (`ESC P tmux; …ESC\`, inner `ESC` doubled) when `tmux` is true. Strip control characters from the text, since account names come from config. In `finishRefresh`, when the round's total is above 0, send `tea.Raw(notify.Sequence(...))` using the `[notify]` settings and `os.Getenv("TMUX") != ""` (read once in `New`). Never put sender, subject or any message content in the text.

**Acceptance criteria:**
- [x] `Text` and `Sequence` table tests cover one and several accounts, bell only, desktop only, both, tmux wrapping, and a name with control characters.
- [x] A UI test with 2 new in one account and 1 in another emits exactly one `tea.RawMsg` for the round, containing `dk-email: 2 new in Personal, 1 in Work`.
- [x] A round with 0 new, or with `bell` and `desktop` both off, emits nothing.

**Verification:**
- [x] Tests pass: `go test -race ./internal/notify ./internal/ui`
- [x] Build succeeds: `go build -o bin/dk-email ./cmd/dk-email`
- [x] Manual check: in Ghostty, switch to another app, send yourself mail, press nothing; within `refresh_interval` a banner shows `dk-email: 1 new in <account>` and the bell fires. Repeat in iTerm2 and in tmux with `allow-passthrough on`. (Ghostty, 2026-11-19; iTerm2 and tmux not yet checked)

**Dependencies:** Task 1, Task 2

**Files likely touched:**
- `internal/notify/alert.go`, `internal/notify/alert_test.go`
- `internal/ui/refresh.go`, `internal/ui/model.go`, tests

**Estimated scope:** Small-Medium: 3-5 files

---

## Task 4: No alerts while the window has focus

**Description:** Set `v.ReportFocus` in `View` whenever `bell` or `desktop` is on. Keep `focused bool` on `Model`: false at start, true on `tea.FocusMsg` and on any key press, click or wheel, false on `tea.BlurMsg`. `finishRefresh` sends the alert only when not focused. The new-mail counts are still consumed, so they don't pile up into a later alert. The title (Task 1) updates regardless of focus.

**Acceptance criteria:**
- [x] After a `tea.FocusMsg`, a round with new mail emits no `tea.RawMsg`; after a `tea.BlurMsg` it does.
- [x] Without any focus message, a round alerts until the first key press, then doesn't.
- [x] New mail that arrives while focused doesn't alert later, after blur.

**Verification:**
- [x] Tests pass: `go test -race ./internal/ui`
- [x] Build succeeds: `go build -o bin/dk-email ./cmd/dk-email`
- [x] Manual check: with dk-email in front, `ctrl+r` after new mail shows no banner or bell; switch away, send another, and the alert arrives. (now: silent only for the INBOX on screen; checked 2026-11-19)

**Dependencies:** Task 3

**Files likely touched:**
- `internal/ui/model.go`, `internal/ui/refresh.go`, tests

**Estimated scope:** Small: 2-3 files

---

## Checkpoint: Step 1 works
- [x] Full test suite passes: `go test -race ./...`
- [x] Lint and format pass: `go vet ./... && gofmt -l .`
- [x] Manual checks of Tasks 1, 3 and 4 done in Ghostty and iTerm2
- [x] Diff reviewed in the main session before commit

---

## Task 5: User docs for step 1

**Description:** Write `docs/notifications.md` as the user guide: what alerts and when (new INBOX mail, count only, skipped while focused, one per refresh round), the `[notify]` settings, and how the terminal decides what an alert looks like. Setup for Ghostty (`desktop-notifications`, `bell-features`), iTerm2 (Profiles → Terminal: notifications, bell), Terminal.app (bell and title only, no OSC 9), tmux (`allow-passthrough on`, `focus-events on`, `monitor-bell`), and SSH (OSC 9 and bell pass through). macOS: System Settings → Notifications → (terminal app) chooses Banners, Alerts or None. Troubleshooting: no banner, alerts while looking, empty title after quit. Link it from README (config sample gains `[notify]`) and update SPEC.md decision 6 and the config section. Add `internal/notify` to AGENTS.md.

**Acceptance criteria:**
- [x] `docs/notifications.md` exists and follows the style of `docs/attachments.md` (task-first, short sections).
- [x] README's config sample shows `[notify]` and links the guide; SPEC.md and AGENTS.md describe the behaviour as built.
- [x] Every setting and terminal option named in the docs matches the code and the terminal's real option name. (Code settings match. To verify by hand: Ghostty `desktop-notifications` / `bell-features`, iTerm2 "Filter Alerts…", tmux `allow-passthrough` / `focus-events` / `monitor-bell`.)

**Verification:**
- [x] Manual check: follow the guide from scratch in Ghostty and in tmux, and get a banner.

**Dependencies:** Task 4

**Files likely touched:**
- `docs/notifications.md`, `README.md`, `SPEC.md`, `AGENTS.md`

**Estimated scope:** Small: docs only

---

## Task 6: `imap.Watcher` waits for new INBOX mail over its own connection

**Description:** Add `mail.Watcher` (`Wait(ctx, folder string) error`) and `mail.ErrUnsupported` to `internal/mail`. In `internal/mail/imap`, add `Watcher`, built like `Client` from the account, credentials and logger with no network I/O until the first `Wait`. It holds its own lazily dialed connection with an `UnilateralDataHandler` that records `EXISTS`, selects `folder`, and remembers the message count. `Wait` returns nil at once if the count grew since the last `Wait`, otherwise `IDLE`s until it grows, ending and re-issuing `IDLE` every 25 minutes. It returns `mail.ErrUnsupported` when the server lacks `IDLE`, drops the connection on any error (the next `Wait` dials again), and honours `ctx` by stopping `IDLE` and closing the connection. Debug-log dials, drops and wake-ups like `Client` does, never credentials. `Close` logs out.

**Acceptance criteria:**
- [x] With `imapmemserver`, appending to INBOX from a second session makes a pending `Wait` return nil within 1s; an expunge doesn't.
- [x] A message appended between two `Wait` calls makes the second return at once.
- [x] A server without `IDLE` returns `mail.ErrUnsupported`; a cancelled `ctx` returns its error promptly; after the server drops the connection, the next `Wait` reconnects and works.
- [x] The re-IDLE interval is a field tests can shorten, and a test sees `IDLE` restarted.
- [x] No log record contains the password or token.

**Verification:**
- [x] Tests pass: `go test -race ./internal/mail/imap`
- [x] Build succeeds: `go build -o bin/dk-email ./cmd/dk-email`
- [x] Manual check: none (covered by the in-process server).

**Dependencies:** None (can start any time after the Step 1 checkpoint)

**Files likely touched:**
- `internal/mail/mail.go`
- `internal/mail/imap/watch.go`, `internal/mail/imap/watch_test.go`
- `internal/mail/imap/server_test.go`

**Estimated scope:** Medium: 3-4 files

---

## Task 7: The UI refreshes an account when its watcher wakes

**Description:** Add `Watcher mail.Watcher` to `ui.Conn`; `cmd/dk-email`'s `dial` builds an `imap.Watcher` with the same credentials and logger (nil in tests that don't need one). When an account connects, `Model` starts `waitInbox(watcher, account, id, ctx)`, a `tea.Cmd` that calls `Wait(ctx, "INBOX")` and returns `inboxMsg{account, id, err}`. On nil: refresh that account through the `refresh.rerun` path (so a running refresh is followed by another), then wait again. On `mail.ErrUnsupported`: debug-log and stop for that account. On any other error: debug-log and wait again after a backoff of 30s, doubling up to 5 minutes, reset after a success. Each account's wait has a cancel func, called when the account is removed, replaced or reconnected, and in `Model.Close`, which also closes the watchers.

**Acceptance criteria:**
- [x] A fake watcher that returns nil leads to exactly one refresh of that account and a new wait; while a refresh is running it leads to a rerun, not a dropped refresh.
- [x] Errors back off 30s, 60s, … up to 5 min (via the injectable `after`), and a success resets it; `ErrUnsupported` stops the waits.
- [x] Removing or replacing an account cancels its wait, and a late `inboxMsg` for it is ignored (matched by `id`).
- [x] `cmd/dk-email` connector tests show a `Watcher` on successful dials.

**Verification:**
- [x] Tests pass: `go test -race ./internal/ui ./cmd/dk-email`
- [x] Build succeeds: `go build -o bin/dk-email ./cmd/dk-email`
- [x] Manual check: with `refresh_interval = "30m"`, send yourself mail with dk-email in the background; the alert arrives within ~10 s for Gmail, Fastmail and Proton Bridge.

**Dependencies:** Task 4, Task 6

**Files likely touched:**
- `internal/ui/accounts.go`, `internal/ui/refresh.go`, `internal/ui/model.go`, tests
- `cmd/dk-email/main.go`, `cmd/dk-email/connector_test.go`

**Estimated scope:** Medium: 5-6 files

---

## Checkpoint: Push works
- [x] Full test suite passes: `go test -race ./...`
- [x] Lint and format pass: `go vet ./... && gofmt -l .`
- [x] Manual check of Task 7 done on all three accounts
- [x] Diff reviewed in the main session before commit

---

## Task 8: Docs for push and an overnight run

**Description:** Add a "Push" section to `docs/notifications.md`: IDLE is used automatically when the server supports it, it opens one extra connection per account, polling stays as the fallback, and the debug log shows watcher dials and drops. Update SPEC.md decision 6 ("No IMAP IDLE in v1" → push via IDLE on INBOX), AGENTS.md (the watcher connection beside the serialized `Client`, and `mail.Watcher`), and the `password_cmd` cache entry in `tasks/future-plans.md`. Then leave dk-email open overnight with `DK_EMAIL_DEBUG=1` and read the watcher lines in `debug.log`.

**Acceptance criteria:**
- [x] Docs, SPEC.md and AGENTS.md describe push as built.
- [ ] After an overnight run, mail sent in the morning alerts within seconds for each account, or the log shows why not and a follow-up is recorded in `tasks/future-plans.md`.

**Verification:**
- [ ] Manual check: the overnight run above.

**Dependencies:** Task 7

**Files likely touched:**
- `docs/notifications.md`, `SPEC.md`, `AGENTS.md`, `tasks/future-plans.md`

**Estimated scope:** Small: docs + field check

---

## Checkpoint: Complete
- [ ] All acceptance criteria met
- [ ] Diff reviewed; ready to tag
