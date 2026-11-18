# Implementation Plan: Open and Attach Files

Ideas: [open-attachments.md](open-attachments.md),
[attach-files.md](attach-files.md). Tasks:
[attachments-todo.md](attachments-todo.md).

## Overview

Two features that share one user guide. **Opening**: `o`, while a thread is
open in the reader (the popup, or the reader pane in split view), saves an
attachment to the Downloads folder and opens it with the system's default
app. A picker grouped by message, with sizes, appears when the thread has
more than one attachment. **Attaching**: in the composer, `ctrl+o` (a path
prompt with tab completion) or dropping files onto the terminal adds them;
an Attachments row lists every attachment (local, forwarded, from a reopened
draft), and `d` removes one. Underneath, a draft carries one attachment list
with the bytes, replacing the lazy `ForwardOf` copy.

Opening ships first (Phase 1): it's smaller, has no effect on sending, and
creates `docs/attachments.md`, which Phase 2 extends.

## Architecture Decisions

- **`Raw` joins `mail.Mailbox`.** `imap.Client.Raw` already exists (used by
  `send` through its own `Store`). The UI gets attachment bytes with
  `Raw` + `render.Parts`, whose order matches `Message.Attachments` by
  contract. The UI already imports `render` (in `reader`). `mailtest.Fake`
  gains raw messages so UI tests can exercise it.
- **Saving lives in a new `internal/download` package**, free of UI code and
  tested against a temp dir: Downloads folder lookup (macOS/Windows
  `~/Downloads`; Linux/BSD `$XDG_DOWNLOAD_DIR`, then `user-dirs.dirs`, then
  `~/Downloads`), filename sanitizing, reuse of identical files, `name (1).ext`
  on a clash, never overwriting, and the list of extensions that are saved
  but not opened.
- **Opening lives in a new `internal/sysopen` package**: `openBrowser` moves
  there out of `cmd/dk-email/signin.go` (which then calls it), and gains
  `CanOpen()`: false without `DISPLAY`/`WAYLAND_DISPLAY` on Linux/BSD, inside
  SSH (`SSH_CONNECTION`/`SSH_TTY`, which would open on the remote screen), or
  when the opener isn't on `PATH`. The UI never calls `exec`: `cmd/dk-email`
  hands it an opener with `ui.Model.WithOpener`, as it does for onboarding;
  tests pass a fake.
- **One attachment list on the draft.** A new standard-library-only type in
  `mail` (name, content type, bytes) is separate from the display-only
  `mail.Attachment`. `mail.Draft.Attachments` holds it;
  `compose.Build`/`BuildDraft` write it; `send.OpenDraft` fills it from the
  saved draft. A forward fetches the original's raw bytes **before** the
  composer opens (as replies already fetch first) and takes both the message
  and its attachments from them, so a composer never exists with
  attachments still loading or failed. Only after that path is proven does
  a separate, deletion-only task remove `ForwardOf`, `compose.Attachment`
  and `send`'s forward fetch.
- **The composer owns the list display, the UI owns the bytes.** `popup.Compose`
  shows names and sizes and reports removals; `draftState` keeps the data, so
  the popup stays free of mail types and I/O.
- **All I/O in `tea.Cmd`s with typed messages**: raw fetches, file writes,
  file reads, opening. `Update` stays the only state transition.

## Dependency Graph

```
mail.Mailbox.Raw + mailtest raw ─┬─► o: single attachment (T3) ─► picker (T4) ─► help/docs (T5)
download pkg ────────────────────┤
sysopen pkg + WithOpener ────────┘

mail draft attachment type + Draft.Attachments (T6)
    └─► forward fetches raw before the composer opens (T7a)
            └─► delete ForwardOf, deletions only (T7b)
                    └─► composer Attachments row: show + remove (T8)
                    ├─► ctrl+o prompt (T9)
                    │       └─► drop to attach (T10)
                    └─────────────► help/docs extended (T11)
```

T6 needs Raw (T1) only through T7. Phases 1 and 2 touch different
code, except `mail.go` and `keys.go`.

## Task List

### Phase 1: Open attachments

- [ ] Task 1: `Raw` on `mail.Mailbox`, raw messages in `mailtest.Fake`
- [ ] Task 2: `internal/download` and `internal/sysopen`
- [ ] Task 3: `o` saves and opens the only attachment
- [ ] Task 4: Attachment picker, grouped by message, with sizes
- [ ] Task 5: Help dialog, SPEC, and `docs/attachments.md` (opening)

### Checkpoint: Opening

- [ ] `go test -race ./...`, `go vet ./...`, `gofmt -l .` clean; build OK
- [ ] Manual: open a PDF and an image on macOS; on Linux over SSH the status
  reports the saved path; a `.sh` is saved and not opened
- [ ] Review with human before Phase 2

### Phase 2: Attach local files

- [ ] Task 6: Draft attachment type; drafts keep attachments via the list
- [ ] Task 7a: Forwards fill `d.Attachments` before the composer opens
- [ ] Task 7b: Remove `ForwardOf` and what only served it
- [ ] Task 8: Attachments row in the composer: show and remove

### Checkpoint: Attachment model

- [ ] Full suite green; forwarding, saving and reopening drafts behave as
  before, now with attachments visible
- [ ] Review with human

- [ ] Task 9: `ctrl+o` attach prompt with tab completion
- [ ] Task 10: Drop files to attach
- [ ] Task 11: Help dialog, SPEC, and `docs/attachments.md` (attaching)

### Checkpoint: Complete

- [ ] All acceptance criteria met; full suite, vet, gofmt, `goreleaser check`
- [ ] Manual: attach by prompt and by drop in two terminals, send to
  yourself, save, reopen, remove one, send; forward and drop one attachment
- [ ] Ready for review

## Risks and Mitigations

| Risk | Impact | Mitigation |
|------|--------|------------|
| Opening runs code (`.command`, `.desktop`, …) | High | Saved but never opened; list tested in `download`; status says why |
| Sender-chosen filename escapes the folder or hides itself | High | Base name only, leading dots and control characters stripped, length cap; tests with `../`, `.bashrc`, NUL, 300 chars |
| `o` on a remote session opens on the far screen | Med | `sysopen.CanOpen` treats SSH as headless: save and report the path |
| Forward sent without its attachments after `ForwardOf` goes | Med | Composer opens only after the raw fetch succeeded (no loading/failed state to mishandle); 7a proves N-in/N-out while the old path still exists; 7b is deletions only |
| Forwarding opens more slowly for large attachments | Low | Same `Loading message…` as replies; part fetch (`BODY[n]`) is a follow-up |
| Drop formats differ by terminal and OS | Med | Pure parser with table tests per format; attach only when every item is an existing regular file |
| Removing `ForwardOf` breaks drafts saved by older builds | Low | `OpenDraft` reads attachments from the stored message, which older drafts already contain |
| Whole-message fetch is slow for large attachments | Low | Accept for v1; `BODY[n]` part fetch is a follow-up |

## Open Questions

- Names: `internal/download` and `internal/sysopen`, and `mail.File` for the
  draft attachment type. Renaming is cheap before Task 2/6.
