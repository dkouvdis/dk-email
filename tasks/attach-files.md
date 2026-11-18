# Attach Local Files

## Problem Statement

How might we let someone writing a message add files from disk, see
everything the message will carry, and remove what they don't want, without
leaving the composer or the terminal?

## Recommended Direction

**`ctrl+o` or drop, an Attachments row, and one attachment list on the
draft.** In the composer, `ctrl+o` opens a one-line path prompt with tab
completion (`~` expanded, relative paths from the working directory); `enter`
attaches. Dropping files onto the terminal attaches them wherever the cursor
is, as long as the pasted text is only paths to existing regular files;
anything else stays a normal paste. An **Attachments** row under Subject
lists every attachment with its size (local files, a forward's attachments,
a reopened draft's attachments) and joins the tab order only when it's not
empty: `←`/`→` choose an attachment, `d` or `backspace` removes it.

Underneath, `mail.Draft` gets one `Attachments` list holding the bytes,
loaded eagerly: local files are read when attached (so "no such file" or "is
a directory" show up at once), forwards fetch the original's attachments
when the composer opens, and `OpenDraft` keeps the attachments it already
extracts. `ForwardOf`, and the trick of pointing it at the draft itself, are
removed: `Build`/`BuildDraft` just use `d.Attachments`. Drafts keep their
attachments for free, because `BuildDraft` writes them and `OpenDraft` reads
them back.

This follows the mutt/aerc/meli convention (a path prompt with completion and
a visible list you can remove from) and adds drag and drop, which none of
them treat as attaching.

## Key Assumptions to Validate

- [ ] **Terminal drops arrive as a parseable paste.** Check Ghostty, iTerm2,
  Terminal.app, kitty, WezTerm, GNOME Terminal and Windows Terminal: escaped
  (`My\ File.pdf`), single-quoted, double-quoted Windows paths, `file://`
  URLs, several files at once.
- [ ] **Pasting a path as text into the body is rare.** If it isn't, fall
  back to "drops attach only in the `ctrl+o` prompt".
- [ ] **Fetching a forward's attachments when the composer opens feels
  fine.** Time it on a large forward; the composer must open at once and
  fill the row when the fetch finishes.
- [ ] **Holding attachment bytes in memory is fine.** Check memory with a
  25 MB attachment through compose, save, reopen and send.

## MVP Scope

In:

- `mail.Draft.Attachments`: a list of a new `mail` type (name, content type,
  bytes), separate from the display-only `mail.Attachment`; `mail` stays
  standard-library only. Duplicates are skipped with a note (see Decisions). `ForwardOf` removed; `send` builds from the list;
  `OpenDraft` keeps the extracted attachments.
- Forward: the composer opens immediately, shows `Loading attachments…` in
  the row, and fills it when the fetch finishes. If the fetch fails, sending
  is blocked with a clear error until the user retries or removes them; a
  forward never goes out without its attachments.
- `ctrl+o` attach prompt with filesystem tab completion; errors (missing,
  directory, unreadable) shown in the prompt. File reads in a `tea.Cmd`.
- Content type from the extension (`mime.TypeByExtension`), then content
  sniffing, then `application/octet-stream`.
- Drop to attach: a paste is treated as attachments only when every item
  resolves to an existing regular file; the status bar says what was
  attached.
- Attachments row: shown when not empty, in tab order, `←`/`→` choose and
  `d`/`backspace` remove; names and sizes truncate to the width.
- "Changed?" (the save/discard prompt on `esc`) counts added or removed
  attachments.
- Drafts: attachments survive save and reopen, both local and forwarded.
- `?` help dialog: the Composer section gains `ctrl+o` (attach a file), drop
  to attach, and removing on the Attachments row; the composer's footer hints
  gain `ctrl+o`.
- `docs/attachments.md`: one user guide for opening (see
  [open-attachments.md](open-attachments.md)) and attaching, linked from the
  README. Written with whichever of the two ships first and extended by the
  second.

Out: everything below.

## Not Doing (and Why)

- **Built-in file browser** (`bubbles/filepicker`): typing a path with
  completion, and dragging, cover the way files are found.
- **External pickers (`fzf`, OS file dialogs)**: the UI never hands the
  terminal to another program, and OS dialogs fail headless.
- **Size warning before sending**: servers reject oversized mail; make sure
  that error reads clearly instead. Revisit if it bites.
- **Attach from another message**: a separate idea built on the same list.
- **Inline images or `Content-ID` parts**: outgoing mail is plain text.
- **Renaming an attachment or changing its content type**: rare; remove and
  re-attach a renamed file.
- **Attaching from the command line (`--attach`, `mailto:`)**: different
  audience (scripts); later.

## Decisions

- **Its own type.** The draft's list gets a new type in `mail` (name, content
  type, bytes) rather than adding `Data` to `mail.Attachment`, which stays
  the display-only type of `Message.Attachments`.
- **No duplicates.** Attaching a file that is already on the list (by `ctrl+o`
  or by drop) is ignored with a status note, e.g. `report.pdf is already
  attached`. A duplicate is the same name and the same bytes, so a forward's
  attachment and an identical local file count as one.
