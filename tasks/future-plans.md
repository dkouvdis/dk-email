# Future Plans

Ideas deliberately deferred from the current scope, and known bugs. Each
entry notes why it matters and, where it applies, what the current design
must keep possible.

## Bugs

- **Authentication lost overnight** (fix pending confirmation): an IMAP
  session the server expired while keeping the connection open was reused
  forever, because a server `NO`/`BAD` counted as final. `imap.Client` now
  probes with `LIST "" ""` and reconnects, and logs drops and reconnects to
  the debug log. Confirm with an overnight run under `DK_EMAIL_DEBUG=1`; see
  [reconnect-plan.md](reconnect-plan.md).
- **Attachments, to confirm by hand**: opening (`o`) and attaching (`ctrl+o`,
  drag and drop) pass the test suite but haven't been run against a real
  account. Drag and drop is the riskiest: each terminal pastes a drop in its
  own format, and only the formats in `internal/ui/drop.go`'s tests are
  handled. Try Ghostty, iTerm2, Terminal.app and a Linux terminal; a drop
  that lands as text means a format to add. Windows is untested. The checks
  are listed in [attachments-todo.md](attachments-todo.md).

## High Priority

- **Undo for delete, archive and move**: `d` deletes without confirmation by
  design, so offer a short undo (e.g. `z` within a few seconds). The
  optimistic updates in `internal/ui/actions.go` already keep what's needed to
  reverse a move.

## Medium Priority

### Incoming

- **Multi-select and bulk actions**: mark several threads, then archive,
  delete, move or mark them read at once. Includes "mark folder as read".

### Search

See `docs/search.md` for how folder search (`/`) works today.

- **More date formats**: `since:`/`after:`/`before:` take only `YYYY-MM-DD`
  and `YYYY/MM/DD`; anything else silently becomes text and finds nothing.
  Accept Gmail's American `MM/DD/YYYY` (document that `03/04/2026` is
  4 March, as in Gmail), and add Gmail's relative `newer_than:` /
  `older_than:` with `d`, `w`, `m`, `y` (e.g. `newer_than:7d`), compiled to
  the same IMAP `SINCE`/`BEFORE`. Leave out `20260115` and `15.01.2026`.
  All in `internal/search/parse.go`.
- **Gmail `X-GM-RAW`**: pass the query through as-is when the server has
  `X-GM-EXT-1`, so Gmail users get Gmail's own matching.
  `mail.SearchQuery.Raw` already keeps the typed query for this.
- **Search all folders**: one `SEARCH` per folder and a results view mixing
  folders. On Gmail, searching All Mail covers it today.
- **OR and grouping**, and **saved searches** as sidebar entries.

### Outgoing

- **Markdown → HTML sending**: write Markdown and send
  `multipart/alternative` (plain text + HTML) so links like `[here](https://…)`
  render as clickable text. Body building stays behind a seam so this can be
  added without a rewrite.
- **Mark the original answered for resumed replies**: a reply saved to Drafts
  and sent later keeps its threading headers, but the original isn't flagged
  `\Answered` because the link to it isn't stored with the draft.
- **Signature**: a per-account signature added to new messages, replies and
  forwards, shared by the account's aliases.

### Attachments

See [docs/attachments.md](../docs/attachments.md) for how they work today.

- **Fetch one part, not the whole message**: opening an attachment and
  forwarding both download the full message (`BODY.PEEK[]`). Fetching only
  the needed part (`BODY.PEEK[n]`) would make large mail fast. `render.Parts`
  ordering is the contract the part numbers must follow.
- **Collapsed messages in the `o` picker**: only expanded messages (bodies
  loaded) are listed, so attachments of collapsed ones need a `space` first.
  Fetch the missing bodies when `o` is pressed instead.
- **Size warning before sending**: dk-email sets no limit and the server's
  rejection is the only signal. Warn in the composer when the encoded size
  passes a threshold (attachments grow about a third when encoded).
- **Pipe an attachment to a command** (`|`): the useful action over SSH or
  without a display, e.g. `| pdftotext - -`.
- **Save all attachments** of a message or thread at once; needs a
  multi-select picker.
- **`download_dir` setting**: Downloads is fixed today; add the config key
  when someone asks.
- **Per-type openers**: choose the app per MIME type (as mailcap or aerc's
  `[openers]`) instead of the system default.
- **Attach from another message**: add an attachment of a different email,
  reusing the `Draft.Attachments` list.

### General

- **Any `password_cmd` from the connect and edit popups**: replace the
  **1Password** login choice with **Command**, a field holding the whole
  `password_cmd` (placeholder `op read op://Vault/Item/password`, note
  pointing to `docs/accounts.md`). A bare `op://…` reference is still saved
  as `op read '<ref>'`, so 1Password stays one paste. Editing shows the
  account's current command as saved, not only `op read` ones, which
  `opReference` in `internal/ui/popup/connect.go` limits today. Today other
  password managers (pass, gpg, Bitwarden) need `config.toml` edited by hand.
- **Cache `password_cmd` between reconnects**: `imap.Client` reuses the
  password it was created with, but reconnecting a failed account (`ctrl+r`)
  runs `password_cmd` again and can bring up another 1Password or pinentry
  prompt. Keep the password in memory for the session, optionally with a
  lifetime, and drop it when the server rejects it so the next try runs the
  command again. The password never touches disk. The IDLE watcher is built
  and reuses the credentials resolved at connect (for a password account, a
  static password resolved once), so it adds no prompts; an oauth2 account
  with `password_cmd` runs the command per connection, watcher included. The
  open part is only `ctrl+r` reconnects of failed accounts.
- **Settings in the app**: change UI settings without editing the config file
  by hand.

## Low Priority

- **Undo in the composer**: the Bubbles textarea has no undo history.
- **Hard wrapping / format=flowed**: the body is sent exactly as typed;
  wrapping long lines or sending `format=flowed` would read better in some
  clients.
- **Address autocomplete**: suggest recipients from recent mail.
- **Open in browser**: open HTML-heavy mail that renders poorly as Markdown in
  the system browser.
- **Local cache**: keep folders and messages on disk for faster startup and
  reading offline. A large change; defer until the rest is settled.
