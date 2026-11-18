# Open and Attach Files: Tasks

Plan: [attachments-plan.md](attachments-plan.md).

Verification commands (as CI): `go build -o bin/dk-email ./cmd/dk-email`,
`go test -race ./...`, `go vet ./...`, `gofmt -l .`.

---

## Phase 1: Open attachments

## Task 1: `Raw` on `mail.Mailbox`, raw messages in `mailtest.Fake`

**Description:** Add `Raw(ctx, folder, uid) ([]byte, error)` to
`mail.Mailbox`, documented as the exact stored bytes. `imap.Client` already
implements it. Give `mailtest.Fake` a way to store a message from raw bytes
(e.g. `AddRaw(folder, raw)`, deriving the `mail.Message` with `render.Parse`
if that keeps `mailtest` within its imports, else taking both), a
`MethodRaw` call record, and `Fail` support.

**Acceptance criteria:**
- [x] `var _ mail.Mailbox = (*imap.Client)(nil)` and `(*mailtest.Fake)(nil)`
  compile.
- [x] The fake returns the stored bytes, records the call, and returns
  `mail.ErrFolderNotFound` / the injected error like its other methods.
- [x] `internal/mail` stays standard-library only (existing test passes).

**Verification:**
- [x] Tests pass: `go test -race ./internal/mail/...`
- [x] Build succeeds: `go build -o bin/dk-email ./cmd/dk-email`
- [ ] Manual check: none.

**Dependencies:** None

**Files likely touched:**
- `internal/mail/mail.go`
- `internal/mail/mailtest/fake.go`, `fake_test.go`

**Estimated scope:** Small

---

## Task 2: `internal/download` and `internal/sysopen`

**Description:** `download.Dir()` finds the Downloads folder (macOS/Windows
`~/Downloads`; Linux/BSD `$XDG_DOWNLOAD_DIR`, then `XDG_DOWNLOAD_DIR` in
`$XDG_CONFIG_HOME/user-dirs.dirs` with `$HOME` expanded, then `~/Downloads`).
`download.Save(dir, name string, data []byte) (path string, err error)`
creates `dir` if needed, sanitizes `name`, reuses an existing file with
identical bytes, otherwise picks `name (1).ext`, `name (2).ext`, … and
creates the file exclusively (`O_EXCL`) so nothing is overwritten.
`download.Runnable(name) bool` reports extensions that are saved but not
opened. `sysopen.Open(target)` is today's `openBrowser`, moved;
`sysopen.CanOpen()` is false on Linux/BSD without `DISPLAY`/`WAYLAND_DISPLAY`,
inside SSH, or without the opener on `PATH`. `cmd/dk-email/signin.go` calls
`sysopen.Open`.

**Acceptance criteria:**
- [x] Sanitizing: `../../.zshrc` → `zshrc`, control characters and `/`/`\`
  dropped, length capped keeping the extension, empty → `attachment`;
  table-tested.
- [x] Saving twice with the same bytes returns the same path; different
  bytes give `name (1).ext`; an existing file is never modified (temp dir
  tests).
- [x] `Runnable` covers at least `.command .sh .app .pkg .jar .desktop .exe
  .bat .cmd .ps1 .msi .scr`, case-insensitive; `CanOpen` is tested through
  injected env/lookPath.

**Verification:**
- [x] Tests pass: `go test -race ./internal/download ./internal/sysopen ./cmd/dk-email`
- [x] Build succeeds
- [ ] Manual check: OAuth sign-in still opens the browser.

**Dependencies:** None

**Files likely touched:**
- `internal/download/download.go`, `download_test.go`
- `internal/sysopen/sysopen.go`, `sysopen_test.go`
- `cmd/dk-email/signin.go`

**Estimated scope:** Medium

---

## Task 3: `o` saves and opens the only attachment

**Description:** Add an `OpenAttachment` binding (`o`, "open attachment"),
active in the reader popup and, in split view, whenever the reader pane has
a thread open (`m.read.IsOpen()`), whichever pane has focus. With exactly
one attachment in the thread, a `tea.Cmd` fetches `Raw`, takes the part from
`render.Parts` at that attachment's index, saves it with `download.Save`, and
opens it when `sysopen.CanOpen()` and not `download.Runnable`. One result
message reports `Saved <path>`, `Saved <path> — not opened: it can run code`,
`Saved <path> — no app to open it here`, or the error. With no attachments:
`No attachments`. `cmd/dk-email` passes the opener and directory through
`ui.Model.WithOpener`; tests use fakes.

**Acceptance criteria:**
- [x] With the fake mailbox and a temp Downloads dir, `o` on a thread with
  one attachment writes its exact bytes and calls the fake opener with the
  path; the status names the path.
- [x] A runnable attachment is saved and the opener is not called; a fetch
  error shows in the status and writes nothing.
- [x] `o` does nothing on the thread list with no thread open; the reader's
  key hints include `o`.

**Verification:**
- [x] Tests pass: `go test -race ./internal/ui/...`
- [x] Build succeeds
- [ ] Manual check: open a PDF from a real account; it lands in Downloads and
  opens in Preview.

**Dependencies:** Tasks 1, 2

**Files likely touched:**
- `internal/ui/keys.go`
- `internal/ui/model.go`
- `internal/ui/attachments.go` (new), `attachments_test.go`
- `cmd/dk-email/main.go`

**Estimated scope:** Medium

---

## Task 4: Attachment picker, grouped by message, with sizes

**Description:** With more than one attachment in the open thread, `o` opens
a `popup.Picker` with one group per loaded message that has attachments
(heading: sender and date), each item labelled `name  12 KB` (using
`reader.FormatSize`). `enter` runs Task 3's save-and-open for that message
and index; `esc`/`q` close. Messages whose body hasn't loaded yet are fetched
first or shown as loading, so the picker never offers a stale index.

**Acceptance criteria:**
- [x] A thread with two messages, one and two attachments, shows two groups
  and three items; choosing the second item of the second group saves that
  part.
- [x] A thread with one attachment still skips the picker.
- [x] `esc` closes the picker and returns to the reader without side effects.

**Verification:**
- [x] Tests pass: `go test -race ./internal/ui/...`
- [x] Build succeeds
- [ ] Manual check: a thread with several attachments in a narrow terminal
  truncates long names cleanly.

**Dependencies:** Task 3

**Files likely touched:**
- `internal/ui/attachments.go`, `attachments_test.go`
- `internal/ui/model.go` (picker routing)

**Estimated scope:** Small

---

## Task 5: Help dialog, SPEC, and `docs/attachments.md` (opening)

**Description:** Add `o` to the `?` help dialog (Threads section) and the
reader hints. Update SPEC.md's keymap and reader sections. Write
`docs/attachments.md` with an "Opening attachments" section: the key, where
files go (per OS), reuse and `(1)` naming, why some files aren't opened, what
happens over SSH. Link it from the README. Mark the future-plans entry as
shipped.

**Acceptance criteria:**
- [x] Help-dialog test covers `o`; the hint bar shows it in the reader.
- [x] `docs/attachments.md` exists, linked from README; SPEC keymap lists `o`.

**Verification:**
- [x] Tests pass: `go test -race ./internal/ui/...`
- [ ] Manual check: `?` in the app shows `o`; read the doc against the app.

**Dependencies:** Task 4

**Files likely touched:**
- `internal/ui/keys.go`, `model_test.go` or help test
- `SPEC.md`, `README.md`, `docs/attachments.md`, `tasks/future-plans.md`

**Estimated scope:** Small

---

## Checkpoint: Opening

- [x] `go test -race ./...`, `go vet ./...`, `gofmt -l .` clean; build OK
- [ ] Manual: PDF and image open on macOS; over SSH the status reports the
  saved path; a `.sh` is saved and not opened
- [ ] Review with human before Phase 2

---

## Phase 2: Attach local files

## Task 6: Draft attachment type; drafts keep attachments via the list

**Description:** Add `mail.File{Name, ContentType string; Data []byte}` and
`mail.Draft.Attachments []mail.File`. `compose.Build`/`BuildDraft` write
`d.Attachments` (plus, for now, any passed forward attachments).
`send.OpenDraft` fills `d.Attachments` from the stored draft instead of
setting `ForwardOf` to the draft itself; `SaveDraft`/`Send` write the list.
Forwards still use `ForwardOf` until Task 7.

**Acceptance criteria:**
- [x] Saving a draft with two `Attachments` and reopening it yields the same
  names, types and bytes, and `ForwardOf` is nil.
- [x] Sending a reopened draft carries its attachments exactly once (no copy
  from both the list and `ForwardOf`).
- [x] Existing forward tests pass unchanged.

**Verification:**
- [x] Tests pass: `go test -race ./internal/mail/... ./internal/compose ./internal/send ./internal/ui/...`
- [x] Build succeeds
- [ ] Manual check: reopen a draft with attachments saved by the current
  release; send it to yourself; attachments arrive.

**Dependencies:** None (Phase 1 done first by choice)

**Files likely touched:**
- `internal/mail/mail.go`
- `internal/compose/mime.go`, `mime_test.go`
- `internal/send/send.go`, `send_test.go`

**Estimated scope:** Medium

---

## Task 7a: Forwards fill `d.Attachments` before the composer opens

**Description:** Fetch first, then open, the way replies already work. For a
forward, `startFromTarget` always fetches (even when the reader has the
message, since it has no bytes), holding the draft slot with `loading: true`
and `Loading message…` as today. The `tea.Cmd` calls `Mailbox.Raw` and
derives both the message (`render.Parse`) and its attachments
(`render.Parts`) from the same bytes, so they match by construction. The
composer opens only with both in hand; a failed fetch never opens it and
shows the error in the status, like a failed reply fetch. New forwards no
longer set `ForwardOf`; the field and the send-time copy stay in place,
unused, until 7b. There is no loading or failed state inside the composer.

**Acceptance criteria:**
- [x] A UI test forwards a message with N attachments (N = 0, 1, 3) through
  the fake mailbox and a capturing fake sender: the sent draft has exactly N
  attachments with the original names, types and bytes, and `ForwardOf` is
  nil.
- [x] A failed `Raw` fetch leaves no composer open, frees the draft slot and
  shows the error; a second `f` retries.
- [x] All existing forward tests pass unchanged (the old path still exists).

**Verification:**
- [x] Tests pass: `go test -race ./...`
- [x] Build succeeds
- [ ] Manual check: forward a message with a PDF to yourself, from the list
  and from the reader.

**Dependencies:** Tasks 1, 6

**Files likely touched:**
- `internal/ui/reply.go`, `forward_test.go`
- `internal/ui/compose.go` (draft carries the attachments)

**Estimated scope:** Small

---

## Task 7b: Remove `ForwardOf` and what only served it

**Description:** Nothing sets `ForwardOf` after 7a. Delete it from
`mail.Draft`, together with `send`'s `forwardedAttachments`, the `extract`
hook if `OpenDraft` no longer needs it separately, and `compose.Attachment`
(`Build`/`BuildDraft` take only the draft). Deletions only; no behaviour
change.

**Acceptance criteria:**
- [x] `ForwardOf` and `compose.Attachment` no longer exist; `go vet` is
  clean.
- [x] The full suite passes, including 7a's forward test and Task 6's draft
  round trip; tests that only covered the deleted path are removed, not
  rewritten to pass.

**Verification:**
- [x] Tests pass: `go test -race ./...`
- [x] Build succeeds
- [ ] Manual check: forward with attachments, and reopen and send a draft
  with attachments.

**Dependencies:** Task 7a

**Files likely touched:**
- `internal/mail/mail.go`
- `internal/compose/forward.go`, `mime.go`, tests
- `internal/send/send.go`, `send_test.go`

**Estimated scope:** Small (deletions)

---

## Task 8: Attachments row in the composer: show and remove

**Description:** `popup.Compose` gets an Attachments row under Subject, shown
only when the list isn't empty, in tab order then. It shows
`name (12 KB)` items, truncated to the width; `←`/`→` choose and
`d`/`backspace` remove one, reported to the UI, which drops it from
`draftState`. "Changed?" (the save/discard prompt on `esc`) counts added or
removed attachments.

**Acceptance criteria:**
- [x] A forward or reopened draft shows its attachments; removing one and
  sending sends the rest.
- [x] Removing an attachment then pressing `esc` brings up the save/discard
  prompt; with no changes, `esc` closes directly.
- [x] With no attachments the row is absent and tab order is unchanged
  (existing composer tests pass).

**Verification:**
- [x] Tests pass: `go test -race ./internal/ui/...`
- [x] Build succeeds
- [ ] Manual check: the row in a narrow and a wide terminal.

**Dependencies:** Task 7b

**Files likely touched:**
- `internal/ui/popup/compose.go`, `compose_test.go`
- `internal/ui/compose.go`, `compose_test.go`

**Estimated scope:** Medium

---

## Checkpoint: Attachment model

- [x] Full suite green; forwarding, saving and reopening drafts work, with
  attachments visible and removable
- [ ] Review with human

---

## Task 9: `ctrl+o` attach prompt with tab completion

**Description:** `ctrl+o` in the composer opens a one-line prompt (modal over
the composer). Tab completes the path against the filesystem (`~` expanded,
relative to the working directory, directories end in `/`); `enter` reads the
file in a `tea.Cmd` and adds it with a content type from
`mime.TypeByExtension`, then `http.DetectContentType`, else
`application/octet-stream`. Errors (missing, directory, unreadable) stay in
the prompt. A file whose name and bytes match one already on the list is
skipped with `<name> is already attached`. Footer hints include `ctrl+o`.

**Acceptance criteria:**
- [x] Attaching a temp file adds it to the row and it is sent with the
  right content type and bytes.
- [x] Tab completes a unique prefix and lists choices for an ambiguous one;
  a directory path or missing file shows an error and attaches nothing.
- [x] Attaching the same file twice leaves one copy and shows the note.

**Verification:**
- [x] Tests pass: `go test -race ./internal/ui/...`
- [x] Build succeeds
- [ ] Manual check: attach `~/Downloads/<file>` with tab completion and send
  to yourself.

**Dependencies:** Task 8

**Files likely touched:**
- `internal/ui/popup/compose.go` (prompt), `compose_test.go`
- `internal/ui/compose.go`, `internal/ui/attachfile.go` (new, completion and
  reading)

**Estimated scope:** Medium

---

## Task 10: Drop files to attach

**Description:** A pure parser turns pasted text into paths: backslash-escaped
(`My\ File.pdf`), single- or double-quoted, `file://` URLs (percent-decoded),
Windows paths, several separated by spaces or newlines. In the composer, a
`tea.PasteMsg` whose every item is an existing regular file attaches them
(same reading and duplicate rule as Task 9) instead of inserting text, in any
field; the status names what was attached. Anything else pastes as before.

**Acceptance criteria:**
- [x] Table tests cover each format, mixed text (stays text), a missing
  file (stays text) and multiple files.
- [x] Pasting two existing files' paths into the body attaches both and
  leaves the body unchanged.

**Verification:**
- [x] Tests pass: `go test -race ./internal/ui/...`
- [ ] Manual check: drag one and two files into the composer in Ghostty,
  iTerm2 and Terminal.app (and a Linux terminal if available); note any
  terminal whose format isn't handled.

**Dependencies:** Task 9

**Files likely touched:**
- `internal/ui/drop.go` (new), `drop_test.go`
- `internal/ui/compose.go` or `popup/compose.go` (paste routing)

**Estimated scope:** Small

---

## Task 11: Help dialog, SPEC, and `docs/attachments.md` (attaching)

**Description:** Add `ctrl+o` (attach a file), "drop files to attach" and the
Attachments row keys (`←/→` choose, `d` remove) to the Composer section of
the `?` help dialog. Extend `docs/attachments.md` with "Attaching files":
the prompt, dropping (and the terminals tested), the row, duplicates,
forwards and drafts. Update SPEC.md and the future-plans entry.

**Acceptance criteria:**
- [x] Help-dialog test covers the new composer keys.
- [x] `docs/attachments.md` documents attaching; SPEC matches the behaviour.

**Verification:**
- [ ] Tests pass: `go test -race ./...`; `go vet ./...`; `gofmt -l .`;
  `goreleaser check`
- [ ] Manual check: `?` shows the keys; read the doc against the app.

**Dependencies:** Tasks 9, 10

**Files likely touched:**
- `internal/ui/keys.go`, help test
- `docs/attachments.md`, `SPEC.md`, `tasks/future-plans.md`

**Estimated scope:** Small

---

## Checkpoint: Complete

- [ ] All acceptance criteria met; full suite, vet, gofmt, `goreleaser check`
- [ ] Manual: attach by prompt and by drop, send to yourself, save, reopen,
  remove one, send; forward and remove one attachment
- [ ] Ready for review
