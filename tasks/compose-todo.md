# Tasks: Built-in compose popup with drafts

Plan: [compose-plan.md](compose-plan.md)

Every task also meets the standing bar before it counts as done:

- `go test -race ./...`, `go vet ./...` and `gofmt -l .` all pass, the last with empty output.
- Commits follow Conventional Commits, with no `Co-authored-by` trailer.
- No secrets, real mail or hardcoded version strings are committed.
- No real address other than your own receives mail during development.

---

## Phase 0

### P0: Plan files

- [x] `tasks/compose-plan.md` and this file exist.
- [x] `tasks/future-plans.md` lists the deferred ideas from the plan.

---

## Phase 1: Composer replaces `$EDITOR` and the confirm popup

### C1: compose `Fields`

**Acceptance criteria:**

- [x] `compose.Fields{To, Cc, Bcc, Subject, Body string}`, `FieldsOf(mail.Draft) Fields` and `(Fields).Draft(base mail.Draft) (mail.Draft, error)` exist.
- [x] `Draft` keeps `base`'s From, threading headers, `ReplyTo` and `ForwardOf`, and replaces only addresses, subject and body.
- [x] Address errors name the field, e.g. `To: …`. A draft with no recipient at all is an error. Subject newlines are flattened.
- [x] `FieldsOf` then `Draft` round-trips names with commas and quotes.

**Verification:** `go test -race ./internal/compose`

**Files:** `internal/compose/fields.go`, `internal/compose/fields_test.go`

### C2: `popup.FrameTitled` and `popup.Compose`

**Acceptance criteria:**

- [x] `FrameTitled(th, title, content, w, h)` draws the title in the top border, truncated with `…`, at exactly w × h.
- [x] `Compose` has To, Cc, Bcc and Subject inputs and a body textarea (`MaxHeight = 0`, `CharLimit = 0`). `tab` / `shift+tab` cycle focus. The initial focus is set by the caller.
- [x] `ctrl+s` returns a send result. `esc` returns at once when nothing changed; otherwise it opens the prompt. In the prompt, `d` discards, `e` goes back to editing, other keys are ignored. Saving (`s`, second `esc`) is off until D3 turns it on.
- [x] `SetError` shows a footer line; `SetBusy` shows "Sending…" / "Saving…" and ignores keys except `ctrl+c` (which the model handles).
- [x] `SetSize` resizes every input; the body fills the rest of the frame above the footer.
- [x] A benchmark types into a 5,000-line body; the result is reported at Checkpoint 1. (66–70 ms per keystroke on a 5,000-line body, M4 Pro.)

**Verification:** `go test -race ./internal/ui/popup`

**Files:** `internal/ui/popup/overlay.go`, `internal/ui/popup/compose.go`, `internal/ui/popup/compose_test.go`

### C3: Wire the composer into the UI

**Acceptance criteria:**

- [x] `c`, `r`, `R` and `f` open the composer in the 90% popup with the title `New message` / `Reply` / `Reply all` / `Forward` · subject · From address.
- [x] New message and forward focus To; reply and reply-all focus the body with the cursor at the top, above the quote.
- [x] While the composer is open every key goes to it, except `ctrl+c` (quit). Pane, archive and delete keys don't fire.
- [x] `ctrl+s` parses and sends. Parse and send errors show in the footer and editing continues; `ErrAfterDelivery` closes the popup with the error in the status bar.
- [x] Closing returns to the reader if it was open, otherwise the list, with selection and focus unchanged.
- [x] The hints row shows the composer keys while it's open.
- [x] `editor.go`, `popup/confirm.go`, `compose.Format` / `compose.Parse` and their tests are removed; `$VISUAL` / `$EDITOR` are no longer read.

**Verification:** `go test -race ./internal/...`

**Files:** `internal/ui/compose.go`, `internal/ui/reply.go`, `internal/ui/model.go`, `internal/ui/keys.go`, tests

### Checkpoint 1

- [x] Automated: tests, vet and gofmt are green.
- [ ] Manual: `c`, `r`, `R` and `f` send to yourself; a bad address shows in the footer; a send with the network off fails in the footer and retries with `ctrl+s`.

---

## Phase 2: Drafts

### D1: mail and IMAP

- [x] `mail.FlagDraft`; `Append` sets `\Draft`.
- [x] `imap.Client.DraftsFolder(ctx)` mirrors `SentFolder`.
- [x] `imap.Client.Delete(ctx, folder, uids)` sets `\Deleted` and runs UID EXPUNGE; without UIDPLUS it returns an error and never runs a plain EXPUNGE.
- [x] Tested against the in-process IMAP server.

### D2: compose and send

- [x] `mail.Draft.DraftOf *MessageRef`; `mail.Drafter` interface with `SaveDraft(ctx, d) error` and `OpenDraft(ctx, ref) (Draft, error)`.
- [x] `compose.Build` has a draft mode that writes Bcc; sending never does.
- [x] `send.Sender.SaveDraft` appends to Drafts with `\Draft` and `\Seen`, includes forwarded attachments, then deletes `DraftOf`. A failed delete returns an error wrapping `ErrDraftSaved`.
- [x] `Send` deletes `DraftOf` after delivery; failure wraps `ErrAfterDelivery`.
- [x] `compose.FromRaw` rebuilds a draft from the saved raw message (addresses incl. Bcc, subject, text body, In-Reply-To, References); `OpenDraft` sets `DraftOf`, and `ForwardOf` when the draft has attachments.

### D3: Save from the composer

- [x] In the prompt, `s` and a second `esc` save to Drafts and close; "Saving…" shows meanwhile, then "Draft saved" in the status bar.
- [x] A failed save keeps the composer open with the error in the footer; `ErrDraftSaved` closes it with a status warning.
- [x] If the Drafts folder is on screen, its list and counts refresh.

### D4: Reopen drafts

- [ ] `enter` on a message in a Drafts-role folder opens it in the composer (`DraftOf` set) instead of the reader.
- [ ] Saving again replaces the old copy; sending deletes it.
- [ ] A resumed forward keeps its attachments (taken from the saved draft).

### Checkpoint 2 (manual, Gmail and one other account)

- [ ] A new message, a reply and a forward with a PDF each save and show in Drafts in the web UI.
- [ ] Reopen, edit, save: only one copy remains.
- [ ] Reopen and send: delivered (same PDF `shasum`) and gone from Drafts. A resumed reply threads.

---

## Phase 3

### E1: Docs

- [ ] README: no `$EDITOR`; composer keys, Drafts and reopening drafts.
- [ ] Help popup Compose group lists the composer keys.
- [ ] SPEC.md keybindings and boundaries.
- [ ] AGENTS.md and `.github/copilot-instructions.md` updated with identical bodies.

### Checkpoint 3

- [ ] All green. You decide the release tag.
