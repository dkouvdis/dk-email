# Attachments

This guide covers opening attachments. Attaching files is coming.

- [Opening attachments](#opening-attachments)

## Opening attachments

Press `o` in the reader. With one attachment, dk-email saves it and opens it
at once. With several, a picker lists them grouped by message (sender and
date), each with its size:

| Key | What it does |
|---|---|
| `enter` | Save and open the selected attachment. |
| `/` | Filter the list. |
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
