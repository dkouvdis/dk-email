# Attachments

This guide covers attaching files to a message you're writing and opening
the attachments of messages you receive.

- [Attaching files](#attaching-files)
  - [The attach prompt](#the-attach-prompt)
  - [Drag and drop](#drag-and-drop)
  - [The Attachments row](#the-attachments-row)
- [Opening attachments](#opening-attachments)

## Attaching files

There are two ways to attach a file while writing a message: press `ctrl+o`
and type its path, or drag it from Finder (or your file manager) and drop it
onto the terminal window. Every attachment is listed in the composer, whether
you added it, it came with a forward or it was saved in a draft.

### The attach prompt

`ctrl+o` opens a path prompt under the fields:

| Key | What it does |
|---|---|
| `tab` | Complete the path. |
| `enter` | Attach the file. |
| `esc` | Close the prompt. |

`~` and relative paths work. Relative paths start from the directory
dk-email was started in. `tab` completes a unique match, and lists the
choices when several match. A problem, such as a folder or a file that doesn't
exist, shows in the prompt, which stays open so you can fix the path.

### Drag and drop

1. Open the composer (`c`, `r`, `R` or `f`).
2. Drag one or more files from Finder, your file manager or the desktop.
3. Drop them anywhere on the terminal window: on the body, a header field or
   the attach prompt.

dk-email attaches them, lists them on the `Attach` row, and the status line
says `Attached <name>.` The body is left as it was.

How it works: when you drop files on a terminal, it pastes their paths.
dk-email treats a paste as a drop when it's only file paths (escaped or quoted
paths, or `file://` URLs), and each one is a readable file. If any of them
isn't, such as a folder or a file that's gone, the whole paste is inserted as
ordinary text instead. Pasted text that merely mentions a file, like
`see notes.txt`, is never treated as a drop.

To put a file's path in your message as text, type it, or drop the file and
then remove the attachment and type the path.

Drag and drop is designed for Ghostty, iTerm2, Terminal.app, kitty, WezTerm,
GNOME Terminal and Windows Terminal. It hasn't been tested in all of them; if
a drop in your terminal ends up as text, please report what was pasted.

### The Attachments row

The `Attach` row sits under Subject. It lists every attachment with its size.
It only shows when there is something attached.

| Key | What it does |
|---|---|
| `tab` | Move to the row. |
| `←` / `→` | Choose an attachment. |
| `d` or `backspace` | Remove the chosen attachment. |

Adding or removing an attachment counts as a change, so `esc` offers to save
the draft or discard it.

### Details

- **Duplicates:** a file with the same name and contents as one already
  attached is skipped, and the status line says `<name> is already attached.`
- **Content type:** taken from the extension. If there's none we know, it's
  detected from the contents, and failing that it's `application/octet-stream`.
- **Forwards:** the original's attachments are loaded before the composer
  opens, and the status line shows `Loading message…`. If that fails, the
  composer doesn't open, so a forward never goes out without them. Remove the
  ones you don't want.
- **Drafts:** saving a draft keeps its attachments, and reopening it lists
  them again.
- **Size:** dk-email sets no limit. Providers reject large messages, often
  around 20 to 25 MB, and attachments grow by about a third when encoded.
  The error comes from the server.

### Status messages

| Message | Meaning |
|---|---|
| `Loading message…` | A forward's original is being fetched. |
| `Attached <name>.` | The file was added. |
| `Attached <a>, <b>.` | Several files were added (dropped). |
| `<name> is already attached.` | The same name and contents are already attached. |
| `Attached <a>; <b> is already attached.` | Some files were added, some skipped. |
| `open message: <reason>` | A forward's original couldn't be fetched; the composer didn't open. |
| `no such file: <path>` | In the prompt: the path doesn't exist. |
| `<path> is a folder` | In the prompt: only files can be attached. |

## Opening attachments

Press `o` in the reader. With one attachment, dk-email saves it and opens it
at once. With several, a picker lists every attachment with its size, in thread
order. The reader shows `press o to open` next to its `Attachments:` line.

| Key | What it does |
|---|---|
| `enter` | Save and open the selected attachment. |
| `esc` | Close the picker. |

Only messages expanded in the reader, the ones whose bodies are loaded, are
included. Press `space` on a collapsed message to expand it and include its
attachments. If none of the expanded messages has an attachment, the status
line says `No attachments.`

### Where files go

Files are saved in your Downloads folder, which is created if it's missing:

- **macOS and Windows:** `~/Downloads`.
- **Linux and the BSDs:** `$XDG_DOWNLOAD_DIR` if set, then `XDG_DOWNLOAD_DIR`
  in `~/.config/user-dirs.dirs` (or under `$XDG_CONFIG_HOME`), then
  `~/Downloads`.

Opening the same attachment again reuses the file already saved. A different
file with the same name is saved as `name (1).ext`, then `name (2).ext`, and
so on. Existing files are never overwritten. Saved files are readable only by
you (mode 0600).

dk-email cleans the name an attachment claims: folders in the name, leading
dots, and control and invisible characters are removed, and characters
Windows forbids (`< > : " | ? *`) become `_`.

### Files that aren't opened

A file that could run code is saved but never opened. That covers scripts,
apps, installers and shortcuts, such as `.sh`, `.command`, `.app`, `.exe`,
`.pkg`, `.desktop` and `.url`. The status line says so. Open one yourself
only if you trust it.

### When nothing can be opened

dk-email doesn't try to open files when it can't show them to you:

- over SSH, because the file would open on the remote machine;
- on Linux and the BSDs without a display;
- when `open` (macOS) or `xdg-open` (Linux and the BSDs) isn't installed.

The file is still saved, and the status line shows where.

### Status messages

| Message | Meaning |
|---|---|
| `Saving <name>…` | The message is being downloaded. |
| `Saved and opened ~/Downloads/<name>.` | Saved, and handed to your default app. |
| `Saved ~/Downloads/<name> — not opened: it can run code.` | The file type could run code. |
| `Saved ~/Downloads/<name> — no app to open it here.` | Saved, but nothing can open it here (see above). |
| `Saved <path>, but couldn't open it: <reason>` | Saved, but the system's opener failed. |
| `Couldn't save <name>: <reason>` | Nothing was saved. |
| `No attachments.` | No expanded message has an attachment. |

### Large attachments

To extract one attachment, dk-email downloads the whole message. A big
attachment takes a moment, and the status line shows `Saving <name>…` until
it's done.
