# Plan: Built-in compose popup with drafts (continuation of SMTP)

Tasks: [compose-todo.md](compose-todo.md) · Previous work: [smtp-plan.md](smtp-plan.md)

## Problem

Composing today:

- `c`, `r`, `R` and `f` hand the terminal to `$EDITOR` through `tea.ExecProcess` (`internal/ui/editor.go`).
- They edit a header-block file (`compose.Format` / `compose.Parse`).
- Then a y/e/d confirm popup (`popup/confirm.go`) sends, re-edits or discards.

You want:

- **A built-in plain-text editor in a popup covering 90% of the screen.**
- **No `$EDITOR`.**
- **`ctrl+s` sends directly.** There's no confirm popup.
- **`esc` asks whether to save to Drafts.**
- **Saved drafts can be reopened** and finished later.

The earlier idea of embedding `$EDITOR` in a PTY is dropped. Because dk-email holds the text itself, `ctrl+s` always sends exactly what's on screen.

## Decisions

| Topic | Decision |
|---|---|
| Editor | Built with Bubbles v2 `textinput` for To, Cc, Bcc and Subject, and `textarea` for the body. It's plain text only. `$VISUAL` and `$EDITOR` are no longer read. |
| Size | The existing `Panes.Popup` rect (`popupPercent = 90`). The inbox stays visible around it. |
| Title | Shown in the border as action · subject · From address, e.g. `Reply · Re: Lunch? · me@example.com`. |
| Focus | `tab` and `shift+tab` cycle To → Cc → Bcc → Subject → Body. New message and forward start in To. Reply and reply-all start at the top of the body, above the quote. |
| Send | `ctrl+s` parses the fields and sends. A parse error shows on a **footer line in the popup**, and editing continues. |
| Sending state | The popup stays open and locked, with the footer saying "Sending…". On success it closes and the status bar shows "Sent to …". On failure the error shows in the footer, editing unlocks, and `ctrl+s` retries. When delivery worked but saving to Sent or flagging the original failed (`ErrAfterDelivery`), the popup closes and the status shows the error, because a retry would send twice. |
| Close | `esc` opens a prompt with: **s** save to Drafts · **d** discard · **e** keep editing · **esc** save and close. A second `esc` does the same as **s**: it saves to Drafts and closes, so a reflexive double `esc` never loses work, and only an explicit `d` throws a draft away. If the save fails (no Drafts folder, network), the composer stays open with the error in the footer. If nothing changed from the starting template, the first `esc` closes straight away and saves nothing. Closing, sending, saving or discarding return to the view underneath (the reader if a message was open, otherwise the list), because the composer is an overlay and that view's state is never changed. |
| Confirm popup | Removed, together with its recipient and attachment summary. Forwarded attachments are still sent, just not listed. |
| Hints | The app's existing bottom hints row shows `ctrl+s send · tab next field · esc close` while composing. |
| Mouse | Released while the popup is open, as for other popups. In the alt screen the wheel arrives as ↑/↓. |
| Drafts: saving | Appended to the account's Drafts folder (found by role, like Sent) with the `\Draft` and `\Seen` flags. The saved copy keeps the Bcc header, In-Reply-To and References, and any forwarded attachments. |
| Drafts: reopening | `enter` on a message in a Drafts-role folder opens it in the composer instead of the reader. Saving it again appends the new version, then deletes the old one. Sending it deletes the saved draft after delivery. |
| Resumed reply | Threading headers are kept, so it still threads. The original is **not** marked `\Answered`, because the link to it isn't stored. This goes to future plans. |
| Wrapping | The body soft-wraps on screen and is sent exactly as typed. dk-email doesn't add line breaks. |
| `ctrl+c` | Still force-quits the app, as it does today. |
| Release | Decide later. |

## Design

**`internal/compose`**

- New `Fields{To, Cc, Bcc, Subject, Body string}`.
  - `FieldsOf(mail.Draft) Fields`.
  - `(Fields).Draft(base mail.Draft) (mail.Draft, error)`: reuses `parseAddresses`, and errors name the field ("To: …").
- `FromRaw(raw)` rebuilds a `mail.Draft` from a saved draft's raw bytes: addresses (Bcc included, which `mail.Message` doesn't carry), subject, the text/plain body, In-Reply-To and References.
- `Build` gains a draft mode that writes the Bcc header. Sending still never writes it.
- `editorfile.go` (`Format` / `Parse`) is deleted once unused.

**`internal/mail`**

- New `FlagDraft`.
- `Draft` gains `DraftOf *MessageRef`, the saved draft this one replaces.
- A new `Drafter` interface with `SaveDraft(ctx, d) error` and `OpenDraft(ctx, ref) (Draft, error)`, which `send.Sender` implements.

**`internal/mail/imap`**

- `DraftsFolder(ctx)`, mirroring `SentFolder`.
- `Append` supports the `\Draft` flag.
- `Delete(ctx, folder, uids)` sets `\Deleted`, then runs UID EXPUNGE. UIDPLUS is required; without it the call errors and never runs a plain EXPUNGE.

**`internal/send`**

- `SaveDraft`
  - Builds the message in draft mode.
  - Includes forwarded attachments, extracted from `ForwardOf`.
  - Appends it to Drafts, then deletes `DraftOf` if it's set.
  - If that delete fails, it returns an error wrapping a new `ErrDraftSaved`, so the UI knows the save worked.
- `Send`
  - After delivery, also deletes `DraftOf`.
  - If that fails, it's reported as `ErrAfterDelivery`, so there's no retry.
- A resumed forward uses `ForwardOf` = the saved draft itself, so its attachments come from the draft.

**`internal/ui/popup/compose.go`: new `Compose` component**

- Inputs: four `textinput`s plus a `textarea`.
  - The textarea is set to `MaxHeight = 0`; the default of 99 rows would cut off long quoted replies.
  - `CharLimit` stays 0 (no limit).
- Interface: `SetSize`, `Update`, `View` and `Fields()`.
  - `Update` returns a result: none, send, close-request, save or discard.
  - `SetError` and `SetBusy("Sending…" | "Saving…")` control the footer line and locking.
  - `Dirty()` reports whether the fields changed since the start.
  - The inner prompt state lives inside the component: `s` and `esc` save, `d` discards, `e` keeps editing.
- Rendering uses a new `popup.FrameTitled(th, title, content, w, h)`.
  - The title is drawn into the top border, truncated with `…`.
  - The footer is the last inner row.
- Uses the textarea's virtual cursor, so no terminal cursor offset is needed.

**`internal/ui`**

- `startDraft` opens the composer. All keys go to it while it's open. `ctrl+c` stays global.
- Send, save and discard follow the results above.
- `sentMsg` and the new `draftSavedMsg` update the composer or close it.
- Window resizes call `SetSize` on the composer.
- Opening a draft: on a Drafts-role folder, `enter` calls `mail.Drafter.OpenDraft` (raw fetch + `compose.FromRaw`, with `DraftOf` set and `ForwardOf` when it has attachments) in a loading `tea.Cmd`, then opens the composer.
- After a draft is saved or deleted, the Drafts folder's summaries and counts are refreshed if it's the folder on screen, plus the sidebar count.
- Deleted:
  - `editor.go`, the `editorPath` / `editorCmd` seam and `editorDoneMsg`;
  - `popup/confirm.go` with `openConfirm` / `handleConfirmKey`;
  - the `showConfirm` state.

## Risks

| Risk | Mitigation |
|---|---|
| Typing gets slow on long quoted replies, because the textarea re-renders everything. | A benchmark test with a 5,000-line body. If it's too slow, render only the visible rows or cap the quote. Decide at Checkpoint 1. |
| The textarea has no undo. | Accepted. Undo goes to future plans. |
| A server without UIDPLUS can't delete one specific message. | Don't fall back to a plain EXPUNGE. Report "old draft kept" and leave both copies. Gmail and Fastmail support UIDPLUS. |
| Gmail Drafts quirks: `[Gmail]/Drafts`, and Gmail treating appended messages as its own drafts. | Live test in S-checkpoint: save, see it in Gmail web, reopen, send, and confirm the draft is gone from Gmail. |
| A saved draft's own Message-ID and threading headers. | A new Message-ID each time it's saved. In-Reply-To and References are copied unchanged. |
| Removing `$EDITOR` takes away vim users' workflow. | Your call. It's recorded in future plans as a possible opt-in later. |
| Key clashes: `tab` is normally the pane switch, and `esc` normally means back. | Every key goes to the composer while it's open. Tests check that the pane, archive and delete keys don't fire. |

## Workflow (unchanged)

- Sonnet sub-agents write the code.
- Haiku runs gofmt, vet, the build and `go test -race`.
- Opus reviews every diff against this plan and commits, using Conventional Commits with no Co-authored-by trailer.
- You do the manual checks.
- Merging, pushing and tagging each need your explicit OK.

## Todos

**Phase 0**

- **P0 – Plan files.**
  - Write `tasks/compose-plan.md` and `tasks/compose-todo.md`, with acceptance criteria per task. The older `tasks/plan.md` and `todo.md` are left alone.
  - Add to `tasks/future-plans.md`:
    - marking the original `\Answered` when a resumed reply is sent;
    - undo in the composer;
    - address autocomplete;
    - an optional `$EDITOR` mode;
    - format=flowed / hard wrapping;
    - adding attachments.

**Phase 1: Composer replaces `$EDITOR` and the confirm popup**

- **C1 – compose `Fields`.** `FieldsOf`, `Fields.Draft` with errors naming the field, and table tests.
- **C2 – `popup.FrameTitled` + `popup.Compose`.**
  - Fields, tab focus, title, footer error and busy state, the close prompt, and `Dirty`.
  - Golden and key tests.
  - Benchmark with a 5,000-line body.
- **C3 – Wire into the UI.**
  - `c`, `r`, `R` and `f` open the composer, with the focus rules above.
  - `ctrl+s` sends, and the in-popup send state replaces the confirm popup.
  - The `esc` prompt offers `d` discard and `e` keep editing. `s` and the second `esc` (both save) are added in D3; until then the prompt shows only `d` and `e`.
  - Test: closing the composer returns to the reader if it was open, otherwise to the list, with selection and focus unchanged.
  - The hints row changes while composing.
  - Remove `editor.go`, `confirm.go` and editorfile `Format` / `Parse`.
  - Update the ui, compose, reply and forward tests.
- **Checkpoint 1.**
  - Automated: tests, vet and gofmt are green.
  - Manual, by you: `c`, `r`, `R` and `f` send to yourself; a bad address shows in the footer; send failure and retry (network off).

**Phase 2: Drafts**

- **D1 – mail + imap.** `FlagDraft`, `DraftsFolder`, `Append` with `\Draft`, and `Delete` (UIDPLUS). Tested against the in-process IMAP server.
- **D2 – compose + send.**
  - The draft-mode `Build` keeps Bcc.
  - `SaveDraft`, with attachments and replacing `DraftOf`.
  - `Send` deletes `DraftOf` after delivery.
  - Add `ErrDraftSaved` and the `ErrAfterDelivery` path.
  - `compose.FromRaw` and `Sender.OpenDraft`.
- **D3 – Save from the composer.** The prompt's **s** and second-`esc` options (both save and close; a failed save keeps the composer open), "Saving…", "Draft saved", and errors in the footer. The Drafts folder is refreshed when it's on screen.
- **D4 – Reopen drafts.**
  - `enter` in a Drafts-role folder opens the composer.
  - Saving again replaces the old draft; sending deletes it.
  - A resumed forward keeps its attachments.
- **Checkpoint 2, manual on Gmail and one other account.**
  - Save a new message, a reply and a forward with a PDF. Each shows in Drafts in the web UI.
  - Reopen each one, edit and save, and confirm only one copy remains.
  - Reopen and send, and confirm it's delivered (same PDF `shasum`) and gone from Drafts.
  - A resumed reply threads correctly.

**Phase 3**

- **E1 – Docs.**
  - README:
    - drop `$EDITOR`;
    - describe the composer keys, Drafts, and reopening drafts;
    - list the keys while composing.
  - The help popup's Compose group.
  - SPEC.md keybindings and boundaries.
  - AGENTS.md and its Copilot mirror: `compose` and `popup.Compose`, and no editor hand-off. The bodies stay identical.
- **Checkpoint 3.** All green. Then you decide the release tag.

**Dependencies**

- P0 → C1 → C2 → C3 → Checkpoint 1.
- D1 and D2 can start once C1 is done.
- D3 needs C3, D1 and D2. D4 needs D3.
- E1 needs D4.
