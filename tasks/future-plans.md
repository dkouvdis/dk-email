# Future Plans

Ideas deliberately deferred from the current scope. Each entry notes why it was
deferred and what the current design must keep possible.

## Incoming

_Nothing yet._

## Outgoing

Deferred from SMTP v1 (reply, reply-all, forward, new message; plain text,
composed in `$EDITOR`).

- **Attach local files**: add files from disk to replies and new messages
  (e.g. an `Attach:` header line in the editor file, or a file picker).
  Forwarding the original message's attachments is already in v1.
- **Markdown → HTML sending**: write Markdown in `$EDITOR` and send
  `multipart/alternative` (plain text + HTML) so links like `[here](https://…)`
  render as clickable text. v1 must keep body building behind a seam so this
  can be added without a rewrite.
- **Save drafts to the IMAP Drafts folder**: persist unfinished or failed
  messages to the account's Drafts-role folder so they survive restarts and
  sync with other clients. In v1, a failed send keeps the draft in memory only
  (retry or re-edit), and an empty/unchanged body cancels.

## Accounts

- **Generic password sources**: support credential sources beyond
  `password_cmd` and the OS keychain, with finer control over how secrets are
  resolved. The rule against storing a plaintext `password` in config stays.
