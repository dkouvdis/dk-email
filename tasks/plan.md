# Implementation Plan: dk-email v0.1.0

Source of truth: [SPEC.md](../SPEC.md). Task details, acceptance criteria and verification steps: [todo.md](todo.md).

## Overview

Build the v1 spec as `v0.1.0`: a Go + Bubble Tea terminal client for triaging several IMAP accounts, with a Thunderbird-style responsive layout, credentials from 1Password or the keychain, and semver releases via GoReleaser plus a Homebrew tap.

The order follows dependencies:
1. Start with the riskiest parts: go-imap v2 is a beta library, and Gmail behaves differently from other IMAP servers.
2. Next, deliver a **read-only client** that's usable against real Gmail. It ships as `v0.1.0-beta.1`.
3. Then add triage actions, the in-app account onboarding, and the stable release.

## Architecture Decisions

- **`mail.Mailbox` is the only seam between the UI and the network.** The UI is developed and tested against a fake (`mail/mailtest`), and the IMAP code is tested against an in-process `imapmemserver`. Both sides can then progress independently, and Gmail is touched only in explicit live checks.
- **Core packages are pure:** `config`, `secret`, `thread` and `render` do no TUI work. They're cheap to unit test, have no dependencies on each other, and can be built in parallel.
- **There's one IMAP connection per account, opened lazily and serialized with a mutex.** Every call runs inside a `tea.Cmd` with a context timeout, so a slow server never blocks key handling.
- **`password_cmd` runs off the UI thread with a 60s timeout,** because `op read` may wait for a Touch ID prompt.
- **Optimistic UI for triage actions:** the list updates immediately and rolls back if the server returns an error, which is shown in the status line.
- **Layout is a pure function** of terminal width and mode (`ui.Layout(width, height, mode) → pane rects`), so the 120 and 80 column breakpoints can be table-tested.
- **The config is written by appending text, never re-encoded,** to keep the user's comments.
- **The release pipeline lands at the end of Phase 2,** so betas can be cut as soon as the client can read mail.
- **Library APIs are checked against current docs before use.** Bubble Tea v2 and go-imap v2 changed their APIs from v1, so code is written against their current documentation, not remembered v1 patterns.

## Task List

The full details for every task are in [todo.md](todo.md).

### Phase 1: Foundation and de-risking
- [x] T1: Scaffold the module with a `--version` flag, `.gitignore`, and CI
- [x] T2: Config loading, paths, and the config-folder `.gitignore`
- [x] T3: Password resolution (`password_cmd` and keychain)
- [x] T4: Mail domain types, the `Mailbox` interface, and a fake
- [ ] T5: IMAP connect + `Folders` (memserver tests + live read-only Gmail check)

**Checkpoint 1:** foundation tests pass, and real Gmail folders are listed through `op read`.

### Phase 2: Read-only client → `v0.1.0-beta.1`
- [ ] T6: Threading package
- [ ] T7: IMAP `Summaries` with UID paging
- [ ] T8: UI shell (layout engine, hint bar, status line, help, debug log)
- [ ] T9: Sidebar with folder selection (wide mode)
- [ ] T10: Thread list (expand/collapse, navigation, paging)
- [ ] T11: Render package (MIME → text)
- [ ] T12: IMAP `Message` fetch + reader pane + mark read on open
- [ ] T13: Narrow mode header + account/folder picker (`a`)
- [ ] T14: List + popup mode (`v`, forced below 80 columns)
- [ ] T15: Release pipeline (GoReleaser, `release.yml`, Homebrew cask)

**Checkpoint 2:** read-only use against real Gmail works in all three layouts. You approve, then `v0.1.0-beta.1` is tagged.

### Phase 3: Triage
- [ ] T16: IMAP `SetFlag` + `Move` + archive/Trash folder resolution
- [ ] T17: Triage keys `u` / `s` / `e` / `d` with optimistic update
- [ ] T18: Move-to-folder picker (`m`)
- [ ] T19: Auto-refresh (120s) + `r`

**Checkpoint 3:** spec success criteria 5–7 are verified on real Gmail. You approve, then `v0.1.0-beta.2` is tagged.

### Phase 4: Account onboarding
- [ ] T20: Append an account to the config without re-encoding the file
- [ ] T21: Connect-account popup (first run, `A`, re-entering a missing password)

**Checkpoint 4:** spec success criteria 1–3 are verified.

### Phase 5: Release v0.1.0
- [ ] T22: README (install, config, keys, quarantine fallback)

**Checkpoint 5:** all of spec success criteria 1–9 are met. You approve, then `v0.1.0-rc.1` is tagged, followed by `v0.1.0`.

## Dependency Graph

```
T1 ─┬─ T2 ─────────────┬──────────────── T20 ── T21
    ├─ T3 ─────────────┤                   ▲
    └─ T4 ─┬─ T5 ── T7 ┼─ T12 ── T16 ──────┘
           │     │     │    ▲      │
           │     │     │   T11     ├─ T17 ── T18
           ├─ T6 ┼─────┤           └─ T19
           └─ T8 ┴─ T9 ┴─ T10 ─┬─ T13
                                └─ T14
T1 ── T15 (needs a runnable binary; lands after T14)
```

**Can run in parallel:**
- T2, T3, T4 and T6 once T1 is done.
- T11 at any time after T4.
- T8 alongside T5 and T7, since it only depends on the fake `Mailbox`.

**Must run in order:**
- T5 → T7 → T12 → T16 (they all build on the shared IMAP client file).
- T8 → T9 → T10 (they all modify the root model's wiring).

## Risks and Mitigations

| Risk | Impact | Mitigation |
|---|---|---|
| go-imap v2 is a beta; its API changes or has bugs | High | Isolated behind `mail.Mailbox`, with the version pinned in `go.mod`. T5 exercises it against real Gmail early (fail fast). |
| Gmail behaves differently from other IMAP servers (special-use folders, `\Flagged` = Starred, archiving as a move to All Mail) | Med | Live read-only check in T5. Live write checks in Checkpoint 3 use a throwaway test email only. Archive-folder resolution is table-tested in T16. |
| Bubble Tea v2, Bubbles v2 and Lip Gloss v2 are new and differ from v1 | Med | Read the v2 docs and examples before T8. teatest golden files lock in the layouts. |
| `op read` blocks waiting on Touch ID | Med | Run it asynchronously with a 60s timeout. The status line shows "Waiting for 1Password…". |
| `op` behaves differently when run by dk-email (own process group, no stdin) than in a terminal | Med | T5's live check resolves the password through the real `op read` before any UI work. |
| Threads split across a 200-message page boundary | Low | Re-thread the whole loaded set whenever another page arrives (T10). |
| A textual config append corrupts an unusual file (no trailing newline, CRLF line endings) | Med | Covered by the fixture tests in T20. The result is parsed back after writing, and the original is restored if parsing fails. |
| macOS quarantines the unsigned binary installed via Homebrew | Low | The cask clears it with `postflight_steps`, and the README documents the `xattr` fallback. |
| The tap token expires | Low | GitHub releases still publish and only the tap step fails. Recorded in the README release notes section. |
| teatest v2 has no tagged version | Low | Pinned to a pseudo-version. Used only in tests. |

## Live-account policy

- Live tests use the Go build tag `live` and the environment variable `DK_EMAIL_LIVE_ACCOUNT=<name>`. They never run in CI.
- Phases 1–2 only read. Phase 3 write checks (flag, move, archive, delete) need your OK and run against a single throwaway email sent to yourself.

## Open Questions

None. All spec questions are resolved.
