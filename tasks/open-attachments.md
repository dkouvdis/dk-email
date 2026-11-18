# Open Attachments

## Problem Statement

How might we let a reader get any attachment out of the terminal and into the
right app in at most two keystrokes, on any OS, including headless sessions,
without temp files that vanish or pile up?

## Recommended Direction

**`o` saves to Downloads, then opens.** Whenever a message is shown (the reader
popup, or the reader pane in split view while it's open), `o` opens a
`popup.Picker[int]` of that thread's attachments, grouped by message, each
with its size. `enter` writes the chosen file to the Downloads folder and
hands it to the OS opener. A thread with a single attachment skips the
picker: one key.

Opening and saving are one action. This avoids mutt's temp-file race
(`open`/`xdg-open` return before the app reads the file), and the file stays
where the user can find it later. The status bar always names the path:
`Saved ~/Downloads/invoice.pdf`. Where opening is impossible (no opener, or no
display over SSH) or unsafe (executable types), the action still saves and
says why it didn't open: it degrades instead of failing.

Most of this already exists: `render.Parts` returns attachment bytes in the
same order as `Message.Attachments`, the IMAP client implements `Raw`, the
per-OS opener exists for OAuth sign-in, and `popup.Picker` supports groups.
The new parts are one `mail.Mailbox` method, safe filename handling, and the
picker wiring.

## Key Assumptions to Validate

- [ ] **Downloads clutter is acceptable.** Use it for a week; reusing
  identical files keeps duplicates down.
- [ ] **Fetching the whole raw message is fast enough.** Time a 20 MB
  attachment on Gmail and Outlook with `DK_EMAIL_DEBUG=1`. If slow, fetching
  single parts (`BODY[n]`) is the follow-up.
- [ ] **`Parts` order matches `Message.Attachments` order.** Already a
  documented contract; add a test in the UI path that relies on the index.
- [ ] **Headless detection is reliable.** Test Linux with and without
  `DISPLAY`/`WAYLAND_DISPLAY`, and with no `xdg-open`, over SSH.

## MVP Scope

In:

- `Raw(ctx, folder, uid) ([]byte, error)` on `mail.Mailbox`, with a fake in
  `mailtest`.
- `o` in the reader popup and in the split-view reader pane while it's open;
  not from the thread list alone. A picker grouped by message, labels with
  the attachment's size, the one-attachment shortcut, and "No attachments" in
  the status bar when there are none.
- Saving to `~/Downloads` on macOS and Windows. On Linux, `$XDG_DOWNLOAD_DIR`
  (from the environment or `user-dirs.dirs`), falling back to `~/Downloads`.
  The folder is created if missing.
- Filename sanitizing: base name only, leading dots and control characters
  stripped, length capped, `attachment` as the fallback.
- Collisions: identical bytes reuse the existing file; different bytes get
  `name (1).ext`. Never overwrite.
- Executable and script extensions (`.command`, `.sh`, `.app`, `.pkg`,
  `.jar`, `.desktop`, `.exe`, `.bat`, …) are saved but not opened, with the
  reason in the status bar. The list lives next to the opener.
- The OS opener moves out of `cmd/dk-email/signin.go` and is passed into the
  UI, so the UI never calls `exec` and tests replace it.
- All I/O in a `tea.Cmd` returning a typed message.
- `?` help dialog: `o` (open an attachment) in the Threads section and the
  reader's key hints.
- `docs/attachments.md`: one user guide for opening and attaching (see
  [attach-files.md](attach-files.md)), linked from the README. Written with
  whichever of the two ships first and extended by the second.

Out: everything below.

## Not Doing (and Why)

- **Separate Save with a path prompt**: open already saves; a prompt with
  completion is real work for a rare need.
- **Hidden cache folder**: needs a cleanup policy, and users can't find files
  later.
- **Per-MIME openers (mailcap or `[openers]`)**: the strict config makes new
  keys a long-term commitment; the OS default covers most cases.
- **`download_dir` config key**: add it when someone asks.
- **Piping to a command (`|`)**: the real headless power-user feature, but a
  separate idea; save-and-report covers SSH for now.
- **Inline preview (text, kitty/sixel images)**: patchy terminal support and
  a large rendering change.
- **Bulk "save all"**: needs a multi-select picker; not needed for the
  two-key goal.
- **macOS quarantine flag**: not opening executables covers the dangerous
  case without platform-specific extended attributes.
